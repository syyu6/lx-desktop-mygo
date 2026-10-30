package kuwo

import (
	"testing"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// Kuwo's square answers with categories, and with playlists under one.
// Kuwo's servers are asked, so this is a live test.
func TestSquare(t *testing.T) {
	var p source.Provider = Provider{}
	sq, ok := p.(source.Square)
	if !ok {
		t.Fatal("Kuwo has no square")
	}
	tags, err := sq.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) < 3 {
		t.Fatalf("%d tags", len(tags))
	}
	for _, tg := range tags {
		if tg.Name == "" {
			t.Fatalf("a tag of no name: %+v", tags)
		}
	}
	hot, err := sq.SquarePlaylists("", 36, 0)
	if err != nil {
		t.Fatalf("SquarePlaylists (all): %v", err)
	}
	if len(hot) == 0 || hot[0].Name == "" || hot[0].ID == 0 {
		t.Fatalf("the hot page %+v", hot)
	}
	if hot[0].Source != music.SourceKuwo {
		t.Errorf("source %q", hot[0].Source)
	}
	// A page after the first is asked for by its number.
	next, err := sq.SquarePlaylists("", 36, 36)
	if err != nil {
		t.Fatalf("SquarePlaylists (page 2): %v", err)
	}
	if len(next) > 0 && next[0].ID == hot[0].ID {
		t.Error("the page after the first is the first again")
	}
	// A category's playlists are the ones it names.
	under, err := sq.SquarePlaylists(tags[0].Name, 36, 0)
	if err != nil {
		t.Fatalf("SquarePlaylists (%s): %v", tags[0].Name, err)
	}
	if len(under) == 0 {
		t.Errorf("no playlists under %q", tags[0].Name)
	}
}
