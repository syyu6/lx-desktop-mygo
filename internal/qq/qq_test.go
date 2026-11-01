package qq

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"netease-music-mygo/internal/music"
)

// The answers below are cut down from what QQ sends: the fields the app
// reads, in the shapes it reads them from. A test that answers the app's
// calls itself notices the day a reader stops matching the shape it was
// written for, without the network.

// fakeQQ is a server of QQ's two endpoints: musicu answers by module and
// method, cgi by path.
type fakeQQ struct {
	musicu map[string]any
	cgi    map[string]any
	seen   []string
	// query is the parameters of the last call to each cgi path: a page's
	// numbers are what the caller asked, and the answer does not say.
	query map[string]url.Values
}

func newFakeQQ(t *testing.T) *fakeQQ {
	t.Helper()
	f := &fakeQQ{musicu: map[string]any{}, cgi: map[string]any{}, query: map[string]url.Values{}}
	srv := httptest.NewServer(f)
	oldMusicu, oldCGI := musicuURL, cgiURL
	musicuURL, cgiURL = srv.URL+"/musicu", srv.URL
	t.Cleanup(func() {
		musicuURL, cgiURL = oldMusicu, oldCGI
		srv.Close()
	})
	return f
}

func (f *fakeQQ) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/musicu" {
		var call struct {
			Req0 struct {
				Module string `json:"module"`
				Method string `json:"method"`
			} `json:"req_0"`
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		key := call.Req0.Module + "/" + call.Req0.Method
		f.seen = append(f.seen, key)
		data, ok := f.musicu[key]
		if !ok {
			http.Error(w, "unknown "+key, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(data)
		return
	}
	f.seen = append(f.seen, r.URL.Path)
	f.query[r.URL.Path] = r.URL.Query()
	data, ok := f.cgi[r.URL.Path]
	if !ok {
		http.Error(w, "unknown "+r.URL.Path, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(data)
}

// answer puts a module's method's data where rpc reads it, with the codes a
// good answer carries.
func (f *fakeQQ) answer(module, method string, data any) {
	f.musicu[module+"/"+method] = map[string]any{
		"code":  0,
		"req_0": map[string]any{"code": 0, "subcode": 0, "data": data},
	}
}

// answerCGI puts a body at a path under c.y.qq.com.
func (f *fakeQQ) answerCGI(path string, data any) { f.cgi[path] = data }

// asked reports whether a module's method was called.
func (f *fakeQQ) asked(key string) bool {
	for _, k := range f.seen {
		if k == key {
			return true
		}
	}
	return false
}

// lastQuery is the parameters of the last call to a cgi path, or nil.
func (f *fakeQQ) lastQuery(path string) url.Values { return f.query[path] }

// signedIn puts a session in place for a test, and takes it out after.
func signedIn(t *testing.T, s Session) {
	t.Helper()
	previous := currentSession()
	setSession(s)
	t.Cleanup(func() { setSession(previous) })
}

func TestSearchSongsReadsDesktopAnswer(t *testing.T) {
	f := newFakeQQ(t)
	f.answer("music.search.SearchCgiService", "DoSearchForQQMusicDesktop", map[string]any{
		"meta": map[string]any{"sum": 2033},
		"body": map[string]any{"song": map[string]any{"list": []any{
			map[string]any{
				"id": 102065756, "mid": "003OUlho2HcRHC", "name": "晴天",
				"interval": 269,
				"album":    map[string]any{"id": 8229, "mid": "000MkMni19ClKG", "name": "叶惠美"},
				"singer":   []any{map[string]any{"id": 4558, "mid": "0025NhlN2yWrP4", "name": "周杰伦"}},
			},
		}}},
	})
	tracks, total, err := Provider{}.SearchSongs("晴天", 10, 0)
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}
	if total != 2033 || len(tracks) != 1 {
		t.Fatalf("got %d songs of %d", len(tracks), total)
	}
	tr := tracks[0]
	if tr.Source != music.SourceQQ || tr.ID != 102065756 || tr.Key != "003OUlho2HcRHC" || tr.Name != "晴天" {
		t.Errorf("song %+v", tr)
	}
	if tr.Duration != 269000 {
		t.Errorf("duration %d", tr.Duration)
	}
	if len(tr.Artists) != 1 || tr.Artists[0].Name != "周杰伦" {
		t.Errorf("artists %+v", tr.Artists)
	}
	// The cover is the album's, named by its mid.
	if tr.Album.Name != "叶惠美" || !strings.Contains(tr.Album.PicURL, "000MkMni19ClKG") {
		t.Errorf("album %+v", tr.Album)
	}
	// An artist's page is asked by the mid the answer carried.
	if mid := artistMID(4558); mid != "0025NhlN2yWrP4" {
		t.Errorf("artist mid %q", mid)
	}
	// A song's mid is remembered, which is what lets a like name it back.
	if mid := songMID(102065756); mid != "003OUlho2HcRHC" {
		t.Errorf("song mid %q", mid)
	}
}

func TestSearchAlbumsReadsDesktopAnswer(t *testing.T) {
	f := newFakeQQ(t)
	f.answer("music.search.SearchCgiService", "DoSearchForQQMusicDesktop", map[string]any{
		"meta": map[string]any{"sum": 2157},
		"body": map[string]any{"album": map[string]any{"list": []any{
			map[string]any{
				"albumID": 436025, "albumMID": "002VeS6r4L5fLZ", "albumName": "安和桥北",
				"singer_list": []any{map[string]any{"id": 40449, "mid": "001Lr98T0yEWAk", "name": "宋冬野"}},
				"song_count":  13,
			},
		}}},
	})
	albums, total, err := Provider{}.SearchAlbums("安和桥北", 10, 0)
	if err != nil {
		t.Fatalf("SearchAlbums: %v", err)
	}
	if total != 2157 || len(albums) != 1 {
		t.Fatalf("got %d albums of %d", len(albums), total)
	}
	al := albums[0]
	if al.ID != 436025 || al.Name != "安和桥北" || al.Size != 13 {
		t.Errorf("album %+v", al)
	}
	if len(al.Artists) != 1 || al.Artists[0].Name != "宋冬野" {
		t.Errorf("artists %+v", al.Artists)
	}
	if mid := albumMID(436025); mid != "002VeS6r4L5fLZ" {
		t.Errorf("album mid %q", mid)
	}
}

func TestSearchArtistsReadsDesktopAnswer(t *testing.T) {
	f := newFakeQQ(t)
	f.answer("music.search.SearchCgiService", "DoSearchForQQMusicDesktop", map[string]any{
		"meta": map[string]any{"sum": 200},
		"body": map[string]any{"singer": map[string]any{"list": []any{
			map[string]any{
				"singerID": 40449, "singerMID": "001Lr98T0yEWAk", "singerName": "宋冬野",
				"songNum": 1013, "albumNum": 43,
			},
		}}},
	})
	artists, total, err := Provider{}.SearchArtists("宋冬野", 10, 0)
	if err != nil {
		t.Fatalf("SearchArtists: %v", err)
	}
	if total != 200 || len(artists) != 1 {
		t.Fatalf("got %d artists of %d", len(artists), total)
	}
	ar := artists[0]
	if ar.ID != 40449 || ar.Name != "宋冬野" || ar.MusicSize != 1013 || ar.AlbumSize != 43 {
		t.Errorf("artist %+v", ar)
	}
	if ar.Avatar == "" {
		t.Error("an artist's picture is missing")
	}
}

func TestSearchPlaylistsReadsDesktopAnswer(t *testing.T) {
	f := newFakeQQ(t)
	f.answer("music.search.SearchCgiService", "DoSearchForQQMusicDesktop", map[string]any{
		"meta": map[string]any{"sum": 302},
		"body": map[string]any{"songlist": map[string]any{"list": []any{
			map[string]any{
				"dissid": "8057512716", "dissname": "华语民谣&#58;故事和酒",
				"imgurl":       "http://qpic.y.qq.com/music_cover/abc/300?n=1",
				"introduction": "民谣有三", "listennum": 120418354, "song_count": 172,
				"creator": map[string]any{"creator_uin": "1791747120", "name": "张大佛爷"},
			},
		}}},
	})
	lists, total, err := Provider{}.SearchPlaylists("民谣", 10, 0)
	if err != nil {
		t.Fatalf("SearchPlaylists: %v", err)
	}
	if total != 302 || len(lists) != 1 {
		t.Fatalf("got %d playlists of %d", len(lists), total)
	}
	pl := lists[0]
	// The name comes back with the entities QQ writes into it undone.
	if pl.Name != "华语民谣:故事和酒" || pl.ID != 8057512716 || pl.TrackCount != 172 {
		t.Errorf("playlist %+v", pl)
	}
	if pl.PlayCount != 120418354 || pl.Creator.Nickname != "张大佛爷" {
		t.Errorf("playlist %+v", pl)
	}
}

func TestSearchRejectsLongQuery(t *testing.T) {
	newFakeQQ(t)
	if _, _, err := (Provider{}).SearchSongs(strings.Repeat("a", 61), 10, 0); err == nil {
		t.Error("a query of 61 characters was accepted")
	}
	if _, _, err := (Provider{}).SearchSongs("   ", 10, 0); err == nil {
		t.Error("an empty query was accepted")
	}
}

func TestStreamSkipsQQInternalServers(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{})
	f.answer("vkey.GetVkeyServer", "CgiGetVkey", map[string]any{
		"sip": []string{"http://ws.stream.qqmusic.qq.com/", "https://dl.stream.qqmusic.qq.com/"},
		"midurlinfo": []any{
			map[string]any{"filename": "M500003OUlho2HcRHC.mp3", "purl": "M500003OUlho2HcRHC.mp3?vkey=x", "result": 0},
		},
	})
	st, err := Provider{}.Stream(music.Track{Source: music.SourceQQ, Key: "003OUlho2HcRHC"}, "standard")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// The first server is one QQ's own servicing uses, and the next is
	// the one anyone may play from.
	if st.URL != "https://dl.stream.qqmusic.qq.com/M500003OUlho2HcRHC.mp3?vkey=x" {
		t.Errorf("url %q", st.URL)
	}
	if st.Level != "standard" || st.Type != "mp3" {
		t.Errorf("stream %+v", st)
	}
}

func TestStreamUnavailable(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{})
	f.answer("vkey.GetVkeyServer", "CgiGetVkey", map[string]any{
		"sip":        []string{"https://dl.stream.qqmusic.qq.com/"},
		"midurlinfo": []any{map[string]any{"filename": "M500003OUlho2HcRHC.mp3", "purl": "", "result": 104003}},
	})
	if _, err := (Provider{}).Stream(music.Track{Key: "003OUlho2HcRHC"}, "standard"); err != ErrUnavailable {
		t.Errorf("Stream err = %v, want ErrUnavailable", err)
	}
}

