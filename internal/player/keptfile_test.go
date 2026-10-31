package player

import (
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/netease"
)

// A song the listener downloaded plays from its own file on any later
// play: no source is asked, and no network is used. The file is kept at
// the level the listener chose, which is not necessarily the level playing
// asks for.
func TestOpenPlaysADownloadedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "晴天.mp3")
	if err := os.WriteFile(path, testMP3(t), 0o644); err != nil {
		t.Fatal(err)
	}
	p := New(func(func()) {})
	asked := false
	p.Resolve = func(netease.Track, string) (*netease.Stream, string, error) {
		asked = true
		return nil, "", os.ErrNotExist
	}
	p.KeptFile = func(t netease.Track) (string, string, bool) {
		return path, "lossless", true
	}
	info, s, _, err := p.open(netease.Track{ID: 1, Name: "晴天", Duration: 60000}, "exhigh", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if asked {
		t.Error("a downloaded song asked a source for its stream")
	}
	if info.Level != "lossless" {
		t.Errorf("opened at %q, want the level it was kept at", info.Level)
	}
	if info.Type != "mp3" {
		t.Errorf("opened as %q, want the file's own type", info.Type)
	}
	buf := make([]float32, 1024)
	if n, _ := s.Read(buf); n == 0 {
		t.Error("nothing decoded from the downloaded file")
	}
}

// A file that is gone, or will not open, gives way to the sources rather
// than failing the song.
func TestOpenFallsBackWhenTheFileIsGone(t *testing.T) {
	srv := serve(t, testMP3(t), "")
	p := New(func(func()) {})
	gone := filepath.Join(t.TempDir(), "gone.mp3")
	p.KeptFile = func(netease.Track) (string, string, bool) { return gone, "lossless", true }
	asked := false
	p.Resolve = func(t netease.Track, level string) (*netease.Stream, string, error) {
		asked = true
		return &netease.Stream{URL: srv.URL + "/song.mp3", Type: "mp3", Level: level}, "kuwo", nil
	}
	_, s, _, err := p.open(netease.Track{ID: 1, Name: "晴天", Duration: 60000}, "exhigh", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s.Close()
	if !asked {
		t.Error("a file that is gone did not give way to the sources")
	}
}
