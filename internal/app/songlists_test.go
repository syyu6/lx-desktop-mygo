package app

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// A fake source whose square is what these tests say, and which reports
// what it was asked.
type fakeSquare struct {
	tags    []music.Tag
	lists   []music.Playlist
	asked   []string
	tagErr  error
	listErr error
}

func (f *fakeSquare) ID() string   { return "fake" }
func (f *fakeSquare) Name() string { return "假源" }
func (f *fakeSquare) SearchSongs(string, int, int) ([]music.Track, int, error) {
	return nil, 0, nil
}
func (f *fakeSquare) Playlist(string) (*music.Playlist, error) { return nil, errors.New("none") }
func (f *fakeSquare) Stream(music.Track, string) (*music.Stream, error) {
	return nil, errors.New("none")
}
func (f *fakeSquare) Lyrics(music.Track) (music.Lyrics, error) {
	return music.Lyrics{}, errors.New("none")
}
func (f *fakeSquare) Tags() ([]music.Tag, error) { return f.tags, f.tagErr }
func (f *fakeSquare) SquarePlaylists(tag string, limit, offset int) ([]music.Playlist, error) {
	f.asked = append(f.asked, tag)
	if f.listErr != nil {
		return nil, f.listErr
	}
	if offset >= len(f.lists) {
		return nil, nil
	}
	return f.lists[offset:min(offset+limit, len(f.lists))], nil
}

// mustTags and mustLists ask the fake source as the page would, for the
// tests to hand what it answers to the handlers post would run.
func mustTags(t *testing.T, f *fakeSquare) []music.Tag {
	t.Helper()
	tags, err := f.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	return tags
}

func mustLists(t *testing.T, f *fakeSquare, tag string) []music.Playlist {
	t.Helper()
	lists, err := f.SquarePlaylists(tag, squareLimit, 0)
	if err != nil {
		t.Fatalf("SquarePlaylists: %v", err)
	}
	return lists
}

func squareApp(f *fakeSquare) *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.reg = source.NewRegistry(f)
	return a
}

// The square's categories load, then its playlists under the first one:
// all of them, when none is chosen.
func TestSquareLoadsTagsThenLists(t *testing.T) {
	f := &fakeSquare{
		tags:  []music.Tag{{Name: "华语"}, {Name: "欧美"}},
		lists: []music.Playlist{{Source: "fake", ID: 1, Name: "老歌"}},
	}
	a := squareApp(f)
	a.loadSquare()
	s := a.square
	if s == nil {
		t.Fatal("no square")
	}
	// As the answers land, which post drops without a window.
	a.tagsArrived(s, f, mustTags(t, f), nil)
	a.squareArrived(s, mustLists(t, f, ""), nil, false)
	if !s.tagsLoaded || len(s.tags) != 2 {
		t.Fatalf("tags %+v", s.tags)
	}
	if !s.listsLoaded() || len(s.lists) != 1 {
		t.Fatalf("lists %+v", s.lists)
	}
	if len(f.asked) == 0 || f.asked[0] != "" {
		t.Errorf("asked for %q, want all", f.asked)
	}
}

// A category's playlists are asked for by its name, from its first.
func TestSquareChoosesTag(t *testing.T) {
	f := &fakeSquare{
		tags:  []music.Tag{{Name: "华语"}, {Name: "欧美"}},
		lists: []music.Playlist{{Source: "fake", ID: 1}, {Source: "fake", ID: 2}},
	}
	a := squareApp(f)
	a.loadSquare()
	s := a.square
	a.tagsArrived(s, f, mustTags(t, f), nil)
	a.chooseSquareTag(s, 1)
	a.squareArrived(s, mustLists(t, f, "欧美"), nil, false)
	if s.tag != 1 {
		t.Fatalf("tag %d", s.tag)
	}
	if len(s.lists) != 2 {
		t.Fatalf("lists %+v", s.lists)
	}
	if last := f.asked[len(f.asked)-1]; last != "欧美" {
		t.Errorf("asked for %q", last)
	}
}

// Categories that fail say so, and load again on a retry.
func TestSquareTagsFail(t *testing.T) {
	f := &fakeSquare{tagErr: errors.New("offline")}
	a := squareApp(f)
	a.loadSquare()
	a.tagsArrived(a.square, f, nil, f.tagErr)
	if a.square.tagsErr == "" {
		t.Fatal("no error")
	}
	f.tagErr = nil
	f.tags = []music.Tag{{Name: "华语"}}
	a.loadSquare()
	a.tagsArrived(a.square, f, f.tags, nil)
	if a.square.tagsErr != "" || !a.square.tagsLoaded {
		t.Errorf("retried: err %q, loaded %v", a.square.tagsErr, a.square.tagsLoaded)
	}
}

// With no source that has a square, the page says so rather than show
// nothing.
func TestSquareWithoutASource(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.reg = source.NewRegistry(netease.Provider{})
	a.reg.SetSearched(music.SourceNetease, false)
	if a.reg.Searched(music.SourceNetease) {
		t.Fatal("NetEase stayed on")
	}
	if got := a.squareSource(); got != "" {
		t.Errorf("square source %q", got)
	}
}

