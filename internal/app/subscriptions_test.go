package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// fakePlays serves playlists of the fake source, and says whether it was
// asked.
type fakePlays struct {
	src   string // the source id it answers to, "fake" when unset
	pl    *music.Playlist
	err   error
	asked int
}

func (f *fakePlays) ID() string {
	if f.src == "" {
		return "fake"
	}
	return f.src
}
func (f *fakePlays) Name() string { return "假源" }
func (f *fakePlays) SearchSongs(string, int, int) ([]music.Track, int, error) {
	return nil, 0, errors.New("none")
}
func (f *fakePlays) Playlist(string) (*music.Playlist, error) {
	f.asked++
	return f.pl, f.err
}
func (f *fakePlays) Stream(music.Track, string) (*music.Stream, error) {
	return nil, errors.New("none")
}
func (f *fakePlays) Lyrics(music.Track) (music.Lyrics, error) {
	return music.Lyrics{}, errors.New("none")
}

// subApp is an app whose followed playlists are kept in a fresh file, and
// whose source answers what the test says.
func subApp(t *testing.T, f *fakePlays) *app {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.reg = source.NewRegistry(f)
	a.openSubscriptions(filepath.Join(t.TempDir(), "subs.json"))
	return a
}

func subPlaylistOf(name string, tracks ...music.Track) *music.Playlist {
	pl := &music.Playlist{Source: "fake", ID: 7, Name: name, Tracks: tracks}
	pl.Creator.Nickname = "某人"
	return pl
}

// Following a playlist keeps its songs, and its page opens them without
// asking the source again.
func TestSubscribeKeepsTheSongs(t *testing.T) {
	f := &fakePlays{}
	a := subApp(t, f)
	a.subscribe(subPlaylistOf("通勤", music.Track{Source: "fake", ID: 1, Name: "甲"},
		music.Track{Source: "fake", ID: 2, Name: "乙"}))
	if f.asked != 0 {
		t.Errorf("asking the source %d times for what it just gave", f.asked)
	}
	sub := a.subs.Get("fake/7")
	if sub == nil || len(sub.Tracks) != 2 || sub.Author != "某人" {
		t.Fatalf("sub %+v", sub)
	}
	if lists := a.subscribedLists(); len(lists) != 1 || lists[0].Name != "通勤" {
		t.Fatalf("sidebar lists %+v", lists)
	}
	// Opened, the page has the songs already: no loading, no asking.
	a.route = playlistRoute("fake", 7)
	a.loadPlaylist("fake", 7, false)
	if f.asked != 0 {
		t.Errorf("the page asked the source %d times", f.asked)
	}
	if pl := a.playlists[playlistKey("fake", 7)]; pl == nil || len(pl.Tracks) != 2 {
		t.Fatalf("page playlist %+v", pl)
	}
}

// Fetching a subscription again takes the songs the service has now, and
// keeps the copy where the page reads it.
func TestSyncSubscription(t *testing.T) {
	pl := subPlaylistOf("通勤", music.Track{Source: "fake", ID: 1})
	f := &fakePlays{pl: pl}
	a := subApp(t, f)
	a.subscribe(pl)
	pl = subPlaylistOf("通勤新", music.Track{Source: "fake", ID: 1}, music.Track{Source: "fake", ID: 2})
	f.pl = pl
	sub := a.subs.Get("fake/7")
	// As the page would fetch it, and hand the answer to the app: post
	// drops what it is given without a window.
	got, err := f.Playlist("7")
	if err != nil {
		t.Fatal(err)
	}
	a.subscriptionArrived(sub, 7, got, nil)
	if got := a.subs.Get("fake/7"); got.Name != "通勤新" || len(got.Tracks) != 2 {
		t.Errorf("kept %+v", got)
	}
	if got := a.playlists[playlistKey("fake", 7)]; got == nil || len(got.Tracks) != 2 {
		t.Errorf("page %+v", got)
	}
}

