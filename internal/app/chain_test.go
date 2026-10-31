package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// chainTestApp is an app with the sources the app ships, and the chain
// built from them, as Run does.
func chainTestApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp()
	a.loadRegistry()
	return a
}

// The chain has every source the app ships that streams, NetEase among
// them, and only the ones the listener asks. A catalogue that streams none
// of its songs is not a link: it is asked for its songs' words alone.
func TestChainOfTheSourcesOn(t *testing.T) {
	a := chainTestApp(t)
	order := a.chain.Order()
	for _, id := range []string{music.SourceNetease, music.SourceKuwo, music.SourceQQ, music.SourceMigu} {
		if !slices.Contains(order, id) {
			t.Errorf("chain %q has no %s", order, id)
		}
	}
	if slices.Contains(order, music.SourceKugou) {
		t.Errorf("chain %q streams Kugou, which it cannot", order)
	}
	// Turned off, a source is not tried, and keeps its place in the list.
	a.setSourceCap(music.SourceNetease, source.CapStream, false)
	if got := a.chain.Order(); slices.Contains(got, music.SourceNetease) {
		t.Errorf("chain %q still has NetEase", got)
	}
	if !slices.Contains(a.sourceOrder(), music.SourceNetease) {
		t.Error("the settings' list lost NetEase")
	}
	a.setSourceCap(music.SourceNetease, source.CapStream, true)
	if got := a.chain.Order(); !slices.Contains(got, music.SourceNetease) {
		t.Errorf("chain %q, want NetEase back", got)
	}
}

// The listener's order is what the chain tries the sources in, and it is
// kept in the settings.
func TestChainFollowsTheListenersOrder(t *testing.T) {
	a := chainTestApp(t)
	a.moveSource(music.SourceKuwo, true)
	order := a.chain.Order()
	if len(order) < 2 || order[0] != music.SourceKuwo {
		t.Fatalf("chain %q, want Kuwo first", order)
	}
	if len(a.settings.SourceOrder) < 2 || a.settings.SourceOrder[0] != music.SourceKuwo {
		t.Errorf("settings keep %q", a.settings.SourceOrder)
	}
	// The ends of the list cannot move further.
	a.moveSource(music.SourceKuwo, true)
	if got := a.chain.Order(); got[0] != music.SourceKuwo {
		t.Errorf("the first source moved: %q", got)
	}
	a.moveSource(music.SourceKuwo, false)
	if got := a.chain.Order(); got[0] == music.SourceKuwo {
		t.Errorf("the source did not move down: %q", got)
	}
}

// A search asks every source at once, and its results take turns between
// the sources rather than one source's page before another's.
func TestSearchTakesTurns(t *testing.T) {
	a := chainTestApp(t)
	if a.reg == nil {
		t.Fatal("no registry")
	}
	// The sources are replaced by fakes, which say what they found.
	one := &turnSource{id: "a", tracks: []music.Track{{Source: "a", ID: 1}, {Source: "a", ID: 2}, {Source: "a", ID: 3}}}
	two := &turnSource{id: "b", tracks: []music.Track{{Source: "b", ID: 4}, {Source: "b", ID: 5}}}
	r := source.NewRegistry(one, two)
	res, err := r.SearchSongs("x", 10, 0)
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}
	var got []int64
	for _, s := range res.Songs {
		got = append(got, s.ID)
	}
	if !slices.Equal(got, []int64{1, 4, 2, 5, 3}) {
		t.Errorf("songs %v, want the sources' taking turns", got)
	}
}

// turnSource is a provider for the tests, which finds what it is given.
type turnSource struct {
	id     string
	tracks []music.Track
}

func (s *turnSource) ID() string   { return s.id }
func (s *turnSource) Name() string { return s.id }
func (s *turnSource) SearchSongs(string, int, int) ([]music.Track, int, error) {
	return s.tracks, len(s.tracks), nil
}
func (s *turnSource) Playlist(string) (*music.Playlist, error) { return nil, source.ErrUnknownSource }
func (s *turnSource) Stream(music.Track, string) (*music.Stream, error) {
	return nil, source.ErrNotPlayable
}
func (s *turnSource) Lyrics(music.Track) (music.Lyrics, error) {
	return music.Lyrics{}, source.ErrNotPlayable
}

