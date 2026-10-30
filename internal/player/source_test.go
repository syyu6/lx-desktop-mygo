package player

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
)

// serve plays what the test's songs stream: an MP3 for a song, and
// whatever else the test passes.
func serve(t *testing.T, body []byte, contentType string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testMP3 is the song the tests stream.
func testMP3(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A song its own source will not stream is asked of the sources, and plays
// from the first that has it: what the app's chain answers.
func TestOpenAsksTheSources(t *testing.T) {
	srv := serve(t, testMP3(t), "")
	var asked []string
	p := New(func(func()) {})
	p.Resolve = func(t netease.Track, level string) (*netease.Stream, string, error) {
		asked = append(asked, level)
		return &netease.Stream{URL: srv.URL + "/song.mp3", Type: "mp3", Level: level}, music.SourceKuwo, nil
	}
	info, s, served, err := p.open(netease.Track{ID: 1, Name: "会员歌", Duration: 60000}, "exhigh", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if len(asked) != 1 || asked[0] != "exhigh" {
		t.Errorf("asked at %q, want exhigh once", asked)
	}
	if info.URL != srv.URL+"/song.mp3" {
		t.Errorf("opened %q, want the source's", info.URL)
	}
	// The song says where it came from, for the app to show.
	if served != music.SourceKuwo {
		t.Errorf("served by %q, want Kuwo", served)
	}
	// It decodes as the song it is.
	buf := make([]float32, 1024)
	if n, _ := s.Read(buf); n == 0 {
		t.Error("nothing decoded from the source's song")
	}
}

// A song the sources have, from a source whose stream the cache does not
// keep: what it plays is not named the song's own, as another service's
// recording may differ.
func TestOpenDoesNotCacheAnotherSourcesSong(t *testing.T) {
	srv := serve(t, testMP3(t), "")
	p := New(func(func()) {})
	p.Resolve = func(netease.Track, string) (*netease.Stream, string, error) {
		return &netease.Stream{URL: srv.URL + "/song.mp3", Type: "mp3"}, music.SourceKugou, nil
	}
	_, s, served, err := p.open(netease.Track{ID: 1, Duration: 60000}, "exhigh", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if served != music.SourceKugou {
		t.Errorf("served by %q, want Kugou", served)
	}
}

// The reason no source had the song is what the listener is told, rather
// than a source's own failure.
func TestOpenKeepsTheChainsReason(t *testing.T) {
	p := New(func(func()) {})
	p.Resolve = func(netease.Track, string) (*netease.Stream, string, error) {
		return nil, "", netease.ErrUnavailable
	}
	if _, _, _, err := p.open(netease.Track{ID: 1}, "exhigh", false); !errors.Is(err, netease.ErrUnavailable) {
		t.Errorf("open = %v, want the chain's reason", err)
	}
}

// With no chain at all, as in a test, NetEase answers for its own songs as
// it did.
func TestOpenWithoutChain(t *testing.T) {
	songURL = func(int64, string) (*netease.Stream, error) { return nil, netease.ErrUnavailable }
	defer func() { songURL = netease.SongURL }()
	p := New(func(func()) {})
	if _, _, _, err := p.open(netease.Track{ID: 1}, "exhigh", false); !errors.Is(err, netease.ErrUnavailable) {
		t.Errorf("open = %v, want NetEase's reason", err)
	}
	// A song of another source has no source without a chain.
	if _, _, _, err := p.open(netease.Track{Source: music.SourceKugou, ID: 2}, "exhigh", false); err == nil {
		t.Error("a song of a source the app does not have played")
	}
}

// An m4a is played: its AAC is decoded now, so a song a source script
// gives in it is no longer refused.
func TestOpenPlaysM4A(t *testing.T) {
	b, err := os.ReadFile("testdata/sine.m4a")
	if err != nil {
		t.Skipf("sine.m4a: %v", err)
	}
	srv := serve(t, b, "audio/mp4")
	p := New(func(func()) {})
	p.Resolve = func(netease.Track, string) (*netease.Stream, string, error) {
		return &netease.Stream{URL: srv.URL + "/song.m4a", Level: "exhigh"}, music.SourceKuwo, nil
	}
	_, s, _, err := p.open(netease.Track{ID: 1, Duration: 3000}, "exhigh", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if got := s.SampleRate(); got != 44100 {
		t.Errorf("sample rate %d", got)
	}
}

// A song in a format the player cannot decode is refused, rather than read
// as MP3 and played as noise.
func TestOpenRefusesUnsupportedFormat(t *testing.T) {
	// An MP4's first bytes, and nothing after them: an `ftyp` at byte 4,
	// which an MP3 never has, with no index to read.
	mp4 := []byte("\x00\x00\x00\x20ftypM4A \x00\x00\x00\x00")
	srv := serve(t, mp4, "")
	p := New(func(func()) {})
	p.Resolve = func(netease.Track, string) (*netease.Stream, string, error) {
		return &netease.Stream{URL: srv.URL + "/song.m4a", Level: "exhigh"}, music.SourceKuwo, nil
	}
	_, _, _, err := p.open(netease.Track{ID: 1, Duration: 60000}, "exhigh", false)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("open = %v, want ErrUnsupportedFormat", err)
	}
	if !strings.Contains(err.Error(), "格式") {
		t.Errorf("the reason the listener reads is %q", err)
	}
	// And what is not an audio container at all, read as one, is the
	// same.
	flac := []byte("fLaC\x00\x00\x00\x22")
	srv2 := serve(t, flac, "")
	p.Resolve = func(netease.Track, string) (*netease.Stream, string, error) {
		return &netease.Stream{URL: srv2.URL + "/song.flac", Level: "exhigh"}, music.SourceKuwo, nil
	}
	if _, _, _, err := p.open(netease.Track{ID: 2, Duration: 60000}, "exhigh", false); err == nil {
		t.Error("a FLAC with no stream info played")
	}
}

// A source that answers with a page rather than a song is not played as
// noise: the listener is told it got no file.
func TestOpenRefusesAPage(t *testing.T) {
	page := []byte("<!DOCTYPE HTML PUBLIC \"-//W3C//DTD HTML 4.01//EN\">\n<html></html>")
	srv := serve(t, page, "text/html")
	p := New(func(func()) {})
	p.Resolve = func(netease.Track, string) (*netease.Stream, string, error) {
		return &netease.Stream{URL: srv.URL + "/song.mp3", Level: "exhigh"}, music.SourceNetease, nil
	}
	_, _, _, err := p.open(netease.Track{ID: 1, Duration: 60000}, "exhigh", false)
	if !errors.Is(err, ErrNotAudio) {
		t.Errorf("open = %v, want ErrNotAudio", err)
	}
}

// A song NetEase has no lyrics for is asked of the other sources, so its
// lyrics still show rather than an empty pane.
func TestLyricsFallBackToTheOtherSources(t *testing.T) {
	lyricsOf = func(int64) (string, string, string, error) { return "", "", "", nil }
	defer func() { lyricsOf = netease.Lyrics }()
	p := New(func(func()) {})
	own := false
	p.ResolveLyrics = func(netease.Track) (string, string, bool) {
		own = true
		return "", "", false
	}
	p.ResolveLyricsElsewhere = func(netease.Track) (string, string, bool) {
		return "[00:01.00]来自音源", "[00:01.00]from a source", true
	}
	lrc, trans, _, err := p.fetchLyrics(netease.Track{ID: 1})
	if err != nil {
		t.Fatalf("fetchLyrics: %v", err)
	}
	if !strings.Contains(lrc, "来自音源") || !strings.Contains(trans, "from a source") {
		t.Errorf("lyrics = %q, translation = %q, want the chain's", lrc, trans)
	}
	if own {
		t.Error("NetEase's own source was asked again, after NetEase answered")
	}
}

// NetEase's own lyrics win: the chain is asked only where they are absent.
func TestLyricsPreferNetEase(t *testing.T) {
	lyricsOf = func(int64) (string, string, string, error) { return "[00:01.00]网易的", "", "", nil }
	defer func() { lyricsOf = netease.Lyrics }()
	p := New(func(func()) {})
	asked := false
	p.ResolveLyrics = func(netease.Track) (string, string, bool) {
		asked = true
		return "", "", false
	}
	if lrc, _, _, _ := p.fetchLyrics(netease.Track{ID: 1}); !strings.Contains(lrc, "网易的") {
		t.Errorf("lyrics = %q, want NetEase's", lrc)
	}
	if asked {
		t.Error("the chain was asked though NetEase had the lyrics")
	}
}

// A song of another source has no NetEase lyrics to ask for: its own words
// come from the chain.
func TestLyricsOfAnotherSource(t *testing.T) {
	p := New(func(func()) {})
	tt := t
	p.ResolveLyrics = func(t netease.Track) (string, string, bool) {
		if t.Src() != music.SourceKuwo {
			tt.Errorf("lyrics asked for %q", t.Src())
		}
		return "[00:02.00]酷我的", "", true
	}
	if lrc, _, _, err := p.fetchLyrics(netease.Track{Source: music.SourceKuwo, ID: 5}); err != nil || !strings.Contains(lrc, "酷我的") {
		t.Errorf("lyrics = %q, %v", lrc, err)
	}
}

// A link that names an address and then does not serve it — an imported
// script whose upstream is down answers with a URL that is a 404 — gives
// way to the next link that has the song, rather than failing the song.
func TestOpenGivesWayToASourceThatDoesNotServeIt(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("<html>not found</html>"))
	}))
	t.Cleanup(bad.Close)
	good := serve(t, testMP3(t), "")

	var asked []string
	p := New(func(func()) {})
	p.ResolveExcept = func(t netease.Track, level, skip string) (*netease.Stream, string, string, error) {
		asked = append(asked, skip)
		switch skip {
		case "":
			return &netease.Stream{URL: bad.URL, Type: "mp3", Level: level}, music.SourceNetease, "script-a", nil
		case "script-a":
			return &netease.Stream{URL: good.URL, Type: "mp3", Level: level}, music.SourceKuwo, "kw", nil
		}
		return nil, "", "", netease.ErrUnavailable
	}
	_, s, served, err := p.open(netease.Track{ID: 1, Duration: 60000}, "exhigh", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if len(asked) != 2 || asked[0] != "" || asked[1] != "script-a" {
		t.Errorf("asked with skips %q, want the script left out on the second try", asked)
	}
	if served != music.SourceKuwo {
		t.Errorf("served by %q, want the source that played it", served)
	}
}

// When every link that names an address fails to serve it, the first of
// those failures is what the listener is told: the song was there and did
// not come, rather than the sources having nothing.
func TestOpenReportsTheFirstFailuresReason(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("<html>not found</html>"))
	}))
	t.Cleanup(bad.Close)
	p := New(func(func()) {})
	p.ResolveExcept = func(t netease.Track, level, skip string) (*netease.Stream, string, string, error) {
		if skip == "b" {
			return nil, "", "", netease.ErrUnavailable
		}
		return &netease.Stream{URL: bad.URL, Type: "mp3", Level: level}, music.SourceNetease, skip + "b", nil
	}
	_, _, _, err := p.open(netease.Track{ID: 1, Duration: 60000}, "exhigh", false)
	if err == nil {
		t.Fatal("open succeeded with nothing to play")
	}
	if errors.Is(err, netease.ErrUnavailable) {
		t.Errorf("open = %v, want the failure of the address that did not serve it", err)
	}
}

