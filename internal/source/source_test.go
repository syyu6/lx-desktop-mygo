package source

import (
	"errors"
	"slices"
	"testing"

	"netease-music-mygo/internal/music"
)

// fake is a provider that answers what these tests say, and reports what
// it was asked.
type fake struct {
	id     string
	tracks []music.Track
	lists  []music.Playlist
	err    error
	asked  *[]string
}

func (f *fake) ID() string   { return f.id }
func (f *fake) Name() string { return f.id + " 音乐" }
func (f *fake) SearchSongs(string, int, int) ([]music.Track, int, error) {
	if f.asked != nil {
		*f.asked = append(*f.asked, f.id)
	}
	return f.tracks, len(f.tracks), f.err
}
func (f *fake) Playlist(string) (*music.Playlist, error) { return nil, f.err }
func (f *fake) Stream(music.Track, string) (*music.Stream, error) {
	return nil, f.err
}
func (f *fake) Lyrics(music.Track) (music.Lyrics, error) { return music.Lyrics{}, f.err }

type fakeSearcher struct{ *fake }

func (f fakeSearcher) SearchPlaylists(string, int, int) ([]music.Playlist, int, error) {
	return f.lists, len(f.lists), f.err
}

// The sources are asked in the order they were given, and the ones turned
// off are left out of what is asked, though not out of what the app has.
func TestRegistryOrderAndEnabled(t *testing.T) {
	a, b := &fake{id: "a"}, &fake{id: "b"}
	r := NewRegistry(a, b)
	if got := r.EnabledSources(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("sources %q", got)
	}
	r.SetEnabled("b", false)
	if got := r.EnabledSources(); !slices.Equal(got, []string{"a"}) {
		t.Errorf("after turning b off %q", got)
	}
	// A song of b still plays from b: its source is had, though not asked.
	if _, ok := r.Get("b"); !ok {
		t.Error("b went away")
	}
	if r.SetEnabled("nope", true) {
		t.Error("an unknown source was turned on")
	}
}

// A search asks every source turned on, and one that fails does not fail
// it: the others' answers are what the listener gets.
func TestRegistrySearchesTheSourcesOn(t *testing.T) {
	a := &fake{id: "a", tracks: []music.Track{{Source: "a", ID: 1}}}
	b := &fake{id: "b", err: errors.New("offline")}
	r := NewRegistry(a, b)
	res, err := r.SearchSongs("x", 10, 0)
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}
	if len(res.Songs) != 1 || res.Songs[0].Source != "a" {
		t.Errorf("songs %+v", res.Songs)
	}
	if res.Total != 1 {
		t.Errorf("total %d", res.Total)
	}
	// Turned off for its catalogue, it is not searched. What is left is b,
	// which fails, and a search that everyone failed is no search at all.
	// Its songs' switch is apart from this: it does not touch a search.
	r.SetSearched("a", false)
	if _, err := r.SearchSongs("x", 10, 0); !errors.Is(err, ErrNoSource) {
		t.Errorf("SearchSongs with only a failing source on: %v", err)
	}
	if got := r.SearchedSources(); len(got) != 1 || got[0] != "b" {
		t.Errorf("sources searched: %q", got)
	}
	r.SetSearched("a", true)
	if got := r.SearchedSources(); len(got) != 2 {
		t.Errorf("sources searched after turning a on: %q", got)
	}
	r.SetEnabled("a", false)
	if got := r.SearchedSources(); len(got) != 2 {
		t.Errorf("turning a's songs off took it out of a search: %q", got)
	}
	r.SetSearched("a", false)
	r.SetSearched("b", false)
	if _, err := r.SearchSongs("x", 10, 0); !errors.Is(err, ErrNoSource) {
		t.Errorf("with every source's catalogue off: %v", err)
	}
}

// A source's words are their own switch: a source kept for its words alone
// is not searched, and one searched is not asked for words it has none of.
func TestRegistryAsksTheSourcesForWhatTheyHave(t *testing.T) {
	a := &fake{id: "a", tracks: []music.Track{{Source: "a", ID: 1}}}
	r := NewRegistry(a)
	if !r.Asked("a", CapLibrary) || !r.Asked("a", CapStream) || !r.Asked("a", CapLyrics) {
		t.Fatal("a source starts without its catalogue, its songs or its words")
	}
	if r.Asked("a", CapComments) {
		t.Error("a source without a comment thread is asked for remarks")
	}
	r.SetAsked("a", CapStream, false)
	if r.Asked("a", CapStream) {
		t.Error("the songs' switch stayed on")
	}
	// Its words and its catalogue are untouched by its songs.
	if !r.Asked("a", CapLyrics) || !r.Asked("a", CapLibrary) {
		t.Error("turning the songs off turned the words or the catalogue off")
	}
	if r.SetAsked("a", CapComments, true) {
		t.Error("a switch was turned for a capability the source has not")
	}
}

// No source turned on asks nothing, and says so, rather than wait forever.
func TestRegistryWithoutSources(t *testing.T) {
	r := NewRegistry(&fake{id: "a"})
	r.SetSearched("a", false)
	if _, err := r.SearchSongs("x", 10, 0); !errors.Is(err, ErrNoSource) {
		t.Errorf("SearchSongs: %v", err)
	}
	if _, err := r.SearchPlaylists("x", 10, 0); !errors.Is(err, ErrNoSource) {
		t.Errorf("SearchPlaylists: %v", err)
	}
}

// Only the sources that search playlists are asked for playlists.
func TestRegistrySearchesPlaylistsWhereSearched(t *testing.T) {
	songs := &fake{id: "a"}
	lists := fakeSearcher{&fake{id: "b", lists: []music.Playlist{{Source: "b", ID: 2}}}}
	r := NewRegistry(songs, lists)
	res, err := r.SearchPlaylists("x", 10, 0)
	if err != nil {
		t.Fatalf("SearchPlaylists: %v", err)
	}
	if len(res.Playlists) != 1 || res.Playlists[0].Source != "b" {
		t.Errorf("playlists %+v", res.Playlists)
	}
}

// The registry has no source of a song of another service, and says which.
func TestRegistryUnknownSource(t *testing.T) {
	r := NewRegistry(&fake{id: "a"})
	if _, ok := r.Get("zz"); ok {
		t.Error("found a source it does not have")
	}
	if got := r.Name("zz"); got != "zz" {
		t.Errorf("name %q", got)
	}
}

// A song's and a playlist's names in their source are their ids there,
// unless the source names them otherwise.
func TestMusicKeys(t *testing.T) {
	tr := music.Track{Source: music.SourceKugou, ID: 7, Key: "HASH"}
	if tr.SongKey() != "HASH" {
		t.Errorf("key %q", tr.SongKey())
	}
	if tr.Src() != music.SourceKugou {
		t.Errorf("source %q", tr.Src())
	}
	plain := music.Track{ID: 7}
	if plain.SongKey() != "7" || plain.Src() != music.SourceNetease {
		t.Errorf("key %q, source %q", plain.SongKey(), plain.Src())
	}
	pl := music.Playlist{ID: 9}
	if pl.KeyOf() != "9" || pl.Src() != music.SourceNetease {
		t.Errorf("key %q, source %q", pl.KeyOf(), pl.Src())
	}
}