// A sync that fails keeps the songs it had, and keeps trying next time.
func TestSyncFailureKeepsTheCopy(t *testing.T) {
	f := &fakePlays{}
	a := subApp(t, f)
	a.subscribe(subPlaylistOf("通勤", music.Track{Source: "fake", ID: 1}))
	sub := a.subs.Get("fake/7")
	a.syncSubscription(sub)
	if !a.subSyncing["fake/7"] {
		t.Error("not marked as syncing")
	}
	a.subscriptionArrived(sub, 7, nil, errors.New("offline"))
	if got := a.subs.Get("fake/7"); got == nil || len(got.Tracks) != 1 {
		t.Fatalf("sub %+v", got)
	}
	if a.subSyncing["fake/7"] {
		t.Error("it stays marked as syncing")
	}
}

// Only the subscriptions the listener keeps up to date, of the sources they
// still ask, are fetched as the app opens.
func TestAutoSyncFollowsTheSettings(t *testing.T) {
	f := &fakePlays{pl: subPlaylistOf("通勤")}
	a := subApp(t, f)
	a.subscribe(subPlaylistOf("通勤"))
	old := subPlaylistOf("旧的")
	old.ID = 8
	if _, err := a.subs.Add(old); err != nil {
		t.Fatal(err)
	}
	a.setSubAuto(a.subs.Get("fake/8"), false)
	if ask := a.autoSyncAsk(); len(ask) != 1 || ask[0].Key() != "fake/7" {
		t.Errorf("it would fetch %+v, want only the one kept up to date", ask)
	}
	a.reg.SetEnabled("fake", false)
	if ask := a.autoSyncAsk(); len(ask) != 0 {
		t.Errorf("it would fetch %+v with the source off", ask)
	}
}

// A subscription comes back when the app opens again, its songs with it,
// and stop following it forgets them.
func TestSubscriptionsSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subs.json")
	f := &fakePlays{}
	a := subApp(t, f)
	a.subs = nil
	a.openSubscriptions(path)
	a.subscribe(subPlaylistOf("通勤", music.Track{Source: "fake", ID: 1}))
	a.playlists = map[string]*netease.Playlist{}

	again := subApp(t, f)
	again.subs = nil
	again.openSubscriptions(path)
	if pl := again.playlists[playlistKey("fake", 7)]; pl == nil || len(pl.Tracks) != 1 {
		t.Fatalf("opened again with %+v", pl)
	}
	sub := again.subs.Get("fake/7")
	if sub == nil || sub.UpdatedAt.IsZero() || time.Since(sub.UpdatedAt) > time.Minute {
		t.Fatalf("sub %+v", sub)
	}
	again.route = playlistRoute("fake", 7)
	again.unsubscribe(sub)
	if again.subs.Get("fake/7") != nil {
		t.Error("unsubscribed stayed")
	}
	if again.playlists[playlistKey("fake", 7)] != nil {
		t.Error("its songs stayed")
	}
}

// A followed playlist is told apart from one that is not, and NetEase's
// playlists are never followed: they are saved in the app.
func TestSubscriptionOf(t *testing.T) {
	a := subApp(t, &fakePlays{})
	if a.followed(&netease.Playlist{ID: 1}) != nil {
		t.Error("a NetEase playlist was taken for a subscription")
	}
	if a.followed(subPlaylistOf("通勤")) != nil {
		t.Error("a playlist not followed was taken for one")
	}
	a.subscribe(subPlaylistOf("通勤"))
	if a.followed(subPlaylistOf("通勤")) == nil {
		t.Error("the followed playlist is not one")
	}
	if got := a.followed(&netease.Playlist{Source: "fake", ID: 8}); got != nil {
		t.Errorf("another playlist is a subscription: %+v", got)
	}
}

