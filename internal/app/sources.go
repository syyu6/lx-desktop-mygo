package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/kugou"
	"netease-music-mygo/internal/kuwo"
	"netease-music-mygo/internal/lxsource"
	"netease-music-mygo/internal/migu"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/qq"
	"netease-music-mygo/internal/source"
)

// The sources the app browses and plays. Two kinds of them, as
// lx-music-desktop has them: the catalogues it searches (搜索源) and the
// music sources it streams from (音乐源).
//
// NetEase, Kuwo, QQ Music and Migu are built in as both: their catalogues
// are searched, and their services stream, each as far as the service lets
// an app without an account stream — a song it will not serve plays from
// whichever source will, as any song does. Kugou is searched alone: it
// streams through an endpoint that asks for a signature of the song and
// the listener's account, so its songs play through a script the listener
// imported for them, and its words are fetched here.
//
// The listener may turn each on or off, and the app goes on with the rest:
// turning the music sources off leaves the search working, and turning the
// search sources off leaves playback working. A song of a source turned off
// keeps playing from it while it is queued.
//
// The scripts the listener imported (lx.go) are music sources: they answer
// where no built-in service will stream a song. Which of them is asked, and
// in what order, is the chain (chain.go).

// loadRegistry builds the sources, and hands the player the chain to try
// them in.
func (a *app) loadRegistry() {
	a.reg = source.NewRegistry(netease.Provider{}, kuwo.Provider{}, kugou.Catalog{}, qq.Provider{}, migu.Provider{})
	a.applySources()
	a.chain = source.NewChain()
	a.rebuildChain()
	a.pl.Resolve = func(t netease.Track, level string) (*netease.Stream, string, error) {
		res, err := a.chain.Resolve(t, level)
		if err != nil {
			return nil, "", err
		}
		return res.Stream, res.Served, nil
	}
	// The player asks again with the link whose address did not open left
	// out: a script whose upstream is down answers with a URL that is a
	// 404, and the next source that has the song takes its turn.
	a.pl.ResolveExcept = func(t netease.Track, level, skip string) (*netease.Stream, string, string, error) {
		res, err := a.chain.ResolveExcept(t, level, skip)
		if err != nil {
			return nil, "", "", err
		}
		return res.Stream, res.Served, res.Link, nil
	}
	a.pl.ResolveLyrics = func(t netease.Track) (string, string, bool) {
		return a.chain.ResolveLyrics(t)
	}
	// Once the song's own source has answered with no words, the others
	// are asked: a NetEase instrumental often has words on QQ Music.
	a.pl.ResolveLyricsElsewhere = func(t netease.Track) (string, string, bool) {
		return a.chain.ResolveLyricsElsewhere(t)
	}
	a.pl.ResolveCover = func(t netease.Track) (string, bool) {
		return a.chain.ResolveCover(t)
	}
}

// applySources gives the registry the sources and what is asked of them,
// as the settings say.
func (a *app) applySources() {
	if a.reg == nil {
		return
	}
	for _, id := range a.reg.Sources() {
		a.reg.SetAsked(id, source.CapLibrary, !slices.Contains(a.settings.LibrariesOff, id))
		a.reg.SetAsked(id, source.CapStream, !slices.Contains(a.settings.SourcesOff, id))
		a.reg.SetAsked(id, source.CapLyrics, !slices.Contains(a.settings.LyricsOff, id))
		a.reg.SetAsked(id, source.CapComments, !slices.Contains(a.settings.CommentsOff, id))
	}
}

// sourceOn reports whether the listener streams from a music source:
// NetEase is streamed from unless turned off, and so is a service.
func (a *app) sourceOn(id string) bool {
	if id == "" {
		id = music.SourceNetease
	}
	if a.reg == nil {
		return id == music.SourceNetease
	}
	return a.reg.Enabled(id)
}

// searchesOn reports whether the listener searches a search source's
// catalogue: NetEase's is searched unless turned off, and so is a service's.
func (a *app) searchesOn(id string) bool {
	if id == "" {
		id = music.SourceNetease
	}
	if a.reg == nil {
		return id == music.SourceNetease
	}
	return a.reg.Searched(id)
}

// askedOn reports whether the listener asks a source for a capability, the
// suggestions and the lyrics among them: what a source's switches say.
func (a *app) askedOn(id string, cap source.Cap) bool {
	if a.reg == nil {
		return id == "" || id == music.SourceNetease
	}
	return a.reg.Asked(id, cap)
}

// sourcesWith names the sources the listener searches that have the thing
// a page is built on, in their order, and the source among them the page
// shows: the one chosen while it still has it, else the first. It is what
// a page asks before it builds, and what its tabs are made of.
//
// The thing is what the page is for — a square, charts, an artist's page
// — and a source that does not have it is not offered: a page says so
// itself rather than show an empty tab for it.
//
// A page shows only the sources the listener left it (pageOn): a source
// turned off for a page is not offered there, and when none is left the
// page has nothing to show. A page with no row of its own (tabPage "") is
// one source's always, and is not turned off anyone's.
func sourcesWith[T any](a *app, page tabPage, pick string) (ids []string, at int) {
	if a.reg == nil {
		return nil, 0
	}
	for _, id := range a.reg.SearchedSources() {
		p, ok := a.reg.Get(id)
		if !ok {
			continue
		}
		if _, ok := any(p).(T); !ok {
			continue
		}
		if !a.pageOn(page, id) {
			continue
		}
		if pick == id {
			at = len(ids)
		}
		ids = append(ids, id)
	}
	return ids, at
}

