package app

import (
	"runtime"
	"strconv"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// sidebarWidth is how wide the sidebar shows until the listener drags its
// edge.
const sidebarWidth = 232

// sidebarMin is the least wide the sidebar shows: dragged narrower than
// it, it hides, as a sidebar of Music's does.
const sidebarMin = 160

// railWidth is how wide the sidebar shows folded at the least: a rail of
// the icons of its pages, their names a caption under them, without the
// room the names beside the icons would take. On macOS it widens to clear
// the traffic lights, which sit over the rail: a rail narrower than their
// room would leave them hanging over the line it shares with the page.
const railWidth = 76

// railIcon is how large a page's icon shows in the folded rail: large
// enough that the rail reads as a strip of pictures, with the name under it
// as its caption.
const railIcon = 21

// railLabelSize is how large a page's name is under its icon in the rail:
// small enough that a name like 为你推荐 fits the rail's width, and large
// enough to read as a caption rather than a speck.
const railLabelSize = 10

// railItem is how tall a page shows in the rail: its icon, the caption
// under it, and the room around both.
const railItem = 52

// sidebarView returns how wide the sidebar shows now, and whether it
// shows: hidden, or dragged shut, the page takes the window.
//
// It is what the frame was built as: folding the sidebar takes effect on
// the next one, so that the width the page is laid out with and the
// sidebar it is laid out beside do not disagree within a frame.
func (a *app) sidebarView() (width float32, shown bool) {
	if a.railFrame {
		return a.railWidth(), !a.sideHidden
	}
	width = a.sideWidth
	if width < sidebarMin {
		// Never dragged, or dragged shut: as wide as it shows until the
		// listener changes it.
		width = sidebarWidth
	}
	return width, !a.sideHidden
}

// railWidth returns how wide the rail shows: railWidth, or the room the
// window's controls take over the sidebar's left, whichever is wider, so
// that the traffic lights sit over the rail rather than over the line it
// shares with the page. The controls' room is the frame's, kept as the
// frame was built.
func (a *app) railWidth() float32 {
	w := float32(railWidth)
	if a.barLeft > 0 {
		// Their room, and the rail's own around them.
		w = max(w, a.barLeft+2*railEdge)
	}
	return w
}

// railEdge is the room the rail leaves at its sides.
const railEdge = 8

// keepSidebarWidth keeps the width the split was last dragged to: a drag
// narrower than it may show shuts the sidebar, and a width is saved as the
// drag settles — no oftener than twice a second, as every frame of a drag
// writes a new one.
func (a *app) keepSidebarWidth() {
	if !a.sideDirty || a.railFrame {
		return
	}
	if a.sideWidth < sidebarMin {
		// Dragged shut, as a sidebar of Music's is: it goes, and the width
		// it comes back to is the one it had.
		a.sideWidth = a.settings.Sidebar
		if a.sideWidth < sidebarMin {
			a.sideWidth = sidebarWidth
		}
		if !a.sideHidden {
			a.sideHidden = true
			a.settings.SidebarHidden = true
			a.settings.save()
		}
		a.sideDirty = false
		return
	}
	if a.sideWidth == a.settings.Sidebar {
		a.sideDirty = false
		return
	}
	now := time.Now()
	if now.Sub(a.sideSavedAt) >= 500*time.Millisecond {
		a.settings.Sidebar = a.sideWidth
		a.settings.save()
		a.sideSavedAt = now
	}
}

// toggleSidebar hides or shows the sidebar, and keeps it so: shown, it
// opens at the width it was, folded or not.
func (a *app) toggleSidebar() {
	a.sideHidden = !a.sideHidden
	a.settings.SidebarHidden = a.sideHidden
	if !a.sideHidden {
		// Shown again, it unfolds: a rail is what a fold makes of it, and
		// the toggle over the page asks for the whole sidebar.
		a.sideRail = false
		a.settings.SidebarRail = false
	}
	a.settings.save()
	a.redraw()
}

func (a *app) view(c *ui.Context) {
	t := *c.Theme()
	p := a.paletteFor(&t, c)
	a.pal = p
	t.Accent, t.AccentText = p.accent, p.onAccent
	t.Selection = p.accent.Alpha(0.3)
	if p.immersive {
		// The theme is the dark one's, over the cover's backdrop: what the
		// window draws with it is light, and the controls' surfaces are
		// veils of white.
		t.Dark = true
		t.Background = p.bg
		t.Surface, t.SurfaceHover, t.SurfacePressed = p.hover, p.press, p.press
		t.Border = p.hairline
		t.Text, t.TextMuted = p.text, p.muted
	}
	c.SetTheme(&t)
	a.setWindowColor(p)

	// The fold a click asks for takes effect on the next frame: this one
	// is built as the fold was when it began, all of it together.
	a.railFrame = a.sideRail
	// The room the window's controls take, for the rail to clear: kept as
	// the frame is built, since the width the panes are laid out with is
	// asked for without the frame.
	a.barLeft = c.TitleBar().Left

	a.shortcuts(c)

	if c.Vibrancy() && !p.immersive {
		// Following the player, the window is painted rather than showing
		// the system's material: the backdrop is the window's background,
		// and the sidebar has a shade of its own over it.
		c.Root().Background(ui.Transparent)
	}
	root := ui.Box(c).Fill()
	if p.immersive {
		// The playing song's backdrop, under everything: the window is
		// painted the way Now Playing paints itself, so that the two read as
		// one place. It is painted here, rather than within the page, so that
		// the sidebar and the bars are over it too.
		still := c.Preferences().ReduceMotion
		art, tint := a.coverArt, a.tint
		if c.Vibrancy() {
			c.Root().Background(ui.Transparent)
		}
		root.Draw(func(pa *ui.Painter, r ui.Rect) {
			a.paintBackdrop(pa, r, &a.pageBackdrop, art, tint, still, false)
		})
	}
	if !c.Focused(a.searchBox) {
		// Space plays and pauses wherever the focus is, as in Music. On
		// the container around everything, it comes before a button the
		// pointer focused, which would take Space as a click; on the
		// window, it would come after.
		root.OnShortcut(0, ui.KeySpace, a.pl.TogglePlay)
		// And where nothing has the focus, as once Now Playing covers the
		// button that had it.
		c.OnShortcut(0, ui.KeySpace, a.pl.TogglePlay)
	}
	npOpen, npSettled := a.npState(c)
	root.Children(func() {
		page := ui.Row(c).Fill().AlignItems(ui.Stretch)
		page.Children(func() {
			if npOpen && npSettled {
				// Covered by Now Playing: not built, which would cost each
				// of its frames as much again as the stage itself. Where
				// it is scrolled is the app's (pageScroll, navScroll,
				// trackList), so it shows again as it was.
				return
			}
			a.mainPanes(c)
		})
		a.nowPlayingLayer(c, npOpen, npSettled)
	})
	a.loginDialog(c)
	a.playlistNameDialog(c)
	a.deletePlaylistDialog(c)
	a.subscribeDialogView(c)
	if a.toast != "" {
		c.Toast(a.toast)
		a.toast = ""
	}
	a.syncWindows()
}

// mainPanes lays the sidebar beside the page, with the edge between them
// that the listener drags, or the page alone while the sidebar is hidden.
func (a *app) mainPanes(c *ui.Context) {
	p := a.pal
	width, shown := a.sidebarView()
	page := func() {
		// Keyed, so that a popover of the sidebar opening or closing
		// keeps what the page and its bars hold, their edges' fades. The
		// line beside the sidebar is the split's own divider, which the
		// pointer takes hold of.
		// Following the player, the page has no fill of its own: the window's
		// backdrop, which is the song's, shows through it.
		fill := p.bg
		if p.immersive {
			fill = ui.Transparent
		}
		box := ui.Box(c.Key("main")).Grow(1).Background(fill)
		box.Children(func() {
			a.content(c)
			// The bars float over the page, which scrolls under them:
			// positioned, they paint over it.
			a.pageTop(c)
			a.playerBar(c)
		})
	}
	if !shown {
		page()
		return
	}
	side := func() {
		col := ui.Column(c).Fill()
		// Folded, the sidebar draws nothing of its own color: it is the
		// page's, and the icons alone say where it is. Unfolding it brings
		// the color back, fading in with the names, and folding it takes
		// it out again.
		if !c.Vibrancy() || p.immersive {
			target := p.sidebar
			if a.railFrame {
				target = p.bg
			}
			col.Background(col.AnimateColor("canvas", target, ui.Animation{Duration: 220 * time.Millisecond}))
		}
		col.Children(func() { a.sidebar(c) })
	}
	// The split keeps the width it is dragged to, and moves between the
	// rail's width and the listener's as the sidebar folds and unfolds:
	// folded, it is not theirs to drag, the rail being one width.
	w := width
	ui.Split(c.Key("split"), &w, side, page).Fill()
	if a.railFrame {
		// Nothing is dragged while folded: the rail is its width, and the
		// width the sidebar comes back to is kept from before.
		if w != a.railWidth() {
			a.sideWidth = a.railWidth()
		}
		a.sideDirty = false
		return
	}
	if w != width {
		a.sideWidth = w
		a.sideDirty = true
	}
	a.keepSidebarWidth()
}

func (a *app) shortcuts(c *ui.Context) {
	// The side buttons of a mouse, the keys of keyboards that have them
	// and swipes between pages, and the keys of Music.
	c.OnShortcut(0, ui.KeyBack, a.back)
	c.OnShortcut(0, ui.KeyForward, a.goForward)
	c.OnShortcut(ui.Cmd, ui.KeyBracketLeft, a.back)
	c.OnShortcut(ui.Cmd, ui.KeyBracketRight, a.goForward)
	c.OnShortcut(ui.Cmd, ui.KeyF, func() {
		a.nowPlaying = false
		a.searchBox.Focus(c)
	})
	if a.nowPlaying {
		c.OnShortcut(0, ui.KeyEscape, func() { a.nowPlaying = false })
		// ⌘ and a number choose Now Playing's tab of that place.
		if a.pl.Current() != nil {
			for i, t := range a.npTabs() {
				c.OnShortcut(ui.Cmd, ui.Key1+ui.Key(i), func() { a.choosePane(t.pane) })
			}
		}
	}
}

// go opens a page, remembering the one before for the back button, and
// closes Now Playing over it, though it is the page open.
func (a *app) open(r route) {
	a.nowPlaying = false
	if r == a.route {
		return
	}
	a.history, a.forward = append(a.history, a.route), nil
	a.show(r)
}

// show shows the page of r.
func (a *app) show(r route) {
	a.route = r
	if r.page == pageSearch {
		a.query, a.searched = r.query, r
	}
	a.load(r)
}

// load loads what the page of r shows, unless it has.
func (a *app) load(r route) {
	// With NetEase's catalogue off, its pages are not asked for what they
	// would show.
	if neteaseOnly(r.page) && !a.searchesOn(music.SourceNetease) {
		return
	}
	switch r.page {
	case pageHome:
		a.freshenHome()
	case pagePlaylist:
		a.loadPlaylist(r.src, r.id, false)
	case pageSearch:
		a.loadSearch(r, false)
	case pageArtist:
		a.loadArtist(r.id)
	case pageAlbum:
		a.loadAlbum(r.id)
	case pageUser:
		a.loadUser(r.id)
	case pagePodcast:
		a.loadPodcast(r.id)
	case pagePodcasts:
		a.loadPodcastTab()
	case pageSonglists:
		a.loadSquare()
	}
}

// back goes to the page before, as the back button of a mouse does, or
// closes Now Playing, which covers the pages.
func (a *app) back() {
	if a.nowPlaying {
		a.nowPlaying = false
		return
	}
	if len(a.history) == 0 {
		return
	}
	a.forward = append(a.forward, a.route)
	r := a.history[len(a.history)-1]
	a.history = a.history[:len(a.history)-1]
	a.show(r)
}

// goForward goes to the page back left, as the forward button of a mouse
// does.
func (a *app) goForward() {
	if len(a.forward) == 0 {
		return
	}
	a.history = append(a.history, a.route)
	r := a.forward[len(a.forward)-1]
	a.forward = a.forward[:len(a.forward)-1]
	a.nowPlaying = false
	a.show(r)
}

// sidebar is the sidebar's column: the fold at its head, beside the window
// buttons, the pages under it, and nothing of its own at its foot: who
// listens is the settings' business.
func (a *app) sidebar(c *ui.Context) {
	rail := a.railFrame
	side := float32(14)
	if rail {
		side = 8
	}
	// The fold sits in the strip with the window buttons, at the right of
	// them, rather than below it: the head of the sidebar is the same strip
	// the page's search is in, so the two read as one row across the
	// window. Where the controls are not over the sidebar, the head keeps
	// clear of the sides alone.
	gap := float32(4)
	headH := topBar(c)
	// Where nothing in it takes the press, the sidebar moves the window, as
	// the title bar it reaches up into would.
	ui.Column(c).Fill().Padding(0, side, 14, side).Gap(gap).DragWindow().Children(func() {
		a.sidebarHead(c, headH)
		ui.Scroll(c.Key("nav")).TrackScroll(&a.navScroll).Grow(1).Margin(0, -6).Padding(0, 6).Gap(4).Children(func() {
			// What NetEase alone has shows while it is asked: with it off,
			// the sidebar is the 歌单 page, the search, and the playlists
			// followed from the other sources.
			// NetEase's own pages: its catalogue, not the songs of it that
			// the app streams, which is another switch.
			if a.searchesOn(music.SourceNetease) {
				a.navItem(c, iconHome, "为你推荐", route{page: pageHome})
				a.navItem(c, iconDaily, "每日推荐", route{page: pageDaily})
				a.navItem(c, iconRadio, "私人 FM", route{page: pageFM})
				a.navItem(c, iconCharts, "排行榜", route{page: pageCharts})
			}
			a.navItem(c, iconPlaylist, "歌单", route{page: pageSonglists})
			if a.searchesOn(music.SourceNetease) {
				a.navItem(c, iconPodcast, "播客", route{page: pagePodcasts})
			}
			if a.searched.query != "" {
				a.navItem(c, iconSearch, "搜索结果", a.searched)
			}
			// The playlists are lists of names: folded, the rail keeps the
			// pages, and they come back with it.
			if !rail {
				a.myPlaylists(c, "我的歌单", a.mine, true, &a.mineCollapsed)
				a.myPlaylists(c, "收藏的歌单", a.saved, false, &a.savedCollapsed)
				a.myPlaylists(c, "订阅的歌单", a.subscribedLists(), false, &a.subsCollapsed)
			}
		})
	})
}

// sidebarHead is the room the strip along the top of the sidebar takes: the
// window buttons sit over it where they do (the traffic lights on macOS),
// and the pages start under it. The fold is not here: it is the strip over
// the page's, at the window's right, where the listener's hand finds it
// whatever the sidebar is doing.
func (a *app) sidebarHead(c *ui.Context, height float32) {
	ui.Box(c).Height(height)
}

// foldButton is the sidebar's one toggle, at the window's top right: it
// hides the sidebar, folds it to its rail of icons, or unfolds it again, by
// what it is doing now.
func (a *app) foldButton(c *ui.Context) {
	p := a.pal
	rail := a.railFrame
	icon, label := iconSidebar, "折叠侧边栏"
	switch {
	case a.sideHidden:
		// Hidden, the button brings it back whole: a fold would leave the
		// rail's icons without a reason.
		label = "显示侧边栏"
	case rail:
		icon, label = iconSidebarOpen, "展开侧边栏"
	}
	b := iconButton(c.Key("rail"), icon, 17, p.muted, label)
	if b.Hovered() {
		b.Background(p.hover)
	}
	if b.Clicked() {
		switch {
		case a.sideHidden:
			a.toggleSidebar()
		default:
			a.setSidebarRail(!rail)
		}
	}
}

// setSidebarRail folds the sidebar to a rail, or unfolds it, and keeps it
// so.
func (a *app) setSidebarRail(rail bool) {
	a.sideRail = rail
	a.settings.SidebarRail = rail
	a.settings.save()
	a.redraw()
}

// navItem is a page of the sidebar: its icon and its name. Folded to the
// rail, the icon grows to stand on its own and the name under it shrinks to
// a caption, so that the rail still says which page is which without
// taking the width the names need.
func (a *app) navItem(c *ui.Context, icon *ui.SVG, label string, r route) {
	p := a.pal
	selected := a.route.page == r.page && !a.nowPlaying
	rail := a.railFrame
	b := ui.ButtonBase(c.Key(label)).FocusRing(false).AlignItems(ui.Center)
	if rail {
		// Folded, the page is a tile: the icon over its name, across the
		// rail's own width, taller than a row so that both fit. The width
		// is the rail's now, not the least it shows at: on macOS the rail
		// widens to clear the traffic lights, and a tile sized from the
		// constant would sit off centre in it.
		b = b.Width(a.railWidth() - 2*railEdge).Height(railItem).Radius(10).Justify(ui.Center).Gap(3).
			Column().PaddingX(2).Label(label).Tooltip(label)
	} else {
		b = b.Height(34).PaddingX(10).Gap(10).Radius(8).Justify(ui.Start)
	}
	bg := ui.Transparent
	switch {
	case selected:
		bg = p.press
	case b.Hovered():
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	color := p.text
	if selected {
		color = p.accent
	}
	b.Children(func() {
		size := float32(17)
		if rail {
			size = railIcon
		}
		ui.Icon(c, icon).Size(size, size).TextColor(color)
		if !rail {
			ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.text)
			return
		}
		// Folded, the name is a caption under the icon: small enough to fit
		// the rail, and in the muted color so that the icon leads.
		ui.Text(c, label).FontSize(railLabelSize).FontWeight(500).
			TextColor(mutedWhen(selected, p)).SingleLine()
	})
	if b.Clicked() {
		a.open(r)
	}
}

// mutedWhen returns the color a rail's caption takes: the accent while its
// page is chosen, and the muted text otherwise.
func mutedWhen(selected bool, p palette) ui.Color {
	if selected {
		return p.accent
	}
	return p.muted
}

// playerBarHeight is the height of the player along the foot of the pages.
const playerBarHeight = 84

// pageGap is the room between the bar over a page and its content.
const pageGap = 12

// pageWidth is the room a page has across: the window's, less the sidebar
// while it shows.
func (a *app) pageWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	if width, shown := a.sidebarView(); shown {
		w -= width
	}
	return w
}

