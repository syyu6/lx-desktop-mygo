package app

import (
	"errors"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// What a source's home page is made of: the playlists it recommends, the
// charts it has, the songs it picks for the day, and those it picks from
// the listener's taste. The pages that show them read one source's at a
// time, chosen by the tabs at their head, as the 歌单 page does its square.
//
// A source that has none of a section has none of its loader either — the
// interface says so — and the page leaves the section out rather than show
// it empty. A source that needs an account for one says so with
// source.ErrNoAccount, and the page says that, so that "sign in to QQ
// Music" reads differently from "the network is down".

// homeState is what a source's home page has loaded.
type homeState struct {
	src      string
	featured []music.Playlist
	charts   []music.Playlist
	daily    []music.Track
	rec      []music.Playlist
	// err is why the page as a whole did not load, and errDaily why the
	// day's songs alone did not: the source may answer everything but
	// those, as one that wants an account does.
	err      string
	errDaily string
	loading  bool
	at       time.Time // when what it holds was asked for
}

// home returns a source's home, making it if it is not there.
func (a *app) home(src string) *homeState {
	if src == "" {
		src = music.SourceNetease
	}
	if a.homes == nil {
		a.homes = map[string]*homeState{}
	}
	h := a.homes[src]
	if h == nil {
		h = &homeState{src: src}
		a.homes[src] = h
	}
	return h
}

// homeSource is the source the home page shows: the one the listener chose,
// while it still has a home, and else the first that has one.
//
// A registry that has no source with a home page is the app as a test
// builds it, or one whose built-ins were all turned off: NetEase is what
// the page shows, as it did before there were others.
func (a *app) homeSource() string {
	ids, at := sourcesWith[source.FeaturedSource](a, homeTabPage(a.route.page), a.homePick)
	if len(ids) == 0 {
		if a.reg == nil || a.reg.Searched(music.SourceNetease) {
			return music.SourceNetease
		}
		return ""
	}
	return ids[at]
}

// The pages that show one section of a source's home — the daily songs,
// the charts, the station — show the home's own source: a source is asked
// of its home as a whole, and switching the page's source there is
// switching it for every page that shows what that source offers.

// homeView is the state a page shows: the source's home, that source, and
// whether what it shows is still loading.
//
// The load is started here, which is what makes a page opened first ask its
// own source. A page drawn while its source is being asked shows its
// shimmer rather than ask again every frame.
func (a *app) homeView() (h *homeState, src string, loading bool) {
	src = a.homeSource()
	if src == "" {
		return nil, "", false
	}
	h = a.home(src)
	if h.at.IsZero() && !h.loading && h.err == "" {
		a.loadHomeOf(src, false)
	}
	return h, src, h.at.IsZero()
}

// providerOf is a source's provider, or nil.
// providerOf is a source's provider, or nil.
func (a *app) providerOf(src string) source.Provider {
	if a.reg == nil {
		return nil
	}
	p, _ := a.reg.Get(src)
	return p
}

// loadHome loads the home page of the source it shows.
func (a *app) loadHome() { a.loadHomeOf(a.homeSource(), false) }

// loadHomeOf loads one source's home: what it has of each section, on a
// goroutine, each section's failure kept apart so that one failing does not
// empty the page.
func (a *app) loadHomeOf(src string, again bool) {
	if src == "" {
		return
	}
	h := a.home(src)
	if h.loading || !again && !h.at.IsZero() {
		return
	}
	p := a.providerOf(src)
	if p == nil {
		// Nothing to ask: the page shows what it has rather than shimmer
		// for an answer that is not coming. A registry without the source
		// is the app as a test builds it, or one whose sources are all off
		// and whose page still points at one.
		h.at = time.Now()
		return
	}
	h.loading, h.err, h.errDaily = true, "", ""
	at := time.Now()
	go func() {
		f := p
		var (
			featured, charts, rec []music.Playlist
			daily                 []music.Track
			errF, errC, errD      error
		)
		// Each is asked only where the source has it: an interface a source
		// does not satisfy is a section it does not have, not one that
		// failed.
		if s, ok := f.(source.FeaturedSource); ok {
			featured, errF = s.Featured(30)
		}
		if s, ok := f.(source.Charted); ok {
			charts, errC = s.Charts()
		}
		if s, ok := f.(source.Daily); ok {
			daily, errD = s.Daily()
		}
		if s, ok := f.(source.Recommended); ok {
			rec, _ = s.Recommended()
		}
		a.post(func() {
			a.homeArrived(src, h, featured, charts, rec, daily, errF, errC, errD, at)
		})
	}()
}

// homeArrived takes what a source answered of its home. It is apart from
// the goroutine that asked so that a test drives it without a window, as
// the square's answers are.
func (a *app) homeArrived(src string, h *homeState, featured, charts, rec []music.Playlist, daily []music.Track, errF, errC, errD error, at time.Time) {
	h.loading = false
	// Both lists failing is a source that is not answering at all; one
	// failing leaves the rest of the page to show.
	if errF != nil && errC != nil {
		h.err = a.connectionError(src)
		return
	}
	h.featured, h.charts, h.daily, h.rec = featured, charts, daily, rec
	if errD != nil {
		h.errDaily = a.dailyError(src, errD)
	}
	h.at = at
}

// dailyError says why a source's day's songs did not come, as the listener
// needs to hear it: a source that wants an account is told apart from one
// that is not answering.
func (a *app) dailyError(src string, err error) string {
	if errors.Is(err, source.ErrNoAccount) {
		return "登录" + a.sourceShortName(src) + "后每天有一份"
	}
	return "暂时没有推荐"
}

// connectionError says what a page shows when its source is not answering.
func (a *app) connectionError(src string) string {
	return "无法连接" + a.sourceShortName(src) + "，请检查网络"
}

// pageError is what a page says when something of src did not load: a
// source that wants an account is told apart from one that is not
// answering, so that "sign in to QQ Music" does not read as "the network
// is down".
func (a *app) pageError(src string, what string, err error) string {
	if errors.Is(err, source.ErrNoAccount) {
		return "登录" + a.sourceShortName(src) + "后才能打开"
	}
	if err != nil && a.reg != nil && src != "" && src != music.SourceNetease {
		return "无法连接" + a.sourceShortName(src) + "，请检查网络"
	}
	return what
}

// sourceShortName is what the app calls a source in a sentence: NetEase is
// named, so that a page which speaks of one source among several does not
// leave the reader guessing which.
func (a *app) sourceShortName(src string) string {
	if src == "" || src == music.SourceNetease {
		return "网易云音乐"
	}
	if name := a.sourceName(src); name != "" {
		return name
	}
	return src
}

// freshenHome loads the home page again once the source has picked the
// day's songs anew since it last did: the app may stay open overnight.
func (a *app) freshenHome() {
	src := a.homeSource()
	if src == "" {
		return
	}
	h := a.home(src)
	if !h.loading && !h.at.IsZero() && dailyRenewed(h.at, time.Now()) {
		a.loadHomeOf(src, true)
	}
}

// homeSourceTabs shows the sources the home page may show, and reports the
// one chosen: the source the listener picked for the home page. The daily,
// chart and station pages carry the same tabs, from the same choice, so
// that the app says one thing about which source it is listening to.
func (a *app) homeSourceTabs(c *ui.Context) {
	tabRow(c, func() { a.homeSourceTabsIn(c) })
}

// homeSourceTabsIn is the row of tabs itself, for the page whose header
// keeps something beside it: the station puts its modes on the same line.
func (a *app) homeSourceTabsIn(c *ui.Context) {
	page := homeTabPage(a.route.page)
	ids, at := sourcesWith[source.FeaturedSource](a, page, a.homeSource())
	if chosen, ok := a.sourceTabs(c, "home-src", page, ids, at); ok {
		a.homePick = chosen
		a.loadHomeOf(chosen, false)
	}
}

// homeTabPage is which of the settings' switches a page of the home's own
// source falls under: the charts are their own, and everything else that
// shows a source's home — the daily songs, the station — goes with the
// home page, as it is one listener's choice of a source made there.
func homeTabPage(page pageKind) tabPage {
	if page == pageCharts {
		return tabsCharts
	}
	return tabsHome
}
