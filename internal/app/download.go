package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"netease-music-mygo/internal/source"
)

// The download engine's other half: what a task does to get its song. The
// manager (internal/download) knows which tasks wait, run and end; this
// knows where a song comes from, how its file is named, and what goes in
// it.
//
// A task reuses the cache the player keeps: a song played lately is
// already on the disk, whole, and is copied from there rather than fetched
// again — the bytes are the same, and the listener paid for them once.

// downloadRun is the manager's Run: it resolves the song, fetches it into
// the folder, and writes its tags and words.
func (a *app) downloadRun(ctx context.Context, task *download.Task, p *download.Progress) error {
	if !a.settings.DownloadOn {
		return errors.New("下载已在设置中关闭")
	}
	s := &a.settings
	dir := a.downloadDir()
	if dir == "" {
		return errors.New("还没有设置下载目录")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.New("无法创建下载目录")
	}
	base := download.FileName(s.DownloadName, task.Track)
	// The name the file would take, by a format the source named: replaced
	// once the bytes have come and say which it is.
	dest := filepath.Join(dir, base+"."+guessExt(task))
	if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() && s.DownloadSkip {
		// Told to leave what is there: the file the listener has is the
		// file they wanted.
		p.Update(func(t *download.Task) {
			t.Path, t.Name, t.Done, t.Size, t.Note = dest, filepath.Base(dest), info.Size(), info.Size(), "文件已存在"
		})
		return nil
	}

	// The cache first: a song played lately is there already, and costs
	// nothing but a copy.
	var cached string
	var st *music.Stream
	// kept is the level the bytes are actually at, which may be below the
	// one asked for, and is what the row and the player read.
	var kept string
	if a.pl.Cache != nil {
		if path, hit, ok := a.pl.Cache.Cached(task.Track, task.Level); ok {
			cached, st, kept = path, &hit, hit.Level
			p.Total(hit.Size)
			p.Update(func(t *download.Task) {
				t.Served, t.FromCache, t.Size = hit.Level, true, hit.Size
			})
		}
	}
	if cached == "" {
		var err error
		if st, kept, err = a.downloadStream(ctx, task.Track, task.Level); err != nil {
			return err
		}
		if kept == "" {
			kept = task.Level
		}
		p.Update(func(t *download.Task) { t.Served = kept })
		if st.Size > 0 {
			p.Total(st.Size)
		}
	}

	part, err := os.CreateTemp(dir, ".lxmuc-*.part")
	if err != nil {
		return errors.New("无法写入下载目录")
	}
	partName := part.Name()
	// What a stopped task leaves behind is not the listener's to clean up.
	defer os.Remove(partName)

	if cached != "" {
		err = copyFile(ctx, cached, part, p)
		if errors.Is(err, errCacheGone) {
			// The cache dropped it between the look and the copy: the song
			// is fetched, as it would have been without one.
			var ferr error
			if st, kept, ferr = a.downloadStream(ctx, task.Track, task.Level); ferr == nil {
				if kept == "" {
					kept = task.Level
				}
				p.Update(func(t *download.Task) { t.Served = kept })
				err = fetchStream(ctx, st.URL, part, p)
			} else {
				err = ferr
			}
		}
	} else {
		err = fetchStream(ctx, st.URL, part, p)
	}
	if cerr := part.Close(); cerr != nil && err == nil {
		err = errors.New("写入文件失败")
	}
	if err != nil {
		return err
	}
	// The format is what the bytes say, not what the source named: a script
	// may label a FLAC an mp3, and the tags follow the file's own format.
	format := player.Format(partName, saidType(st))
	if format == "" {
		return errors.New("下载到的不是音频文件")
	}

	// Its words and cover, fetched while the bytes are at hand: failing to
	// get them does not fail the song, which is saved without.
	lrc, tlyric := a.downloadLyrics(task.Track)
	cover := a.downloadCover(task.Track)
	if err := ctx.Err(); err != nil {
		return err
	}

	opts := s.tagOpts()
	if err := writeTags(partName, format, task.Track, lrc, tlyric, cover, opts); err != nil {
		// The tags are what the listener asked to find in the file; one
		// they could not be written to is still the song they asked for.
		p.Update(func(t *download.Task) { t.Note = "已保存，但没能写入标签" })
	}

	name := base + "." + format
	if !s.DownloadSkip {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			name = uniqueName(dir, base) + "." + format
		}
	}
	path := filepath.Join(dir, name)
	if err := os.Rename(partName, path); err != nil {
		return errors.New("无法保存文件")
	}
	if opts.lyricsFile && strings.TrimSpace(lrc) != "" {
		// The lyric file is named after the song's, wherever that ended up.
		if err := writeLrc(path, lrc, tlyric, opts.lrcFormat, opts.lrcTranslation); err != nil {
			p.Update(func(t *download.Task) { t.Note = "已保存，但没能写出歌词文件" })
		}
	}
	p.Update(func(t *download.Task) {
		t.Path, t.Name, t.Ext = path, name, format
		if kept != "" {
			t.Served = kept
		}
	})
	return nil
}

