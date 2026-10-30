package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// searchResults and searchSuggest search NetEase; tests replace them.
var (
	searchResults = netease.SearchPage
	searchSuggest = netease.SearchSuggest
)

// searchLimit is how many results load at a time.
const searchLimit = 60

// searchKind is a kind of search, as its tab shows it. NetEase's alone
// has artists, albums, users and podcasts, so those tabs show while the
// listener asks it.
type searchKind struct {
	kind        netease.SearchType
	label, unit string
	neteaseOnly bool
}

// searchKinds are the kinds of search, as their tabs show them.
var searchKinds = []searchKind{
	{kind: netease.SearchSongs, label: "单曲", unit: "首单曲"},
	{kind: netease.SearchPlaylists, label: "歌单", unit: "个歌单"},
	{kind: netease.SearchArtists, label: "歌手", unit: "位歌手", neteaseOnly: true},
	{kind: netease.SearchAlbums, label: "专辑", unit: "张专辑", neteaseOnly: true},
	{kind: netease.SearchUsers, label: "用户", unit: "位用户", neteaseOnly: true},
	{kind: netease.SearchPodcasts, label: "播客", unit: "个播客", neteaseOnly: true},
}

// searchTabKind is kind where the tabs show it now, or else songs: NetEase's
// catalogue turned off leaves no artists, albums, users or podcasts to look
// for.
func (a *app) searchTabKind(kind netease.SearchType) netease.SearchType {
	if kind == 0 {
		return netease.SearchSongs
	}
	for _, k := range a.searchTabs() {
		if k.kind == kind {
			return kind
		}
	}
	return netease.SearchSongs
}

// searchTabs are the kinds of search the listener may choose now.
func (a *app) searchTabs() []searchKind {
	netease := a.searchesOn(music.SourceNetease)
	var out []searchKind
	for _, k := range searchKinds {
		if k.neteaseOnly && !netease {
			continue
		}
		out = append(out, k)
	}
	return out
}

// searchData is what a search found, as far as its pages have loaded.
type searchData struct {
	res  netease.SearchResult
	page paged
}

// search searches for q, of the kind last searched for.
func (a *app) search(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	kind := a.searchTabKind(a.searched.kind)
	a.searchFor(route{page: pageSearch, query: q, kind: kind})
}

// searchFor opens the page of a search, remembering its query. Searched
// for again, it loads anew.
func (a *app) searchFor(r route) {
	a.rememberSearch(r.query)
	a.dismissSuggestions()
	if r == a.route {
		a.nowPlaying = false
		a.loadSearch(r, true)
		return
	}
	a.open(r)
}

// loadSearch loads the first page of a search, unless it has, or again.
func (a *app) loadSearch(r route, again bool) {
	// A tab chosen before NetEase was turned off is gone from the tabs, and
	// its page looks for songs instead.
	if k := a.searchTabKind(r.kind); k != r.kind {
		r.kind = k
		a.route = r
	}
	if a.searches[r] != nil && !again {
		return
	}
	d := &searchData{}
	a.searches[r] = d
	a.moreResults(r, d)
}

// moreResults loads the next page of a search. A search done again has
// new data, so the last one's pages land where nothing shows them.
//
// Songs and playlists come from every source the listener asks, NetEase's
// and the services', as lx-music-desktop searches them all; artists and
// the rest are NetEase's, whose catalogues alone have them.
func (a *app) moreResults(r route, d *searchData) {
	if d.page.loading {
		return
	}
	d.page.loading, d.page.err = true, ""
	offset := d.res.Len()
	go func() {
		res, err := a.searchAll(r, searchLimit, offset)
		a.post(func() {
			d.page.loading = false
			if err != nil {
				d.page.err = "搜索失败，请稍后再试"
				return
			}
			d.res.Append(res)
			// A server can claim more while sending none.
			d.page.loaded, d.page.more = true, res.Len() > 0 && d.res.Len() < d.res.Total
		})
	}()
}

// searchAll searches the sources for what kind of thing, from offset: the
// search source r.only alone, or every one the listener searches.
func (a *app) searchAll(r route, limit, offset int) (netease.SearchResult, error) {
	query, kind := r.query, r.kind
	if a.reg != nil {
		switch kind {
		case netease.SearchSongs:
			res, err := a.searchSongs(query, r.only, limit, offset)
			return netease.SearchResult{Songs: res.Songs, Total: res.Total}, err
		case netease.SearchPlaylists:
			res, err := a.searchPlaylists(query, r.only, limit, offset)
			return netease.SearchResult{Playlists: res.Playlists, Total: res.Total}, err
		}
		// Artists and the rest are NetEase's alone, and are not asked while
		// it is turned off.
		if !a.reg.Searched(music.SourceNetease) {
			return netease.SearchResult{}, source.ErrNoSource
		}
	}
	return searchResults(query, kind, limit, offset)
}