// The search page filters its results by search source: together, as
// searched, or one platform's alone, as lx-music-desktop offers them.
func TestSearchFiltersBySource(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.reg = source.NewRegistry(
		&turnSource{id: "a", tracks: []music.Track{{Source: "a", ID: 1}}},
		&turnSource{id: "b", tracks: []music.Track{{Source: "b", ID: 2}}},
	)
	a.open(route{page: pageSearch, query: "x", kind: netease.SearchSongs})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// Both sources are searched together at first.
	if !a.reg.Searched("a") || !a.reg.Searched("b") {
		t.Fatal("a source is not searched")
	}
	if err := tt.Click("b"); err != nil {
		t.Fatalf("the source's tab: %v", err)
	}
	tt.Frame()
	if a.route.only != "b" {
		t.Errorf("the page shows %q alone", a.route.only)
	}
	// Back to all of them.
	if err := tt.Click("全部"); err != nil {
		t.Fatalf("the all tab: %v", err)
	}
	tt.Frame()
	if a.route.only != "" {
		t.Errorf("the page still shows %q alone", a.route.only)
	}
}

// The sidebar shows at first, with the fold and the search at its head,
// and hides and shows again from the strip over the page, keeping how it
// was.
func TestSidebarHidesAndShows(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if _, ok := tt.Find("为你推荐"); !ok {
		t.Fatal("the sidebar's nav is not built")
	}
	// Shown, the fold is the only toggle: the strip over the page leaves
	// the sidebar to show or hide it.
	if _, ok := tt.Find("隐藏侧边栏"); ok {
		t.Error("the strip over the page hides the sidebar as well")
	}
	if err := tt.Click("折叠侧边栏"); err != nil {
		t.Fatalf("the fold: %v", err)
	}
	tt.Frame()
	// The fold is at the head of the rail now, and unfolds it again.
	if err := tt.Click("展开侧边栏"); err != nil {
		t.Fatalf("the fold, to unfold it: %v", err)
	}
	tt.Frame()
	if a.sideRail {
		t.Error("the sidebar did not unfold")
	}
}

// The search is the strip over the page's, whatever the sidebar is doing:
// it stays where it is as the sidebar folds and unfolds, and the sidebar's
// own toggle is at its left.
func TestSearchFieldAtTheTop(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(a *app)
	}{
		{"hidden", func(a *app) { a.sideHidden = true }},
		{"folded", func(a *app) { a.sideRail = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := chainTestApp(t)
			a.art = newArtCache(func(func()) {})
			tc.set(a)
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			if _, ok := tt.Find("搜索"); !ok {
				t.Errorf("the search field is not there with the sidebar %s", tc.name)
			}
		})
	}
}

// The search is the strip over the page's, in the one place, whatever the
// sidebar does: it is never in the sidebar, and never twice.
func TestSearchFieldFollowsTheSidebar(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	tt := ui.NewTester(a.view, 1200, 800)
	for _, tc := range []struct {
		name       string
		rail, hide bool
	}{
		{"shown", false, false},
		{"folded", true, false},
		{"hidden", false, true},
	} {
		a.sideRail, a.sideHidden = tc.rail, tc.hide
		tt.Frame()
		if n := searchFields(tt); n != 1 {
			t.Errorf("the search shows %d times with the sidebar %s", n, tc.name)
		}
		// It is over the page, to the right of the sidebar's own column.
		el, ok := tt.Find("搜索")
		if !ok {
			t.Fatalf("no search field with the sidebar %s", tc.name)
		}
		if width, shown := a.sidebarView(); shown && el.X < width {
			t.Errorf("the search is at X=%v, within the sidebar's %v", el.X, width)
		}
	}
}

// The sidebar folds to a rail of icons from the one toggle at its foot, and
// the foot's other controls stay with it: the picture, the settings and the
// fold, in that order, as the sidebar goes from whole to rail and back.
func TestSidebarToggleIsAtTheFoot(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	fold, ok := tt.Find("折叠侧边栏")
	if !ok {
		t.Fatal("no fold button")
	}
	if fold.Y+fold.H < 800-playerBarHeight-24 {
		t.Errorf("the fold is at Y=%v of 800: it is not at the sidebar's foot", fold.Y)
	}
	// It is in the sidebar, not at the window's top right where it was.
	if fold.X+fold.W > sidebarWidth+80 {
		t.Errorf("the fold ends at X=%v, past the sidebar's %d", fold.X+fold.W, sidebarWidth)
	}
	// Folded, the way back is the same toggle at the foot, now a column:
	// it stays in the foot, where the pointer left it.
	railFold := fold
	a.sideRail = true
	tt.Frame()
	open, ok := tt.Find("展开侧边栏")
	if !ok {
		t.Fatal("no unfold button")
	}
	if open.Y+open.H < 800-playerBarHeight-24 {
		t.Errorf("folded, the unfold is at Y=%v: it left the foot", open.Y)
	}
	if open.X+open.W > railWidth+80 {
		t.Errorf("folded, the unfold ends at X=%v, outside the rail", open.X+open.W)
	}
	if open.Y == railFold.Y && open.X == railFold.X {
		t.Error("folded, the unfold did not move into the rail's column")
	}
	// The strip over the page holds no toggle while the sidebar shows.
	if _, ok := tt.Find("显示侧边栏"); ok {
		t.Error("the page strip shows a toggle while the sidebar does")
	}
	// Hidden, the sidebar has no foot to hold it: the same toggle is the
	// page strip's, to bring it back.
	a.sideRail, a.sideHidden = false, true
	tt.Frame()
	show, ok := tt.Find("显示侧边栏")
	if !ok {
		t.Fatal("no button to show the sidebar again")
	}
	if show.Y > topBarHeight {
		t.Errorf("the way back is at Y=%v, below the page's strip", show.Y)
	}
	if err := tt.Click("显示侧边栏"); err != nil {
		t.Fatalf("the way back: %v", err)
	}
	tt.Frame()
	if a.sideHidden {
		t.Error("the toggle did not bring the sidebar back")
	}
}

