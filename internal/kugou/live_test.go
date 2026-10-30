package kugou

import (
	"strings"
	"testing"

	"netease-music-mygo/internal/music"
)

// A song's cover is the address the search names, its size filled in.
func TestSongCover(t *testing.T) {
	s := song{Image: "http://imge.kugou.com/stdmusic/{size}/20230920/x.jpg"}
	if got := s.cover(240); got != "http://imge.kugou.com/stdmusic/240/20230920/x.jpg" {
		t.Errorf("cover = %q", got)
	}
	if got := (&song{}).cover(240); got != "" {
		t.Errorf("cover of a song without one = %q", got)
	}
	// The album's is the song's, when the search names it so.
	if got := (&song{Image: "http://imge.kugou.com/stdmusic/{size}/a.jpg"}).track().Album.PicURL; !strings.Contains(got, "/240/") {
		t.Errorf("track cover = %q", got)
	}
}

// The lyric service finds a song's words by its hash, and serves them
// base64-encoded, timed.
func TestLyrics(t *testing.T) {
	tracks, _, err := Catalog{}.SearchSongs("晴天 周杰伦", 3, 0)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("SearchSongs: %v, %d songs", err, len(tracks))
	}
	var tr music.Track
	for _, c := range tracks {
		if c.Key != "" {
			tr = c
			break
		}
	}
	if tr.Key == "" {
		t.Fatal("no song has a hash")
	}
	ly, err := Catalog{}.Lyrics(tr)
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if !strings.Contains(ly.Lyric, "[00:") {
		t.Errorf("the words are not timed: %q", ly.Lyric[:min(len(ly.Lyric), 80)])
	}
}
