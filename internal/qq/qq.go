// Package qq is QQ Music as a source: the catalogue the app searches, the
// playlists it opens, the songs it streams, and their words, as
// lx-music-desktop asks them.
//
// The endpoints are the ones its web player calls, with no account behind
// them: a search, a playlist, a lyric, and a stream, whose link QQ signs
// for the song and a session id of the app's own. What it will not stream
// without a listener's account — a VIP song, and lossless almost always —
// comes back without a link, and the chain looks for the song on the other
// sources, as it does for any song its own source will not serve.
package qq

import (
	"bytes"
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

// UserAgent is what QQ Music's web player sends, which its servers expect.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

var httpClient = &http.Client{Timeout: 20 * time.Second}

// Provider is QQ Music as package source asks it.
type Provider struct{}

func (Provider) ID() string   { return music.SourceQQ }
func (Provider) Name() string { return "QQ 音乐" }

// get asks addr for JSON, as out, with the headers QQ's API wants: it
// answers only to a referer from its own site.
func get(addr string, out any) error {
	req, err := http.NewRequest("GET", addr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://y.qq.com/")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("qq: %s", res.Status)
	}
	return decode(res.Body, out)
}

// post asks the musicu endpoint, which takes the request as a JSON body.
func post(out any, call map[string]any) error {
	body, err := json.Marshal(call)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://u.y.qq.com/cgi-bin/musicu.fcg", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://y.qq.com/")
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("qq: %s", res.Status)
	}
	return decode(res.Body, out)
}

func decode(r io.Reader, out any) error {
	b, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("qq: %w", err)
	}
	return nil
}

// comm is what every musicu call carries: the client the app says it is.
func comm() map[string]any {
	return map[string]any{"uin": 0, "format": "json", "ct": 24, "cv": 0}
}

// guid is the session id a stream's link is signed for. QQ does not check
// that it is a real session, only that the same one asks and plays.
const guid = "10000"

// signer is a singer of a song, as search and playlists give them.
type signer struct {
	ID   int64  `json:"id"`
	Mid  string `json:"mid"`
	Name string `json:"name"`
}

// album is the album a song is in.
type album struct {
	ID   int64  `json:"id"`
	Mid  string `json:"mid"`
	Name string `json:"name"`
}

// songRef is a song as QQ's search and playlists give it. The search names
// its fields flatly, the playlists nest them, so each is read as either.
type songRef struct {
	ID    int64  `json:"id"`
	Mid   string `json:"mid"`
	Name  string `json:"name"`
	Title string `json:"title"`
	// The song's mid and name, as the search's older shape gives them.
	SongMid  string `json:"songmid"`
	SongName string `json:"songname"`
	Interval int64  `json:"interval"` // seconds

	Singers []signer `json:"singer"`
	Album   album    `json:"album"`
	// The search's flat names of the album.
	AlbumMid  string `json:"albummid"`
	AlbumName string `json:"albumname"`
}

// key is the song's mid: the search and the playlists name it differently.
func (s *songRef) key() string {
	if s.Mid != "" {
		return s.Mid
	}
	return s.SongMid
}

func (s *songRef) name() string {
	return cmpOr(s.Name, cmpOr(s.SongName, s.Title))
}

// track returns the song as the app knows it. QQ names a song by its mid,
// which is no number, so it is kept as the track's key; and its album's
// mid is turned into the cover QQ serves at a size.
func (s *songRef) track() music.Track {
	artists := make([]music.Artist, 0, len(s.Singers))
	for _, a := range s.Singers {
		if a.Name != "" {
			artists = append(artists, music.Artist{ID: a.ID, Name: a.Name})
		}
	}
	al := music.Album{ID: s.Album.ID, Name: s.Album.Name}
	mid := s.Album.Mid
	if al.Name == "" {
		al.Name, mid = s.AlbumName, s.AlbumMid
	}
	if mid != "" {
		al.PicURL = AlbumCover(mid, 300)
	}
	return music.Track{
		Source:   music.SourceQQ,
		ID:       s.ID,
		Key:      s.key(),
		Name:     s.name(),
		Artists:  artists,
		Album:    al,
		Duration: s.Interval * 1000,
	}
}

// AlbumCover is an album's cover at size, which QQ's image CDN serves at
// any of the sizes it names.
func AlbumCover(mid string, size int) string {
	return fmt.Sprintf("https://y.gtimg.cn/music/photo_new/T002R%dx%dM000%s.jpg", size, size, mid)
}