// The sidebar's foot holds the listener's picture, the settings and the
// fold, in that order, folded and unfolded: folded, they stack, since three
// buttons do not fit across a rail.
func TestSidebarFootHoldsTheAccountAndTheSettings(t *testing.T) {
	for _, rail := range []bool{false, true} {
		a := chainTestApp(t)
		a.art = newArtCache(func(func()) {})
		a.sideRail = rail
		tt := ui.NewTester(a.view, 1200, 800)
		tt.Frame()
		acc, ok := tt.Find("登录")
		if !ok {
			t.Fatalf("rail %v: the foot has no way to sign in", rail)
		}
		set, ok := tt.Find("设置")
		if !ok {
			t.Fatalf("rail %v: the foot has no settings", rail)
		}
		foldLabel, openLabel := "折叠侧边栏", "展开侧边栏"
		if rail {
			foldLabel, openLabel = openLabel, foldLabel
		}
		fold, ok := tt.Find(foldLabel)
		if !ok {
			t.Fatalf("rail %v: the foot has no fold", rail)
		}
		// The three are the foot's own: they are below the pages.
		if nav, ok := tt.Find("为你推荐"); ok && acc.Y <= nav.Y {
			t.Errorf("rail %v: the picture is at Y=%v, above the pages at %v", rail, acc.Y, nav.Y)
		}
		// In one row unfolded, in one column folded, each above the fold.
		switch {
		case rail:
			if acc.Y >= set.Y || set.Y >= fold.Y {
				t.Errorf("folded, the foot is not stacked picture, settings, fold: %v %v %v", acc.Y, set.Y, fold.Y)
			}
		default:
			if acc.X >= set.X || set.X >= fold.X {
				t.Errorf("unfolded, the foot is not picture, settings, fold across: %v %v %v", acc.X, set.X, fold.X)
			}
			if set.Y != acc.Y {
				t.Errorf("unfolded, the foot's controls are not on one row: %v and %v", acc.Y, set.Y)
			}
		}
		if got, want := a.sideRail, rail; got != want {
			t.Errorf("the frame went on without the fold: %v, want %v", got, want)
		}
	}
}