// insets returns the room the bars over a page take at its top and its
// bottom: pages pad their content by it, and scroll under the bars.
func (a *app) insets(c *ui.Context) (top, bottom float32) {
	top = topBar(c)
	if runtime.GOOS == "darwin" {
		// The edge reaches a little below the traffic lights' strip.
		top += topEdgePad
	}
	return top, playerBarHeight
}

// topEdgePad is the room the edge over a page leaves below the strip, on
// macOS.
const topEdgePad = 6

// buttonsHeight is the height of the window buttons off macOS, which fill
// the edge over a page.
const buttonsHeight = topBarHeight + topEdgePad

// topBar returns the height of the strip along the top of the window, as
// tall as the window buttons and the room above and below them, which
// what sits in it centers on.
func topBar(c *ui.Context) float32 {
	if runtime.GOOS != "darwin" {
		// The whole edge, which the buttons fill: what sits in the strip
		// centers on their glyphs.
		return buttonsHeight
	}
	return max(c.TitleBar().Height, topBarHeight)
}

// topBarHeight is the least height of the strip along the top.
const topBarHeight = 40

// startMark marks where a page's content starts, first in what scrolls:
// once it has moved up, content is under the bar at the top.
func (a *app) startMark(c *ui.Context) {
	b := ui.Box(c.Key("start-mark")).Height(0).Bounds()
	if b.W > 0 {
		top, _ := a.insets(c)
		a.underTop = b.Y < top+pageGap-0.5
	}
}

