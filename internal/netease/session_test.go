package netease

import (
	"os"
	"path/filepath"
	"testing"
)

// sessionIn puts the session's file in a directory of the test's own.
func sessionIn(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := sessionDir
	sessionDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { sessionDir = old })
	return dir
}

// The session is kept in its file and read back, and forgotten when the
// listener signs out: the login was a Keychain item, which every update
// made the app ask about again.
func TestSessionKeptInItsFile(t *testing.T) {
	dir := sessionIn(t)
	if RestoreSession() {
		t.Fatal("a session was read from an empty directory")
	}
	if err := writeSession([]byte("MUSIC_U=abc; __csrf=def")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session")
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("the session is not at %s: %v", path, err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the session's file is %o, want 600", perm)
	}
	if !RestoreSession() {
		t.Fatal("the session was not read back")
	}
	if got, want := len(httpClient.Jar.Cookies(siteURL)), 2; got != want {
		t.Errorf("%d cookies in the jar, want %d", got, want)
	}
	ClearSession()
	if RestoreSession() {
		t.Error("a session was read after signing out")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("signing out left %s", path)
	}
}