func TestStreamWithoutMid(t *testing.T) {
	newFakeQQ(t)
	if _, err := (Provider{}).Stream(music.Track{}, "standard"); err == nil {
		t.Error("a song without a mid was streamed")
	}
}

func TestLyricsDecodesBase64(t *testing.T) {
	f := newFakeQQ(t)
	const words = "[00:00.00]W1wwMF0wMDpbMDBd乙"
	f.answerCGI("/lyric/fcgi-bin/fcg_query_lyric_new.fcg", map[string]any{
		"code": 0, "lyric": base64.StdEncoding.EncodeToString([]byte(words)), "trans": "",
	})
	ly, err := Provider{}.Lyrics(music.Track{Key: "003OUlho2HcRHC"})
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if ly.Lyric != words {
		t.Errorf("lyric %q", ly.Lyric)
	}
}

func TestLyricsPlainText(t *testing.T) {
	f := newFakeQQ(t)
	f.answerCGI("/lyric/fcgi-bin/fcg_query_lyric_new.fcg", map[string]any{
		"code": 0, "lyric": "[00:00.00]晴",
	})
	ly, err := Provider{}.Lyrics(music.Track{Key: "003OUlho2HcRHC"})
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if ly.Lyric != "[00:00.00]晴" {
		t.Errorf("lyric %q", ly.Lyric)
	}
}