// endMark marks where a page's content ends, last in what scrolls: while it
// is below the top of the player, content is under the player.
func (a *app) endMark(c *ui.Context) {
	b := ui.Box(c.Key("end-mark")).Height(0).Bounds()
	if b.W > 0 {
		_, h := c.Size()
		_, bottom := a.insets(c)
		a.underBottom = b.Y > h-bottom+0.5
	}
}

// edgeShown returns how much of a scroll edge shows over el: fully while
// content is under its bar, and none at rest, as AppKit shows its edges.
func edgeShown(el ui.Element, under bool) float32 {
	target := float32(0)
	if under {
		target = 1
	}
	return el.AnimateTo("shown", target, ui.Animation{Duration: 180 * time.Millisecond})
}

// pageTop is the strip over the top of a page, frosting what scrolls under
// it as macOS 26's hard scroll edge does: the search field sits in it, the
// buttons back and forward where there is a page to go back to, and the
// sidebar's toggle at its end. It drags the window where nothing in it
// takes the press.
func (a *app) pageTop(c *ui.Context) {
	p := a.pal
	top, _ := a.insets(c)
	edge := ui.Box(c.Key("top-edge")).Absolute().Top(0).Left(0).Right(0).Height(top).DragWindow().
		Material(glass.ScrollEdge{Hard: true, Background: p.bg})
	edge.Opacity(edgeShown(edge, a.underTop))
	bar := ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(top).AlignItems(ui.Center).DragWindow()
	// Where the window controls sit over the page — the sidebar hidden,
	// under them — the strip starts after them.
	left, right := float32(16), float32(16)
	if _, shown := a.sidebarView(); !shown {
		if tb := c.TitleBar(); tb.Left > 0 {
			left = tb.Left + 8
		}
	}
	if tb := c.TitleBar(); tb.Right > 0 {
		right = tb.Right + 8
	}
	bar.Padding(0, left, 0, right).Gap(10)
	bar.Children(func() {
		if len(a.history) > 0 || len(a.forward) > 0 {
			back := iconButton(c.Key("back"), iconBack, 18, p.muted, "返回").Disabled(len(a.history) == 0)
			if len(a.history) == 0 {
				back.Opacity(0.35)
			}
			if back.Clicked() {
				a.back()
			}
			if len(a.forward) > 0 && iconButton(c.Key("forward"), iconForward, 18, p.muted, "前进").Clicked() {
				a.goForward()
			}
			ui.Box(c).Width(1).Height(18).Background(p.hairline)
		}
		// The search is always here, over the page, at the strip's own
		// start: it stays where it is as the sidebar folds and unfolds,
		// rather than moving between the two. A narrow window gives it less
		// room rather than pushing the fold off the strip's end.
		a.searchField(c).Width(320).Shrink(1).MinWidth(140)
		ui.Spacer(c)
		// The fold is the strip's last control, at the window's top right:
		// it does not move as the sidebar folds, hides or shows, and it is
		// over the page rather than over the rail, where the traffic lights
		// leave it the room to sit clear of the sidebar's edge.
		ui.Box(c).Width(1).Height(18).Background(p.hairline)
		a.foldButton(c)
	})
}

