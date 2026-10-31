package app

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The account is in the settings, beside the sources it plays from, and the
// sidebar's foot has the way in as well, by the listener's picture alone.
func TestSignInIsInTheSettings(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.loadRegistry()
	openSettingsTab(a, settingsSources)
	tt := ui.NewTester(a.settingsView, settingsWidth, 1400)
	tt.Frame()
	if _, ok := tt.Find("扫码登录"); !ok {
		t.Fatal("the settings have no way to sign in")
	}
	if _, ok := tt.Find("网易云音乐账号"); !ok {
		t.Error("the account's group is not in the settings")
	}
	// And the sidebar's foot signs in too, from a picture rather than a
	// row that names the service.
	main := ui.NewTester(a.view, 1000, 700)
	main.Frame()
	if _, ok := main.Find("登录"); !ok {
		t.Error("the sidebar's foot has no way to sign in")
	}
	if _, ok := main.Find("登录网易云音乐"); ok {
		t.Error("the sidebar still names the sign-in in a row")
	}
}

// With the account signed in, the settings show who it is and a way out.
func TestSignedInAccountInTheSettings(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.loadRegistry()
	a.profile = &netease.Profile{UserID: 1, Nickname: "小明"}
	openSettingsTab(a, settingsSources)
	tt := ui.NewTester(a.settingsView, settingsWidth, 1400)
	tt.Frame()
	if !tt.HasText("已登录 · 小明") {
		t.Error("the settings do not say who is signed in")
	}
	if _, ok := tt.Find("退出登录"); !ok {
		t.Error("the settings have no way to sign out")
	}
}

// fakeSubscriptions answers for the artists and podcasts the listener
// follows, and tells on each time they are asked for.
func fakeSubscriptions(t *testing.T) chan struct{} {
	asked := make(chan struct{}, 8)
	ars, pods := followedArtists, subscribedPodcasts
	t.Cleanup(func() { followedArtists, subscribedPodcasts = ars, pods })
	followedArtists = func() ([]netease.ArtistProfile, error) { asked <- struct{}{}; return nil, nil }
	subscribedPodcasts = func() ([]netease.Podcast, error) { return nil, nil }
	return asked
}

// ownPageApp is signed in as user 9, whose page has what they follow.
func ownPageApp() *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 9, Nickname: "听歌的人"}
	user := netease.User{UserID: 9, Nickname: "听歌的人", Signature: "在音乐里，寻找同路的人。", Follows: 12, Followeds: 24}
	a.users[9] = &userData{
		user:      &user,
		playlists: []netease.Playlist{{ID: 30, Name: "通勤路上", TrackCount: 40}},
		artists:   []netease.ArtistProfile{{Artist: netease.Artist{ID: 7, Name: "MyGO!!!!!"}}, {Artist: netease.Artist{ID: 10, Name: "Ave Mujica"}}},
		podcasts: []netease.Podcast{
			{ID: 5, Name: "夜航电台", DJ: netease.User{Nickname: "主播"}, ProgramCount: 120, Description: "每晚陪你说说话。"},
			{ID: 6, Name: "通勤十分钟", ProgramCount: 48},
		},
		artistsShown: true, podcastsShown: true,
	}
	return a
}

// The listener's own page asks again for what they follow each time it
// shows, as they may have followed someone since, and a followed artist
// opens their page.
func TestOwnPageFollows(t *testing.T) {
	asked := fakeSubscriptions(t)
	a := ownPageApp()
	a.open(route{page: pageUser, id: 9})
	wait(t, asked, "the artists followed")
	tt := ui.NewTester(a.view, 1200, 1200)
	tt.Frame()
	if err := tt.Click("Ave Mujica"); err != nil {
		t.Fatal(err)
	}
	if a.route.page != pageArtist || a.route.id != 10 {
		t.Fatalf("route = %+v, want the page of artist 10", a.route)
	}
	a.users[9].subsLoading = 0 // what was asked for has landed
	a.back()
	wait(t, asked, "the artists followed, again")
}

// Someone else's page doesn't ask for what the listener follows, nor shows
// it.
func TestOthersPageHidesFollows(t *testing.T) {
	asked := fakeSubscriptions(t)
	a := ownPageApp()
	a.profile = &netease.Profile{UserID: 1}
	a.open(route{page: pageUser, id: 9})
	tt := ui.NewTester(a.view, 1200, 1200)
	tt.Frame()
	if len(asked) > 0 {
		t.Fatal("asked for what the listener follows")
	}
	if err := tt.Click("Ave Mujica"); err == nil {
		t.Fatal("the listener's artists showed on someone else's page")
	}
}

// The listener's own page asks for no frames once shown.
func TestOwnPageFrameBudget(t *testing.T) {
	fakeSubscriptions(t)
	a := ownPageApp()
	a.open(route{page: pageUser, id: 9})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()
	tt.Advance(time.Second)
	if tt.FramePending() {
		t.Error("the page asks for frames while idle")
	}
}

// With NM_FRAMES set, saves the listener's own page, scrolled to what they
// follow, in the light and the dark.
func TestOwnPageFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	fakeSubscriptions(t)
	for _, dark := range []bool{false, true} {
		a := ownPageApp()
		a.open(route{page: pageUser, id: 9})
		tt := ui.NewTester(a.view, 1200, 1200)
		tt.SetDark(dark)
		tt.Advance(time.Second)
		tt.Frame()
		name := "user-own.png"
		if dark {
			name = "user-own-dark.png"
		}
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, tt.Image())
		f.Close()
	}
}

// With NM_FRAMES set, saves the foot of the sidebar with the account menu
// open, light and dark.
func TestAccountMenuFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		a := newTestApp()
		a.settings = defaultSettings()
		a.profile = &netease.Profile{UserID: 1, Nickname: "小明"}
		tt := ui.NewTester(a.view, 1000, 600)
		tt.SetDark(dark)
		tt.Frame()
		tt.Click("小明")
		tt.Advance(time.Second)
		tt.Frame()
		img := tt.Image()
		s := float32(img.Bounds().Dx()) / 1000
		crop := image.Rect(0, int(400*s), int((sidebarWidth+20)*s), img.Bounds().Dy())
		out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
		draw.Draw(out, out.Bounds(), img, crop.Min, draw.Src)
		name := "account-light.png"
		if dark {
			name = "account-dark.png"
		}
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, out)
		f.Close()
	}
}
