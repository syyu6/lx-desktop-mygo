package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// fakeHome is a second source with a home page, a day's songs and a
// station, answering what the test says and reporting what was asked.
type fakeHome struct {
	featured []music.Playlist
	charts   []music.Playlist
	daily    []music.Track
	radio    []music.Track
	modes    []music.RadioMode
	dailyErr error
	asked    int
	radioAsk int
}

func (f *fakeHome) ID() string   { return "fake" }
func (f *fakeHome) Name() string { return "假源音乐" }
func (f *fakeHome) SearchSongs(string, int, int) ([]music.Track, int, error) {
	return nil, 0, nil
}
func (f *fakeHome) Stream(music.Track, string) (*music.Stream, error) {
	return nil, nil
}
func (f *fakeHome) Lyrics(music.Track) (music.Lyrics, error) { return music.Lyrics{}, nil }
func (f *fakeHome) Featured(int) ([]music.Playlist, error)   { return f.featured, nil }
func (f *fakeHome) Charts() ([]music.Playlist, error)        { return f.charts, nil }
func (f *fakeHome) Daily() ([]music.Track, error) {
	f.asked++
	return f.daily, f.dailyErr
}
func (f *fakeHome) RadioModes() []music.RadioMode { return f.modes }
func (f *fakeHome) Radio(string) ([]music.Track, error) {
	f.radioAsk++
	return f.radio, nil
}

// homeApp is an app whose sources are NetEase and the fake one, with the
// home page's source chosen as the test says.
func homeApp(t *testing.T, f *fakeHome) *app {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.reg = source.NewRegistry(netease.Provider{}, f)
	a.homePick = "fake"
	a.route = route{page: pageHome}
	return a
}

// The home page shows one source's playlists at a time, and switching to
// the other asks that one: the two sources' pages are apart, not merged.
func TestHomeIsOneSourceAtATime(t *testing.T) {
	f := &fakeHome{featured: []music.Playlist{{Source: "fake", ID: 1, Name: "假的推荐"}}}
	a := homeApp(t, f)
	if src := a.homeSource(); src != "fake" {
		t.Fatalf("home shows %q", src)
	}
	a.loadHome()
	// As the answer lands, which post drops without a window.
	h := a.home("fake")
	a.homeArrived("fake", h, f.featured, f.charts, nil, nil, nil, nil, nil, time.Now())
	if got := h.featured; len(got) != 1 || got[0].Name != "假的推荐" {
		t.Fatalf("fake home %+v", got)
	}
	// NetEase's is its own, and asking the fake one again did not touch it.
	if h := a.home(music.SourceNetease); len(h.featured) != 0 {
		t.Errorf("NetEase's home was filled from the other source: %+v", h.featured)
	}
}

// A source that has a day's songs only for a listener signed in to it says
// so, rather than show an empty card: the page tells that apart from a
// source that is not answering.
func TestHomeDailySaysItNeedsAnAccount(t *testing.T) {
	f := &fakeHome{dailyErr: source.ErrNoAccount}
	a := homeApp(t, f)
	a.loadHome()
	h := a.home("fake")
	a.homeArrived("fake", h, nil, nil, nil, nil, nil, nil, f.dailyErr, time.Now())
	if h.errDaily == "" {
		t.Fatal("the daily card says nothing")
	}
	if h.errDaily == a.connectionError("fake") {
		t.Errorf("the daily error reads as a network one: %q", h.errDaily)
	}
	if h.err != "" {
		t.Errorf("the whole page failed: %q", h.err)
	}
}