// With the next song fetched while the one playing plays, its turn plays
// what was fetched, rather than asking the sources again.
func TestPrefetchTakesTheNextSongsSong(t *testing.T) {
	srv := serve(t, testMP3(t), "")
	var mu sync.Mutex
	var asked []int64
	p := New(func(func()) {})
	p.Prefetch = func() bool { return true }
	p.Resolve = func(t netease.Track, level string) (*netease.Stream, string, error) {
		mu.Lock()
		asked = append(asked, t.ID)
		mu.Unlock()
		return &netease.Stream{URL: srv.URL + "/song.mp3", Type: "mp3", Level: level}, music.SourceNetease, nil
	}
	// The second song's, fetched ahead of its turn.
	p.Cue([]netease.Track{{ID: 1, Duration: 60000}, {ID: 2, Duration: 60000}})
	p.startAhead()
	if p.ahead == nil || p.ahead.track.ID != 2 {
		t.Fatal("the next song was not fetched")
	}
	ahead := p.takeAhead(netease.Track{ID: 2, Duration: 60000}, "exhigh")
	if ahead == nil {
		t.Fatal("the fetched song was not kept for its turn")
	}
	if got, ok := ahead.wait(); !ok || got.err != nil {
		t.Fatalf("the fetched song did not open: %v", got.err)
	} else {
		defer got.s.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0] != 2 {
		t.Errorf("asked %v, want the second song once, ahead of its turn", asked)
	}
}

// A song fetched ahead that is not the next one's is let go, rather than
// played as another song's.
func TestPrefetchDropsAnotherSongsSong(t *testing.T) {
	srv := serve(t, testMP3(t), "")
	p := New(func(func()) {})
	p.Prefetch = func() bool { return true }
	p.Resolve = func(netease.Track, string) (*netease.Stream, string, error) {
		return &netease.Stream{URL: srv.URL + "/song.mp3", Type: "mp3"}, music.SourceNetease, nil
	}
	p.Cue([]netease.Track{{ID: 1, Duration: 60000}, {ID: 2, Duration: 60000}})
	p.startAhead()
	if got := p.takeAhead(netease.Track{ID: 3, Duration: 60000}, "exhigh"); got != nil {
		t.Error("another song's fetched song was kept")
	}
	if p.ahead != nil {
		t.Error("the fetched song is still held")
	}
}
