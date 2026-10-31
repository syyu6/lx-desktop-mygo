package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// A run saves the song as it is: the file takes the format its own bytes
// say, its name comes from the settings, and the task keeps where it went,
// so that the page and the player can find it.
func TestDownloadRunSavesTheSong(t *testing.T) {
	a, dir := downloadsApp(t)
	a.settings.DownloadSkip = false
	a.settings.EmbedCover = false
	a.settings.EmbedLyric = false
	song := testSongMP3(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(song)
	}))
	defer srv.Close()
	a.chain = oneLink(srv.URL, "exhigh")

	task := download.Task{ID: "a@exhigh", Level: "exhigh", Dir: dir,
		Track: music.Track{ID: 1, Name: "晴天", Artists: []music.Artist{{Name: "周杰伦"}}}}
	runOne(t, a, &task)
	name := "晴天 - 周杰伦.mp3"
	if task.Path != filepath.Join(dir, name) || task.Name != name {
		t.Errorf("saved as %q in %q", task.Name, task.Path)
	}
	if task.Ext != "mp3" {
		t.Errorf("the file is named %q, want the format its bytes say", task.Ext)
	}
	if task.Served != "exhigh" {
		t.Errorf("kept at %q", task.Served)
	}
	// The bytes are the song's, and no .part is left behind.
	saved, err := os.ReadFile(task.Path)
	if err != nil || len(saved) == 0 {
		t.Fatalf("the file is not there: %v", err)
	}
	if parts, _ := filepath.Glob(filepath.Join(dir, ".lxmuc-*.part")); len(parts) > 0 {
		t.Errorf("a temporary file was left: %v", parts)
	}
}

// Told to leave what is there, a run whose file is already in the folder
// takes it for its own and saves nothing over it: the file the listener
// has is the file they wanted.
func TestDownloadRunKeepsAnExistingFile(t *testing.T) {
	a, dir := downloadsApp(t)
	a.settings.DownloadSkip = true
	mine := []byte("the listener's own")
	path := filepath.Join(dir, "晴天 - 周杰伦.mp3")
	if err := os.WriteFile(path, mine, 0o600); err != nil {
		t.Fatal(err)
	}
	task := download.Task{ID: "a@exhigh", Level: "exhigh", Dir: dir,
		Track: music.Track{ID: 1, Name: "晴天", Artists: []music.Artist{{Name: "周杰伦"}}}}
	// No source is asked: the file is found before any is.
	runOne(t, a, &task)
	if task.Path != path || task.Note != "文件已存在" {
		t.Errorf("the existing file was not kept: %+v", task)
	}
	if got, _ := os.ReadFile(path); string(got) != string(mine) {
		t.Error("the listener's file was written over")
	}
}

// runOne runs a task through the manager, as the page does, and returns it
// as it ended: the run reports its progress to a task, not to nothing.
func runOne(t *testing.T, a *app, task *download.Task) {
	t.Helper()
	m := download.New(func(ctx context.Context, tk *download.Task, p *download.Progress) error {
		return a.downloadRun(ctx, tk, p)
	}, func() {}, func([]download.Task) {})
	m.SetMax(1)
	m.Add([]download.Task{*task})
	waitFor(t, func() bool {
		got, ok := m.Task(task.ID)
		return ok && (got.Status == download.Done || got.Status == download.Failed)
	})
	got, _ := m.Task(task.ID)
	if got.Status != download.Done {
		t.Fatalf("the run ended %q: %s", got.Status, got.Reason)
	}
	*task = got
}

// oneLink is a chain of a single source that streams from url at level.
func oneLink(url, level string) *source.Chain {
	c := source.NewChain()
	c.Set([]source.Link{{ID: music.SourceNetease, Name: "网易云",
		Provider: &fakeProvider{src: music.SourceNetease, level: level, url: url}}})
	return c
}