func TestLyricsWithoutSong(t *testing.T) {
	f := newFakeQQ(t)
	f.answerCGI("/lyric/fcgi-bin/fcg_query_lyric_new.fcg", map[string]any{"code": 0, "lyric": ""})
	if _, err := (Provider{}).Lyrics(music.Track{Key: "003OUlho2HcRHC"}); err == nil {
		t.Error("a song without words answered with no error")
	}
}

func TestPlaylistReadsSongs(t *testing.T) {
	f := newFakeQQ(t)
	f.answer("music.srfDissInfo.DissInfo", "CgiGetDiss", map[string]any{
		"dirinfo": map[string]any{"title": "电子", "songnum": 2},
		"songlist": []any{
			map[string]any{"id": 1, "mid": "aaa", "title": "一", "singer": []any{map[string]any{"id": 9, "mid": "sm", "name": "甲"}}},
			map[string]any{"id": 2, "mid": "bbb", "title": "二"},
		},
	})
	pl, err := Provider{}.Playlist("8057512716")
	if err != nil {
		t.Fatalf("Playlist: %v", err)
	}
	if pl.Name != "电子" || len(pl.Tracks) != 2 || pl.Tracks[0].Key != "aaa" {
		t.Errorf("playlist %+v", pl)
	}
}

func TestChartPlaylistReadsSongs(t *testing.T) {
	f := newFakeQQ(t)
	f.answer("musicToplist.ToplistInfoServer", "GetDetail", map[string]any{
		"data":         map[string]any{"title": "飙升榜", "recType": 10005},
		"songInfoList": []any{map[string]any{"id": 3, "mid": "ccc", "title": "三"}},
	})
	pl, err := Provider{}.Playlist(keyOf(chartBase + 4))
	if err != nil {
		t.Fatalf("Playlist: %v", err)
	}
	if pl.Name != "飙升榜" || len(pl.Tracks) != 1 || pl.Tracks[0].Key != "ccc" {
		t.Errorf("playlist %+v", pl)
	}
}

