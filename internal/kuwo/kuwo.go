// Package kuwo is Kuwo Music as a source: the services lx-music-desktop
// asks, whose songs the app browses and plays on its own, without NetEase.
//
// Kuwo serves its playlists, searches and lyric through endpoints its web
// and mobile clients call, and a song's stream through antiserver, which
// answers with a link to the file. That is why it is built in: with
// NetEase turned off, the app still searches, browses and plays.
package kuwo

import (
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
	"netease-music-mygo/internal/network"
)

// UserAgent is what Kuwo's clients send, which its servers expect.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

var httpClient = network.Client(20 * time.Second)

// Provider is Kuwo as package source asks it.
type Provider struct{}

func (Provider) ID() string   { return music.SourceKuwo }
func (Provider) Name() string { return "酷我音乐" }

// get asks url for JSON, as out.
func get(addr string, out any) error {
	req, err := http.NewRequest("GET", addr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "http://www.kuwo.cn/")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("kuwo: %s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	// Its mobile endpoints pad the JSON with blank lines.
	body = []byte(strings.TrimSpace(string(body)))
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("kuwo: %w", err)
	}
	return nil
}

// num is a number Kuwo gives as a JSON string, or as a number: its
// endpoints are of both kinds.
type num string

func (n *num) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*n = ""
		return nil
	}
	*n = num(strings.Trim(s, `"`))
	return nil
}

func (n num) Int() int64 {
	v, _ := strconv.ParseInt(string(n), 10, 64)
	return v
}

func (n num) Float() float64 {
	v, _ := strconv.ParseFloat(string(n), 64)
	return v
}

// song is a song as Kuwo's search and playlist endpoints give it.
type song struct {
	Name     string `json:"NAME"`
	SongName string `json:"SONGNAME"`
	Artist   string `json:"ARTIST"`
	Album    string `json:"ALBUM"`
	AlbumID  num    `json:"ALBUMID"`
	Duration num    `json:"DURATION"` // seconds
	Rid      string `json:"MUSICRID"` // MUSIC_<id>
	// Pic is the cover's path on Kuwo's image CDN, its size a prefix of
	// it: 120/s3s94/93/211513640.jpg is the same cover as 500's.
	Pic string `json:"web_albumpic_short"`
	// hts_PIC is what the older endpoints name, and is empty here.
	LegacyPic string `json:"hts_PIC"`
}

// cover returns the song's cover at size, as Kuwo's image CDN serves it.
// A song whose album names no cover has none, as lx-music-desktop finds
// it: Kuwo's picture service names one by the song's id, but answers with
// that address rather than the picture, which would be a request of its
// own for every row of a page.
func (s *song) cover(size int) string {
	path := s.Pic
	if path == "" {
		path = s.LegacyPic
	}
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http") {
		return path
	}
	// The path begins with the size it was asked at, which is the one part
	// of it that changes.
	if i := strings.IndexByte(path, '/'); i > 0 {
		path = strconv.Itoa(size) + path[i:]
	}
	return "https://img1.kuwo.cn/star/albumcover/" + path
}

// id is the song's id, as Kuwo's own addresses name it.
func (s *song) id() string {
	return strings.TrimPrefix(s.Rid, "MUSIC_")
}

// track returns the song as the app plays it.
func (s *song) track() music.Track {
	name := s.Name
	if name == "" {
		name = s.SongName
	}
	return music.Track{
		Source:   music.SourceKuwo,
		ID:       num(s.id()).Int(),
		Name:     name,
		Artists:  artists(s.Artist),
		Album:    music.Album{ID: s.AlbumID.Int(), Name: s.Album, PicURL: s.cover(300)},
		Duration: s.Duration.Int() * 1000,
	}
}

// artists splits a credits line as Kuwo joins it: with `&`, and its other
// artists with `###`.
func artists(s string) []music.Artist {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []music.Artist
	for _, name := range strings.Split(s, "&") {
		name = strings.TrimSpace(name)
		if name != "" {
			out = append(out, music.Artist{Name: name})
		}
	}
	return out
}

// page is a page of what Kuwo's search found.
type page struct {
	Total num    `json:"TOTAL"`
	List  []song `json:"abslist"`
}

const searchBase = "http://search.kuwo.cn/r.s?client=kt&uid=0&ver=kwplayer_ar_9.2.2.1&vipver=1&show_copyright_off=1&newver=1&cluster=0&strategy=2012&encoding=utf8&rformat=json&mobi=1"

// SearchSongs returns a page of the songs matching query, and how many
// match in all.
func (Provider) SearchSongs(query string, limit, offset int) ([]music.Track, int, error) {
	addr := searchBase + "&ft=music&all=" + url.QueryEscape(query) +
		"&pn=" + strconv.Itoa(offset/limit) + "&rn=" + strconv.Itoa(limit)
	var res page
	if err := get(addr, &res); err != nil {
		return nil, 0, err
	}
	out := make([]music.Track, len(res.List))
	for i := range res.List {
		out[i] = res.List[i].track()
	}
	return out, int(res.Total.Int()), nil
}

