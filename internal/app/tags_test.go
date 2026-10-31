package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	id3v2 "github.com/bogem/id3v2/v2"

	"netease-music-mygo/internal/music"
)

// The words a file keeps are the song's, and its translation only where
// the words have a line: a translation's own lines would drift from them.
func TestTranslationKeepsTheWordsTimes(t *testing.T) {
	lrc := "[00:01.00]第一行\n[00:05.50]第二行\n[00:10.20]第三行"
	tlyric := "[00:01.00]first\n[00:03.00]a note of its own\n[0:05.5]second\n[00:10.20]third"
	got := translationOf(lrc, tlyric)
	if strings.Contains(got, "a note") {
		t.Errorf("kept a line the words do not have: %q", got)
	}
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") || !strings.Contains(got, "third") {
		t.Errorf("lost a line the words have: %q", got)
	}
	// A translation without words of its own is dropped, rather than
	// written as the words twice.
	if got := translationOf(lrc, ""); got != "" {
		t.Errorf("an empty translation wrote %q", got)
	}
}

// The words' file carries the translation under the words when asked, and
// the words alone otherwise.
func TestLyricsText(t *testing.T) {
	words := lyricsText("[00:01.00]a", "[00:01.00]一", false)
	if strings.Contains(words, "一") {
		t.Errorf("the words alone carry the translation: %q", words)
	}
	both := lyricsText("[00:01.00]a", "[00:01.00]一", true)
	if !strings.Contains(both, "a") || !strings.Contains(both, "一") {
		t.Errorf("the words and their translation are %q", both)
	}
	// Nothing to translate: the words still come.
	if got := lyricsText("[00:01.00]a", "  ", true); got != "[00:01.00]a\n" {
		t.Errorf("with no translation the file is %q", got)
	}
}

// A .lrc is written beside its song, named after it, in UTF-8 with a byte
// order mark or in GBK as the settings say.
func TestWriteLrc(t *testing.T) {
	dir := t.TempDir()
	song := filepath.Join(dir, "晴天 - 周杰伦.flac")
	for _, c := range []struct {
		format string
		head   []byte
	}{
		{"utf8", []byte{0xef, 0xbb, 0xbf}},
		{"gbk", nil},
	} {
		if err := writeLrc(song, "[00:01.00]故事的小黄花", "", c.format, false); err != nil {
			t.Fatalf("%s: %v", c.format, err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "晴天 - 周杰伦.lrc"))
		if err != nil {
			t.Fatal(err)
		}
		if c.head != nil && !strings.HasPrefix(string(b), "\xef\xbb\xbf") {
			t.Errorf("%s has no byte order mark", c.format)
		}
		if c.head == nil {
			// GBK of 故 starts 0xB9: the file is not UTF-8 then.
			if strings.Contains(string(b), "故事") {
				t.Errorf("gbk wrote UTF-8")
			}
			if b[0] == 0xef && b[1] == 0xbb {
				t.Errorf("gbk wrote a UTF-8 byte order mark")
			}
		}
	}
}

// An MP3's tags carry the song's name, artists and album, and the cover
// when asked for: what the listener's player shows of a saved song.
func TestWriteMP3Tags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")
	// A minimal MP3: frame headers, which the tag writer keeps. The parser
	// reads a header's worth before the audio, so the file needs a few
	// bytes of it.
	frame := make([]byte, 64)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x64})
	if err := os.WriteFile(path, frame, 0o600); err != nil {
		t.Fatal(err)
	}
	tr := music.Track{Name: "晴天", Artists: []music.Artist{{Name: "周杰伦"}}, Album: music.Album{Name: "叶惠美"}}
	cover := []byte("\x89PNG\r\n\x1a\n") // only the MIME is read here
	if err := writeMP3(path, tr, "[00:01.00]故事的小黄花", cover, tagOpts{cover: true, lyrics: true}); err != nil {
		t.Fatalf("writeMP3: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The frames are UTF-16, which no easy byte search finds: open the tag
	// and read it back, as a player would.
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatalf("the tags do not read back: %v", err)
	}
	defer tag.Close()
	for frame, want := range map[string]string{
		"TIT2": "晴天",
		"TPE1": "周杰伦",
		"TALB": "叶惠美",
	} {
		if got := tag.GetTextFrame(frame).Text; got != want {
			t.Errorf("%s = %q, want %q", frame, got, want)
		}
	}
	if string(b[0:3]) != "ID3" {
		t.Error("the file has no tag at its start")
	}
	if n := len(tag.GetFrames("USLT")); n != 1 {
		t.Errorf("%d lyric frames, want the song's words", n)
	}
	if n := len(tag.GetFrames("APIC")); n != 1 {
		t.Errorf("%d pictures, want the cover", n)
	}
}

// The tag options follow the switches: a translation needs the words it
// goes into, and a lyric file its own.
func TestTagOptsFollowTheSwitches(t *testing.T) {
	s := settings{EmbedLyric: true, EmbedTranslation: true, DownloadLrc: true, DownloadLrcTranslation: true, DownloadLrcFormat: "gbk"}
	o := s.tagOpts()
	if !o.lyrics || !o.translation {
		t.Errorf("the tags leave out the words: %+v", o)
	}
	if !o.lyricsFile || !o.lrcTranslation || o.lrcFormat != "gbk" {
		t.Errorf("the lyric file's options are %+v", o)
	}
	off := settings{}
	if o := off.tagOpts(); o.lyrics || o.translation || o.lyricsFile || o.lrcTranslation {
		t.Errorf("settings off ask for %+v", o)
	}
}
