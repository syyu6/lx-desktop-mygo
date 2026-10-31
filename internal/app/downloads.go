package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/download"
)

// The download tasks are kept in downloads.json in the user data directory:
// their songs, levels and where their files went. They are written as one
// ends or is added, and read once as the app opens, paused: nothing starts
// on its own after a restart, as lx-music-desktop leaves it.

// downloadsPath is where the tasks are kept.
func downloadsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "downloads.json"), nil
}

// saveDownloads keeps the tasks where the app reads them again, written to
// a file beside and renamed into place: a write that fails half way leaves
// the list it had rather than none.
func saveDownloads(tasks []download.Task) {
	path, err := downloadsPath()
	if err != nil {
		return
	}
	b, err := json.MarshalIndent(downloadFile{Version: 1, Tasks: tasks}, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
	}
}

// loadDownloads returns the tasks kept from the last session.
func loadDownloads() []download.Task {
	path, err := downloadsPath()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f downloadFile
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	return f.Tasks
}

// downloadFile is what downloads.json holds.
type downloadFile struct {
	Version int             `json:"version"`
	Tasks   []download.Task `json:"tasks"`
}

// newDownloads makes the manager, hands it the tasks the last session kept,
// and sets how many run at once.
func (a *app) newDownloads() {
	a.dl = download.New(a.downloadRun, a.redraw, saveDownloads)
	a.dl.SetMax(a.settings.DownloadMax)
	a.dl.Restore(loadDownloads())
	// A task whose file the listener took out of the folder is not done any
	// more: it is paused, with the song it was asked for, and the page's
	// 「继续」 asks for it again. Not started here: at launch nothing has
	// been asked, and with downloads switched off the run would fail with
	// an error about the settings for a song that was saved fine.
	a.dl.Requeue(func(t *download.Task) bool {
		if t.Status != download.Done || t.Path == "" {
			return false
		}
		info, err := os.Stat(t.Path)
		return err != nil || !info.Mode().IsRegular()
	})
}

// setDownloadMax gives the manager the number of tasks the settings allow
// at once.
func (a *app) setDownloadMax() {
	if a.dl != nil {
		a.dl.SetMax(a.settings.DownloadMax)
	}
}
