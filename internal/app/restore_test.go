package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
)

// fakeOldLogin replaces the read of the keychain with one of the test's own.
func fakeOldLogin(t *testing.T, prof *netease.Profile, err error) {
	t.Helper()
	adopt, account := adoptOldLogin, accountOf
	t.Cleanup(func() { adoptOldLogin, accountOf = adopt, account })
	adoptOldLogin = func() (bool, error) {
		if err != nil {
			return false, err
		}
		return true, nil
	}
	accountOf = func() (*netease.Profile, error) { return prof, nil }
}

// The dialog offers the login an older release left in the keychain, and
// reads it only when the listener asks: the Keychain asks them first, and
// that question must follow their click rather than an update.
func TestRestoreOldLoginIsOffered(t *testing.T) {
	fakeOldLogin(t, &netease.Profile{UserID: 7, Nickname: "旧用户"}, nil)
	a := newTestApp()
	a.settings = defaultSettings()
	a.login = loginState{open: true}
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	if err := tt.Click("从旧版本恢复登录"); err != nil {
		t.Fatalf("the dialog offers no old login: %v", err)
	}
	if !a.login.restoring {
		t.Fatal("the click did not start reading the old login")
	}
}

// The old login, once read, signs the listener in, as a scan does.
func TestRestoredOldLoginSignsIn(t *testing.T) {
	prof := &netease.Profile{UserID: 7, Nickname: "旧用户"}
	fakeOldLogin(t, prof, nil)
	a := newTestApp()
	a.settings = defaultSettings()
	a.login = loginState{open: true}
	a.oldLoginRead(a.login.gen, nil, prof, nil)
	if a.profile == nil || a.profile.UserID != prof.UserID || a.profile.Nickname != prof.Nickname {
		t.Fatalf("signed in as %+v, want %+v", a.profile, prof)
	}
	if !a.signedInTo(music.SourceNetease) {
		t.Error("the restored login is not NetEase's account")
	}
	if !a.login.finished {
		t.Error("the dialog did not show the welcome")
	}
}

// With no old login there, the dialog says so and stays for the scan, and
// the system's question is not brought up for nothing.
func TestRestoreWithNoOldLogin(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.login = loginState{open: true}
	a.oldLoginRead(a.login.gen, netease.ErrNoOldLogin, nil, nil)
	if !a.login.open {
		t.Error("the dialog closed with no old login to restore")
	}
	if a.login.restoreErr == "" {
		t.Error("the dialog does not say there is no old login")
	}
	if a.profile != nil {
		t.Error("signed in with no old login")
	}
}
