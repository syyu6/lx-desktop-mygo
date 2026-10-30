// Package kugou is Kugou Music as a search source: the catalogue the app
// searches, whose songs play through a music source that can stream them.
//
// It is a catalogue alone. Kugou streams through the app's own endpoint,
// which asks for a signature of the song and the listener's account, and
// lx-music-desktop streams it the same way; the app does not, so it leaves
// the streaming to the scripts the listener imports, which are imported to
// answer for exactly these services. What it does is search, which needs
// nothing of the listener: with NetEase and Kuwo off, a search still finds
// songs.
package kugou

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"netease-music-mygo/internal/music"
)

// UserAgent is what Kugou's clients send, which its servers expect.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

var httpClient = &http.Client{Timeout: 15 * time.Second}

// searchBase is Kugou's search, as its Android client calls it.
const searchBase = "http://songsearch.kugou.com/song_search_v2?platform=AndroidFilter&iscorrection=1&hifiquality=0&PrivilegeFilter=0&pagesize="

// Catalog is Kugou as package source asks it: a search source alone.
type Catalog struct{}

func (Catalog) ID() string   { return music.SourceKugou }
func (Catalog) Name() string { return "酷狗音乐" }

// song is a song as Kugou's search gives it. Its numbers come as strings
// and as numbers, so each is read as whichever it is.
type song struct {
	FileHash   string `json:"FileHash"`
	HQFileHash string `json:"HQFileHash"`
	SQFileHash string `json:"SQFileHash"`
	ResHash    string `json:"ResFileHash"`
	Name       string `json:"SongName"`
	OtherName  string `json:"OriSongName"`
	Album      string `json:"AlbumName"`
	AlbumID    any    `json:"AlbumID"`
	// Singers is the artists, each with its own name.
	Singers  []struct{ Name string } `json:"Singers"`
	Duration any                     `json:"Duration"` // seconds
	Audioid  any                     `json:"Audioid"`
	MixID    any                     `json:"MixSongID"`
	// Image is the album's cover, its size left as `{size}` for the asker
	// to fill in; AlbumImage is what older answers name instead.
	Image      string `json:"Image"`
	AlbumImage string `json:"AlbumImage"`
}

// cover returns the song's cover at size, as Kugou's image CDN serves it.
// The search names it with its size left out, or with an empty album's.
func (s *song) cover(size int) string {
	img := s.Image
	if img == "" {
		img = s.AlbumImage
	}
	if img == "" {
		return ""
	}
	return strings.ReplaceAll(img, "{size}", strconv.Itoa(size))
}

// track returns the song as the app knows it. Its hash is what a music
// source asks Kugou by, so it is kept, as is the album's id.
func (s *song) track() music.Track {
	hash := s.FileHash
	if hash == "" {
		hash = s.HQFileHash
	}
	artists := make([]music.Artist, 0, len(s.Singers))
	for _, a := range s.Singers {
		if a.Name != "" {
			artists = append(artists, music.Artist{Name: a.Name})
		}
	}
	return music.Track{
		Source:   music.SourceKugou,
		ID:       num(s.Audioid),
		Key:      hash,
		Name:     cmpOr(s.Name, s.OtherName),
		Artists:  artists,
		Album:    music.Album{ID: num(s.AlbumID), Name: s.Album, PicURL: s.cover(240)},
		Duration: num(s.Duration) * 1000,
	}
}

// num reads a number Kugou gives as a JSON string, or as a number.
func num(v any) int64 {
	switch n := v.(type) {
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case float64:
		return int64(n)
	}
	return 0
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// SearchSongs returns a page of the songs matching query, and how many
// match in all.
func (Catalog) SearchSongs(query string, limit, offset int) ([]music.Track, int, error) {
	page := offset/max(limit, 1) + 1
	addr := searchBase + strconv.Itoa(limit) + "&keyword=" + url.QueryEscape(query) + "&page=" + strconv.Itoa(page)
	req, err := http.NewRequest("GET", addr, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", UserAgent)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("kugou: %s", res.Status)
	}
	var body struct {
		Status int `json:"status"`
		Data   struct {
			Total int    `json:"total"`
			Lists []song `json:"lists"`
		} `json:"data"`
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, 0, err
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, 0, fmt.Errorf("kugou: %w", err)
	}
	if body.Status != 1 {
		return nil, 0, fmt.Errorf("kugou: status %d", body.Status)
	}
	out := make([]music.Track, 0, len(body.Data.Lists))
	seen := map[string]bool{}
	for i := range body.Data.Lists {
		t := body.Data.Lists[i].track()
		if t.Key == "" || seen[t.Key] {
			continue
		}
		seen[t.Key] = true
		out = append(out, t)
	}
	return out, body.Data.Total, nil
}

// lyricHeaders are what Kugou's lyric service asks of its callers: they
// are the search client's, which is the only one it answers.
var lyricHeaders = map[string]string{
	"KG-RC":      "1",
	"KG-THash":   "expand_search_manager.cpp:852736169:451",
	"User-Agent": "KuGou2012-9020-ExpandSearchManager",
}

// Lyrics returns a song's words, as Kugou's own client shows them: the
// lyric service finds the words that go with the song's hash, and serves
// them base64-encoded.
//
// It offers several sets, of which some are the words alone, without their
// times, and one is the song's own: each is read until one comes timed.
func (Catalog) Lyrics(t music.Track) (music.Lyrics, error) {
	addr := "http://lyrics.kugou.com/search?ver=1&man=yes&client=pc&lrctxt=1" +
		"&keyword=" + url.QueryEscape(t.Name+" - "+t.ArtistNames()) +
		"&hash=" + url.QueryEscape(t.SongKey()) +
		"&timelength=" + strconv.FormatInt(t.Duration, 10)
	var found struct {
		Candidates []struct {
			ID        string `json:"id"`
			AccessKey string `json:"accesskey"`
		} `json:"candidates"`
	}
	if err := getWith(addr, lyricHeaders, &found); err != nil {
		return music.Lyrics{}, err
	}
	for _, c := range found.Candidates {
		if c.ID == "" || c.AccessKey == "" {
			continue
		}
		addr := "http://lyrics.kugou.com/download?ver=1&client=pc&fmt=lrc&charset=utf8" +
			"&id=" + url.QueryEscape(c.ID) + "&accesskey=" + url.QueryEscape(c.AccessKey)
		var dl struct {
			Content string `json:"content"`
		}
		if err := getWith(addr, lyricHeaders, &dl); err != nil {
			continue
		}
		if lrc := decodeLyric(dl.Content); timed(lrc) {
			return music.Lyrics{Lyric: lrc}, nil
		}
	}
	return music.Lyrics{}, errors.New("kugou: no lyrics")
}

// timed reports whether lrc is words with their times, rather than words
// alone: a lyric line begins with its stamp.
func timed(lrc string) bool {
	for _, line := range strings.Split(lrc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[00:") {
			return true
		}
	}
	return false
}

// decodeLyric reads what Kugou's lyric service serves: its words, as LRC,
// come base64-encoded.
func decodeLyric(s string) string {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return s
	}
	return string(b)
}

// getWith asks addr for JSON, as out, with the headers given.
func getWith(addr string, headers map[string]string, out any) error {
	req, err := http.NewRequest("GET", addr, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("kugou: %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("kugou: %w", err)
	}
	return nil
}
