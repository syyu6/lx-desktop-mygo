package player

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/netease"
)

// A song that was played lately is in the cache, and Cached finds it: the
// download engine copies from there rather than fetching it again, which is
// what keeps a download the listener already paid for from costing traffic.
func TestCachedFindsAPlayedSong(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 64<<20)
	song := netease.Track{ID: 7, Name: "晴天"}
	b := testMP3(t)
	key := cacheKey(cacheName(song), "exhigh")
	if err := os.WriteFile(filepath.Join(dir, key+".mp3"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(netease.Stream{Size: int64(len(b)), Type: "mp3", Level: "exhigh"})
	if err := os.WriteFile(filepath.Join(dir, key+".json"), meta, 0o600); err != nil {
		t.Fatal(err)
	}

	path, st, ok := c.Cached(song, "exhigh")
	if !ok {
		t.Fatal("a song in the cache was not found")
	}
	if path != filepath.Join(dir, key+".mp3") || st.Level != "exhigh" {
		t.Errorf("found %q at %q", st.Level, path)
	}
	// The cache is searched from the level asked upward: asking for a
	// lower one finds what was kept, and asking for a higher one does not,
	// since a song at 320k is not the Hi-Res one that was asked for.
	if _, _, ok := c.Cached(song, "standard"); !ok {
		t.Error("a lower level asked did not find the song kept above it")
	}
	if _, _, ok := c.Cached(song, "hires"); ok {
		t.Error("a higher level asked found the song kept below it")
	}
	// A song the cache does not have is nothing.
	if _, _, ok := c.Cached(netease.Track{ID: 8, Name: "夜曲"}, "exhigh"); ok {
		t.Error("the cache found a song it never kept")
	}
}
