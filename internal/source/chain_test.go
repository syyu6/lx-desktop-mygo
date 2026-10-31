package source

import (
	"errors"
	"slices"
	"testing"

	"netease-music-mygo/internal/music"
)

// link is a provider for these tests: it streams the songs it was given,
// and searches what the test says.
type link struct {
	id      string
	streams map[string]bool // by name: whether it will stream the song
	finds   []music.Track   // what its search answers
	asked   *[]string       // the searches it was asked, recorded
}

func (l *link) ID() string   { return l.id }
func (l *link) Name() string { return l.id }
func (l *link) SearchSongs(query string, limit, offset int) ([]music.Track, int, error) {
	if l.asked != nil {
		*l.asked = append(*l.asked, l.id)
	}
	return l.finds, len(l.finds), nil
}
func (l *link) Playlist(string) (*music.Playlist, error) { return nil, ErrUnknownSource }
func (l *link) Stream(t music.Track, level string) (*music.Stream, error) {
	if !l.streams[t.Name] {
		return nil, errors.New("no")
	}
	return &music.Stream{URL: "http://" + l.id + "/" + t.Name, Level: level}, nil
}
func (l *link) Lyrics(t music.Track) (music.Lyrics, error) {
	if l.id != t.Src() {
		return music.Lyrics{}, errors.New("not mine")
	}
	return music.Lyrics{Lyric: "[" + l.id + "]"}, nil
}

// A song plays from its own source where that source will stream it, and
// the chain asks no other.
func TestChainPlaysFromItsOwnSource(t *testing.T) {
	var asked []string
	netease := &link{id: music.SourceNetease, streams: map[string]bool{"歌": true}, asked: &asked}
	kuwo := &link{id: music.SourceKuwo, streams: map[string]bool{"歌": true}, asked: &asked}
	c := NewChain(Link{ID: netease.id, Provider: netease}, Link{ID: kuwo.id, Provider: kuwo})
	res, err := c.Resolve(music.Track{Name: "歌"}, "exhigh")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Served != music.SourceNetease || res.Stream.URL != "http://netease/歌" {
		t.Errorf("played %q from %q", res.Stream.URL, res.Served)
	}
	if len(asked) != 0 {
		t.Errorf("looked the song up on %v", asked)
	}
}

// A song its source will not stream is looked up on the others, and plays
// from the one that has it: the source it was found on, by that source's
// numbering.
func TestChainFindsTheSongElsewhere(t *testing.T) {
	netease := &link{id: music.SourceNetease, streams: map[string]bool{}}
	kuwo := &link{
		id:      music.SourceKuwo,
		streams: map[string]bool{"歌": true},
		finds: []music.Track{
			{Source: music.SourceKuwo, ID: 7, Name: "歌", Duration: 200000},
		},
	}
	c := NewChain(Link{ID: netease.id, Provider: netease}, Link{ID: kuwo.id, Provider: kuwo})
	res, err := c.Resolve(music.Track{Name: "歌", Duration: 200000}, "exhigh")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Served != music.SourceKuwo {
		t.Fatalf("served by %q, want Kuwo", res.Served)
	}
	if res.Track.ID != 7 || res.Track.Source != music.SourceKuwo {
		t.Errorf("played %+v, want Kuwo's song", res.Track)
	}
	if res.Stream.URL != "http://kw/歌" {
		t.Errorf("stream %q", res.Stream.URL)
	}
}

