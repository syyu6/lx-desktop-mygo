package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// The playlist square: the playlists a source lists by category, for a
// listener browsing rather than looking one up.
//
// Each source that has a square brings its own: NetEase's is its 歌单
// page, with its categories. A source that has none, as a built-in
// service's does not, is not offered here, but its playlists are still
// found by searching for them.

// squareState is what a source's square has loaded, and what it is
// showing.
type squareState struct {
	src   string // the source the square is of
	tag   int    // the tag chosen, in tags: -1 for all of them
	tags  []music.Tag
	lists []music.Playlist
	page  paged
	// tagsLoaded and tagsLoading say whether the categories have come, or
	// are coming; tagsErr why they did not.
	tagsLoaded, tagsLoading bool
	tagsErr                 string
}

// squareLimit is how many playlists of the square load at a time.
const squareLimit = 36

// squareSource is the source whose square the page shows: the one the
// listener chose, while they still ask it, and else the first one that has
// a square.
func (a *app) squareSource() string {
	if a.reg == nil {
		return ""
	}
	if id := a.squarePick; id != "" && a.reg.Searched(id) {
		if p, ok := a.reg.Get(id); ok {
			if _, ok := p.(source.Square); ok {
				return id
			}
		}
	}
	// The square is a catalogue's: a source kept for searching shows it
	// though the app streams nothing of its own from it.
	for _, id := range a.reg.SearchedSources() {
		if p, ok := a.reg.Get(id); ok {
			if _, ok := p.(source.Square); ok {
				return id
			}
		}
	}
	return ""
}

// loadSquare loads the square of the source chosen, its categories unless
// they have loaded.
func (a *app) loadSquare() {
	src := a.squareSource()
	if src == "" {
		return
	}
	s := a.square
	if s == nil {
		s = &squareState{src: src, tag: -1}
		a.square = s
	}
	p, ok := a.reg.Get(src)
	if !ok {
		return
	}
	sq := p.(source.Square)
	if !s.tagsLoaded && !s.tagsLoading {
		s.tagsLoading, s.tagsErr = true, ""
		go func() {
			tags, err := sq.Tags()
			a.post(func() { a.tagsArrived(s, sq, tags, err) })
		}()
	}
}

// listsLoaded reports whether the square's playlists have loaded.
func (s *squareState) listsLoaded() bool { return s.page.loaded }

// loadSquareLists loads the first page of the tag chosen.
func (a *app) loadSquareLists(s *squareState, sq source.Square) {
	if sq == nil || s.page.loading {
		return
	}
	s.page.loading, s.page.err = true, ""
	name := a.squareTagName(s)
	go func() {
		lists, err := sq.SquarePlaylists(name, squareLimit, 0)
		a.post(func() { a.squareArrived(s, lists, err, false) })
	}()
}

// tagsArrived takes the square's categories, and loads their playlists
// once it has them.
func (a *app) tagsArrived(s *squareState, sq source.Square, tags []music.Tag, err error) {
	s.tagsLoading = false
	if err != nil {
		s.tagsErr = "歌单分类加载失败"
		return
	}
	s.tags, s.tagsLoaded = tags, true
	a.loadSquareLists(s, sq)
}

// squareArrived takes a page of the square's playlists: more appends it to
// those shown.
func (a *app) squareArrived(s *squareState, lists []music.Playlist, err error, more bool) {
	s.page.loading = false
	if err != nil {
		s.page.err = "歌单加载失败"
		return
	}
	if more {
		s.lists = append(s.lists, lists...)
	} else {
		s.lists = lists
		s.page.loaded = true
	}
	s.page.more = len(lists) == squareLimit
}

// squareSquare is the square of the source the page shows, if it has one.
func (a *app) squareSquare(s *squareState) source.Square {
	if s == nil || a.reg == nil {
		return nil
	}
	p, ok := a.reg.Get(s.src)
	if !ok {
		return nil
	}
	sq, _ := p.(source.Square)
	return sq
}

// moreSquare loads the next page of the square.
func (a *app) moreSquare() {
	s := a.square
	if s == nil || s.page.loading || !s.page.more {
		return
	}
	p, ok := a.reg.Get(s.src)
	if !ok {
		return
	}
	sq := p.(source.Square)
	s.page.loading, s.page.err = true, ""
	name := a.squareTagName(s)
	offset := len(s.lists)
	go func() {
		lists, err := sq.SquarePlaylists(name, squareLimit, offset)
		a.post(func() { a.squareArrived(s, lists, err, true) })
	}()
}

// squareTagName is the category chosen, as the source names it: empty for
// its all.
func (a *app) squareTagName(s *squareState) string {
	if s.tag < 0 || s.tag >= len(s.tags) {
		return ""
	}
	return s.tags[s.tag].Name
}

// chooseSquareTag shows another category's playlists, from its first.
func (a *app) chooseSquareTag(s *squareState, tag int) {
	if s.tag == tag {
		return
	}
	s.tag = tag
	s.lists, s.page = nil, paged{}
	p, ok := a.reg.Get(s.src)
	if !ok {
		return
	}
	a.loadSquareLists(s, p.(source.Square))
}