// searchSongs searches the songs on one search source, or on every one the
// listener searches.
func (a *app) searchSongs(query, only string, limit, offset int) (music.SearchResult, error) {
	if only == "" {
		return a.reg.SearchSongs(query, limit, offset)
	}
	c, ok := a.reg.Catalog(only)
	if !ok {
		return music.SearchResult{}, source.ErrNoSource
	}
	tracks, total, err := c.SearchSongs(query, limit, offset)
	return music.SearchResult{Songs: tracks, Total: total}, err
}

// searchPlaylists searches the playlists on one search source, or on every
// one the listener searches that has them.
func (a *app) searchPlaylists(query, only string, limit, offset int) (music.SearchResult, error) {
	if only == "" {
		return a.reg.SearchPlaylists(query, limit, offset)
	}
	c, ok := a.reg.Catalog(only)
	if !ok {
		return music.SearchResult{}, source.ErrNoSource
	}
	s, ok := c.(source.Searcher)
	if !ok {
		// A catalogue without playlists finds none: not a failure, an
		// answer.
		return music.SearchResult{}, nil
	}
	lists, total, err := s.SearchPlaylists(query, limit, offset)
	return music.SearchResult{Playlists: lists, Total: total}, err
}

// searchHistoryLimit is how many searches the history keeps.
const searchHistoryLimit = 20

// rememberSearch puts q first in the history of searches, once.
func (a *app) rememberSearch(q string) {
	history := []string{q}
	for _, s := range a.settings.SearchHistory {
		if s != q && len(history) < searchHistoryLimit {
			history = append(history, s)
		}
	}
	a.settings.SearchHistory = history
	a.settings.save()
}

func (a *app) clearSearchHistory() {
	a.settings.SearchHistory = nil
	a.settings.save()
}

func (a *app) searchPage(c *ui.Context) {
	r := a.route
	d := a.searches[r]
	header := func(c *ui.Context) { a.searchHeader(c, d) }
	more := func() { a.moreResults(r, d) }
	if r.kind == netease.SearchSongs {
		if !d.page.loaded && d.page.loading {
			a.trackSkeleton(c, a.pageShimmer(c), true, header)
			return
		}
		a.trackRows(c, d.res.Songs, true, header, func(c *ui.Context) {
			a.pageFoot(c, &d.page, more)
			a.searchEmpty(c, d)
		})
		return
	}
	a.scrollPage(c, func() {
		header(c)
		res := &d.res
		switch r.kind {
		case netease.SearchArtists:
			ui.Grid(c).Columns(a.columns(c, 132)).GapX(20).GapY(28).Children(func() {
				for i := range res.Artists {
					a.artistCard(c.Key(res.Artists[i].ID), &res.Artists[i])
				}
			})
		case netease.SearchAlbums:
			a.albumGrid(c, res.Albums)
		case netease.SearchPlaylists:
			ui.Grid(c).Columns(a.columns(c, 156)).GapX(20).GapY(28).Children(func() {
				for i := range res.Playlists {
					a.playlistCard(c.Key(playlistKey(res.Playlists[i].Src(), res.Playlists[i].ID)), &res.Playlists[i])
				}
			})
		case netease.SearchUsers:
			for i := range res.Users {
				a.userRow(c.Key(res.Users[i].UserID), &res.Users[i])
			}
		case netease.SearchPodcasts:
			for i := range res.Podcasts {
				a.podcastRow(c.Key(res.Podcasts[i].ID), &res.Podcasts[i])
			}
		}
		if !d.page.loaded && d.page.loading {
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 156), 156, false)
			return
		}
		a.pageFoot(c, &d.page, more)
		a.searchEmpty(c, d)
	})
}

func (a *app) searchEmpty(c *ui.Context, d *searchData) {
	if d.page.loaded && d.res.Len() == 0 {
		a.empty(c, "没有找到相关结果")
	}
}

