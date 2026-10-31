package download

import (
	"fmt"
	"strings"

	"netease-music-mygo/internal/music"
)

// The naming schemes the listener picks, as lx-music-desktop names them:
// the words 歌名 and 歌手 stand for the song's name and its artists, and
// the rest is kept as it is written.
var NameFormats = []struct{ Format, Label string }{
	{"歌名 - 歌手", "歌名 - 歌手"},
	{"歌手 - 歌名", "歌手 - 歌名"},
	{"歌名", "歌名"},
}

// maxFileName is how many characters a file's name may hold: long enough
// for a long title and a crowd of artists, short enough that a folder of
// songs stays readable.
const maxFileName = 150

// maxFileNameBytes is how many bytes it may hold. Most file systems count
// characters, but ext4 and the file systems of network shares count bytes,
// and a Chinese title is three bytes a character: 150 of them is 450, past
// the 255 such a system takes. 240 leaves room for the extension and the
// " (2)" a second copy gets.
const maxFileNameBytes = 240

// maxArtistNames is how long the artists' names may run in a file's name:
// a song with a dozen guests makes a name no one reads and no folder fits
// beside its neighbours, so the names are cut at a separator rather than
// mid-name.
const maxArtistNames = 80

// unsafe in a file's name: what Windows refuses, and the separators of the
// paths of every system. A question mark is a song's own often enough
// ("真的爱你?"), so it is dropped rather than replaced.
const unsafe = `\/:*?#"<>|`

// FileName returns the name of the file a task's song is saved as, without
// its extension: format's words, filtered of what a file's name may not
// hold, and cut to a length a file system takes.
func FileName(format string, t music.Track) string {
	name, artist := t.Name, t.ArtistNames()
	if len(artist) > maxArtistNames {
		artist = clipNames(artist, maxArtistNames)
	}
	out := strings.ReplaceAll(format, "歌手", artist)
	out = strings.ReplaceAll(out, "歌名", name)
	out = strings.Map(func(r rune) rune {
		if strings.ContainsRune(unsafe, r) || r < 0x20 {
			return -1
		}
		return r
	}, out)
	// Whitespace around, and around what the words left behind: a scheme
	// that drops the artists leaves "歌名 - ", which names a file with a
	// dash hanging off its end.
	out = strings.Join(strings.Fields(out), " ")
	out = strings.Trim(out, " -_·,.")
	if out == "" {
		out = "未知歌曲"
	}
	if r := []rune(out); len(r) > maxFileName {
		out = strings.TrimSpace(string(r[:maxFileName]))
	}
	if len(out) > maxFileNameBytes {
		out = strings.TrimSpace(clip([]byte(out), maxFileNameBytes))
	}
	return out
}

// clipNames cuts a list of names joined by "、" to at most n characters,
// at a separator.
func clipNames(names string, n int) string {
	parts := strings.Split(names, "、")
	if len(parts) < 2 {
		return clip([]byte(names), n)
	}
	var kept []string
	size := 0
	for _, p := range parts {
		need := len(p)
		if size > 0 {
			need += len("、")
		}
		if size+need > n {
			break
		}
		kept, size = append(kept, p), size+need
	}
	if len(kept) == 0 {
		return clip([]byte(parts[0]), n)
	}
	return strings.Join(kept, "、")
}

// clip cuts s to n bytes without splitting a character.
func clip(s []byte, n int) string {
	if len(s) <= n {
		return string(s)
	}
	cut := n
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return string(s[:cut])
}

// Size returns bytes as the listener reads them: MB with one decimal for
// the large ones, KB for a song that is not yet a megabyte, and a dash for
// none. A value past a gigabyte goes in GB: a test's or a bug's absurd
// speed reads as absurd, not as a million megabytes.
func Size(n int64) string {
	switch {
	case n <= 0:
		return "—"
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}

// Speed returns bytes a second as the listener reads them.
func Speed(n float64) string {
	if n <= 0 {
		return ""
	}
	return Size(int64(n)) + "/s"
}
