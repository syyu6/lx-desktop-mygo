package app

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/jpeg" // registered for the cover's size, written into a FLAC's picture block
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bogem/id3v2/v2"
	flac "github.com/go-flac/go-flac/v2"
	"golang.org/x/text/encoding/simplifiedchinese"

	"netease-music-mygo/internal/music"
)

// What goes into a downloaded file, and beside it: the tags the listener
// asked to find, and the words as a .lrc file of its own.
//
// An MP3 keeps its tags in an ID3v2 frame, which the library rewrites in
// place; a FLAC keeps them in metadata blocks before its audio, which
// go-flac rewrites. Both write another file and replace the song's, so that
// one that fails half way is never left looking whole.

// tagOpts are the parts of the settings the tags follow.
type tagOpts struct {
	cover          bool // embed the cover
	lyrics         bool // embed the words
	translation    bool // with their translation
	lyricsFile     bool // write a .lrc beside the file
	lrcFormat      string
	lrcTranslation bool
}

func (s *settings) tagOpts() tagOpts {
	return tagOpts{
		cover:          s.EmbedCover,
		lyrics:         s.EmbedLyric,
		translation:    s.EmbedTranslation,
		lyricsFile:     s.DownloadLrc,
		lrcFormat:      s.DownloadLrcFormat,
		lrcTranslation: s.DownloadLrcTranslation,
	}
}

// writeTags writes the song's tags into the file at path, whose format is
// mp3 or flac. The two carry tags of their own; an M4A's live in its atom
// tree, which the app does not rewrite, so such a song keeps its name and
// its folder and no tags.
func writeTags(path, format string, t music.Track, lrc, tlyric string, cover []byte, o tagOpts) error {
	words := ""
	if o.lyrics {
		words = wordsWith(lrc, tlyric, o.translation)
	}
	switch format {
	case "mp3":
		return writeMP3(path, t, words, cover, o)
	case "flac":
		return writeFLAC(path, t, words, cover, o)
	}
	return nil
}

// writeMP3 writes an MP3's tags.
func writeMP3(path string, t music.Track, words string, cover []byte, o tagOpts) error {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer tag.Close()
	// What the source had is not the listener's: the file is written anew,
	// so that a tag written twice — a re-download — does not stack its
	// pictures and lyrics.
	tag.DeleteAllFrames()
	// ID3v2.3, which every player reads, and UTF-16 with it: 2.3's default
	// is Latin-1, which no Chinese title survives.
	tag.SetVersion(3)
	tag.SetDefaultEncoding(id3v2.EncodingUTF16)
	tag.SetTitle(t.Name)
	tag.SetArtist(t.ArtistNames())
	if t.Album.Name != "" {
		tag.SetAlbum(t.Album.Name)
	}
	if o.cover && len(cover) > 0 {
		tag.AddAttachedPicture(id3v2.PictureFrame{
			Encoding:    id3v2.EncodingUTF8,
			MimeType:    coverMIME(cover),
			PictureType: id3v2.PTFrontCover,
			Description: "Cover",
			Picture:     cover,
		})
	}
	if words != "" {
		tag.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{
			Encoding:          id3v2.EncodingUTF8,
			Language:          "chi",
			ContentDescriptor: "",
			Lyrics:            words,
		})
	}
	return tag.Save()
}