// sourceTabs draws the sources a page can show, one lit, and reports the
// one chosen: the id of it, or empty while the pointer is elsewhere or
// only one source has the page.
//
// It is drawn only when there are two: with one, the page says which in
// its header's text, and a single tab is a control that does nothing. It
// draws nothing but the control — where it sits, and the room under it,
// is the caller's.
func (a *app) sourceTabs(c *ui.Context, key string, page tabPage, ids []string, at int) (string, bool) {
	if len(ids) < 2 || a.reg == nil {
		return "", false
	}
	labels := make([]string, len(ids))
	for i, id := range ids {
		if p, ok := a.reg.Get(id); ok {
			labels[i] = shortSource(p.Name())
		} else {
			labels[i] = id
		}
	}
	chosen := ""
	if segmented(c.Key(key), &at, labels...) {
		chosen = ids[at]
	}
	return chosen, chosen != ""
}

// tabRow is the room a page's row of tabs keeps under it, in the pages that
// show one under their heading and nothing beside it.
func tabRow(c *ui.Context, tabs func()) {
	ui.Row(c).Margin(0, 0, 18, 0).Children(tabs)
}

// tabPage is a page whose header offers the sources it can show: a page
// the listener may browse across the services, or keep on one.
type tabPage string

const (
	tabsHome      tabPage = "home"      // 推荐: the home page, and the daily songs under it
	tabsCharts    tabPage = "charts"    // 排行榜
	tabsSonglists tabPage = "songlists" // 歌单
)

// tabPages is every page the settings offer, in the order they are shown
// there, with the name the settings give them.
var tabPages = []struct {
	page  tabPage
	label string
}{
	{tabsHome, "推荐"},
	{tabsCharts, "排行榜"},
	{tabsSonglists, "歌单"},
}

// pageKey is how a source is turned off for a page in the settings: the
// page and the source, since one switch is about one source in one place.
func pageKey(page tabPage, id string) string {
	return string(page) + "/" + id
}

// pageOn reports whether a page may show a source, and so whether the
// source's own switch for that page is on. A page the settings have no row
// for — the artist's, the album's, a playlist's — is every source's that
// has it, as it always was: those are not places to browse the services.
func (a *app) pageOn(page tabPage, id string) bool {
	if page == "" || id == "" {
		return true
	}
	return !slices.Contains(a.settings.PagesAskOff, pageKey(page, id))
}

// pageAlone reports whether a page has only one source left to show, so
// that turning that one off would be turning the page off: a switch that
// cannot move is not offered. A source turned off for the page is not one of
// them, and a page no source has has none to be the last of.
func (a *app) pageAlone(page tabPage, id string) bool {
	if a.reg == nil || page == "" || !a.pageOn(page, id) {
		return false
	}
	n := 0
	for _, other := range a.reg.SearchedSources() {
		if a.pageHas(page, other) && a.pageOn(page, other) {
			n++
		}
	}
	return n == 1 && a.pageHas(page, id)
}

// pageHas reports whether a source has the things a page is made of: a
// home, charts, a square, a station. It is what sourcesWith asks of a
// source, by the page rather than by a type parameter, for the settings,
// which show a source's pages as a row of switches.
func (a *app) pageHas(page tabPage, id string) bool {
	if a.reg == nil {
		return false
	}
	p, ok := a.reg.Get(id)
	if !ok {
		return false
	}
	switch page {
	case tabsHome:
		if _, ok := any(p).(source.FeaturedSource); ok {
			return true
		}
		if _, ok := any(p).(source.Daily); ok {
			return true
		}
		_, ok := any(p).(source.Radio)
		return ok
	case tabsCharts:
		_, ok := any(p).(source.Charted)
		return ok
	case tabsSonglists:
		_, ok := any(p).(source.Square)
		return ok
	}
	return false
}

// setPageAsk turns a source on or off for a page, keeps it, and redraws.
// Turning off the square's source leaves the square read from whichever
// takes it, rather than showing what a source no longer drawn had loaded.
func (a *app) setPageAsk(page tabPage, id string, on bool) {
	a.settings.PagesAskOff = setOff(a.settings.PagesAskOff, pageKey(page, id), !on)
	if !on && page == tabsSonglists {
		a.square = nil
	}
	a.settings.save()
	a.redraw()
}

func (a *app) sourceName(id string) string {
	if id == "" || id == music.SourceNetease {
		return ""
	}
	if a.reg != nil {
		if c, ok := a.reg.Catalog(id); ok {
			return c.Name()
		}
	}
	for _, info := range a.sources {
		if info.ID == id {
			return info.Name
		}
	}
	return id
}

// sourceTag is what a search result's badge says: the source's name, and
// NetEase's too, so that the listener knows what each row is from.
func (a *app) sourceTag(id string) string {
	if name := a.sourceName(id); name != "" && name != id {
		return shortSource(name)
	}
	if id == "" || id == music.SourceNetease {
		return "网易云"
	}
	if a.reg != nil {
		if p, ok := a.reg.Get(id); ok {
			return shortSource(p.Name())
		}
	}
	return id
}

// shortSource is a source's name as a badge says it: the service's own
// short name, without the 音乐 most of them end in.
func shortSource(name string) string {
	name = strings.TrimSuffix(name, "音乐")
	return strings.TrimSpace(name)
}

// sourceTrackOf is a music.Track as a source script is told about it: a
// script knows a song by its id on its own service, which is the one the
// track carries.
func sourceTrackOf(t music.Track) lxsource.Track {
	src := lxsource.SourceNetease
	if s, ok := lxSourceOf(t.Src()); ok {
		src = s
	}
	return lxsource.Track{
		Source:   src,
		ID:       t.SongKey(),
		Name:     t.Name,
		Singer:   t.ArtistNames(),
		Album:    t.Album.Name,
		Interval: formatInterval(t.Duration),
		PicURL:   netease.Sized(t.Cover(), 300),
	}
}
