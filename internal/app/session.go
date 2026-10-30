package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// Where playback was left is kept in session.json in the user data
// directory: on quitting, and every little while as it plays, in case the
// app does not get to quit.

func sessionPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// sessionFile is where the session is kept.
func (a *app) sessionFile() string {
	path, err := sessionPath()
	if err != nil {
		return ""
	}
	return path
}

// saveSession keeps where playback is, if it moved on since last kept.
func (a *app) saveSession() {
	s, ok := a.pl.Session()
	if !ok {
		return
	}
	b, err := json.Marshal(s)
	if err != nil || string(b) == a.savedSession {
		return
	}
	path, err := sessionPath()
	if err != nil {
		return
	}
	if os.WriteFile(path+".part", b, 0o600) == nil && os.Rename(path+".part", path) == nil {
		a.savedSession = string(b)
	}
}

// sessionEvery is how long after playback changed, and how often as it
// plays, the session is kept.
const sessionEvery = 30 * time.Second

// keepSession saves the session a while after playback changed, and every
// while again as it plays. Paused, nothing is due, and the app sleeps.
func (a *app) keepSession() {
	if a.sessionDue {
		return
	}
	a.sessionDue = true
	sessionAfter(sessionEvery, func() {
		a.sessionDue = false
		a.saveSession()
		if a.pl.Playing() {
			a.keepSession()
		}
	})
}

// sessionAfter runs fn on the main thread after d, without redrawing a
// window as Update would.
var sessionAfter = func(d time.Duration, fn func()) {
	time.AfterFunc(d, func() { mygo.RunOnMain(fn) })
}

// restoreSession picks up playback where it was left, paused, once the
// listener's session is back: the songs they may play depend on it.
func (a *app) restoreSession() {
	if a.restored {
		return
	}
	a.restored = true
	// The player has what plays now, or nothing: the song the window was
	// opened in is no longer needed.
	a.seed = nil
	s, ok := a.readSession()
	if !ok {
		return
	}
	a.pl.Restore(s)
	if s.Radio != "" && a.pl.Current() != nil {
		a.pl.AttachRadio(s.Radio, a.radioFeed(s.Radio))
	}
}

// readSession returns where playback was left, as the last time it was
// kept, and keeps it as what is there.
func (a *app) readSession() (player.Session, bool) {
	path := a.sessionFile()
	if path == "" {
		return player.Session{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return player.Session{}, false
	}
	var s player.Session
	if json.Unmarshal(b, &s) != nil {
		return player.Session{}, false
	}
	a.savedSession = string(b)
	return s, true
}

// seedTrack returns the song the session kept, as the window is about to
// be made: the window opens in that song's colors, rather than in the
// system's until the session is back and the player has it. It reads a
// file, and starts nothing.
func (a *app) seedTrack() *netease.Track {
	s, ok := a.readSession()
	if !ok || s.At < 0 || s.At >= len(s.Queue) {
		return nil
	}
	t := s.Queue[s.At]
	return &t
}