// The 歌单 page builds with a source's square loaded, its cards opening
// the playlists of that source.
func TestSonglistsPageBuilds(t *testing.T) {
	f := &fakeSquare{
		tags:  []music.Tag{{Name: "华语"}},
		lists: []music.Playlist{{Source: "fake", ID: 1, Name: "老歌"}},
	}
	a := squareApp(f)
	a.route = route{page: pageSonglists}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	a.tagsArrived(a.square, f, mustTags(t, f), nil)
	a.squareArrived(a.square, mustLists(t, f, ""), nil, false)
	tt.Frame()
	if _, ok := tt.Find("老歌"); !ok {
		t.Fatal("the square's playlist is not on the page")
	}
	if err := tt.Click("老歌"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route != (route{page: pagePlaylist, src: "fake", id: 1}) {
		t.Errorf("opened %+v", a.route)
	}
}

// sourcesWith is what a page uses to know which sources it can show: a
// source that does not have the page's thing is not offered, and one
// turned off is not asked. It is asked by interface, so a page built on
// charts finds QQ Music's charts and not its square.
func TestSourcesWithAsksByInterface(t *testing.T) {
	squareOnly := &fakeSquare{}
	both := idProvider{&fakeSquare{}, "other"}
	a := newTestApp()
	a.settings = defaultSettings()
	a.reg = source.NewRegistry(netease.Provider{}, squareOnly, both)

	ids, at := sourcesWith[source.Square](a, "")
	if len(ids) != 3 {
		t.Fatalf("squares %v, want NetEase, the fake and other", ids)
	}
	if ids[at] != music.SourceNetease {
		t.Errorf("the square shown is %q", ids[at])
	}

	// The fake has a square and no charts; NetEase has both. A page built
	// on charts offers NetEase alone.
	ids, at = sourcesWith[source.Charted](a, "")
	if len(ids) != 1 || ids[0] != music.SourceNetease {
		t.Errorf("charts on %v, want NetEase's alone", ids)
	}
	if at != 0 {
		t.Errorf("the charts shown start at %d", at)
	}

	// A source turned off is not offered, whatever it has.
	a.reg.SetSearched("other", false)
	ids, _ = sourcesWith[source.Square](a, "")
	for _, id := range ids {
		if id == "other" {
			t.Errorf("offered other, which the listener turned off: %v", ids)
		}
	}
	_ = at
}

// The square follows the catalogues the listener searches: a source turned
// off is not the square's, and turning off the one showing hands the page
// to another that has one. Its 单曲 switch is beside the point: a square is
// read from a catalogue.
func TestSquareFollowsTheSources(t *testing.T) {
	kw := &fakeSquare{} // the fake's id is "fake"
	a := newTestApp()
	a.settings = defaultSettings()
	other := &fakeSquare{}
	a.reg = source.NewRegistry(netease.Provider{}, kw, idProvider{other, "other"})
	if got := a.squareSource(); got != music.SourceNetease {
		t.Fatalf("the square is %q, want NetEase's first", got)
	}
	a.reg.SetSearched(music.SourceNetease, false)
	if got := a.squareSource(); got != "fake" {
		t.Errorf("with NetEase off the square is %q", got)
	}
	a.reg.SetSearched("fake", false)
	if got := a.squareSource(); got != "other" {
		t.Errorf("the square is %q", got)
	}
	a.reg.SetSearched("other", false)
	if got := a.squareSource(); got != "" {
		t.Errorf("with every square off it is %q", got)
	}
}

// idProvider is a fake square under an id of its own.
type idProvider struct {
	*fakeSquare
	id string
}

func (p idProvider) ID() string { return p.id }

// A source's square chosen in the switch stays the page's, while the
// listener asks it: the first source with a square is not chosen over it
// again as the page builds.
func TestSquareKeepsTheChoice(t *testing.T) {
	kw := &fakeSquare{}
	other := idProvider{&fakeSquare{}, "other"}
	a := newTestApp()
	a.settings = defaultSettings()
	a.reg = source.NewRegistry(netease.Provider{}, kw, other)
	a.squarePick = "other"
	if got := a.squareSource(); got != "other" {
		t.Errorf("the square is %q, not the one chosen", got)
	}
	a.reg.SetSearched("other", false)
	if got := a.squareSource(); got != music.SourceNetease {
		t.Errorf("with the one chosen off it is %q", got)
	}
}

// Every category is a chip with its name in it: a chosen one is lit, and
// the name is readable on it, as a bare button base left it out.
func TestSquareTagsShowTheirNames(t *testing.T) {
	f := &fakeSquare{
		tags:  []music.Tag{{Name: "华语"}, {Name: "欧美"}},
		lists: []music.Playlist{{Source: "fake", ID: 1, Name: "老歌"}},
	}
	a := squareApp(f)
	a.route = route{page: pageSonglists}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	a.tagsArrived(a.square, f, mustTags(t, f), nil)
	a.squareArrived(a.square, mustLists(t, f, ""), nil, false)
	tt.Frame()
	// A chip is as wide as its name and the room around it: a bare button
	// base draws none, and leaves the pill as wide as its padding alone.
	for _, name := range []string{"全部", "华语", "欧美"} {
		r, ok := tt.Find(name)
		if !ok {
			t.Fatalf("the chip %q is not on the page", name)
		}
		if r.W < 40 {
			t.Errorf("the chip %q is %.0f wide: it draws no text", name, r.W)
		}
	}
	// Chosen, a chip keeps its name drawn.
	if err := tt.Click("华语"); err != nil {
		t.Fatalf("choosing a category: %v", err)
	}
	a.squareArrived(a.square, mustLists(t, f, "华语"), nil, false)
	tt.Frame()
	if r, _ := tt.Find("华语"); r.W < 40 {
		t.Errorf("the chosen chip is %.0f wide: it draws no text", r.W)
	}
}
