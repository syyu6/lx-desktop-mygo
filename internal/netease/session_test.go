package netease

import (
	"errors"
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

// keychainIn replaces what the keychain would answer, and tells on each
// read: launching the app must not make one.
func keychainIn(t *testing.T, secret string) *int {
	t.Helper()
	reads := new(int)
	old := keychainGet
	keychainGet = func(key string) ([]byte, error) {
		*reads++
		if secret == "" {
			return nil, ErrNoOldLogin
		}
		return []byte(secret), nil
	}
	t.Cleanup(func() { keychainGet = old })
	return reads
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

// Launching reads no keychain: every update is a new binary to it, and it
// asks the listener before one reads, which is the question the app no
// longer brings up on its own. The old login is there for the asking.
func TestOldLoginOnlyOnAsking(t *testing.T) {
	dir := sessionIn(t)
	reads := keychainIn(t, "MUSIC_U=old; __csrf=csrf")
	if RestoreSession() {
		t.Fatal("launching read the keychain")
	}
	if *reads != 0 {
		t.Fatalf("%d keychain reads at launch, want none", *reads)
	}
	ok, err := AdoptKeychainSession()
	if err != nil || !ok {
		t.Fatalf("adopting: %v, %v", ok, err)
	}
	if *reads != 1 {
		t.Errorf("%d keychain reads, want one", *reads)
	}
	if _, err := os.Stat(filepath.Join(dir, "session")); err != nil {
		t.Errorf("the old login was not kept in its file: %v", err)
	}
	if got := len(httpClient.Jar.Cookies(siteURL)); got != 2 {
		t.Errorf("%d cookies from the old login, want 2", got)
	}
}

// With nothing left in the keychain there is nothing to take, and the
// dialog says so rather than bringing up the system's question for nothing.
func TestAdoptingWithNoOldLogin(t *testing.T) {
	sessionIn(t)
	keychainIn(t, "")
	if ok, err := AdoptKeychainSession(); ok || !errors.Is(err, ErrNoOldLogin) {
		t.Fatalf("adopted %v, %v", ok, err)
	}
	if RestoreSession() {
		t.Error("a login was restored from nothing")
	}
}
