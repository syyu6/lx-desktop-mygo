package kuwo

import (
	"strings"
	"testing"

	"netease-music-mygo/internal/music"
)

// The endpoints are Kuwo's, over the network: these ask them, and fail
// when it does not answer, rather than skip.
func TestSearchSongs(t *testing.T) {
	tracks, total, err := Provider{}.SearchSongs("周杰伦", 3, 0)
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}
	if total == 0 || len(tracks) == 0 {
		t.Fatalf("found %d songs of %d", len(tracks), total)
	}
	for _, tr := range tracks {
		if tr.Source != "kw" || tr.ID == 0 || tr.Name == "" {
			t.Errorf("song %+v", tr)
		}
		if len(tr.Artists) == 0 {
			t.Errorf("song %q has no artist", tr.Name)
		}
	}
}

func TestSearchSongsPages(t *testing.T) {
	first, _, err := Provider{}.SearchSongs("周杰伦", 2, 0)
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}
	second, _, err := Provider{}.SearchSongs("周杰伦", 2, 2)
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}
	if len(first) > 0 && len(second) > 0 && first[0].ID == second[0].ID {
		t.Error("the second page repeats the first")
	}
}

func TestStream(t *testing.T) {
	tracks, _, err := Provider{}.SearchSongs("周杰伦", 1, 0)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("SearchSongs: %v, %d songs", err, len(tracks))
	}
	st, err := Provider{}.Stream(tracks[0], "exhigh")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if !strings.HasPrefix(st.URL, "http") {
		t.Errorf("stream = %+v", st)
	}
	if st.Level != "exhigh" || st.Type != "mp3" {
		t.Errorf("stream = %+v", st)
	}
}

func TestPlaylistAndItsSongs(t *testing.T) {
	lists, total, err := Provider{}.SearchPlaylists("周杰伦", 1, 0)
	if err != nil {
		t.Fatalf("SearchPlaylists: %v", err)
	}
	if total == 0 || len(lists) == 0 {
		t.Fatalf("found %d playlists of %d", len(lists), total)
	}
	pl, err := Provider{}.Playlist(lists[0].KeyOf())
	if err != nil {
		t.Fatalf("Playlist: %v", err)
	}
	if pl.Name == "" || len(pl.Tracks) == 0 {
		t.Fatalf("playlist %+v, %d songs", pl, len(pl.Tracks))
	}
	for _, tr := range pl.Tracks {
		if tr.Source != "kw" {
			t.Fatalf("a song of the playlist is from %q", tr.Source)
		}
	}
}

func TestLyrics(t *testing.T) {
	tracks, _, err := Provider{}.SearchSongs("晴天 周杰伦", 5, 0)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("SearchSongs: %v", err)
	}
	var l music.Lyrics
	for _, tr := range tracks {
		l, err = Provider{}.Lyrics(tr)
		if err == nil && l.Lyric != "" {
			break
		}
	}
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if !strings.Contains(l.Lyric, "[00:") {
		t.Errorf("lyrics = %.80q", l.Lyric)
	}
}