func TestChartsKeepsSongCharts(t *testing.T) {
	f := newFakeQQ(t)
	f.answer("musicToplist.ToplistInfoServer", "GetAll", map[string]any{
		"group": []any{map[string]any{"toplist": []any{
			map[string]any{"topId": 4, "recType": 10005, "title": "飙升榜", "totalNum": 100},
			map[string]any{"topId": 5, "recType": 10002, "title": "专辑榜"},
		}}},
	})
	charts, err := Provider{}.Charts()
	if err != nil {
		t.Fatalf("Charts: %v", err)
	}
	if len(charts) != 1 || charts[0].Name != "飙升榜" {
		t.Fatalf("charts %+v", charts)
	}
	// A chart's id says which kind it is, so it opens the chart's page.
	if charts[0].ID != chartBase+4 {
		t.Errorf("id %d", charts[0].ID)
	}
}

func TestSquareTags(t *testing.T) {
	f := newFakeQQ(t)
	f.answerCGI("/splcloud/fcgi-bin/fcg_get_diss_tag_conf.fcg", map[string]any{
		"code": 0,
		"data": map[string]any{"categories": []any{
			map[string]any{"categoryGroupName": "热门", "items": []any{
				map[string]any{"categoryId": 10000000, "categoryName": "全部", "usable": 1},
			}},
			map[string]any{"categoryGroupName": "语种", "items": []any{
				map[string]any{"categoryId": 6, "categoryName": "国语", "usable": 1},
				map[string]any{"categoryId": 7, "categoryName": "标题", "usable": 0},
			}},
		}},
	})
	tags, err := Provider{}.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("tags %+v", tags)
	}
	if tags[0].Name != "全部" || tags[1].Name != "语种 · 国语" || tags[1].ID != 6 {
		t.Errorf("tags %+v", tags)
	}
}

