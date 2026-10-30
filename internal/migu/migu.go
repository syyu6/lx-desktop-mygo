// Package migu is Migu Music as a source: the catalogue the app searches,
// the songs it streams, and their words and covers, as lx-music-desktop
// asks them.
//
// Its search is the app client's, which signs each request with the query,
// the time and the app's own secrets; its stream is the player's, which
// answers with a link anyone may play for a song that is free, and with a
// link to a sample, or none, for one that is not.
package migu

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
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

// UserAgent is what Migu's Android client sends, which its servers expect.
const UserAgent = "Mozilla/5.0 (Linux; U; Android 11.0.0; zh-cn; MI 11 Build/OPR1.170623.032) AppleWebKit/534.30 (KHTML, like Gecko) Version/4.0 Mobile Safari/534.30"

// The app's own identity, as lx-music-desktop signs with it: its device,
// its channel, and the secrets its client is built with.
const (
	deviceID  = "963B7AA0D21511ED807EE5846EC87D20"
	signature = "6cdc72a439cef99a3418d2a78aa28c73"
	appKey    = "d16148780a1dcc7408e06336b98cfd50"
	channel   = "0146921"
	uiVersion = "A_music_3.6.1"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// Provider is Migu Music as package source asks it.
type Provider struct{}

func (Provider) ID() string   { return music.SourceMigu }
func (Provider) Name() string { return "咪咕音乐" }

// signedHeaders are what a search carries: the query signed with the
// time, so that a request cannot be replayed, and when it was signed.
func signedHeaders(text, ts string) map[string]string {
	sum := md5.Sum([]byte(text + signature + "yyapp2" + appKey + deviceID + ts))
	return map[string]string{
		"uiVersion":  uiVersion,
		"deviceId":   deviceID,
		"timestamp":  ts,
		"sign":       hex.EncodeToString(sum[:]),
		"channel":    channel,
		"User-Agent": UserAgent,
	}
}

// get asks addr for JSON, as out, with the headers it is given.
func get(addr string, headers map[string]string, out any) error {
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
		return fmt.Errorf("migu: %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("migu: %w", err)
	}
	return nil
}

// playerHeaders are what the player's endpoints carry: its channel, and
// the browser it says it is.
func playerHeaders() map[string]string {
	return map[string]string{
		"channel":    channel,
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/69.0.3497.100 Safari/537.36",
	}
}

// song is a song as Migu's search gives it. Its ids are strings, and a
// song has two of them: the one the catalogue names, and the one the
// player asks for.
type song struct {
	ID          string   `json:"id"`
	SongID      string   `json:"songId"`
	CopyrightID string   `json:"copyrightId"`
	Name        string   `json:"name"`
	SongName    string   `json:"songName"`
	AlbumID     string   `json:"albumId"`
	Album       string   `json:"album"`
	Duration    int64    `json:"duration"` // seconds
	Singers     []signer `json:"singerList"`
	LrcURL      string   `json:"lrcUrl"`
	LyricURL    string   `json:"lyricUrl"`
	// The album's images, the first the smallest and the last the largest.
	Img1 string `json:"img1"`
	Img2 string `json:"img2"`
	Img3 string `json:"img3"`
}

type signer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// track returns the song as the app knows it. The player asks for a song by
// its songId, which is what the track's key keeps; the catalogue's id is
// the number the track keeps.
func (s *song) track() music.Track {
	name := s.Name
	if name == "" {
		name = s.SongName
	}
	artists := make([]music.Artist, 0, len(s.Singers))
	for _, a := range s.Singers {
		if a.Name != "" {
			id, _ := strconv.ParseInt(a.ID, 10, 64)
			artists = append(artists, music.Artist{ID: id, Name: a.Name})
		}
	}
	albumID, _ := strconv.ParseInt(s.AlbumID, 10, 64)
	return music.Track{
		Source:   music.SourceMigu,
		ID:       int64(atoi(s.ID)),
		Key:      cmpOr(s.SongID, s.ID),
		Name:     name,
		Artists:  artists,
		Album:    music.Album{ID: albumID, Name: s.Album, PicURL: Cover(s.img())},
		Duration: s.Duration * 1000,
	}
}

// img is the largest of the album's images the search names: the smallest
// is a thumbnail, which a page showing a cover at any size outgrows.
func (s *song) img() string {
	for _, u := range []string{s.Img3, s.Img2, s.Img1} {
		if u != "" {
			return u
		}
	}
	return ""
}

// Cover makes an image path absolute, as Migu names its album's images by
// the path alone.
func Cover(path string) string {
	if path == "" || strings.HasPrefix(path, "http") {
		return path
	}
	return "https://d.musicapp.migu.cn" + path
}

func atoi(s string) int64 {
	i, _ := strconv.ParseInt(s, 10, 64)
	return i
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// searchSwitch asks for songs and their best show alone: the app looks for
// playlists and albums on pages of their own.
const searchSwitch = `{"song":1,"album":0,"singer":0,"tagSong":1,"mvSong":0,"bestShow":1,"songlist":0,"lyricSong":0}`

// SearchSongs returns a page of the songs matching query, and how many
// match in all.
func (Provider) SearchSongs(query string, limit, offset int) ([]music.Track, int, error) {
	page := offset/max(limit, 1) + 1
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	addr := "https://jadeite.migu.cn/music_search/v3/search/searchAll?isCorrect=0&isCopyright=1&sort=0&sid=USS" +
		"&searchSwitch=" + url.QueryEscape(searchSwitch) +
		"&pageSize=" + strconv.Itoa(limit) + "&pageNo=" + strconv.Itoa(page) +
		"&text=" + url.QueryEscape(query)
	var res struct {
		Code string `json:"code"`
		Info string `json:"info"`
		Data struct {
			Total json.Number `json:"totalCount"`
			// The results come in groups, each a song and its versions.
			List [][]song `json:"resultList"`
		} `json:"songResultData"`
	}
	if err := get(addr, signedHeaders(query, ts), &res); err != nil {
		return nil, 0, err
	}
	if res.Code != "000000" {
		return nil, 0, fmt.Errorf("migu: %s %s", res.Code, res.Info)
	}
	out := make([]music.Track, 0, len(res.Data.List))
	seen := map[string]bool{}
	for _, group := range res.Data.List {
		for i := range group {
			t := group[i].track()
			if t.Key == "" || seen[t.Key] {
				continue
			}
			seen[t.Key] = true
			out = append(out, t)
		}
	}
	total, _ := res.Data.Total.Int64()
	return out, int(total), nil
}

// Stream returns where a song streams from, at level, or the best Migu
// serves of it below. A song only its members may hear comes back as a
// sample of a minute, which is what Migu's player itself gives anyone.
func (Provider) Stream(t music.Track, level string) (*music.Stream, error) {
	tone := "PQ"
	switch level {
	case "higher", "exhigh":
		tone = "HQ"
	case "lossless", "hires":
		tone = "SQ"
	}
	addr := "https://app.c.nf.migu.cn/MIGUM2.0/v2.0/content/listen-url?netType=00&resourceType=2" +
		"&songId=" + url.QueryEscape(t.SongKey()) + "&toneFlag=" + tone
	var res struct {
		Code string `json:"code"`
		Info string `json:"info"`
		Data struct {
			URL string `json:"url"`
			// The player's own item, which names the song's covers.
			SongItem struct {
				AlbumImgs []struct {
					Size string `json:"imgSizeType"`
					Img  string `json:"img"`
				} `json:"albumImgs"`
			} `json:"songItem"`
		} `json:"data"`
	}
	if err := get(addr, playerHeaders(), &res); err != nil {
		return nil, err
	}
	if res.Code != "000000" || res.Data.URL == "" {
		return nil, fmt.Errorf("migu: %s %s", res.Code, res.Info)
	}
	typ := "mp3"
	if strings.Contains(strings.ToLower(res.Data.URL), ".flac") {
		typ = "flac"
	}
	return &music.Stream{URL: res.Data.URL, Level: toneLevel(tone), Type: typ}, nil
}

// toneLevel is the stream level a tone is: the app's own names, which the
// player shows, rather than Migu's.
func toneLevel(tone string) string {
	switch tone {
	case "HQ":
		return "exhigh"
	case "SQ":
		return "lossless"
	}
	return "standard"
}

// Lyrics returns a song's words, as Migu's player shows them: the song's
// own record names a file of them, which its CDN serves.
func (Provider) Lyrics(t music.Track) (music.Lyrics, error) {
	id := strconv.FormatInt(t.ID, 10)
	addr := "https://app.c.nf.migu.cn/MIGUM2.0/v2.0/content/querySongBySongId.do?songId=" + url.QueryEscape(id)
	var res struct {
		Code string `json:"code"`
		List []struct {
			LrcURL string `json:"lrcUrl"`
			Words  string `json:"lyrics"`
		} `json:"resource"`
	}
	if err := get(addr, playerHeaders(), &res); err != nil {
		return music.Lyrics{}, err
	}
	for _, r := range res.List {
		// The words come base64 of the LRC where the record carries them,
		// and else as a file of them, which the record names.
		if r.Words != "" {
			if lrc := decodeBase64(r.Words); strings.TrimSpace(lrc) != "" {
				return music.Lyrics{Lyric: lrc}, nil
			}
		}
		if r.LrcURL == "" {
			continue
		}
		if body, err := fetchText(Cover(r.LrcURL)); err == nil && strings.TrimSpace(body) != "" {
			return music.Lyrics{Lyric: body}, nil
		}
	}
	return music.Lyrics{}, errors.New("migu: no lyrics")
}

// decodeBase64 reads what Migu gives base64-encoded, and returns it as it
// is where it is not base64 at all.
func decodeBase64(s string) string {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return s
	}
	return string(b)
}

// fetchText returns what is at addr, as text.
func fetchText(addr string) (string, error) {
	req, err := http.NewRequest("GET", addr, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	res, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("migu: %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