// The station page shows the source the home pages show, and its modes are
// that source's: a source with one mode offers no mode to choose.
func TestFMFollowsTheHomeSource(t *testing.T) {
	f := &fakeHome{
		featured: []music.Playlist{{Source: "fake", ID: 1, Name: "假的"}},
		radio:    []music.Track{{Source: "fake", ID: 2, Name: "假的歌"}},
		modes:    []music.RadioMode{{ID: "rec", Name: "推荐"}},
	}
	a := homeApp(t, f)
	a.route = route{page: pageFM}
	if src := a.fmSource(); src != "fake" {
		t.Fatalf("the station plays %q", src)
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// One mode is no choice: the page says which it plays rather than show
	// a control that does nothing.
	if _, ok := tt.Find("私人 FM · 推荐"); !ok {
		t.Errorf("the card does not say which mode: %q", tt.Texts())
	}
}

// The station of a source is asked through the source, under its own name,
// so that two sources' default modes are not the same station.
func TestFMRadioNameCarriesTheSource(t *testing.T) {
	if got := fmRadio("", "DEFAULT"); got != "fm:DEFAULT" {
		t.Errorf("NetEase's station is %q", got)
	}
	if got := fmRadio(music.SourceQQ, "DEFAULT"); got != "fm:tx:DEFAULT" {
		t.Errorf("QQ's station is %q", got)
	}
	src, mode, ok := fmParts("fm:tx:DEFAULT")
	if !ok || src != music.SourceQQ || mode != "DEFAULT" {
		t.Errorf("read back %q %q %v", src, mode, ok)
	}
	if src, mode, ok := fmParts("fm:DEFAULT"); !ok || src != "" || mode != "DEFAULT" {
		t.Errorf("NetEase's read back %q %q %v", src, mode, ok)
	}
}

// A source turned off for a page is not offered there, nor searched for it:
// the page shows what is left, and with one source left its row of tabs goes
// away. The switch is about that source in that page, not about the page.
func TestPageAsksOneSource(t *testing.T) {
	f := &fakeHome{featured: []music.Playlist{{Source: "fake", ID: 1, Name: "假的推荐"}}}
	a := homeApp(t, f)
	if a.homeSource() != "fake" {
		t.Fatalf("the page shows %q, the source chosen", a.homeSource())
	}

	a.setPageAsk(tabsHome, "fake", false)
	if src := a.homeSource(); src != music.SourceNetease {
		t.Errorf("with the fake source off the page shows %q, want NetEase", src)
	}
	a.route = route{page: pageHome}
	h := a.home(music.SourceNetease)
	a.homeArrived(music.SourceNetease, h, []music.Playlist{{ID: 2, Name: "网易的"}}, nil, nil, nil, nil, nil, nil, time.Now())
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// Nothing left to switch between: the page offers no row of tabs.
	if tt.HasText("假源") {
		t.Error("the page still offers a tab for the source turned off")
	}

	// The source is still searched, and still shows its other pages.
	if !a.reg.Searched("fake") {
		t.Error("the fake source stopped being searched")
	}
	a.route = route{page: pageCharts}
	if !a.pageOn(tabsCharts, "fake") {
		t.Error("turning the source off for 推荐 took the charts")
	}

	// On again, the page comes back to the source chosen.
	a.setPageAsk(tabsHome, "fake", true)
	a.route = route{page: pageHome}
	if src := a.homeSource(); src != "fake" {
		t.Errorf("with the source on again the page shows %q", src)
	}
}

// A page offers no switch to turn off the last source it has: turning it off
// would be turning the page off, which is not a source's switch.
func TestTheLastSourceOfAPageCannotBeTurnedOff(t *testing.T) {
	f := &fakeHome{featured: []music.Playlist{{Source: "fake", ID: 1}}}
	a := homeApp(t, f)
	a.setPageAsk(tabsHome, "fake", false)
	// NetEase is the only source the home has left.
	if !a.pageAlone(tabsHome, music.SourceNetease) {
		t.Error("NetEase is not the home's last source")
	}
	if a.pageAlone(tabsHome, "fake") {
		t.Error("a source turned off is counted as the page's last")
	}
	// A source that has no such page is not its last: the fake one has no
	// square, so the square is NetEase's alone and says so.
	if a.pageAlone(tabsSonglists, "fake") {
		t.Error("a source with no square is counted as the square's last")
	}
	if !a.pageAlone(tabsSonglists, music.SourceNetease) {
		t.Error("NetEase is the square's only source, and does not say so")
	}
}
func TestLikesArePerSource(t *testing.T) {
	a := newTestApp()
	a.liked = map[string]bool{}
	net := &netease.Track{ID: 7, Name: "甲"}
	qq := &netease.Track{Source: music.SourceQQ, ID: 7, Key: "abc", Name: "乙"}
	a.setLiked(net.Ident(), true)
	if !a.isLiked(net) {
		t.Error("NetEase's song is not liked")
	}
	if a.isLiked(qq) {
		t.Error("QQ's song of the same number is liked too")
	}
}