func TestSquarePlaylists(t *testing.T) {
	f := newFakeQQ(t)
	f.answerCGI("/splcloud/fcgi-bin/fcg_get_diss_by_tag.fcg", map[string]any{
		"code": 0,
		"data": map[string]any{"list": []any{
			map[string]any{"dissid": "7707261125", "dissname": "民谣", "song_count": 20, "listennum": 8556429},
			map[string]any{"dissid": "0", "dissname": "no id"},
		}},
	})
	lists, err := Provider{}.SquarePlaylists("6", 10, 0)
	if err != nil {
		t.Fatalf("SquarePlaylists: %v", err)
	}
	if len(lists) != 1 || lists[0].ID != 7707261125 || lists[0].Name != "民谣" {
		t.Fatalf("playlists %+v", lists)
	}
}

// The page asks for a category by the name a chip shows, but QQ numbers
// its categories: asked for a name, the square answers everyone's
// playlists, and every chip shows the same page. The number is what the
// name stands for after Tags listed it.
func TestSquarePlaylistsAsksByCategoryNumber(t *testing.T) {
	f := newFakeQQ(t)
	f.answerCGI("/splcloud/fcgi-bin/fcg_get_diss_tag_conf.fcg", map[string]any{
		"code": 0,
		"data": map[string]any{"categories": []any{
			map[string]any{"categoryGroupName": "流派", "items": []any{
				map[string]any{"categoryId": 28, "categoryName": "民谣", "usable": 1},
			}},
		}},
	})
	f.answerCGI("/splcloud/fcgi-bin/fcg_get_diss_by_tag.fcg", map[string]any{"code": 0})
	if _, err := (Provider{}).Tags(); err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if _, err := (Provider{}).SquarePlaylists("流派 · 民谣", 10, 0); err != nil {
		t.Fatalf("SquarePlaylists: %v", err)
	}
	if got := f.lastQuery("/splcloud/fcgi-bin/fcg_get_diss_by_tag.fcg").Get("categoryId"); got != "28" {
		t.Errorf("asked the square for category %q, want the number of 流派 · 民谣", got)
	}
	// A name no Tags listed is nothing rather than everyone's playlists.
	lists, err := (Provider{}).SquarePlaylists("不存在的分类", 10, 0)
	if err != nil {
		t.Fatalf("SquarePlaylists: %v", err)
	}
	if len(lists) != 0 {
		t.Errorf("answered %d playlists for a category no source listed", len(lists))
	}
}

// A page of the square ends where the next one starts: QQ counts the last
// index in, so asking for offset+limit repeats the page's last playlist at
// the head of the next one, and the page lists two cards with one key.
func TestSquarePlaylistsPagesDoNotOverlap(t *testing.T) {
	f := newFakeQQ(t)
	f.answerCGI("/splcloud/fcgi-bin/fcg_get_diss_by_tag.fcg", map[string]any{"code": 0})
	const limit = 36
	if _, err := (Provider{}).SquarePlaylists("", limit, limit); err != nil {
		t.Fatalf("SquarePlaylists: %v", err)
	}
	q := f.lastQuery("/splcloud/fcgi-bin/fcg_get_diss_by_tag.fcg")
	sin, ein := q.Get("sin"), q.Get("ein")
	if sin != "36" || ein != "71" {
		t.Errorf("asked for %s..%s, want 36..71: the next page starts at 36", sin, ein)
	}
}

func TestPersonalPagesNeedAnAccount(t *testing.T) {
	newFakeQQ(t)
	signedIn(t, Session{})
	if _, err := (Provider{}).Daily(); err != errNoAccount {
		t.Errorf("Daily err = %v", err)
	}
	if _, err := (Provider{}).LikedIDs(42); err != errNoAccount {
		t.Errorf("LikedIDs err = %v", err)
	}
	if _, _, err := (Provider{}).Library(42); err != errNoAccount {
		t.Errorf("Library err = %v", err)
	}
	if err := (Provider{}).Like(1, true); err != errNoAccount {
		t.Errorf("Like err = %v", err)
	}
}

func TestDailyReadsSongs(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{MusicID: "42", MusicKey: "key"})
	f.answer("music.srfDissInfo.aiDissInfo", "uniform_get_Dissinfo", map[string]any{
		"songlist": []any{map[string]any{"id": 7, "mid": "ddd", "title": "今日"}},
	})
	tracks, err := Provider{}.Daily()
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	if len(tracks) != 1 || tracks[0].Key != "ddd" {
		t.Errorf("tracks %+v", tracks)
	}
}