// neteaseOnly reports whether a page shows what only NetEase has: the
// day's songs, the charts, the podcasts, and the artists, albums and users
// searched among its catalogue.
func neteaseOnly(page pageKind) bool {
	switch page {
	case pageHome, pageDaily, pageCharts, pageFM, pageArtist, pageAlbum, pageUser, pagePodcast, pagePodcasts:
		return true
	}
	return false
}

func (a *app) content(c *ui.Context) {
	// A new page starts at its top. Not as it is opened: a control in a
	// list, as a page's tabs in its header, opens it as the list builds.
	if a.scrolled != a.route {
		a.trackList, a.pageScroll, a.scrolled = ui.ListState{}, ui.ScrollState{}, a.route
	}
	// Pages mark where their content is, as they scroll.
	a.underTop, a.underBottom = false, false
	// With NetEase off, its pages have nothing to show, and say so rather
	// than sit empty: the sources the listener asks are in 歌单 and the
	// search.
	if neteaseOnly(a.route.page) && !a.searchesOn(music.SourceNetease) {
		a.centered(c, "网易云音乐的曲库已在设置中关闭，这个页面由它提供", func() {
			a.open(route{page: pageSonglists})
		})
		return
	}
	switch a.route.page {
	case pageHome:
		a.homePage(c)
	case pageDaily:
		a.dailyPage(c)
	case pageCharts:
		a.chartsPage(c)
	case pagePlaylist:
		a.playlistPage(c)
	case pageSearch:
		a.searchPage(c)
	case pageFM:
		a.fmPage(c)
	case pageArtist:
		a.artistPage(c)
	case pageAlbum:
		a.albumPage(c)
	case pageUser:
		a.userPage(c)
	case pagePodcast:
		a.podcastPage(c)
	case pagePodcasts:
		a.podcastsPage(c)
	case pageSonglists:
		a.songlistsPage(c)
	}
}

