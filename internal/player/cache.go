package player

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
)

// Cache keeps the songs played on disk, so that they play again at once,
// and without the network: each as <key>-<level>.mp3 or .flac, with what
// its stream was beside it in <key>-<level>.json. Once they take more than
// the limit, those played longest ago go first.
//
// A song is kept under the key of its own source: NetEase's songs by their
// id, as they were before the app played other sources' too, and the
// others' by the id they have there, behind the source's own, so that two
// sources' songs of one number stay apart.
type Cache struct {
	dir string

	mu    sync.Mutex
	limit int64 // in bytes; 0 keeps nothing
}

// NewCache returns a cache in dir of limit bytes.
func NewCache(dir string, limit int64) *Cache {
	return &Cache{dir: dir, limit: limit}
}

// Directory is where the cache keeps its songs: what the settings show, and
// what a migration copies from.
func (c *Cache) Directory() string {
	if c == nil {
		return ""
	}
	return c.dir
}

// SetLimit changes how much the cache may take, and drops what is over.
func (c *Cache) SetLimit(limit int64) {
	c.mu.Lock()
	c.limit = limit
	c.mu.Unlock()
	c.prune("")
}

// cacheName is what the cache names a song by: its id for NetEase's, which
// is what the songs kept before other sources' were playable are named,
// and the id it has on its own source behind the source's own, so that two
// sources' songs of one number stay apart. It returns "" for a song whose
// name would lead out of the cache's folder, as an id from a service takes
// whatever shape it likes.
func cacheName(t netease.Track) string {
	name := t.SongKey()
	if src := t.Src(); src != music.SourceNetease {
		name = src + "-" + name
	}
	if name == "" || len(name) > 96 {
		return ""
	}
	for _, c := range name {
		ok := '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '-' || c == '_'
		if !ok {
			return ""
		}
	}
	return name
}

func cacheKey(name, level string) string { return name + "-" + level }

// keepable reports whether a song of level and type ext may be kept: they
// name its file, so only those known, which keep it in the cache's folder.
func keepable(level, ext string) bool {
	return slices.Contains(netease.Levels, level) &&
		(ext == "mp3" || ext == "flac" || ext == "m4a" || ext == "aac")
}

// lookup returns a song kept at level or a better one, open to read, with
// its stream.
func (c *Cache) lookup(name, level string) (*download, *netease.Stream, bool) {
	if c == nil || name == "" {
		return nil, nil, false
	}
	at := max(slices.Index(netease.Levels, level), 0)
	for _, lv := range netease.Levels[at:] {
		key := cacheKey(name, lv)
		meta, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
		if err != nil {
			continue
		}
		var st netease.Stream
		if json.Unmarshal(meta, &st) != nil || !keepable(lv, st.Type) {
			continue
		}
		path := filepath.Join(c.dir, key+"."+st.Type)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if info, err := f.Stat(); err != nil || info.Size() != st.Size {
			f.Close()
			continue
		}
		// Played now: the last to go.
		now := time.Now()
		os.Chtimes(path, now, now)
		return opened(f, st.Size), &st, true
	}
	return nil, nil, false
}

// Cached returns where the cache keeps a song it already has at level or a
// better one, and the stream it was kept as. A download copies the file
// from there rather than fetching the song again: the listener played it
// already, and the bytes are on the disk. The file stays the cache's, so
// that playing the song again is as quick as it was.
func (c *Cache) Cached(t netease.Track, level string) (path string, st netease.Stream, ok bool) {
	name := cacheName(t)
	if c == nil || name == "" {
		return "", netease.Stream{}, false
	}
	at := max(slices.Index(netease.Levels, level), 0)
	for _, lv := range netease.Levels[at:] {
		key := cacheKey(name, lv)
		meta, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
		if err != nil {
			continue
		}
		var kept netease.Stream
		if json.Unmarshal(meta, &kept) != nil || !keepable(lv, kept.Type) {
			continue
		}
		p := filepath.Join(c.dir, key+"."+kept.Type)
		if info, err := os.Stat(p); err != nil || info.Size() != kept.Size {
			continue
		}
		return p, kept, true
	}
	return "", netease.Stream{}, false
}

// Format returns the format of the song at path, as its own bytes say: mp3,
// flac, m4a or aac, or "" for one that is none of them. What the app does
// not decode, it does not keep in the cache, and does not save either.
func Format(path, said string) string { return sniffType(path, said) }

// download fetches the song of the stream st into the cache's folder,
// under the name its track has there, and keeps it once it has all come
// in. A song the cache would not keep still comes into a file, which goes
// as the song does: a whole song in memory costs more than a file costs,
// and a file the system holds what of it was read lately of.
func (c *Cache) download(name string, st *netease.Stream) *download {
	part := c.part(st)
	if part == nil {
		// No file could be made: the song comes into memory, which is what
		// a song of any size would cost.
		return startDownload(st.URL, st.Size, nil, nil)
	}
	if c == nil || name == "" {
		// Not a song the cache names: the file is the song's own, and goes
		// with it.
		return startDownload(st.URL, st.Size, part, func(path string, n int64) (string, bool) {
			return path, false
		})
	}
	return startDownload(st.URL, st.Size, part, func(path string, n int64) (string, bool) {
		return c.keep(name, st, path, n)
	})
}

