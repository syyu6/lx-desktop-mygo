package qq

import (
	"strings"
	"testing"

	"netease-music-mygo/internal/music"
)

// The endpoints are QQ Music's, over the network: these ask them, and fail
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
		if tr.Source != music.SourceQQ || tr.Key == "" || tr.Name == "" {
			t.Errorf("song %+v", tr)
		}
		if len(tr.Artists) == 0 {
			t.Errorf("song %q has no artist", tr.Name)
		}
		// A cover, which the search names through the album's mid.
		if !strings.HasPrefix(tr.Album.PicURL, "https://y.gtimg.cn/") {
			t.Errorf("song %q has no cover: %q", tr.Name, tr.Album.PicURL)
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
	if len(first) > 0 && len(second) > 0 && first[0].Key == second[0].Key {
		t.Error("the second page repeats the first")
	}
}

// A free song streams without an account, and its words come with it.
func TestStreamAndLyrics(t *testing.T) {
	tracks, _, err := Provider{}.SearchSongs("纯音乐", 10, 0)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("SearchSongs: %v, %d songs", err, len(tracks))
	}
	var played *music.Track
	for i := range tracks {
		st, err := Provider{}.Stream(tracks[i], "standard")
		if err == nil && st.URL != "" {
			played = &tracks[i]
			if !strings.HasPrefix(st.URL, "http") || st.Type == "" {
				t.Errorf("stream of %q = %+v", tracks[i].Name, st)
			}
			break
		}
	}
	if played == nil {
		t.Fatal("no song of the page streams")
	}
	if ly, err := (Provider{}).Lyrics(*played); err == nil {
		if !strings.Contains(ly.Lyric, "[") {
			t.Errorf("the words of %q are not timed: %q", played.Name, ly.Lyric[:min(len(ly.Lyric), 80)])
		}
	}
}

// A playlist opens with its songs, each with the mid a stream is asked by.
func TestPlaylist(t *testing.T) {
	lists, total, err := Provider{}.SearchPlaylists("电子", 3, 0)
	if err != nil {
		t.Fatalf("SearchPlaylists: %v", err)
	}
	if total == 0 || len(lists) == 0 {
		t.Fatalf("found %d playlists of %d", len(lists), total)
	}
	pl, err := Provider{}.Playlist(lists[0].Key)
	if err != nil {
		t.Fatalf("Playlist: %v", err)
	}
	if pl.Name == "" || len(pl.Tracks) == 0 {
		t.Fatalf("playlist %+v", pl)
	}
	for _, tr := range pl.Tracks[:min(len(pl.Tracks), 5)] {
		if tr.Source != music.SourceQQ || tr.Key == "" {
			t.Errorf("song %+v", tr)
		}
	}
}