// writeFLAC writes a FLAC's tags: its Vorbis comments and its picture, as
// metadata blocks before the audio.
func writeFLAC(path string, t music.Track, words string, cover []byte, o tagOpts) error {
	f, err := flac.ParseFile(path)
	if err != nil {
		return err
	}
	// The audio is read after the new blocks are written, in the same pass:
	// what is read is the song's own file, and what is written another
	// beside it.
	defer f.Close()
	meta := make([]*flac.MetaDataBlock, 0, len(f.Meta)+2)
	for _, b := range f.Meta {
		switch b.Type {
		case flac.VorbisComment, flac.Picture:
			// The source's own tags go, as an MP3's do: the listener's
			// file says what they see, once.
			continue
		}
		meta = append(meta, b)
	}
	comment := flacVorbis(t, words)
	meta = append(meta, &comment)
	if o.cover && len(cover) > 0 {
		if block, err := flacPicture(cover); err == nil {
			meta = append(meta, block)
		}
	}
	// StreamInfo is the first block, and must stay so.
	sort.SliceStable(meta, func(i, j int) bool { return meta[i].Type == flac.StreamInfo && meta[j].Type != flac.StreamInfo })

	tmp, err := os.CreateTemp(filepath.Dir(path), ".lxmuc-tags-*.flac")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	out := &flac.File{Meta: meta, Frames: f.Frames}
	if _, err := out.WriteTo(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// flacVorbis builds the Vorbis comment block of a song's tags: the fields
// every player reads, and the words in LYRICS.
func flacVorbis(t music.Track, words string) flac.MetaDataBlock {
	var b bytes.Buffer
	le := func(s string) {
		var n [4]byte
		binary.LittleEndian.PutUint32(n[:], uint32(len(s)))
		b.Write(n[:])
		b.WriteString(s)
	}
	le("LxMuc")
	comments := [][2]string{{"TITLE", t.Name}, {"ARTIST", t.ArtistNames()}}
	if t.Album.Name != "" {
		comments = append(comments, [2]string{"ALBUM", t.Album.Name})
	}
	if words != "" {
		comments = append(comments, [2]string{"LYRICS", words})
	}
	var count [4]byte
	binary.LittleEndian.PutUint32(count[:], uint32(len(comments)))
	b.Write(count[:])
	for _, c := range comments {
		le(c[0] + "=" + c[1])
	}
	return flac.MetaDataBlock{Type: flac.VorbisComment, Data: b.Bytes()}
}

// flacPicture builds the picture block of a song's cover, a front cover:
// its type, its MIME and size, and the image itself.
func flacPicture(data []byte) (*flac.MetaDataBlock, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	be := func(n uint32) {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], n)
		b.Write(buf[:])
	}
	str := func(s string) {
		be(uint32(len(s)))
		b.WriteString(s)
	}
	mime := coverMIME(data)
	be(3) // front cover
	str(mime)
	str("")
	be(uint32(cfg.Width))
	be(uint32(cfg.Height))
	if mime == "image/png" {
		be(32)
	} else {
		be(24)
	}
	be(0)
	be(uint32(len(data)))
	b.Write(data)
	return &flac.MetaDataBlock{Type: flac.Picture, Data: b.Bytes()}, nil
}

// coverMIME names an image's type by its first bytes.
func coverMIME(b []byte) string {
	if len(b) > 3 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		return "image/png"
	}
	return "image/jpeg"
}

// writeLrc writes the song's words beside the file at path, as a .lrc of
// the same name: utf8 with a byte order mark, which players that read
// Chinese need to know it is not the system's, or GBK for the players that
// read only that.
func writeLrc(path, lrc, tlyric, format string, withTranslation bool) error {
	body := []byte(lyricsText(lrc, tlyric, withTranslation))
	switch format {
	case "gbk":
		enc, err := simplifiedchinese.GBK.NewEncoder().Bytes(body)
		if err != nil {
			return err
		}
		body = enc
	default:
		body = append([]byte{0xef, 0xbb, 0xbf}, body...)
	}
	lrcPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".lrc"
	tmp := lrcPath + ".part"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, lrcPath); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// lyricsText is the words, and their translation after them when asked.
func lyricsText(lrc, tlyric string, withTranslation bool) string {
	lrc = strings.TrimSpace(lrc)
	if !withTranslation || strings.TrimSpace(tlyric) == "" {
		return lrc + "\n"
	}
	if extra := translationOf(lrc, tlyric); extra != "" {
		return lrc + "\n\n" + extra + "\n"
	}
	return lrc + "\n"
}

// wordsWith is the words and their translation, joined as a file's tags
// keep them: the lines the words have, the translation under them.
func wordsWith(lrc, tlyric string, withTranslation bool) string {
	return strings.TrimSpace(lyricsText(lrc, tlyric, withTranslation))
}

var lrcTime = regexp.MustCompile(`\[(\d+):(\d+)(?:[.:](\d+))?\]`)

// translationOf returns the lines of translation whose times the words
// have. A translation carries lines of its own often enough — a repeated
// line, a note — and a file whose tags show lines the words do not would
// drift from them.
func translationOf(lrc, tlyric string) string {
	have := map[string]bool{}
	for _, line := range strings.Split(lrc, "\n") {
		for _, tm := range lrcTime.FindAllString(line, -1) {
			have[normalTime(tm)] = true
		}
	}
	var out []string
	for _, line := range strings.Split(tlyric, "\n") {
		line = strings.TrimSpace(line)
		times := lrcTime.FindAllString(line, -1)
		if len(times) == 0 {
			continue
		}
		var kept []string
		for _, tm := range times {
			if have[normalTime(tm)] {
				kept = append(kept, tm)
			}
		}
		if len(kept) == 0 {
			continue
		}
		out = append(out, strings.Join(kept, "")+strings.TrimSpace(lrcTime.ReplaceAllString(line, "")))
	}
	return strings.Join(out, "\n")
}

var leadingZeros = regexp.MustCompile(`\[0+(\d)`)

// normalTime is a time label without its leading zeros or its trailing
// ones, which a lyric file and its translation write differently often
// enough: [00:12.50] against [0:12.5].
func normalTime(tm string) string {
	tm = leadingZeros.ReplaceAllString(tm, "[$1")
	tm = strings.TrimSuffix(tm, "]")
	if i := strings.IndexByte(tm, '.'); i >= 0 {
		tm = strings.TrimRight(tm, "0")
	}
	return tm
}