// guessExt is the extension of a task's file before its bytes are seen:
// the level asked for names FLAC for the lossless ones, and MP3 for the
// rest, as most sources serve them. It only has to be close: the file's
// own format replaces it once the song is in.
func guessExt(t *download.Task) string {
	if t.Ext != "" {
		return t.Ext
	}
	if t.Served != "" && music.Lossless(t.Served) {
		return "flac"
	}
	if music.Lossless(t.Level) {
		return "flac"
	}
	return "mp3"
}

// saidType is the format a stream named, for the file's bytes to confirm.
func saidType(st *music.Stream) string {
	if st == nil {
		return ""
	}
	return st.Type
}

// downloadStream asks the sources where the song streams from at level,
// trying the next link when one names an address that does not open: the
// same walk the player makes, since a script whose upstream is down answers
// with a 404. A song no source will stream is said so in the words it will
// be read as.
//
// What comes back besides the stream is the level it will be stored at: the
// one the source served, which may be below the one asked for, and the one
// asked for when the source did not say.
func (a *app) downloadStream(ctx context.Context, t music.Track, level string) (*music.Stream, string, error) {
	var skip string
	var firstErr error
	for {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		var res source.Resolution
		var err error
		switch {
		case a.chain != nil && a.settings.DownloadOtherSource:
			res, err = a.chain.ResolveExcept(t, level, skip)
		case a.chain != nil:
			// Not to look elsewhere: its own source, and no other, as far
			// as it will stream the song.
			res, err = a.chain.OwnExcept(t, level, skip)
		case a.pl.Resolve != nil:
			var st *music.Stream
			var served string
			st, served, err = a.pl.Resolve(t, level)
			if err == nil {
				res.Stream, res.Served = st, served
			}
		default:
			err = player.ErrUnavailableFrom(t.Src())
		}
		if err != nil {
			if firstErr != nil {
				return nil, "", fmt.Errorf("来源给出的地址无法下载（%v），其它的来源也没有这首歌", firstErr)
			}
			return nil, "", downloadReason(t, err, a.settings.DownloadOtherSource)
		}
		// Its first bytes say whether the address is a song at all: a
		// source that has taken the song down answers with a page, and the
		// next one must get its turn.
		if err := checkAudio(ctx, res.Stream.URL); err != nil {
			firstErr = err
			// Moving on needs a link to leave out. One that names none is
			// the only one there is: asking again would ask the same
			// source the same question, and check its address for ever.
			if res.Link == "" || res.Link == skip {
				return nil, "", err
			}
			skip = res.Link
			continue
		}
		kept := res.Stream.Level
		if kept == "" {
			kept = level
		}
		return res.Stream, kept, nil
	}
}

// checkAudio asks an address for its first bytes, and says whether they are
// a song's: what the player finds as it opens it, found before the task
// spends a file on it.
func checkAudio(ctx context.Context, url string) error {
	if url == "" {
		return errors.New("没有给出地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.New("地址打不开")
	}
	req.Header.Set("User-Agent", netease.UserAgent)
	// The first bytes only: the song itself comes in after, by the file the
	// task writes.
	req.Header.Set("Range", "bytes=0-2047")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("地址打不开")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("地址打不开（%d）", res.StatusCode)
	}
	head := make([]byte, 2048)
	n, _ := io.ReadFull(res.Body, head)
	head = head[:n]
	if len(head) > 0 && (head[0] == '<' || len(head) > 2 && head[0] == 0xef && head[1] == 0xbb && head[2] == '<') {
		return errors.New("地址返回的不是歌曲")
	}
	return nil
}