func TestLikeAlreadyThereWritesNothing(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{MusicID: "42", MusicKey: "key"})
	songMids.put(1, "aaa")
	f.answer("music.musicasset.SongFavRead", "IsSongFanByMid", map[string]any{
		"m_fan": map[string]any{"aaa": true},
	})
	if err := (Provider{}).Like(1, true); err != nil {
		t.Fatalf("Like: %v", err)
	}
	if f.asked("music.musicasset.PlaylistDetailWrite/AddSonglist") {
		t.Error("a like that was already there was written")
	}
}

func TestLikeWritesAndReadsBack(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{MusicID: "42", MusicKey: "key"})
	songMids.put(1, "aaa")
	// The list starts without the song, and has it once the write is made.
	liked := false
	f.musicu["music.musicasset.SongFavRead/IsSongFanByMid"] = funcAnswer(func() any {
		return map[string]any{"m_fan": map[string]any{"aaa": liked}}
	})
	f.musicu["music.musicasset.PlaylistDetailWrite/AddSonglist"] = funcAnswer(func() any {
		liked = true
		return map[string]any{"retCode": 0, "result": map[string]any{}}
	})
	if err := (Provider{}).Like(1, true); err != nil {
		t.Fatalf("Like: %v", err)
	}
	if !f.asked("music.musicasset.PlaylistDetailWrite/AddSonglist") {
		t.Error("the like was not written")
	}
}

func TestLikeThatQQDoesNotConfirmIsAnError(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{MusicID: "42", MusicKey: "key"})
	songMids.put(1, "aaa")
	// The read never changes: the write went nowhere, and the listener is
	// told so rather than shown a like that did not happen.
	f.answer("music.musicasset.SongFavRead", "IsSongFanByMid", map[string]any{
		"m_fan": map[string]any{"aaa": false},
	})
	f.answer("music.musicasset.PlaylistDetailWrite", "AddSonglist", map[string]any{
		"retCode": 0, "result": map[string]any{},
	})
	if err := (Provider{}).Like(1, true); err == nil {
		t.Error("an unconfirmed like was reported as done")
	}
}

// The 我喜欢 directory is the liked songs' list, and the page knows it by
// the type: its hearts write to it, and its albums show beside its songs.
func TestLibraryKnowsTheLikedList(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{MusicID: "42", MusicKey: "key"})
	f.answer("music.playlist.PlaylistBaseRead", "GetPlaylistByUin", map[string]any{
		"v_playlist": []any{
			map[string]any{"tid": 7, "title": "我的收藏"},
			map[string]any{"dirId": 201, "tid": 8, "title": "我喜欢"},
		},
	})
	made, _, err := (Provider{}).Library(42)
	if err != nil {
		t.Fatalf("Library: %v", err)
	}
	if len(made) != 2 || made[0].ID != 8 || made[0].SpecialType != 5 || made[1].SpecialType != 0 {
		t.Fatalf("made %+v", made)
	}
	if made[0].Name != "我喜欢" || made[0].TrackCount != 0 || made[0].PicURL != "" {
		t.Errorf("the liked list %+v", made[0])
	}
}

