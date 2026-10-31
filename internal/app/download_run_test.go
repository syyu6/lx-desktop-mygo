package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// fakeProvider streams one song at a level it names, from a URL the test
// hands it.
type fakeProvider struct {
	src   string
	level string
	url   string
	calls int
}

func (f *fakeProvider) ID() string   { return f.src }
func (f *fakeProvider) Name() string { return f.src }

func (f *fakeProvider) SearchSongs(string, int, int) ([]music.Track, int, error) {
	return nil, 0, nil
}

func (f *fakeProvider) Stream(t music.Track, level string) (*music.Stream, error) {
	f.calls++
	return &music.Stream{URL: f.url, Level: f.level, Type: "mp3"}, nil
}

func (f *fakeProvider) Lyrics(music.Track) (music.Lyrics, error) { return music.Lyrics{}, nil }

// A song downloaded from another source is kept at the level the source
// served, not at the source's own id: the row says 无损, and the file plays
// back as lossless. The source id was written into the level column.
func TestDownloadKeepsTheLevelServed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The first bytes are checked, then the song is read.
		w.Header().Set("Content-Length", "4")
		w.Write([]byte("fLaC"))
	}))
	defer srv.Close()

	a, dir := downloadsApp(t)
	a.settings.DownloadOtherSource = true
	prov := &fakeProvider{src: music.SourceNetease, level: "lossless", url: srv.URL}
	a.chain = source.NewChain()
	a.chain.Set([]source.Link{{ID: music.SourceNetease, Name: "网易云", Provider: prov}})

	task := &download.Task{ID: "a@hires", Track: music.Track{ID: 1, Name: "晴天", Artists: []music.Artist{{Name: "周杰伦"}}},
		Level: "hires", Dir: dir}
	m := download.New(func(ctx context.Context, tk *download.Task, p *download.Progress) error {
		return a.downloadRun(ctx, tk, p)
	}, func() {}, func([]download.Task) {})
	m.Add([]download.Task{*task})
	m.SetMax(1)
	waitFor(t, func() bool {
		got, ok := m.Task(task.ID)
		return ok && (got.Status == download.Done || got.Status == download.Failed)
	})
	got, _ := m.Task(task.ID)
	if got.Status != download.Done {
		t.Fatalf("the download ended %q: %s", got.Status, got.Reason)
	}
	if got.Served != "lossless" {
		t.Errorf("the task is kept at %q, want the level served", got.Served)
	}
	if dlLevelLabel(got.Served) != "无损" {
		t.Errorf("the row would read %q", dlLevelLabel(got.Served))
	}
}

// A link whose address does not open, and which is the only link, ends the
// song rather than being asked for again: a source that answers with a page
// gave the same answer every time.
func TestDownloadGivesUpOnADeadAddress(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte("<html>no</html>"))
	}))
	defer srv.Close()

	a, dir := downloadsApp(t)
	// Not to look elsewhere: its own source alone, which is where the loop
	// was, as the skip the chain was given there was never used.
	a.settings.DownloadOtherSource = false
	prov := &fakeProvider{src: music.SourceNetease, level: "exhigh", url: srv.URL}
	a.chain = source.NewChain()
	a.chain.Set([]source.Link{{ID: music.SourceNetease, Name: "网易云", Provider: prov}})

	task := download.Task{ID: "a@exhigh", Track: music.Track{ID: 1, Name: "晴天"},
		Level: "exhigh", Dir: dir}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := a.downloadRun(ctx, &task, &download.Progress{})
	if err == nil {
		t.Fatal("a page that is not a song was saved as one")
	}
	if calls > 3 {
		t.Errorf("the address was asked %d times; it should be dropped after one", calls)
	}
}

// waitFor runs a condition until it holds, or the test gives up: a
// download runs off the main thread, and the page reads what it did.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("the download did not end")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