// SearchPlaylists returns a page of the playlists matching query, and how
// many match in all.
func (Provider) SearchPlaylists(query string, limit, offset int) ([]music.Playlist, int, error) {
	addr := searchBase + "&ft=playlist&pay=0&all=" + url.QueryEscape(query) +
		"&pn=" + strconv.Itoa(offset/limit) + "&rn=" + strconv.Itoa(limit)
	var res struct {
		Total num `json:"TOTAL"`
		List  []struct {
			ID     string `json:"DC_TARGETID"`
			Name   string `json:"name"`
			Author string `json:"nickname"`
			Pic    string `json:"hts_pic"`
			Desc   string `json:"intro"`
			Songs  num    `json:"songnum"`
			Played num    `json:"playcnt"`
		} `json:"abslist"`
	}
	if err := get(addr, &res); err != nil {
		return nil, 0, err
	}
	out := make([]music.Playlist, len(res.List))
	for i, l := range res.List {
		out[i] = music.Playlist{
			Source:      music.SourceKuwo,
			ID:          num(l.ID).Int(),
			Name:        l.Name,
			CoverURL:    l.Pic,
			Description: l.Desc,
			PlayCount:   l.Played.Float(),
			TrackCount:  int(l.Songs.Int()),
		}
		out[i].Creator.Nickname = l.Author
	}
	return out, int(res.Total.Int()), nil
}

// Playlist returns a playlist with its songs.
func (Provider) Playlist(id string) (*music.Playlist, error) {
	addr := "http://nplserver.kuwo.cn/pl.svc?op=getlistinfo&encode=utf8&keyset=pl2012&identity=kuwo&pcmp4=1" +
		"&vipver=MUSIC_9.0.5.0_W1&newver=1&pid=" + url.QueryEscape(id) + "&pn=0&rn=1000"
	var res struct {
		ID     num    `json:"id"`
		Title  string `json:"title"`
		Pic    string `json:"pic"`
		Info   string `json:"info"`
		Author string `json:"uname"`
		Played num    `json:"playnum"`
		Total  num    `json:"total"`
		Music  []song `json:"musiclist"`
	}
	if err := get(addr, &res); err != nil {
		return nil, err
	}
	if res.Title == "" {
		return nil, errors.New("kuwo: no such playlist")
	}
	pl := &music.Playlist{
		Source:      music.SourceKuwo,
		ID:          res.ID.Int(),
		Name:        res.Title,
		CoverURL:    res.Pic,
		Description: res.Info,
		PlayCount:   res.Played.Float(),
		TrackCount:  int(res.Total.Int()),
		Tracks:      make([]music.Track, len(res.Music)),
	}
	pl.Creator.Nickname = res.Author
	for i := range res.Music {
		pl.Tracks[i] = res.Music[i].track()
	}
	return pl, nil
}

// Stream returns where a song streams from, at level, or the best Kuwo
// serves of it below. Kuwo's lossless needs an account, so the levels
// above standard are asked for 320 kbps MP3, which it serves anyone.
func (Provider) Stream(t music.Track, level string) (*music.Stream, error) {
	br, served := "128kmp3", "standard"
	switch level {
	case "higher", "exhigh", "lossless", "hires":
		br, served = "320kmp3", "exhigh"
	}
	addr := "https://antiserver.kuwo.cn/anti.s?type=convert_url3&response=url&format=mp3&br=" + br +
		"&rid=MUSIC_" + strconv.FormatInt(t.ID, 10)
	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		URL  string `json:"url"`
	}
	if err := get(addr, &res); err != nil {
		return nil, err
	}
	if res.URL == "" {
		return nil, fmt.Errorf("kuwo: %s", res.Msg)
	}
	return &music.Stream{URL: res.URL, Level: served, Type: "mp3"}, nil
}

// Lyrics returns a song's words, timed as its mobile client shows them.
//
// The service answers "音乐查询失败" with a null data on a share of its
// requests at random — about two in five — rather than for a song it has no
// words for: the ask is repeated a few times before that is taken for an
// answer, which leaves the chance of a false "no lyrics" at about one in a
// hundred.
func (Provider) Lyrics(t music.Track) (music.Lyrics, error) {
	addr := "https://m.kuwo.cn/newh5/singles/songinfoandlrc?httpsStatus=1&musicId=" + strconv.FormatInt(t.ID, 10)
	var res struct {
		Data *struct {
			Lrc []struct {
				Line string `json:"lineLyric"`
				Time string `json:"time"` // seconds from the start
			} `json:"lrclist"`
		} `json:"data"`
	}
	for try := 0; try < 5; try++ {
		if try > 0 {
			time.Sleep(time.Duration(try) * 120 * time.Millisecond)
		}
		res.Data = nil
		if err := get(addr, &res); err != nil {
			return music.Lyrics{}, err
		}
		if res.Data != nil && len(res.Data.Lrc) > 0 {
			break
		}
	}
	if res.Data == nil || len(res.Data.Lrc) == 0 {
		return music.Lyrics{}, errors.New("kuwo: no lyrics")
	}
	var b strings.Builder
	for _, l := range res.Data.Lrc {
		secs, _ := strconv.ParseFloat(l.Time, 64)
		if secs < 0 {
			secs = 0
		}
		m := int(secs) / 60
		s := int(secs) % 60
		cs := int(secs*100) % 100
		fmt.Fprintf(&b, "[%02d:%02d.%02d]%s\n", m, s, cs, l.Line)
	}
	return music.Lyrics{Lyric: b.String()}, nil
}