// centered shows a message, with a retry button if retry is set: what a
// page shows in place of what failed to load.
func (a *app) centered(c *ui.Context, err string, retry func()) {
	top, bottom := a.insets(c)
	ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(12).Children(func() {
		ui.Text(c, err).TextColor(a.pal.muted).FontSize(13)
		if retry != nil && ui.Button(c, "重试").Clicked() {
			retry()
		}
	})
}

// Loading what the pages show.

func (a *app) loadHome() {
	if !a.searchesOn(music.SourceNetease) {
		// Its pages are NetEase's alone: with its catalogue off there is
		// nothing to ask it for, and it is not asked.
		return
	}
	a.homeLoading, a.homeErr = true, ""
	signedIn := a.profile != nil
	at := time.Now()
	go func() {
		featured, err1 := netease.FeaturedPlaylists(30)
		charts, err2 := netease.Charts()
		daily, _ := netease.DailySongs()
		var rec []netease.Playlist
		if signedIn {
			rec, _ = netease.RecommendedPlaylists()
		}
		a.post(func() {
			a.homeLoading = false
			if err1 != nil && err2 != nil {
				// Loading the new day's again, what shows stays, to try
				// once more.
				if len(a.featured) == 0 && len(a.charts) == 0 {
					a.homeErr = "无法连接网易云音乐，请检查网络"
				}
				return
			}
			a.featured, a.charts, a.daily, a.recPlaylists = featured, charts, daily, rec
			a.homeAt = at
		})
	}()
}