// The other sources are looked at in the listener's order, and the song
// plays from the first that has it.
func TestChainLooksInTheListenersOrder(t *testing.T) {
	var asked []string
	netease := &link{id: music.SourceNetease, streams: map[string]bool{}}
	kugou := &link{id: music.SourceKugou, streams: map[string]bool{"歌": true}, asked: &asked,
		finds: []music.Track{{Source: music.SourceKugou, ID: 1, Name: "歌"}}}
	kuwo := &link{id: music.SourceKuwo, streams: map[string]bool{"歌": true}, asked: &asked,
		finds: []music.Track{{Source: music.SourceKuwo, ID: 2, Name: "歌"}}}
	c := NewChain(
		Link{ID: netease.id, Provider: netease},
		Link{ID: kuwo.id, Provider: kuwo},
		Link{ID: kugou.id, Provider: kugou},
	)
	res, err := c.Resolve(music.Track{Name: "歌"}, "standard")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Served != music.SourceKuwo {
		t.Errorf("served by %q, want the first in the listener's order", res.Served)
	}
	if !slices.Equal(asked, []string{music.SourceKuwo}) {
		t.Errorf("looked up on %v, want Kuwo alone", asked)
	}
}

// A song played again is not looked up again: what it was found on is
// kept.
func TestChainRemembersWhereItFoundASong(t *testing.T) {
	var asked []string
	netease := &link{id: music.SourceNetease, streams: map[string]bool{}}
	kuwo := &link{id: music.SourceKuwo, streams: map[string]bool{"歌": true}, asked: &asked,
		finds: []music.Track{{Source: music.SourceKuwo, ID: 7, Name: "歌"}}}
	c := NewChain(Link{ID: netease.id, Provider: netease}, Link{ID: kuwo.id, Provider: kuwo})
	song := music.Track{ID: 1, Name: "歌"}
	if _, err := c.Resolve(song, "exhigh"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := c.Resolve(song, "standard"); err != nil {
		t.Fatalf("Resolve again: %v", err)
	}
	if len(asked) != 1 {
		t.Errorf("looked the song up %d times, want once", len(asked))
	}
}

// A song no source has is not played, and the chain says so rather than
// hand back the last source's failure.
func TestChainWithoutTheSong(t *testing.T) {
	netease := &link{id: music.SourceNetease, streams: map[string]bool{}}
	kuwo := &link{id: music.SourceKuwo, streams: map[string]bool{}}
	c := NewChain(Link{ID: netease.id, Provider: netease}, Link{ID: kuwo.id, Provider: kuwo})
	_, err := c.Resolve(music.Track{Name: "歌"}, "exhigh")
	if !errors.Is(err, ErrNotPlayable) {
		t.Errorf("Resolve = %v, want ErrNotPlayable", err)
	}
}

// A song's own source is asked before the others, even when no source can
// search: the chain does not look for it where its own source would.
func TestChainAsksItsOwnSourceFirst(t *testing.T) {
	own := &link{id: music.SourceKuwo, streams: map[string]bool{}}
	other := &link{id: music.SourceNetease, streams: map[string]bool{}}
	c := NewChain(Link{ID: own.id, Provider: own}, Link{ID: other.id, Provider: other})
	if _, err := c.Resolve(music.Track{Source: music.SourceKuwo, Name: "歌"}, "exhigh"); !errors.Is(err, ErrNotPlayable) {
		t.Errorf("Resolve = %v", err)
	}
}

// Songs are matched as lx-music-desktop matches them: by name and artist,
// within five seconds of the same length, and by name alone where the
// length is not known.
func TestBestMatch(t *testing.T) {
	ask := music.Track{Name: "晴天", Artists: []music.Artist{{Name: "周杰伦"}}, Duration: 269000}
	for _, tc := range []struct {
		name  string
		found []music.Track
		ok    bool
		id    int64
	}{
		{"the same song", []music.Track{{ID: 1, Name: "晴天", Artists: []music.Artist{{Name: "周杰伦"}}, Duration: 269500}}, true, 1},
		{"another service spells it apart", []music.Track{{ID: 2, Name: "晴天 (Live)", Duration: 269000}}, true, 2},
		{"a cover by another singer", []music.Track{{ID: 3, Name: "晴天", Artists: []music.Artist{{Name: "别人"}}, Duration: 269000}}, true, 3},
		{"too long to be it", []music.Track{{ID: 4, Name: "晴天", Duration: 400000}}, true, 4},
		{"not it at all", []music.Track{{ID: 5, Name: "雨天"}}, false, 0},
		{"nothing found", nil, false, 0},
	} {
		got, ok := bestMatch(ask, tc.found)
		if ok != tc.ok || (ok && got.ID != tc.id) {
			t.Errorf("%s: bestMatch = %+v, %v", tc.name, got, ok)
		}
	}
}

// A name compares across services' spellings: in lower case, without the
// punctuation and the brackets a version is named in.
func TestNormName(t *testing.T) {
	if got := normName("Hello, World!（Live）"); got != "helloworld" {
		t.Errorf("normName = %q", got)
	}
	if normName("歌") != normName("歌 ") {
		t.Error("spacing told two names apart")
	}
}

// A chain with no links plays nothing, and says why.
func TestChainWithoutLinks(t *testing.T) {
	if _, err := NewChain().Resolve(music.Track{Name: "歌"}, "exhigh"); !errors.Is(err, ErrNoSource) {
		t.Errorf("Resolve = %v, want ErrNoSource", err)
	}
}

// A script is a link like a source: it is asked for a song of a source it
// answers for, where the source's own provider will not stream it, and it
// gives way to the next link when it cannot.
func TestChainAsksAScript(t *testing.T) {
	netease := &link{id: music.SourceNetease, streams: map[string]bool{}}
	script := &fakeScript{supports: map[string]bool{music.SourceNetease: true}, streams: true}
	c := NewChain(
		Link{ID: netease.id, Provider: netease},
		Link{ID: "s1", Name: "音源", Script: script},
	)
	res, err := c.Resolve(music.Track{ID: 1, Name: "歌"}, "exhigh")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Served != music.SourceNetease || res.Stream.URL != "script://歌" {
		t.Errorf("played %q from %q", res.Stream.URL, res.Served)
	}
	// What a script gives says nothing of how loud it is, so it plays as it
	// came: a gain of 1 was read as one decibel to lift it by, five with
	// the preamp, and clipped songs already at full scale.
	if res.Stream.Gain != 0 || res.Stream.Peak != 0 {
		t.Errorf("script stream carries gain %v and peak %v, want none", res.Stream.Gain, res.Stream.Peak)
	}
	// Turned off, the script is not asked, and the song does not play.
	c.Set([]Link{{ID: netease.id, Provider: netease}})
	if _, err := c.Resolve(music.Track{ID: 1, Name: "歌"}, "exhigh"); !errors.Is(err, ErrNotPlayable) {
		t.Errorf("without the script: %v", err)
	}
}

// A script that cannot stream a song gives way to the next link.
func TestChainPassesOverAScript(t *testing.T) {
	script := &fakeScript{supports: map[string]bool{music.SourceNetease: true}}
	c := NewChain(
		Link{ID: "s1", Name: "音源", Script: script},
		Link{ID: "kw", Provider: &link{id: music.SourceKuwo, streams: map[string]bool{"歌": true}}},
	)
	// The song is NetEase's, and its source has no provider here: the
	// script's turn comes, and fails.
	if _, err := c.Resolve(music.Track{Name: "歌"}, "exhigh"); !errors.Is(err, ErrNotPlayable) {
		t.Errorf("Resolve = %v", err)
	}
}

// fakeScript is a source script for these tests, which answers for the
// sources it is given.
type fakeScript struct {
	supports map[string]bool
	streams  bool
}

func (s *fakeScript) Name() string             { return "音源" }
func (s *fakeScript) Supports(src string) bool { return s.supports[src] }
func (s *fakeScript) Stream(src string, t music.Track, level string) (string, error) {
	if !s.streams {
		return "", errors.New("no")
	}
	return "script://" + t.Name, nil
}
func (s *fakeScript) Lyrics(string, music.Track) (string, string, bool) {
	return "[script]", "", s.streams
}
func (s *fakeScript) Pic(string, music.Track) (string, bool) { return "", false }

// Lyrics and covers come from a song's own source, by the source's own
// link, and a source that has none gives way to the next.
func TestChainResolvesLyricsAndCover(t *testing.T) {
	netease := &link{id: music.SourceNetease, streams: map[string]bool{}}
	c := NewChain(Link{ID: netease.id, Provider: netease})
	if lrc, _, ok := c.ResolveLyrics(music.Track{ID: 1}); !ok || lrc != "[netease]" {
		t.Errorf("lyrics = %q, %v", lrc, ok)
	}
	// NetEase's provider here is not a Coverer, and has no script: no cover.
	if url, ok := c.ResolveCover(music.Track{ID: 1}); ok {
		t.Errorf("cover = %q", url)
	}
}

// A track's own source is the one its id is of: NetEase's where it names
// none, and its album's cover where it names no other.
func TestTrackSourceAndCover(t *testing.T) {
	if got := (&music.Track{}).Src(); got != music.SourceNetease {
		t.Errorf("Src = %q", got)
	}
	tr := music.Track{PicURL: "pic", Album: music.Album{PicURL: "album"}}
	if tr.Cover() != "pic" {
		t.Errorf("Cover = %q, want its own", tr.Cover())
	}
	albumOnly := music.Track{Album: music.Album{PicURL: "album"}}
	if albumOnly.Cover() != "album" {
		t.Error("Cover did not fall back to its album's")
	}
}

// lyricOnly is a catalogue the app searches but does not stream from: it
// knows its songs' words, and streams nothing, as Kugou does.
type lyricOnly struct {
	id string
}

func (l *lyricOnly) ID() string   { return l.id }
func (l *lyricOnly) Name() string { return l.id }
func (l *lyricOnly) SearchSongs(string, int, int) ([]music.Track, int, error) {
	return nil, 0, nil
}
func (l *lyricOnly) Lyrics(t music.Track) (music.Lyrics, error) {
	return music.Lyrics{Lyric: "[" + l.id + "]"}, nil
}

// A song whose own source streams nothing is still asked for its words by
// the catalogue that knows them: Kugou's songs play elsewhere, and their
// words come from Kugou.
func TestChainAsksACatalogueForItsSongsWords(t *testing.T) {
	other := &link{id: music.SourceKuwo}
	c := NewChain(Link{ID: other.id, Provider: other})
	c.SetCatalogs([]Link{{ID: music.SourceKugou, Catalog: &lyricOnly{id: music.SourceKugou}}})
	lrc, _, ok := c.ResolveLyrics(music.Track{Source: music.SourceKugou, Name: "歌"})
	if !ok || lrc != "[kg]" {
		t.Errorf("words = %q, ok = %v, want Kugou's", lrc, ok)
	}
	// A catalogue of another source is not asked for them.
	if lrc, _, ok := c.ResolveLyrics(music.Track{Source: music.SourceNetease, Name: "歌"}); ok {
		t.Errorf("words of a NetEase song = %q", lrc)
	}
}

// A song NetEase has no words for is asked of the other sources, which are
// looked up by its name: an instrumental's words are often on QQ Music, and
// a song NetEase knows only by another service's record has them there.
func TestChainAsksTheOtherSourcesForWords(t *testing.T) {
	// QQ Music has the same song, by its own numbering, and words for it.
	qq := &link{
		id:    music.SourceQQ,
		finds: []music.Track{{Source: music.SourceQQ, ID: 77, Name: "歌", Artists: []music.Artist{{Name: "歌手"}}, Duration: 200000}},
	}
	nt := &link{id: music.SourceNetease}
	c := NewChain(Link{ID: nt.id, Provider: nt})
	c.SetCatalogs([]Link{{ID: music.SourceQQ, Catalog: qq}})
	song := music.Track{Source: music.SourceNetease, ID: 1, Name: "歌",
		Artists: []music.Artist{{Name: "歌手"}}, Duration: 200000}
	// NetEase is the song's own source, so it is not asked again; QQ
	// Music's record is found by name, and its words come from it.
	lrc, _, ok := c.ResolveLyricsElsewhere(song)
	if !ok || lrc != "[tx]" {
		t.Errorf("words elsewhere = %q, ok = %v, want QQ Music's", lrc, ok)
	}
}

// The sources are asked for words in the listener's order: the first with
// the song answers, and a source whose words are turned off is not asked.
func TestChainAsksTheLyricistsInOrder(t *testing.T) {
	song := music.Track{Source: music.SourceNetease, ID: 1, Name: "歌"}
	finds := []music.Track{{Source: music.SourceKuwo, ID: 5, Name: "歌"}}
	kw := &link{id: music.SourceKuwo, finds: finds}
	qq := &link{id: music.SourceQQ, finds: []music.Track{{Source: music.SourceQQ, ID: 9, Name: "歌"}}}
	c := NewChain(Link{ID: music.SourceNetease, Provider: &link{id: music.SourceNetease}})
	c.SetCatalogs([]Link{
		{ID: music.SourceQQ, Catalog: qq},
		{ID: music.SourceKuwo, Catalog: kw},
	})
	if lrc, _, ok := c.ResolveLyricsElsewhere(song); !ok || lrc != "[tx]" {
		t.Errorf("words = %q, want QQ Music's first", lrc)
	}
	c.SetCatalogs([]Link{
		{ID: music.SourceKuwo, Catalog: kw},
		{ID: music.SourceQQ, Catalog: qq},
	})
	if lrc, _, ok := c.ResolveLyricsElsewhere(song); !ok || lrc != "[kw]" {
		t.Errorf("words = %q, want Kuwo's first", lrc)
	}
}

// FindOnNetease finds a song of another source on NetEase, by its name and
// artists: what its comments are shown from.
func TestChainFindsASongOnNetease(t *testing.T) {
	twin := music.Track{Source: music.SourceNetease, ID: 42, Name: "歌",
		Artists: []music.Artist{{Name: "歌手"}}, Duration: 200000}
	nt := &link{id: music.SourceNetease, finds: []music.Track{twin}}
	c := NewChain(Link{ID: music.SourceNetease, Provider: nt})
	got, ok := c.FindOnNetease(music.Track{Source: music.SourceQQ, ID: 7, Name: "歌",
		Artists: []music.Artist{{Name: "歌手"}}, Duration: 201000})
	if !ok || got.ID != 42 {
		t.Errorf("found %+v, ok = %v, want NetEase's record", got, ok)
	}
	// NetEase's own song is itself, without a search.
	if got, ok := c.FindOnNetease(twin); !ok || got.ID != 42 {
		t.Errorf("NetEase's own song = %+v, ok = %v", got, ok)
	}
	// A song NetEase does not have is not found.
	if _, ok := c.FindOnNetease(music.Track{Source: music.SourceQQ, ID: 8, Name: "别的歌"}); ok {
		t.Error("a song NetEase does not have was found")
	}
}

// A link whose address the player could not open is left out of the next
// try, so that the next link that has the song gets its turn: an imported
// script whose upstream is down must not keep a working source from it.
func TestChainLeavesOutALinkThatDidNotServe(t *testing.T) {
	script := &fakeScript{supports: map[string]bool{music.SourceNetease: true}, streams: true}
	nt := &link{id: music.SourceNetease, streams: map[string]bool{"歌": true}}
	c := NewChain(
		Link{ID: "script-a", Script: script},
		Link{ID: nt.id, Provider: nt},
	)
	track := music.Track{Source: music.SourceNetease, Name: "歌"}
	res, err := c.Resolve(track, "exhigh")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Link != "script-a" {
		t.Fatalf("Resolve served by link %q, want the script first", res.Link)
	}
	// That address did not open: the source the app ships has the song.
	next, err := c.ResolveExcept(track, "exhigh", "script-a")
	if err != nil {
		t.Fatalf("ResolveExcept: %v", err)
	}
	if next.Link != music.SourceNetease || next.Stream.URL != "http://netease/歌" {
		t.Errorf("ResolveExcept = link %q url %q, want NetEase's", next.Link, next.Stream.URL)
	}
}
