package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/account"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
)

// signingInApp is an app whose sources may be signed in to, as the
// packages register them, and with the dialog on the source it is asked.
func signingInApp(t *testing.T, src string) *app {
	t.Helper()
	a := newTestApp()
	a.loadRegistry()
	a.openLoginTo(src, "")
	return a
}

// The dialog is one source's sign-in at a time, and names the sources it may
// sign in to: a listener signed in to one and not the other is shown both.
func TestTheLoginDialogNamesItsSources(t *testing.T) {
	a := signingInApp(t, music.SourceNetease)
	a.login.code, a.login.err = nil, ""
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	for _, want := range []string{"网易云", "QQ 音乐"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("the dialog offers no %q", want)
		}
	}
	if a.login.src != music.SourceNetease {
		t.Errorf("the dialog is on %q", a.login.src)
	}
}

// Choosing QQ Music asks QQ for its code, and the dialog shows the two ways
// in it has.
func TestQQSignInOffersBothWaysIn(t *testing.T) {
	a := signingInApp(t, music.SourceQQ)
	if a.login.src != music.SourceQQ {
		t.Fatalf("the dialog is on %q", a.login.src)
	}
	// The code is asked for off the main thread; the dialog is drawn with
	// what it has in the meantime, which is the shimmer.
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	for _, want := range []string{"QQ 扫码", "微信扫码"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("QQ's sign-in offers no %q", want)
		}
	}
	// NetEase has one way in, and the wrong code's sentences are not shown.
	if a.loginNote(music.SourceNetease) == a.loginNote(music.SourceQQ) {
		t.Error("the two sources say the same of their sign-in")
	}
}

// A source signed in on its own leaves the others as they were: signing out
// of one does not sign out of the other.
func TestAccountsAreSignedInOneAtATime(t *testing.T) {
	a := newTestApp()
	a.accounts = map[string]*account.Profile{
		music.SourceNetease: {Source: music.SourceNetease, UserID: 1, Nickname: "甲"},
		music.SourceQQ:      {Source: music.SourceQQ, UserID: 2, Nickname: "乙"},
	}
	a.profile = &netease.Profile{UserID: 1, Nickname: "甲"}
	if !a.signedInTo(music.SourceNetease) || !a.signedInTo(music.SourceQQ) {
		t.Fatal("both sources are signed in, and neither says so")
	}
	a.signOut(music.SourceQQ)
	if a.signedInTo(music.SourceQQ) {
		t.Error("QQ is still signed in")
	}
	if !a.signedInTo(music.SourceNetease) {
		t.Error("signing out of QQ signed out of NetEase too")
	}
}

// The foot shows an account when only a source other than NetEase is signed
// in, rather than the way in: the listener is signed in, and the way in says
// they are not.
func TestTheFootShowsWhicheverSourceIsSignedIn(t *testing.T) {
	a := newTestApp()
	a.accounts = map[string]*account.Profile{
		music.SourceQQ: {Source: music.SourceQQ, UserID: 2, Nickname: "乙", AvatarURL: "https://example.test/q.png"},
	}
	if got := a.mainAccount(); got == nil || got.Nickname != "乙" {
		t.Fatalf("the foot's account is %+v", got)
	}
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	if _, ok := tt.Find("乙"); !ok {
		t.Error("the foot shows no account")
	}
	if _, ok := tt.Find("登录"); ok {
		t.Error("the foot offers to sign in while somebody is")
	}
}