// The albums collected are read a page at a time from an inclusive range,
// and a list that stopped short of the total is reported rather than shown
// as a collection of fewer albums than it is.
func TestCollectedAlbumsPages(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{MusicID: "42", MusicKey: "key"})
	f.answerCGI(pathCollected, map[string]any{"code": 0, "data": map[string]any{
		"totalalbum": 3,
		"albumlist": []any{
			map[string]any{"albumid": 100, "albummid": "aaa", "albumname": "迷跡波",
				"singerid": 9, "singermid": "sm", "singername": "MyGO!!!!!"},
			map[string]any{"albumid": 101, "albummid": "bbb", "album_name": "春日影",
				"singers": []any{map[string]any{"id": 9, "mid": "sm", "name": "MyGO!!!!!"}}},
		},
	}})
	albums, more, err := (Provider{}).CollectedAlbums(2, 0)
	if err != nil {
		t.Fatalf("CollectedAlbums: %v", err)
	}
	if len(albums) != 2 || !more {
		t.Fatalf("albums %+v, more %v", albums, more)
	}
	if albums[0].ID != 100 || albums[0].Name != "迷跡波" || albums[0].PicURL == "" {
		t.Errorf("album 0 %+v", albums[0])
	}
	if names := albums[0].ArtistNames(); names != "MyGO!!!!!" {
		t.Errorf("album 0 artists %q", names)
	}
	if albums[1].Name != "春日影" || albums[1].ArtistNames() != "MyGO!!!!!" {
		t.Errorf("album 1 %+v", albums[1])
	}
	// The page is read by an inclusive range: two from the start asks for
	// the first and second, not for a count.
	if got := f.seen[len(f.seen)-1]; got != pathCollected {
		t.Errorf("asked %q", got)
	}
}

// A collected list that stops short of its total is an error, which the
// page says rather than show a collection missing albums.
func TestCollectedAlbumsShortAnswer(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{MusicID: "42", MusicKey: "key"})
	f.answerCGI(pathCollected, map[string]any{"code": 0, "data": map[string]any{"totalalbum": 5}})
	if _, _, err := (Provider{}).CollectedAlbums(2, 0); err == nil {
		t.Fatal("a list short of its total was accepted")
	}
}

// Collecting an album is asked by its mid, and a write QQ does not confirm
// is an error rather than a collection that may not have happened.
func TestCollectAlbumWritesAndReadsBack(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{MusicID: "42", MusicKey: "key"})
	albumMids.put(100, "aaa")
	collected := false
	f.musicu["music.musicasset.AlbumFavRead/IsAlbumFan"] = funcAnswer(func() any {
		return map[string]any{"m_fan": map[string]any{"aaa": collected}}
	})
	f.musicu["music.musicasset.AlbumFavWrite/FavAlbum"] = funcAnswer(func() any {
		collected = true
		return map[string]any{"result": 0, "v_failedAlbumId": []any{}, "v_failedAlbumMid": []any{}}
	})
	if err := (Provider{}).CollectAlbum(100, true); err != nil {
		t.Fatalf("CollectAlbum: %v", err)
	}
	if !f.asked("music.musicasset.AlbumFavWrite/FavAlbum") {
		t.Error("the collection was not written")
	}
	if ok, err := (Provider{}).AlbumCollected(100); err != nil || !ok {
		t.Errorf("AlbumCollected = %v, %v", ok, err)
	}
}

func TestMembership(t *testing.T) {
	f := newFakeQQ(t)
	signedIn(t, Session{MusicID: "42", MusicKey: "key"})
	f.answer("userInfo.VipQueryServer", "SRFVipQuery_V2", map[string]any{
		"infoMap": map[string]any{"42": map[string]any{"iVipFlag": 1, "HugeVip": 0}},
	})
	if m, err := (Provider{}).Membership(); err != nil || m != music.MembershipVIP {
		t.Errorf("Membership = %q, %v", m, err)
	}
	f.answer("userInfo.VipQueryServer", "SRFVipQuery_V2", map[string]any{
		"infoMap": map[string]any{"42": map[string]any{"iVipFlag": 1, "HugeVip": 1}},
	})
	if m, err := (Provider{}).Membership(); err != nil || m != music.MembershipSuper {
		t.Errorf("Membership = %q, %v", m, err)
	}
	f.answer("userInfo.VipQueryServer", "SRFVipQuery_V2", map[string]any{
		"infoMap": map[string]any{"42": map[string]any{"iVipFlag": 0, "HugeVip": 0}},
	})
	if m, err := (Provider{}).Membership(); err != nil || m != music.MembershipFree {
		t.Errorf("Membership = %q, %v", m, err)
	}
}

func TestAccountWithoutSession(t *testing.T) {
	signedIn(t, Session{})
	p, err := Account()
	if err != nil || p != nil {
		t.Errorf("Account = %+v, %v", p, err)
	}
}