// A playlist is followed from its link alone, its service named by the
// link, and one of NetEase's is not: those are the account's.
func TestFollowALink(t *testing.T) {
	// Kuwo, which reads its playlists from its links.
	f := &fakePlays{src: music.SourceKuwo, pl: &music.Playlist{Source: music.SourceKuwo, ID: 12, Name: "通勤"}}
	a := subApp(t, f)
	a.openSubscribeDialog()
	a.subscribeDlg.text = "http://www.kuwo.cn/playlist_detail/12"
	a.submitSubscribe()
	if !a.subscribeDlg.busy {
		t.Fatal("it did not ask for the playlist")
	}
	// As post would run the answer, without a window.
	a.subscribeArrived(&a.subscribeDlg, f.pl, nil)
	if a.subscribeDlg.open {
		t.Error("the dialog stayed open")
	}
	if a.subs.Get("kw/12") == nil {
		t.Fatalf("not followed: %+v", a.subs.List())
	}
	if a.route != playlistRoute(music.SourceKuwo, 12) {
		t.Errorf("opened %+v", a.route)
	}

	// NetsEase's own are saved with the account, not followed here.
	a.openSubscribeDialog()
	a.subscribeDlg.text, a.subscribeDlg.src = "https://music.163.com/playlist?id=1", ""
	a.submitSubscribe()
	if a.subscribeDlg.err == "" || a.subscribeDlg.busy {
		t.Error("a NetEase playlist was asked for")
	}
	// A link of no service says so rather than guess.
	a.subscribeDlg.text, a.subscribeDlg.err = "https://example.com/playlist/1", ""
	a.submitSubscribe()
	if a.subscribeDlg.err == "" {
		t.Error("a link of no service was taken")
	}
}

// The tabs of a search leave out what only NetEase has while it is off,
// and a search for songs still works.
func TestSearchTabsWithoutNetease(t *testing.T) {
	f := &fakePlays{}
	a := newTestApp()
	a.settings = defaultSettings()
	a.reg = source.NewRegistry(netease.Provider{}, f)
	on := a.searchTabs()
	if len(on) != len(searchKinds) {
		t.Fatalf("%d tabs with every source on", len(on))
	}
	// Its catalogue turned off, as the listener does in the search
	// sources: its streaming may stay on, and the tabs are the others'.
	a.reg.SetSearched(music.SourceNetease, false)
	off := a.searchTabs()
	if len(off) != 2 || off[0].kind != netease.SearchSongs || off[1].kind != netease.SearchPlaylists {
		t.Fatalf("tabs with NetEase off: %+v", off)
	}
	if k := a.searchTabKind(netease.SearchArtists); k != netease.SearchSongs {
		t.Errorf("an artist tab stayed: %v", k)
	}
}

// A playlist's page offers to follow it when it is another source's, and
// says it is followed once it is: NetEase's have no such button, saved as
// they are with the account.
func TestSubscribeButtonOnThePage(t *testing.T) {
	f := &fakePlays{}
	a := subApp(t, f)
	pl := subPlaylistOf("通勤", music.Track{Source: "fake", ID: 1, Name: "甲"})
	a.playlists[playlistKey("fake", 7)] = pl
	a.route = playlistRoute("fake", 7)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if _, ok := tt.Find("订阅"); !ok {
		t.Fatal("no button to follow the playlist")
	}
	if err := tt.Click("订阅"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.subs.Get("fake/7") == nil {
		t.Fatal("the playlist was not followed")
	}
	if _, ok := tt.Find("已订阅"); !ok {
		t.Error("it does not say the playlist is followed")
	}

	// NetEase's own page has none.
	a.playlists[playlistKey("", 9)] = &netease.Playlist{ID: 9, Name: "网易云的"}
	a.route = playlistRoute("", 9)
	tt.Frame()
	if _, ok := tt.Find("订阅"); ok {
		t.Error("a NetEase playlist offers to be followed")
	}
}

// The settings' sources group turns NetEase off, and the app goes on with
// the others: it says so, and the choice is kept.
func TestSourceSettingsTurnNeteaseOff(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.reg = source.NewRegistry(netease.Provider{}, &fakePlays{})
	a.route = route{page: pageCharts}
	if !a.setSourceOn(music.SourceNetease, false) {
		t.Fatal("NetEase is not a source")
	}
	if a.sourceOn(music.SourceNetease) {
		t.Fatal("NetEase stayed on")
	}
	if len(a.settings.SourcesOff) != 1 || a.settings.SourcesOff[0] != music.SourceNetease {
		t.Errorf("settings keep %q off", a.settings.SourcesOff)
	}
	// Its page, which it alone provides, gave way to the 歌单 page.
	if a.route.page != pageSonglists {
		t.Errorf("went to %+v", a.route)
	}
	a.setSourceOn(music.SourceNetease, true)
	if !a.sourceOn(music.SourceNetease) || len(a.settings.SourcesOff) != 0 {
		t.Errorf("turned on: off %q", a.settings.SourcesOff)
	}
}