// A listener who is signed in finds their own page, the settings and signing
// out behind their picture at the foot of the sidebar, which names them
// without taking the room a name would.
func TestThePictureOpensTheAccountMenu(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	a.profile = &netease.Profile{UserID: 1, Nickname: "小明"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// The picture says who it is, and the sidebar shows no name for it.
	if _, ok := tt.Find("小明"); !ok {
		t.Fatal("the foot has no picture of the listener")
	}
	if n := searchFields(tt); n != 1 {
		t.Errorf("the sidebar shows %d searches", n)
	}
	if err := tt.Click("小明"); err != nil {
		t.Fatalf("the picture: %v", err)
	}
	tt.Frame()
	for _, item := range []string{"我的主页", "设置…", "退出登录"} {
		if _, ok := tt.Find(item); !ok {
			t.Errorf("the account menu has no %q", item)
		}
	}
	// Signing out takes the menu with it, and the way in is back.
	if err := tt.Click("退出登录"); err != nil {
		t.Fatalf("signing out: %v", err)
	}
	tt.Frame()
	if a.profile != nil {
		t.Error("the listener is still signed in")
	}
	if _, ok := tt.Find("登录"); !ok {
		t.Error("the foot has no way back in")
	}
	if a.accountOpen {
		t.Error("the menu stayed open after signing out")
	}
}

// The rail clears the window's controls: folded, it is wider than the room
// the traffic lights take, so that they sit over it rather than over the
// line it shares with the page.
func TestTheRailClearsTheTrafficLights(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	a.sideRail = true
	tt := ui.NewTester(a.view, 1200, 800)
	// macOS's traffic lights: 78 from the left, in a band 28 high.
	const lights = 78
	tt.SetTitleBar(ui.TitleBar{Height: 28, Left: lights, Right: 10})
	if w, _ := a.sidebarView(); w <= lights {
		t.Errorf("the rail is %.0f wide: the traffic lights, %d wide, hang over its edge", w, lights)
	}
	// A window without controls over the sidebar keeps the narrow rail.
	start := ui.NewTester(a.view, 1200, 800)
	start.Frame()
	if w, _ := a.sidebarView(); w != railWidth {
		t.Errorf("without controls the rail is %.0f wide, want %d", w, railWidth)
	}
}

// searchFields counts the search fields a frame shows.
func searchFields(tt *ui.Tester) int {
	n := 0
	for _, s := range tt.Texts() {
		if s == "搜索" {
			n++
		}
	}
	return n
}

// The sidebar folds to a rail of icons from the one toggle at its head,
// beside the search: the names go, the icons grow, the search and the
// account's name go with them, and the toggle itself stays, over the
// icons, to unfold it again.
func TestSidebarFoldsToARail(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// Unfolded, a page's row is its name, its icon beside it; and the
	// search is the strip over the page, at the top of the window.
	wide, ok := tt.Find("为你推荐")
	if !ok {
		t.Fatal("the sidebar's nav is not built")
	}
	if wide.H > 20 {
		t.Errorf("the nav item is %.0f tall: it is not a row with its name", wide.H)
	}
	if _, ok := tt.Find("搜索"); !ok {
		t.Error("the window has no search")
	}
	// The sidebar has nothing of the account: signing in is the settings'.
	if strings.Contains(strings.Join(tt.Texts(), " "), "登录网易云音乐") {
		t.Error("the sidebar shows an invitation to sign in")
	}
	if err := tt.Click("折叠侧边栏"); err != nil {
		t.Fatalf("the fold: %v", err)
	}
	tt.Frame()
	if !a.sideRail {
		t.Fatal("the sidebar did not fold")
	}
	if w, _ := a.sidebarView(); w != railWidth {
		t.Errorf("the rail is %.0f wide, want %f", w, float32(railWidth))
	}
	// Folded, a page is a tile: its icon over its name, taller than a row,
	// with the name still there as its caption.
	narrow, ok := tt.Find("为你推荐")
	if !ok {
		t.Fatal("the rail lost its page")
	}
	if narrow.H < 40 {
		t.Errorf("the folded nav item is %.0f tall: it has no room for an icon and its caption", narrow.H)
	}
	if !tt.HasText("为你推荐") {
		t.Error("the rail lost the page's name: it should caption the icon")
	}
	// The fold is still there, now unfolding the rail.
	if err := tt.Click("展开侧边栏"); err != nil {
		t.Fatalf("the fold, to unfold it: %v", err)
	}
	tt.Frame()
	if a.sideRail {
		t.Error("the sidebar did not unfold")
	}
	if back, _ := tt.Find("为你推荐"); back.H > 20 {
		t.Errorf("the sidebar came back at %.0f tall, without its row", back.H)
	}
}

// The sidebar's foot holds who listens and nothing else: what plays is in
// the player bar along the foot of the page, which opens Now Playing.
func TestSidebarHasNoNowPlayingCard(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	a.pl.Cue([]netease.Track{{ID: 1, Name: "歌"}})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// The song is in the player bar, and no card offers it again over the
	// account row.
	if _, ok := tt.Find("正在播放"); ok {
		t.Error("the sidebar still shows a now playing card")
	}
}

// The fold stays at the window's top right however narrow the window is: the
// search gives up its room to it, rather than pushing it off the strip.
func TestTheFoldSurvivesANarrowWindow(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	for _, w := range []int{1100, 700, 560, 460} {
		tt := ui.NewTester(a.view, w, 700)
		tt.SetTitleBar(ui.TitleBar{Height: 28, Left: 78, Right: 10})
		fold, ok := tt.Find("折叠侧边栏")
		if !ok || fold.W == 0 {
			t.Errorf("at %d wide the fold is %+v, ok = %v: it has no room", w, fold, ok)
			continue
		}
		if fold.X+fold.W > float32(w) {
			t.Errorf("at %d wide the fold ends at %v, past the window", w, fold.X+fold.W)
		}
	}
}