// freshenHome loads the home page again once NetEase has picked the day's
// songs anew since it last did: the app may stay open overnight.
func (a *app) freshenHome() {
	if !a.homeLoading && !a.homeAt.IsZero() && dailyRenewed(a.homeAt, time.Now()) {
		a.loadHome()
	}
}

// beijing is NetEase's time, without daylight saving.
var beijing = time.FixedZone("CST", 8*60*60)

// dailyRenewed reports whether NetEase has picked new daily songs between
// loaded and now. It does at 6:00, Beijing time.
func dailyRenewed(loaded, now time.Time) bool {
	b := now.In(beijing)
	six := time.Date(b.Year(), b.Month(), b.Day(), 6, 0, 0, 0, beijing)
	if b.Before(six) {
		six = six.AddDate(0, 0, -1)
	}
	return loaded.Before(six)
}

// loadPlaylist loads playlist id of source src, src empty for NetEase's.
func (a *app) loadPlaylist(src string, id int64, thenPlay bool) {
	key := playlistKey(src, id)
	if pl := a.playlists[key]; pl != nil {
		if thenPlay {
			a.pl.PlayQueue(pl.Tracks, firstPlayable(pl.Tracks))
		}
		return
	}
	if a.loadingPl[key] {
		return
	}
	a.loadingPl[key] = true
	delete(a.plErr, key)
	go func() {
		pl, err := a.fetchPlaylist(src, id)
		a.post(func() {
			delete(a.loadingPl, key)
			if err != nil {
				a.plErr[key] = "歌单加载失败"
				return
			}
			a.playlists[key] = pl
			if thenPlay {
				a.pl.PlayQueue(pl.Tracks, firstPlayable(pl.Tracks))
			}
		})
	}()
}

// fetchPlaylist asks the source a playlist is from for it.
func (a *app) fetchPlaylist(src string, id int64) (*netease.Playlist, error) {
	if src == "" || src == music.SourceNetease {
		return netease.PlaylistDetail(id)
	}
	if a.reg == nil {
		return nil, source.ErrNoSource
	}
	p, ok := a.reg.Get(src)
	if !ok {
		return nil, source.ErrUnknownSource
	}
	// Not every source names its playlists: Migu's are the app's, and it
	// streams its songs without opening them.
	l, ok := p.(source.Lister)
	if !ok {
		return nil, source.ErrUnknownSource
	}
	return l.Playlist(strconv.FormatInt(id, 10))
}

// playlistKey names a playlist the app keeps as loaded: the source's, and
// its id there.
func playlistKey(src string, id int64) string {
	return src + "/" + strconv.FormatInt(id, 10)
}

func firstPlayable(tracks []netease.Track) int {
	for i := range tracks {
		if tracks[i].Playable() {
			return i
		}
	}
	return 0
}
