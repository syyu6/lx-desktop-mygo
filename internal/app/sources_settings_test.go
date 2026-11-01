package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lxsource"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// The settings show one group of built-in sources, the services in the
// listener's order, each opening to what is asked of it, and beside it the
// scripts, which are imported, updated and taken away.
func TestSettingsListTheSourcesInOrder(t *testing.T) {
	a := testAppWithSources(t, testSource)
	a.settings = defaultSettings()
	a.loadRegistry()
	openSettingsTab(a, settingsSources)
	tt := ui.NewTester(a.settingsView, settingsWidth, 1600)
	tt.Frame()
	for _, want := range []string{"内置源", "音源脚本", "网易云音乐", "酷我音乐", "上移", "下移", "扫码登录", "导入音源"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("the settings do not show %q", want)
		}
	}
	// A source's row opens to its own switches, one per capability. They
	// are open by default: the first thing the listener does with a source
	// is turn one of its parts off, and it should not be hidden.
	for _, want := range []string{"单曲", "歌词"} {
		if !tt.HasText(want) {
			t.Errorf("the settings do not show a %q switch", want)
		}
	}
	// Kugou is a catalogue alone: it is searched, and the app does not
	// stream its songs. It has no comment thread, so no remarks switch.
	if !a.reg.Searched(music.SourceKugou) {
		t.Error("Kugou is not searched")
	}
	if _, ok := a.reg.Get(music.SourceKugou); ok {
		t.Error("Kugou streams: it should only be searched")
	}
	if a.reg.Has(music.SourceKugou, source.CapComments) {
		t.Error("Kugou is offered a switch for comments it has not")
	}
}

// A source turned off for its songs keeps the search working through the
// others, and the settings keep it off. It is one switch: a source's songs
// are searched and played together, as they are one thing to ask for.
func TestSettingsTurnSourcesOff(t *testing.T) {
	a := testAppWithSources(t, testSource)
	a.settings = defaultSettings()
	a.loadRegistry()
	// The switch's own path ends in a redraw, which a test has no loop for:
	// what it changes is what setSourceOn changes.
	if !a.setSourceOn(music.SourceNetease, false) {
		t.Fatal("NetEase is not a source")
	}
	if a.searchesOn(music.SourceNetease) {
		t.Error("NetEase is still searched")
	}
	if a.sourceOn(music.SourceNetease) {
		t.Error("NetEase is still played from")
	}
	if len(a.settings.SourcesOff) != 1 || a.settings.SourcesOff[0] != music.SourceNetease {
		t.Errorf("settings keep %q off", a.settings.SourcesOff)
	}
	a.setSourceOn(music.SourceNetease, true)
	if !a.searchesOn(music.SourceNetease) || len(a.settings.SourcesOff) != 0 {
		t.Errorf("turned on: searched %v, off %q", a.searchesOn(music.SourceNetease), a.settings.SourcesOff)
	}
}

// The pages a source may appear in are that source's own switches: opened,
// its row carries 功能 — 曲库、单曲、歌词、评论 — and 页面 — the pages it
// can serve — under its name, rather than a group of its own at the foot of
// the page. NetEase is signed in to, so its row says so without opening.
func TestSettingsShowPagesAmongTheSourcesSwitches(t *testing.T) {
	a := testAppWithSources(t, testSource)
	a.settings = defaultSettings()
	a.loadRegistry()
	openSettingsTab(a, settingsSources)
	tt := ui.NewTester(a.settingsView, settingsWidth, 2400)
	tt.Frame()
	// The group of its own is gone.
	if tt.HasText("多来源页面") {
		t.Error("the page tabs still have a group of their own")
	}
	// NetEase's row is open by default: its functions and its pages are
	// both there, as two labelled rows.
	for _, want := range []string{"功能", "页面", "网易云音乐 · 曲库", "网易云音乐 · 推荐", "网易云音乐 · 排行榜", "网易云音乐 · 歌单"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("the settings do not show %q", want)
		}
	}
	// NetEase is signed in to in this app, and its row says so; a source
	// with no sign-in at all offers no state to be in.
	if tt.HasText("已登录") {
		t.Error("a test app with no account says it is signed in")
	}
	// Cool's row is closed, and a closed row still says it has no account.
	// (Kugou has a catalogue alone and offers no pages at all.)
	for _, page := range []tabPage{tabsHome, tabsCharts, tabsSonglists} {
		if !a.pageOn(page, music.SourceNetease) {
			t.Fatalf("%v starts turned off for NetEase", page)
		}
	}
	// Turning a page off for a source is kept, and leaves the others'.
	a.setPageAsk(tabsCharts, music.SourceNetease, false)
	if a.pageOn(tabsCharts, music.SourceNetease) {
		t.Error("the charts still show NetEase")
	}
	if !a.pageOn(tabsHome, music.SourceNetease) || !a.pageOn(tabsSonglists, music.SourceNetease) {
		t.Error("turning one page off for a source took another's")
	}
	if !slices.Contains(a.settings.PagesAskOff, pageKey(tabsCharts, music.SourceNetease)) {
		t.Errorf("the settings keep %q off", a.settings.PagesAskOff)
	}
	a.setPageAsk(tabsCharts, music.SourceNetease, true)
	if !a.pageOn(tabsCharts, music.SourceNetease) || len(a.settings.PagesAskOff) != 0 {
		t.Errorf("turned on: on %v, off %q", a.pageOn(tabsCharts, music.SourceNetease), a.settings.PagesAskOff)
	}
}