// downloadAhead fetches the song of the stream st to wait in, before its
// turn: in a file of its own, which goes when the song does.
func (c *Cache) downloadAhead(st *netease.Stream) *download {
	part := c.part(st)
	if part == nil {
		return startDownload(st.URL, st.Size, nil, nil)
	}
	// The file is not the cache's: it is where the song waits, and goes
	// with the song.
	return startDownload(st.URL, st.Size, part, func(path string, n int64) (string, bool) {
		return path, false
	})
}

// scratch makes a file for a song to come into, outside the cache's
// folder, for one the cache would not keep: a song of more than a quarter
// of the limit, or one it has no room left for. It is the system's
// temporary directory, which is not the listener's cache, and its files
// are dropped as the song closes.
func (c *Cache) scratch() *os.File {
	f, err := os.CreateTemp("", "mygo-*.part")
	if err != nil {
		return nil
	}
	return f
}

// part makes a file for a song of the stream st to come into, or returns
// nil if the cache would not keep it. Each has a name of its own: the same
// song played again while it still comes in has another.
func (c *Cache) part(st *netease.Stream) *os.File {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	limit := c.limit
	c.mu.Unlock()
	// A song of more than a quarter of it would push out too many: it
	// comes into a file of its own, kept or not.
	if limit <= 0 || st.Size > limit/4 {
		return c.scratch()
	}
	if os.MkdirAll(c.dir, 0o700) != nil {
		return c.scratch()
	}
	f, err := os.CreateTemp(c.dir, "*.part")
	if err != nil {
		return c.scratch()
	}
	return f
}

// sniffType returns the format of the song kept at path, as its own bytes
// say, which is what names its file: a source that said its type, as the
// scripts often do, may say one the bytes disagree with, and a name that
// leads out of the cache's folder is no name at all. What the app does not
// decode, it does not keep.
func sniffType(path, said string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 12)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch {
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return "m4a"
	case len(head) >= 4 && string(head[:4]) == "fLaC":
		return "flac"
	case len(head) >= 3 && string(head[:3]) == "ID3":
		return "mp3"
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0 && head[1]&0x06 == 0:
		return "aac" // ADTS, which an MP3 header never looks like
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return "mp3"
	}
	if said == "mp3" || said == "flac" {
		// A file the app reads as MP3 for want of anything saying
		// otherwise, which is how a song it cannot place plays.
		return said
	}
	return ""
}

// keep keeps the song of the stream st, n bytes, under name, from the file
// part, which is closed, and drops what is over the limit then. It returns
// where the song is: kept, or still at part.
func (c *Cache) keep(name string, st *netease.Stream, part string, n int64) (string, bool) {
	c.mu.Lock()
	limit := c.limit
	c.mu.Unlock()
	if n > limit/4 {
		return part, false
	}
	kept := *st
	kept.Size = n
	kept.Type = sniffType(part, kept.Type)
	if !keepable(kept.Level, kept.Type) {
		return part, false
	}
	key := cacheKey(name, kept.Level)
	meta, _ := json.Marshal(kept)
	path := filepath.Join(c.dir, key+"."+kept.Type)
	if os.Rename(part, path) != nil {
		return part, false
	}
	os.WriteFile(filepath.Join(c.dir, key+".json"), meta, 0o600)
	c.prune(path)
	return path, true
}

// Size returns how many bytes the songs kept take.
func (c *Cache) Size() int64 {
	var n int64
	for _, f := range c.songs() {
		n += f.size
	}
	return n
}

// Clear drops every song kept.
func (c *Cache) Clear() {
	for _, f := range c.songs() {
		c.remove(f.path)
	}
}

type cachedSong struct {
	path string
	size int64
	used time.Time
}

func (c *Cache) songs() []cachedSong {
	entries, _ := os.ReadDir(c.dir)
	var out []cachedSong
	for _, e := range entries {
		// The songs kept are those keepable names, and their metadata is
		// beside them: anything else in the folder is not the cache's.
		switch filepath.Ext(e.Name()) {
		case ".mp3", ".flac", ".m4a", ".aac":
		default:
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, cachedSong{filepath.Join(c.dir, e.Name()), info.Size(), info.ModTime()})
	}
	return out
}

func (c *Cache) remove(path string) {
	os.Remove(path)
	os.Remove(strings.TrimSuffix(path, filepath.Ext(path)) + ".json")
}

// prune drops the songs played longest ago until the rest fit the limit,
// but keep, the song just kept.
func (c *Cache) prune(keep string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropParts()
	songs := c.songs()
	slices.SortFunc(songs, func(a, b cachedSong) int { return a.used.Compare(b.used) })
	var total int64
	for _, f := range songs {
		total += f.size
	}
	for _, f := range songs {
		if total <= c.limit {
			return
		}
		if f.path != keep {
			c.remove(f.path)
			total -= f.size
		}
	}
}

// dropParts removes the files of songs that never all came in, left by an
// app that quit as they came: one coming in is written to every moment. The
// copies of a migration that did not finish go the same way.
func (c *Cache) dropParts() {
	for _, pattern := range []string{"*.part", ".migrate-*", ".write-check-*"} {
		stale, _ := filepath.Glob(filepath.Join(c.dir, pattern))
		for _, path := range stale {
			if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > time.Hour {
				os.Remove(path)
			}
		}
	}
}