// searchHeader is what was searched for, how many it found, and the tabs
// of what it may look for instead.
func (a *app) searchHeader(c *ui.Context, d *searchData) {
	r := a.route
	tabs := a.searchTabs()
	sub := "搜索"
	at := 0
	for i, k := range tabs {
		if k.kind == r.kind {
			at = i
			if d.page.loaded {
				// The songs and the playlists come from every source at
				// once: the header says which ones were asked.
				sub = fmt.Sprintf("搜索 · %d %s", d.res.Total, k.unit)
				if srcs := a.searchedSources(k); srcs != "" {
					sub += " · " + srcs
				}
			}
		}
	}
	a.heading(c, "“"+r.query+"”", sub)
	ui.Row(c).Margin(0, 0, 14, 0).Gap(14).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Children(func() {
			labels := make([]string, len(tabs))
			for i, k := range tabs {
				labels[i] = k.label
			}
			if segmented(c.Key("kind"), &at, labels...) {
				r.kind = tabs[at].kind
				a.show(r)
			}
		})
		// The search sources beside the kinds of search: the results
		// together, as searched, or one platform's alone, as
		// lx-music-desktop offers them.
		a.searchSourceBar(c)
	})
}

// searchSourceBar switches the page between every search source's results
// together and one source's alone. It shows while there is more than one
// source to choose, and it is not offered to the kinds of search NetEase
// alone answers.
func (a *app) searchSourceBar(c *ui.Context) {
	r := a.route
	if kind := searchKindOf(r.kind); kind.neteaseOnly || a.reg == nil {
		return
	}
	ids := a.reg.SearchedSources()
	if len(ids) < 2 {
		return
	}
	with := []string{""}
	labels := []string{"全部"}
	at := 0
	for _, id := range ids {
		c, ok := a.reg.Catalog(id)
		if !ok {
			continue
		}
		// A kind of search a source has not is not offered for it.
		if r.kind == netease.SearchPlaylists {
			if _, ok := c.(source.Searcher); !ok {
				continue
			}
		}
		if id == r.only {
			at = len(with)
		}
		with = append(with, id)
		labels = append(labels, shortSource(c.Name()))
	}
	if len(with) < 2 {
		return
	}
	if segmented(c.Key("search-src"), &at, labels...) && with[at] != r.only {
		r.only = with[at]
		a.show(r)
	}
}

// searchKindOf returns the searchKind of a kind of search, as its tabs
// show it.
func searchKindOf(kind netease.SearchType) searchKind {
	for _, k := range searchKinds {
		if k.kind == kind {
			return k
		}
	}
	return searchKinds[0]
}

// searchedSources names the sources a search of kind asks, for its header:
// the search sources turned on that have what is looked for, or the one the
// page shows alone.
func (a *app) searchedSources(k searchKind) string {
	if k.neteaseOnly {
		if a.searchesOn(music.SourceNetease) {
			return a.sourceTag(music.SourceNetease)
		}
		return ""
	}
	if a.reg == nil {
		return ""
	}
	if only := a.route.only; only != "" {
		if c, ok := a.reg.Catalog(only); ok {
			return shortSource(c.Name())
		}
	}
	var names []string
	for _, id := range a.reg.SearchedSources() {
		c, ok := a.reg.Catalog(id)
		if !ok {
			continue
		}
		if k.kind == netease.SearchPlaylists {
			// Only a source that searches playlists is asked for them.
			if _, ok := c.(source.Searcher); !ok {
				continue
			}
		}
		names = append(names, shortSource(c.Name()))
	}
	return strings.Join(names, "、")
}

// artistCard is an artist's round photo over their name, opening their page.
func (a *app) artistCard(c *ui.Context, ar *netease.ArtistProfile) {
	p := a.pal
	card := ui.ButtonBase(c).Column().Gap(10).AlignItems(ui.Stretch).FocusRing(false).Label(ar.Name)
	hovered := card.Hovered()
	card.Children(func() {
		photo := ar.Avatar
		if photo == "" {
			photo = ar.PicURL
		}
		art := ui.Box(c).AspectRatio(1).Radius(999)
		if hovered {
			art.Shadow(0, 12, 28, -6, p.cardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.cardShade)
		}
		art.Children(func() { a.coverFill(c, photo, 400, 999) })
		ui.Text(c, ar.Name).FontSize(13).FontWeight(500).SingleLine().AlignSelf(ui.Center)
	})
	if card.Clicked() {
		a.open(route{page: pageArtist, id: ar.ID})
	}
}