// is not searched, and one searched may be left out of the words.
func TestSettingsTurnLyricsOff(t *testing.T) {
	a := testAppWithSources(t, testSource)
	a.settings = defaultSettings()
	a.loadRegistry()
	if !a.setSourceCap(music.SourceNetease, source.CapLyrics, false) {
		t.Fatal("NetEase has no lyrics")
	}
	if a.askedOn(music.SourceNetease, source.CapLyrics) {
		t.Error("NetEase is still asked for words")
	}
	// Its songs are untouched.
	if !a.sourceOn(music.SourceNetease) {
		t.Error("turning the words off turned the songs off")
	}
	if len(a.settings.LyricsOff) != 1 || a.settings.LyricsOff[0] != music.SourceNetease {
		t.Errorf("settings keep %q off", a.settings.LyricsOff)
	}
}

// A source moved up the list is asked first, and the settings keep the
// order.
func TestSettingsOrderTheSources(t *testing.T) {
	a := testAppWithSources(t, testSource)
	a.settings = defaultSettings()
	a.loadRegistry()
	if got := a.sourceOrder(); len(got) < 2 || got[0] != music.SourceNetease {
		t.Fatalf("order %q", got)
	}
	a.moveSource(music.SourceKuwo, true)
	// Kuwo is after NetEase's two links at first: moved up once, it is
	// still after NetEase while there is another between them.
	order := a.sourceOrder()
	atKuwo, atNetease := indexOf(order, music.SourceKuwo), indexOf(order, music.SourceNetease)
	if atKuwo >= atNetease {
		t.Errorf("order %q: Kuwo not before NetEase", order)
	}
	for i := 0; i < len(order); i++ {
		a.moveSource(music.SourceKuwo, true)
	}
	if got := a.sourceOrder(); got[0] != music.SourceKuwo {
		t.Errorf("order %q, want Kuwo first", got)
	}
	if a.settings.SourceOrder[0] != music.SourceKuwo {
		t.Errorf("settings keep %q", a.settings.SourceOrder)
	}
}

func indexOf(names []string, s string) int {
	for i, n := range names {
		if n == s {
			return i
		}
	}
	return -1
}

// A script's row says which platforms it plays, by their abbreviations,
// once its runtime has said so — a script that would not start says that
// instead.
func TestSettingsTagScriptsWithTheirPlatforms(t *testing.T) {
	a := testAppWithSources(t, testSource)
	a.settings = defaultSettings()
	a.loadRegistry()
	a.askSourceCaps() // no window in a test: nothing is started
	id := a.sources[0].ID
	// Asked, and answered: the tags are the platforms it streams.
	a.srcCapsAsked = map[string]bool{id: true}
	a.setSourceCaps(id, map[lxsource.Source]lxsource.Capability{
		lxsource.SourceNetease: {Actions: []lxsource.Action{lxsource.ActionMusicURL}},
		lxsource.SourceKugou:   {Actions: []lxsource.Action{lxsource.ActionMusicURL}},
		// Lyrics alone is not a platform it plays.
		lxsource.SourceKuwo: {Actions: []lxsource.Action{lxsource.ActionLyric}},
	}, nil)
	openSettingsTab(a, settingsSources)
	tt := ui.NewTester(a.settingsView, settingsWidth, 1600)
	tt.Frame()
	for _, want := range []string{"wy", "kg"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("the script is not tagged %q", want)
		}
	}
	if _, ok := tt.Find("kw"); ok {
		t.Error("the script is tagged with a platform it only has lyrics for")
	}
	// A script that would not start says so, in place of its tags.
	a.setSourceCaps(id, nil, errors.New("脚本没有说明它支持哪些音源"))
	tt.Frame()
	if _, ok := tt.Find("wy"); ok {
		t.Error("a script that failed is still tagged")
	}
	if _, ok := tt.Find("未能启动，无法得知它支持哪些平台"); !ok {
		t.Error("the settings do not say the script would not start")
	}
}