// ArtistCover is an artist's picture at size.
func ArtistCover(mid string, size int) string {
	return fmt.Sprintf("https://y.gtimg.cn/music/photo_new/T001R%dx%dM000%s.jpg", size, size, mid)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// searchBase is QQ's search, as its web player calls it. new_json is what
// makes it answer with the songs' mids and their albums' mids, which is
// what a stream and a cover are asked by.
const searchBase = "https://c.y.qq.com/soso/fcgi-bin/client_search_cp?format=json&new_json=1&cr=1&t=0&aggr=1"

// SearchSongs returns a page of the songs matching query, and how many
// match in all.
func (Provider) SearchSongs(query string, limit, offset int) ([]music.Track, int, error) {
	page := offset/max(limit, 1) + 1
	addr := searchBase + "&p=" + strconv.Itoa(page) + "&n=" + strconv.Itoa(limit) +
		"&w=" + url.QueryEscape(query)
	var res struct {
		Code int `json:"code"`
		Data struct {
			Song struct {
				Total int       `json:"totalnum"`
				List  []songRef `json:"list"`
			} `json:"song"`
		} `json:"data"`
	}
	if err := get(addr, &res); err != nil {
		return nil, 0, err
	}
	if res.Code != 0 {
		return nil, 0, fmt.Errorf("qq: code %d", res.Code)
	}
	out := make([]music.Track, 0, len(res.Data.Song.List))
	seen := map[string]bool{}
	for i := range res.Data.Song.List {
		t := res.Data.Song.List[i].track()
		if t.Key == "" || seen[t.Key] {
			continue
		}
		seen[t.Key] = true
		out = append(out, t)
	}
	return out, res.Data.Song.Total, nil
}

// SearchPlaylists returns a page of the playlists matching query, and how
// many match in all.
func (Provider) SearchPlaylists(query string, limit, offset int) ([]music.Playlist, int, error) {
	page := offset / max(limit, 1)
	addr := "https://c.y.qq.com/soso/fcgi-bin/client_music_search_songlist?page_no=" + strconv.Itoa(page) +
		"&num_per_page=" + strconv.Itoa(limit) + "&format=json&remoteplace=txt.yqq.playlist&inCharset=utf8&outCharset=utf-8" +
		"&query=" + url.QueryEscape(query)
	var res struct {
		Code int `json:"code"`
		Data struct {
			Total int `json:"display_num"`
			List  []struct {
				ID      string  `json:"dissid"`
				Name    string  `json:"dissname"`
				Pic     string  `json:"imgurl"`
				Intro   string  `json:"introduction"`
				Songs   int     `json:"song_count"`
				Listens jsonNum `json:"listennum"`
				Creator struct {
					Name string `json:"name"`
				} `json:"creator"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := get(addr, &res); err != nil {
		return nil, 0, err
	}
	if res.Code != 0 {
		return nil, 0, fmt.Errorf("qq: code %d", res.Code)
	}
	out := make([]music.Playlist, 0, len(res.Data.List))
	for _, l := range res.Data.List {
		id, _ := strconv.ParseInt(l.ID, 10, 64)
		pl := music.Playlist{
			Source:      music.SourceQQ,
			ID:          id,
			Key:         l.ID,
			Name:        unescape(l.Name),
			CoverURL:    l.Pic,
			Description: unescape(l.Intro),
			TrackCount:  l.Songs,
			PlayCount:   l.Listens.Float(),
		}
		pl.Creator.Nickname = l.Creator.Name
		out = append(out, pl)
	}
	return out, res.Data.Total, nil
}

// unescape undoes the entities QQ's search escapes in the names it gives:
// its playlist names come with `&#58;` and `&#32;` in them.
func unescape(s string) string {
	if !strings.Contains(s, "&#") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "&#") {
			end := strings.IndexByte(s[i:], ';')
			if end > 2 {
				if n, err := strconv.ParseInt(s[i+2:i+end], 10, 32); err == nil {
					b.WriteRune(rune(n))
					i += end + 1
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// playlistPage is how many songs a playlist's page loads: QQ's own limit
// for one answer.
const playlistPage = 300

// Playlist returns a playlist with its songs.
func (Provider) Playlist(id string) (*music.Playlist, error) {
	out := &music.Playlist{Source: music.SourceQQ, Key: id}
	num, _ := strconv.ParseInt(id, 10, 64)
	if num == 0 {
		return nil, errors.New("qq: no such playlist")
	}
	out.ID = num
	for begin := 0; ; begin += playlistPage {
		var res struct {
			Code int `json:"code"`
			Req  struct {
				Code int `json:"code"`
				Data struct {
					DirInfo struct {
						Title string `json:"title"`
						Pic   string `json:"picurl"`
						Desc  string `json:"desc"`
						Total int    `json:"songnum"`
					} `json:"dirinfo"`
					Songs []songRef `json:"songlist"`
				} `json:"data"`
			} `json:"req_0"`
		}
		err := post(&res, map[string]any{
			"comm": comm(),
			"req_0": map[string]any{
				"module": "music.srfDissInfo.DissInfo",
				"method": "CgiGetDiss",
				// The id is a number here: sent as a string, QQ answers
				// that no such playlist exists.
				"param": map[string]any{
					"disstid": num, "dirid": 0, "tag": 1,
					"song_begin": begin, "song_num": playlistPage, "userinfo": 1,
				},
			},
		})
		if err != nil {
			return nil, err
		}
		if res.Req.Code != 0 {
			return nil, fmt.Errorf("qq: playlist code %d", res.Req.Code)
		}
		d := res.Req.Data
		if begin == 0 {
			if d.DirInfo.Title == "" {
				return nil, errors.New("qq: no such playlist")
			}
			out.Name, out.CoverURL, out.Description = d.DirInfo.Title, d.DirInfo.Pic, d.DirInfo.Desc
			out.TrackCount = d.DirInfo.Total
		}
		for i := range d.Songs {
			out.Tracks = append(out.Tracks, d.Songs[i].track())
		}
		if len(d.Songs) < playlistPage {
			break
		}
	}
	return out, nil
}

// Stream returns where a song streams from, at level, or the best QQ
// serves of it below. A song it will not stream without an account comes
// back with no link, which is an error like any other.
func (Provider) Stream(t music.Track, level string) (*music.Stream, error) {
	mid := t.SongKey()
	if mid == "" {
		return nil, errors.New("qq: song without a mid")
	}
	// The file names QQ signs: its own prefix, the song's mid, and the
	// format, from the best down. What anyone may have is the lowest, so
	// it is the one asked for when the settings want a plain stream.
	files := []struct{ file, level, typ string }{
		{"F000" + mid + ".flac", "lossless", "flac"},
		{"M800" + mid + ".mp3", "exhigh", "mp3"},
		{"M500" + mid + ".mp3", "standard", "mp3"},
	}
	switch level {
	case "standard":
		files = files[2:]
	case "higher", "exhigh":
		files = files[1:]
	}
	names := make([]string, len(files))
	for i := range files {
		names[i] = files[i].file
	}
	var res struct {
		Code int `json:"code"`
		Req  struct {
			Data struct {
				Sip        []string `json:"sip"`
				MidURLInfo []struct {
					Filename string `json:"filename"`
					Purl     string `json:"purl"`
					Result   int    `json:"result"`
				} `json:"midurlinfo"`
			} `json:"data"`
		} `json:"req_0"`
	}
	err := post(&res, map[string]any{
		"comm": comm(),
		"req_0": map[string]any{
			"module": "vkey.GetVkeyServer",
			"method": "CgiGetVkey",
			"param": map[string]any{
				"guid": guid, "songmid": []string{mid}, "songtype": []int{0},
				"filename": names, "uin": "0", "loginflag": 1, "platform": "20",
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if res.Code != 0 || len(res.Req.Data.MidURLInfo) == 0 {
		return nil, errors.New("qq: no stream")
	}
	host := "https://dl.stream.qqmusic.qq.com/"
	if sip := res.Req.Data.Sip; len(sip) > 0 && sip[0] != "" {
		host = strings.Replace(sip[0], "http://", "https://", 1)
	}
	for i, info := range res.Req.Data.MidURLInfo {
		if info.Purl == "" || info.Result != 0 || i >= len(files) {
			continue
		}
		return &music.Stream{URL: host + info.Purl, Level: files[i].level, Type: files[i].typ}, nil
	}
	return nil, errors.New("qq: the song needs an account")
}

// Lyrics returns a song's words, and their translation, as QQ's player
// shows them.
func (Provider) Lyrics(t music.Track) (music.Lyrics, error) {
	addr := "https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg?songmid=" + url.QueryEscape(t.SongKey()) +
		"&format=json&nobase64=1&g_tk=5381"
	var res struct {
		Code   int    `json:"code"`
		Ret    int    `json:"retcode"`
		Lyric  string `json:"lyric"`
		Tlyric string `json:"trans"`
	}
	if err := get(addr, &res); err != nil {
		return music.Lyrics{}, err
	}
	if res.Ret != 0 || strings.TrimSpace(res.Lyric) == "" {
		return music.Lyrics{}, errors.New("qq: no lyrics")
	}
	return music.Lyrics{Lyric: res.Lyric, Tlyric: res.Tlyric}, nil
}

// jsonNum is a number QQ gives as a JSON number, or as a string.
type jsonNum float64

func (n *jsonNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil // a number QQ names otherwise is no number
	}
	*n = jsonNum(f)
	return nil
}

func (n jsonNum) Float() float64 { return float64(n) }