func (a *app) songlistsPage(c *ui.Context) {
	src := a.squareSource()
	if src == "" {
		a.scrollPage(c, func() {
			a.songlistsHeader(c)
			a.empty(c, "没有可浏览歌单的音源，请在设置中开启网易云音乐")
		})
		return
	}
	if a.square == nil || a.square.src != src {
		a.square = &squareState{src: src, tag: -1}
		a.loadSquare()
	}
	s := a.square
	a.scrollPage(c, func() {
		a.songlistsHeader(c)
		if len(a.reg.SearchedSources()) > 1 {
			a.squareSources(c)
		}
		switch {
		case s.tagsErr != "" && !s.tagsLoaded:
			a.inlineError(c, s.tagsErr, a.loadSquare)
			return
		case !s.tagsLoaded:
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 156)*2, 156, false)
			return
		}
		a.squareTags(c, s)
		if s.page.err != "" && len(s.lists) == 0 {
			a.inlineError(c, s.page.err, func() { a.loadSquareLists(s, a.squareSquare(s)) })
			return
		}
		if !s.listsLoaded() {
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 156)*2, 156, false)
			return
		}
		if len(s.lists) == 0 {
			a.empty(c, "这个分类下还没有歌单")
			return
		}
		ui.Grid(c).Columns(a.columns(c, 156)).GapX(20).GapY(28).Children(func() {
			for i := range s.lists {
				a.playlistCard(c.Key(playlistKey(s.lists[i].Src(), s.lists[i].ID)), &s.lists[i])
			}
		})
		a.pageFoot(c, &s.page, a.moreSquare)
	})
}

// inlineError says what failed, with a button to try again, where a
// scrolled page has room for it rather than centered over the whole of it.
func (a *app) inlineError(c *ui.Context, msg string, retry func()) {
	p := a.pal
	ui.Row(c).Padding(40, 0).Gap(12).Center().Children(func() {
		ui.Text(c, msg).TextColor(p.muted).FontSize(13)
		if retry != nil && ui.Button(c.Key("retry"), "重试").Clicked() {
			retry()
		}
	})
}

// songlistsHeader heads the 歌单 page, with the way to follow a playlist
// by its link beside it.
func (a *app) songlistsHeader(c *ui.Context) {
	p := a.pal
	ui.Row(c).AlignItems(ui.End).Margin(0, 0, 18, 0).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, "按分类浏览").FontSize(12).FontWeight(600).TextColor(p.muted).LetterSpacing(0.4)
			ui.Text(c, "歌单").FontSize(30).FontWeight(700).LetterSpacing(-0.6)
		})
		if pillButton(c.Key("subscribe-open"), iconPlus, "订阅歌单", p.hover.Over(p.bg), p.text).Clicked() {
			a.openSubscribeDialog()
		}
	})
}

// squareSources switches the square between the sources that have one, as
// the source the page shows.
func (a *app) squareSources(c *ui.Context) {
	var with []string
	var labels []string
	at := 0
	for _, id := range a.reg.SearchedSources() {
		p, ok := a.reg.Get(id)
		if !ok {
			continue
		}
		if _, ok := p.(source.Square); !ok {
			continue
		}
		if a.square != nil && a.square.src == id {
			at = len(with)
		}
		with = append(with, id)
		labels = append(labels, p.Name())
	}
	if len(with) < 2 {
		return
	}
	ui.Row(c).Margin(0, 0, 18, 0).Children(func() {
		if segmented(c.Key("square-src"), &at, labels...) {
			// Chosen, it stays the page's, until it is turned off or taken
			// away.
			a.squarePick = with[at]
			a.square = &squareState{src: with[at], tag: -1}
			a.loadSquare()
		}
	})
}

// squareTags shows the square's categories, the one chosen lit.
func (a *app) squareTags(c *ui.Context, s *squareState) {
	p := a.pal
	ui.Row(c).Wrap().Gap(8).GapY(8).Margin(20, 0, 22, 0).Children(func() {
		chips := append([]music.Tag{{Name: "全部"}}, s.tags...)
		for i := range chips {
			tag := i - 1
			chosen := s.tag == tag
			// Each chip a key of its own: a page built twice, as it is redrawn,
			// must find the same ones.
			b := ui.ButtonBase(c.Key("tag-" + chips[i].Name)).Height(28).PaddingX(12).Radius(14).FocusRing(false).Label(chips[i].Name)
			color := p.muted
			switch {
			case chosen:
				b.Background(p.accent)
				color = p.onAccent
			case b.Hovered():
				b.Background(p.hover)
				color = p.text
			}
			// The chip's own text: a base has no look of its own, and a
			// label alone leaves the pill empty.
			b.Children(func() {
				ui.Text(c, chips[i].Name).FontSize(12.5).FontWeight(500).TextColor(color).SingleLine()
			})
			if b.Clicked() {
				a.chooseSquareTag(s, tag)
			}
		}
	})
}
