package kugou

import (
	"testing"

	"netease-music-mygo/internal/music"
)

// A song Kugou's search gives is the app's: its hash kept as its key, as a
// music source asks Kugou by it, and its length and album read from the
// strings and numbers Kugou mixes.
func TestSongBecomesTrack(t *testing.T) {
	s := song{
		FileHash: "B3A52A7A958BF0AED0EBFBA2E9A818B7",
		Name:     "晴天",
		Album:    "叶惠美",
		AlbumID:  "966846",
		Singers:  []struct{ Name string }{{Name: "周杰伦"}},
		Duration: "269",
		Audioid:  float64(20505418),
	}
	tr := s.track()
	if tr.Source != music.SourceKugou || tr.Key != s.FileHash {
		t.Fatalf("track %+v", tr)
	}
	if tr.Name != "晴天" || tr.Album.ID != 966846 || tr.Album.Name != "叶惠美" {
		t.Errorf("track %+v", tr)
	}
	if tr.Duration != 269000 {
		t.Errorf("duration %d", tr.Duration)
	}
	if len(tr.Artists) != 1 || tr.Artists[0].Name != "周杰伦" {
		t.Errorf("artists %+v", tr.Artists)
	}
}

// A song with no hash is no song to stream, and is left out: a music source
// asks Kugou by the hash.
func TestSongWithoutHash(t *testing.T) {
	if got := (&song{Name: "晴天"}).track().Key; got != "" {
		t.Errorf("key %q, want none", got)
	}
}

// The higher quality's hash stands in where the plain one is missing.
func TestSongFallsBackToTheHigherHash(t *testing.T) {
	s := song{Name: "歌", HQFileHash: "HQ"}
	if got := s.track().Key; got != "HQ" {
		t.Errorf("key %q", got)
	}
}

// A number comes as a string or a number, and reads either way.
func TestNumReadsBothWays(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want int64
	}{{"42", 42}, {float64(42), 42}, {nil, 0}, {"", 0}} {
		if got := num(tc.in); got != tc.want {
			t.Errorf("num(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