func TestCommentsPageForward(t *testing.T) {
	f := newFakeQQ(t)
	commentCursors.Lock()
	delete(commentCursors.pages, "42")
	commentCursors.Unlock()
	f.answer("music.globalComment.CommentRead", "GetNewCommentList", map[string]any{
		"CommentList": map[string]any{
			"Comments": []any{map[string]any{
				"CmId": "11", "Content": "hi", "Nick": "a", "PubTime": 100, "PraiseNum": 3, "SeqNo": "s1",
			}},
			"Total":   2,
			"HasMore": 1,
		},
		"TotalVer": "7",
	})
	signedIn(t, Session{})

	first, err := (Provider{}).ThreadComments("42", 0, 20)
	if err != nil {
		t.Fatalf("ThreadComments: %v", err)
	}
	if len(first.Latest) != 1 || first.Latest[0].Content != "hi" || first.Latest[0].LikedCount != 3 {
		t.Fatalf("latest %+v", first.Latest)
	}
	if first.Total != 2 || !first.More {
		t.Errorf("page %+v", first)
	}
	// The second page is asked with the cursor the first left.
	if _, err := (Provider{}).ThreadComments("42", 1, 20); err != nil {
		t.Fatalf("ThreadComments page 2: %v", err)
	}
}

func TestCommentsAfterARestartSaySo(t *testing.T) {
	newFakeQQ(t)
	// A cursor that has gone is what a restart leaves: the app says to
	// open the comments again rather than show the first page as the
	// second.
	if _, err := (Provider{}).ThreadComments("nope", 20, 20); err == nil {
		t.Error("a page without its cursor was answered")
	}
}

func TestThreadNeedsASongOfOurs(t *testing.T) {
	if (Provider{}).Thread(music.Track{Source: music.SourceNetease, ID: 1}) != "" {
		t.Error("a NetEase song named a QQ thread")
	}
	if got := (Provider{}).Thread(music.Track{Source: music.SourceQQ, ID: 42}); got != "42" {
		t.Errorf("thread %q", got)
	}
}

func TestUserIsTheSignedInOne(t *testing.T) {
	signedIn(t, Session{MusicID: "42", MusicKey: "key", Nickname: "甲"})
	u, err := Provider{}.User(42)
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	if u.Nickname != "甲" {
		t.Errorf("user %+v", u)
	}
	if _, err := (Provider{}).User(7); err == nil {
		t.Error("another listener's page was answered")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	old := sessionDir
	sessionDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { sessionDir = old })

	signedIn(t, Session{MusicID: "42", MusicKey: "key", Nickname: "甲"})
	if err := SaveSession(); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	setSession(Session{})
	if !RestoreSession() {
		t.Fatal("RestoreSession found nothing")
	}
	if s := currentSession(); s.MusicID != "42" || s.Nickname != "甲" {
		t.Errorf("session %+v", s)
	}
	if !SignedIn() {
		t.Error("SignedIn with a session")
	}
	ClearSession()
	if SignedIn() {
		t.Error("SignedIn after ClearSession")
	}
	if _, err := os.Stat(SessionPath()); !os.IsNotExist(err) {
		t.Error("the session file is still there after a sign-out")
	}
}

func TestSessionFileIsPrivate(t *testing.T) {
	dir := t.TempDir()
	old := sessionDir
	sessionDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { sessionDir = old })

	signedIn(t, Session{MusicID: "42", MusicKey: "secret"})
	if err := SaveSession(); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	st, err := os.Stat(SessionPath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("the session file is %o, not 600", perm)
	}
}

func TestRestoreWithoutAFile(t *testing.T) {
	dir := t.TempDir()
	old := sessionDir
	sessionDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { sessionDir = old })
	setSession(Session{})
	if RestoreSession() {
		t.Error("RestoreSession found a session in an empty directory")
	}
}

// funcAnswer is an answer that is worked out when it is asked for, for a
// call whose answer changes as the app writes.
func funcAnswer(f func() any) any { return answerFunc(f) }

type answerFunc func() any

func (a answerFunc) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"code":  0,
		"req_0": map[string]any{"code": 0, "subcode": 0, "data": any(a())},
	})
}
