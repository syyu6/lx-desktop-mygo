package player

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"netease-music-mygo/internal/netease"
)

// CopyTo moves the songs kept from the cache into target, for a listener
// who chose another folder for them: each whole song, and the stream beside
// it that names it. It returns what removes the originals, which the caller
// runs only once the new folder is saved as the cache's: were the copy to
// stand alone and the app to quit before the setting was kept, the old
// folder would still be the one asked, and emptied.
//
// It runs before playback is restored, so no song of the cache is open:
// what is copied is a whole file, written under a name of its own and
// renamed into place, and a copy that stops halfway leaves the original.
func (c *Cache) CopyTo(target *Cache, progress func(done, total int64)) (cleanup func() error, err error) {
	noop := func() error { return nil }
	if c == nil || target == nil {
		return nil, errors.New("缓存迁移需要一个源目录和一个目标目录")
	}
	sourceDir, err := filepath.Abs(c.dir)
	if err != nil {
		return nil, err
	}
	targetDir, err := filepath.Abs(target.dir)
	if err != nil {
		return nil, err
	}
	if sourceDir == targetDir {
		return noop, nil
	}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return nil, err
	}
	sourceInfo, err := os.Stat(sourceDir)
	if errors.Is(err, os.ErrNotExist) {
		return noop, nil
	}
	if err != nil {
		return nil, err
	}
	targetInfo, err := os.Stat(targetDir)
	if err != nil {
		return nil, err
	}
	if os.SameFile(sourceInfo, targetInfo) {
		return noop, nil
	}
	songs, total, err := cacheSongs(sourceDir)
	if err != nil {
		return nil, err
	}
	var done int64
	advance := func(n int64) {
		done += n
		if progress != nil {
			progress(done, total)
		}
	}
	advance(0)
	for _, s := range songs {
		for _, f := range s.files {
			if err := f.copyTo(targetDir, advance); err != nil {
				return nil, err
			}
		}
	}
	return func() error {
		var failures []error
		for _, s := range songs {
			for _, f := range s.files {
				if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
					failures = append(failures, err)
				}
			}
		}
		return errors.Join(failures...)
	}, nil
}

type migrationFile struct {
	path string
	size int64
}

// copyTo writes the file into dir under its own name, and renames it there
// when it has all come in: a copy that stops leaves the half file in a
// temporary name, which the next start drops.
func (f migrationFile) copyTo(dir string, advance func(int64)) error {
	in, err := os.Open(f.path)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, ".migrate-*")
	if err != nil {
		return err
	}
	part := out.Name()
	written, err := io.Copy(out, in)
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		os.Remove(part)
		return errors.Join(err, closeErr)
	}
	if err := os.Rename(part, filepath.Join(dir, filepath.Base(f.path))); err != nil {
		os.Remove(part)
		return err
	}
	advance(written)
	return nil
}

// migrationSong is a song kept: its audio, and the stream it was kept as.
type migrationSong struct{ files [2]migrationFile }

// cacheSongs is what the cache at dir keeps that is whole: a song and the
// stream beside it that says its level and size. An audio file whose stream
// does not match it, or is not one the cache would keep, is left where it
// is — it is the cache's to drop, and this is not the place to guess.
func cacheSongs(dir string) ([]migrationSong, int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	var songs []migrationSong
	var total int64
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		switch ext {
		case ".mp3", ".flac", ".m4a", ".aac":
		default:
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		audio := filepath.Join(dir, e.Name())
		meta := strings.TrimSuffix(audio, ext) + ".json"
		b, err := os.ReadFile(meta)
		if err != nil {
			continue
		}
		var st netease.Stream
		if json.Unmarshal(b, &st) != nil || !keepable(st.Level, st.Type) ||
			st.Type != strings.TrimPrefix(ext, ".") || st.Size != info.Size() {
			continue
		}
		metaInfo, err := os.Stat(meta)
		if err != nil {
			continue
		}
		song := migrationSong{files: [2]migrationFile{
			{audio, info.Size()},
			{meta, metaInfo.Size()},
		}}
		songs = append(songs, song)
		total += info.Size() + metaInfo.Size()
	}
	return songs, total, nil
}
