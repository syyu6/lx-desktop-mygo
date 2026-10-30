package migu

import (
	"strings"
	"testing"

	"netease-music-mygo/internal/music"
)

// The endpoints are Migu's, over the network: these ask them, and fail
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
		if tr.Source != music.SourceMigu || tr.Key == "" || tr.Name == "" {
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
	if len(first) > 0 && len(second) > 0 && first[0].Key == second[0].Key {
		t.Error("the second page repeats the first")
	}
}

// A song streams, and its words come with it, as its own record names
// them.
func TestStreamAndLyrics(t *testing.T) {
	tracks, _, err := Provider{}.SearchSongs("周杰伦", 5, 0)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("SearchSongs: %v, %d songs", err, len(tracks))
	}
	tr := tracks[0]
	st, err := Provider{}.Stream(tr, "standard")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if !strings.HasPrefix(st.URL, "http") {
		t.Errorf("stream = %+v", st)
	}
	ly, err := Provider{}.Lyrics(tr)
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if !strings.Contains(ly.Lyric, "[") {
		t.Errorf("the words of %q are not timed: %q", tr.Name, ly.Lyric[:min(len(ly.Lyric), 80)])
	}
}

// The album's cover is one of its images, the largest the search names.
func TestSongHasALargeCover(t *testing.T) {
	s := song{
		Img1: "/data/oss/resource/00/small.webp",
		Img3: "/data/oss/resource/00/large.webp",
	}
	if got := s.img(); got != s.Img3 {
		t.Errorf("img = %q, want the largest", got)
	}
	if got := Cover(s.Img3); !strings.HasPrefix(got, "https://d.musicapp.migu.cn/") {
		t.Errorf("cover = %q, want Migu's host", got)
	}
	// An address already whole is left as it is.
	if got := Cover("https://x/y.webp"); got != "https://x/y.webp" {
		t.Errorf("cover = %q", got)
	}
}