// downloadReason says, in words the listener reads, why a song did not
// download. elsewhere says whether the other sources were asked: when they
// were not, saying they have none of the song would send the listener after
// a recording nobody looked for, where turning 其它来源 on may well find it.
func downloadReason(t music.Track, err error, elsewhere bool) error {
	if !elsewhere {
		if t.VIP() {
			return errors.New("这首歌需要 VIP，自己的来源给不出，可以打开「其它来源」再试")
		}
		return errors.New("这首歌自己的来源给不出，可以打开「其它来源」再试")
	}
	switch {
	case errors.Is(err, netease.ErrUnavailable), errors.Is(err, source.ErrNotPlayable):
		if t.VIP() {
			return errors.New("这首歌需要 VIP，其它来源也没有找到")
		}
		return errors.New("没有来源能提供这首歌")
	case t.VIP():
		return errors.New("这首歌需要 VIP，其它来源也没有找到")
	}
	return errors.New("没有来源能提供这首歌")
}

// errCacheGone says a song the cache was to copy from is no longer there.
var errCacheGone = errors.New("cache file is gone")

// copyFile copies a file the cache keeps into the task's own, reporting how
// far it has come.
func copyFile(ctx context.Context, src string, dst *os.File, p *download.Progress) error {
	in, err := os.Open(src)
	if err != nil {
		// The cache dropped it between the look and the copy: the song is
		// fetched instead.
		return errCacheGone
	}
	defer in.Close()
	buf := make([]byte, 256<<10)
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := in.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return errors.New("无法写入下载目录")
			}
			done += int64(n)
			p.Bytes(done)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("无法读取缓存文件")
		}
	}
}

// fetchStream writes what an address serves into the task's file, reporting
// how far it has come.
func fetchStream(ctx context.Context, url string, dst *os.File, p *download.Progress) error {
	if url == "" {
		return errors.New("来源没有给出地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.New("地址打不开")
	}
	req.Header.Set("User-Agent", netease.UserAgent)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("地址打不开")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("地址打不开（%d）", res.StatusCode)
	}
	if res.ContentLength > 0 {
		p.Total(res.ContentLength)
	}
	buf := make([]byte, 256<<10)
	var done int64
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return errors.New("无法写入下载目录")
			}
			done += int64(n)
			p.Bytes(done)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("下载中断，网络连接失败")
		}
	}
}

// downloadLyrics returns the song's words and their translation, from
// whichever source has them, or "" when none does.
func (a *app) downloadLyrics(t music.Track) (lrc, tlyric string) {
	if a.chain != nil {
		if lrc, tlyric, ok := a.chain.ResolveLyrics(t); ok {
			return lrc, tlyric
		}
		return "", ""
	}
	if a.pl.ResolveLyrics != nil {
		if lrc, tlyric, ok := a.pl.ResolveLyrics(t); ok {
			return lrc, tlyric
		}
	}
	return "", ""
}

// downloadCover returns the song's cover as its bytes, or nil when none
// source names one.
func (a *app) downloadCover(t music.Track) []byte {
	url := t.Cover()
	if a.chain != nil {
		if u, ok := a.chain.ResolveCover(t); ok && u != "" {
			url = u
		}
	}
	if url == "" {
		return nil
	}
	b, err := netease.Fetch(netease.Sized(url, 640))
	if err != nil || len(b) == 0 {
		return nil
	}
	return b
}

// uniqueName returns base with a number after it, for a file already in the
// folder: "歌名 - 歌手 (2)".
func uniqueName(dir, base string) string {
	for i := 2; i < 1000; i++ {
		name := fmt.Sprintf("%s (%d)", base, i)
		if _, err := os.Stat(filepath.Join(dir, name+".mp3")); err != nil {
			if _, err := os.Stat(filepath.Join(dir, name+".flac")); err != nil {
				if _, err := os.Stat(filepath.Join(dir, name+".m4a")); err != nil {
					return name
				}
			}
		}
	}
	return fmt.Sprintf("%s (%d)", base, time.Now().Unix()%10000)
}
