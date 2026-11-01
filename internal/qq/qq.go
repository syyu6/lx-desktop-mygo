// Package qq is QQ Music as a source: the catalogue the app searches, the
// playlists and charts it opens, the songs it streams, and their words.
//
// The endpoints are the ones its own clients call. What needs no account —
// a search, a playlist, a chart, an artist, an album, a lyric, and a
// stream of what QQ serves anyone — works signed out. What is personal —
// the songs QQ picks for the listener, their own lists, what they like,
// the station — needs one, and says so rather than answer with an empty
// page (library.go).
package qq

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/network"
)

// UserAgent is what QQ Music's web player sends, which its servers expect.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

var httpClient = network.Client(20 * time.Second)

// Provider is QQ Music as package source asks it.
type Provider struct{}

func (Provider) ID() string   { return music.SourceQQ }
func (Provider) Name() string { return "QQ 音乐" }

// comm is what every musicu call carries: the client the app says it is.
func comm() map[string]any {
	return map[string]any{"uin": 0, "format": "json", "ct": 24, "cv": 0}
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

// playlistPage is how many songs a playlist's page loads: QQ's own limit
// for one answer.
const playlistPage = 300

// Playlist returns a playlist with its songs: a chart, an album-less list
// of songs, or the listener's own.
func (Provider) Playlist(id string) (*music.Playlist, error) {
	num, err := strconv.ParseInt(id, 10, 64)
	if err != nil || num == 0 {
		return nil, errors.New("qq: no such playlist")
	}
	if num >= chartBase {
		return chartPlaylist(num)
	}
	return listPlaylist(num)
}

// chartPlaylist reads a chart's songs.
func chartPlaylist(id int64) (*music.Playlist, error) {
	var data object
	err := rpcWith("musicToplist.ToplistInfoServer", "GetDetail", map[string]any{
		"topid": id - chartBase, "offset": 0, "num": 100, "period": "",
	}, &data, map[string]any{"ct": 24, "cv": 0}, nil)
	if err != nil {
		return nil, err
	}
	info := obj(data["data"])
	if number(info, "recType") != songChart {
		return nil, errUnsupported
	}
	tracks := convertTracks(objects(data, "songInfoList"))
	if len(tracks) == 0 {
		return nil, fmt.Errorf("qq: chart %d has no songs", id-chartBase)
	}
	return &music.Playlist{
		Source:      music.SourceQQ,
		ID:          id,
		Key:         strconv.FormatInt(id, 10),
		Name:        text(info, "title"),
		CoverURL:    text(info, "headPicUrl", "frontPicUrl"),
		Description: text(info, "intro"),
		TrackCount:  int(number(info, "totalNum")),
		UpdateFreq:  text(info, "updateTime"),
		Tracks:      tracks,
	}, nil
}

// listPlaylist reads a playlist's songs, a page at a time until it has
// them all.
func listPlaylist(num int64) (*music.Playlist, error) {
	// A playlist a session may open, as one only its owner sees, is asked
	// through the account's own endpoint: QQ's public one answers that it
	// does not exist.
	if SignedIn() {
		if pl, err := ownedPlaylist(num); err == nil && len(pl.Tracks) > 0 {
			return pl, nil
		}
	}
	out := &music.Playlist{Source: music.SourceQQ, ID: num, Key: strconv.FormatInt(num, 10)}
	for begin := 0; ; begin += playlistPage {
		var res object
		err := rpc("music.srfDissInfo.DissInfo", "CgiGetDiss", map[string]any{
			// The id is a number here: sent as a string, QQ answers that
			// no such playlist exists.
			"disstid": num, "dirid": 0, "tag": 1,
			"song_begin": begin, "song_num": playlistPage, "userinfo": 1,
		}, &res)
		if err != nil {
			return nil, err
		}
		dir := obj(res["dirinfo"])
		songs := objects(res, "songlist")
		if begin == 0 {
			if dir == nil || text(dir, "title") == "" {
				return nil, errors.New("qq: no such playlist")
			}
			out.Name = unescape(text(dir, "title"))
			out.CoverURL = text(dir, "picurl")
			out.Description = unescape(text(dir, "desc"))
			out.TrackCount = int(number(dir, "songnum"))
		}
		out.Tracks = append(out.Tracks, convertTracks(songs)...)
		if len(songs) < playlistPage {
			break
		}
	}
	return out, nil
}

// ownedPlaylist reads a playlist through the account's endpoint, which is
// what its owner's private ones answer to.
func ownedPlaylist(num int64) (*music.Playlist, error) {
	var data object
	err := rpc("music.srfDissInfo.aiDissInfo", "uniform_get_Dissinfo", map[string]any{
		"disstid": num, "userinfo": 1, "tag": 1, "orderlist": 1,
		"song_begin": 0, "song_num": 1000, "onlysonglist": 0, "enc_host_uin": "",
	}, &data)
	if err != nil {
		return nil, err
	}
	dir := obj(data["dirinfo"])
	if dir == nil || text(dir, "title") == "" {
		return nil, errors.New("qq: no such playlist")
	}
	pl := &music.Playlist{
		Source:      music.SourceQQ,
		ID:          num,
		Key:         strconv.FormatInt(num, 10),
		Name:        unescape(text(dir, "title")),
		CoverURL:    text(dir, "picurl"),
		Description: unescape(text(dir, "desc")),
		TrackCount:  int(number(dir, "songnum")),
		Tracks:      convertTracks(objects(data, "songlist", "song_info")),
	}
	// The 我喜欢 directory is the liked songs' list: the page knows it by the
	// type, and offers the albums collected beside its songs.
	if number(dir, "dirId", "dirid") == likedDir {
		pl.SpecialType = 5
	}
	return pl, nil
}

// Stream returns where a song streams from, at level, or the best QQ
// serves of it below.
//
// Signed out, QQ serves the lowest quality of what anyone may hear, and a
// song it will not serve comes back with no link — an error like any
// other. Signed in, the qualities above it open, as far as the account's
// membership reaches: a song still without a link plays from the next
// source.
func (Provider) Stream(t music.Track, level string) (*music.Stream, error) {
	mid := t.SongKey()
	if mid == "" {
		return nil, errors.New("qq: song without a mid")
	}
	// The file names QQ signs: its own prefix, the song's mid and the
	// format, from the best down. What anyone may have is the lowest, so
	// it is the one asked for when the settings want a plain stream.
	files := []struct{ file, level, typ string }{
		{"F000" + mid + ".flac", "lossless", "flac"},
		{"M800" + mid + ".mp3", "exhigh", "mp3"},
		{"M500" + mid + ".mp3", "standard", "mp3"},
	}
	if !SignedIn() {
		// Without an account QQ signs only the lowest.
		files = files[2:]
	} else {
		switch level {
		case "standard":
			files = files[2:]
		case "higher", "exhigh":
			files = files[1:]
		}
	}
	names := make([]string, len(files))
	for i := range files {
		names[i] = files[i].file
	}
	s := currentSession()
	param := map[string]any{
		"guid": guid, "songmid": []string{mid}, "songtype": []int{0},
		"filename": names, "uin": "0", "loginflag": 1, "platform": "20",
	}
	module, method := "vkey.GetVkeyServer", "CgiGetVkey"
	if s.MusicKey != "" {
		module, method = "music.vkey.GetVkey", "UrlGetVkey"
		param["uin"] = s.MusicID
		param["ctx"] = 0
	}
	var res struct {
		SIP        []string `json:"sip"`
		MidURLInfo []struct {
			Filename string `json:"filename"`
			Purl     string `json:"purl"`
			Result   int    `json:"result"`
		} `json:"midurlinfo"`
	}
	if err := rpc(module, method, param, &res); err != nil {
		return nil, err
	}
	host := streamHost(res.SIP)
	for i, info := range res.MidURLInfo {
		if info.Purl == "" || info.Result != 0 || i >= len(files) {
			continue
		}
		return &music.Stream{URL: host + info.Purl, Level: files[i].level, Type: files[i].typ}, nil
	}
	return nil, ErrUnavailable
}

// streamHost is the first of QQ's stream servers that is an address the
// app may play from: some of them are named for its own servicing, and
// answer nothing to anyone else.
func streamHost(sips []string) string {
	const fallback = "https://dl.stream.qqmusic.qq.com/"
	for _, sip := range sips {
		if sip == "" || strings.HasPrefix(sip, "http://ws") || strings.HasPrefix(sip, "https://ws") {
			continue
		}
		return strings.Replace(strings.Replace(sip, "http://", "https://", 1), "https://", "https://", 1)
	}
	return fallback
}

// guid is the session id a stream's link is signed for. QQ does not check
// that it is a real session, only that the same one asks and plays.
const guid = "10000"

// Lyrics returns a song's words, and their translation, as QQ's player
// shows them.
//
// QQ has no word-by-word lyrics here, so the third of what a lyric is —
// what the stage sings along to — is empty: the lyrics page sets QQ's
// songs a line at a time.
func (Provider) Lyrics(t music.Track) (music.Lyrics, error) {
	var data struct {
		Lyric string `json:"lyric"`
		Trans string `json:"trans"`
	}
	err := cgi("/lyric/fcgi-bin/fcg_query_lyric_new.fcg", url.Values{
		"songmid": {t.SongKey()}, "pcachetime": {strconv.FormatInt(time.Now().UnixNano()/1e6, 10)},
	}, &data)
	if err != nil {
		return music.Lyrics{}, err
	}
	lrc := decodeLyric(data.Lyric)
	if strings.TrimSpace(lrc) == "" {
		return music.Lyrics{}, errors.New("qq: no lyrics")
	}
	return music.Lyrics{Lyric: lrc, Tlyric: decodeLyric(data.Trans)}, nil
}

// decodeLyric reads the base64 a lyric endpoint answers with, or the plain
// text an endpoint that was asked for none answers with.
func decodeLyric(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return string(b)
	}
	return s
}

// HasComments reports that QQ has remarks on its songs.
func (Provider) HasComments() bool { return true }
