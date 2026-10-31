package player

import (
	"testing"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
)

// The cache names a song by its source and its id there, NetEase's by its
// id alone as the songs kept before were, and refuses a name that would
// lead out of its folder.
func TestCacheName(t *testing.T) {
	cases := []struct {
		t    netease.Track
		want string
	}{
		{netease.Track{ID: 123}, "123"},
		{netease.Track{ID: 0, Source: music.SourceKuwo, Key: "MUSIC_7"}, "kw-MUSIC_7"},
		{netease.Track{ID: 0, Source: music.SourceQQ, Key: "003abc"}, "tx-003abc"},
		{netease.Track{ID: 0, Source: music.SourceKugou, Key: "/../../pwned"}, ""},
		{netease.Track{ID: 0, Source: music.SourceKugou, Key: "a/b"}, ""},
		{netease.Track{ID: 0, Source: "x"}, "x-0"},
		{netease.Track{ID: 0, Source: music.SourceKuwo, Key: "MUSIC 7"}, ""},
	}
	for _, c := range cases {
		if got := cacheName(c.t); got != c.want {
			t.Errorf("cacheName(%+v) = %q, want %q", c.t, got, c.want)
		}
	}
}

// A song kept whose source named a type of its own is read as its bytes
// say: an m4a kept as .m4a, not as the .mp3 a source may have called it.
func TestKeptTypeFollowsTheBytes(t *testing.T) {
	for _, c := range []struct{ file, want string }{
		{"testdata/sine.m4a", "m4a"},
		{"testdata/sine.aac", "aac"},
		{"testdata/seek.mp3", "mp3"},
		{"testdata/seek.flac", "flac"},
	} {
		if got := sniffType(c.file, "mp3"); got != c.want {
			t.Errorf("%s: kept as %q, want %q", c.file, got, c.want)
		}
	}
}
