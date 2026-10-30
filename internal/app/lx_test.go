package app

import (
	"strings"
	"testing"

	"netease-music-mygo/internal/lxsource"
)

// A source that answers without the network, of the shape the real ones
// have: it answers for NetEase's songs, so the app asks it when NetEase
// will not play one.
const testSource = `/*!
 * @name 测试音源
 * @version 1.2
 * @author 测试
 */
const { EVENT_NAMES, request, on, send } = globalThis.lx;
on(EVENT_NAMES.request, ({ source, action, info }) => {
  if (action === 'musicUrl') return 'https://example.invalid/' + source + '/' + info.musicInfo.id + '.mp3';
  return Promise.reject(new Error('不支持'));
});
send(EVENT_NAMES.inited, { sources: { wy: { name: '网易云音乐', type: 'music', actions: ['musicUrl'], qualitys: ['128k', '320k'] } } });
`

// testAppWithSources returns an app whose sources are kept in a directory
// of the test's, with the scripts given imported.
func testAppWithSources(t *testing.T, scripts ...string) *app {
	t.Helper()
	a := newTestApp()
	a.srcStore, _ = lxsource.NewStore(t.TempDir())
	a.srcState = &sourceState{}
	a.src = lxsource.NewResolver(a.srcStore)
	a.src.Order = func() []string { return a.sourcesOrdered() }
	for _, code := range scripts {
		if _, err := a.srcStore.Import(code); err != nil {
			t.Fatal(err)
		}
	}
	a.loadSourceList()
	return a
}

// sourcesOrdered is what the resolver asks, as loadSourceList makes it.
func (a *app) sourcesOrdered() []string {
	a.srcState.mu.Lock()
	defer a.srcState.mu.Unlock()
	return append([]string(nil), a.srcState.order...)
}

// TestTurnSourceAsksOrNot checks that a script turned off is not asked for
// a song, and one turned on is. The switch's own path through the settings
// ends in a redraw, which a test has no loop for: what it changes is
// checked here.
func TestTurnSourceAsksOrNot(t *testing.T) {
	a := testAppWithSources(t, testSource)
	if len(a.sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(a.sources))
	}
	id := a.sources[0].ID
	if got := a.sourcesOrdered(); len(got) != 1 || got[0] != id {
		t.Fatalf("order = %v, want the script", got)
	}

	if _, err := a.srcStore.SetEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	a.loadSourceList()
	if got := a.sourcesOrdered(); len(got) != 0 {
		t.Errorf("order = %v after turning it off, want none", got)
	}
	if a.sources[0].Enabled {
		t.Error("the script is still enabled")
	}

	if _, err := a.srcStore.SetEnabled(id, true); err != nil {
		t.Fatal(err)
	}
	a.loadSourceList()
	if got := a.sourcesOrdered(); len(got) != 1 || got[0] != id {
		t.Errorf("order = %v after turning it on, want the script", got)
	}
}

// TestRemoveSourceForgetsIt checks that a script taken away is gone from
// the list and from what is asked.
func TestRemoveSourceForgetsIt(t *testing.T) {
	a := testAppWithSources(t, testSource)
	id := a.sources[0].ID
	// removeSource asks first, which a test cannot click: the store's part
	// is what is checked, then the list's.
	if err := a.srcStore.Remove(id); err != nil {
		t.Fatal(err)
	}
	a.src.Reset(id)
	a.loadSourceList()
	if len(a.sources) != 0 {
		t.Errorf("sources = %v after removing it, want none", a.sources)
	}
	if got := a.sourcesOrdered(); len(got) != 0 {
		t.Errorf("order = %v after removing it, want none", got)
	}
}

// A script already asked is tagged from what the store kept of it, rather
// than a runtime started for the tag.
func TestSourceTagsComeFromWhatWasKept(t *testing.T) {
	a := testAppWithSources(t, testSource)
	id := a.sources[0].ID
	if err := a.srcStore.SetPlatforms(id, []lxsource.Source{lxsource.SourceKugou}); err != nil {
		t.Fatal(err)
	}
	a.loadSourceList()
	a.askSourceCaps()
	tags, asked, failed := a.sourceTags(id)
	if !asked || failed {
		t.Fatalf("asked = %v, failed = %v, want it asked without a runtime", asked, failed)
	}
	if len(tags) != 1 || tags[0] != lxsource.SourceKugou {
		t.Errorf("tags = %v, want what the store kept", tags)
	}
}

// TestSourceDescriptionTellsWhatItIs checks that the list says what a
// script is from its header.
func TestSourceDescriptionTellsWhatItIs(t *testing.T) {
	info := lxsource.Info{Version: "1.2", Author: "测试"}
	got := sourceDescription(&info)
	if !strings.Contains(got, "1.2") || !strings.Contains(got, "测试") {
		t.Errorf("description = %q, want its version and author", got)
	}
	if strings.Contains(sourceDescription(&lxsource.Info{}), "·") {
		t.Error("a script with no header fields got a separator")
	}
}

// TestFormatInterval checks the length a script is told, as mm:ss.
func TestFormatInterval(t *testing.T) {
	for _, tc := range []struct {
		ms   int64
		want string
	}{
		{0, "0:00"},
		{61_000, "1:01"},
		{261_000, "4:21"},
		{3_600_000, "60:00"},
		{-5000, "0:00"},
	} {
		if got := formatInterval(tc.ms); got != tc.want {
			t.Errorf("formatInterval(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}
