package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

func newTestApp() *app {
	a := &app{
		playlists: map[string]*netease.Playlist{}, loadingPl: map[string]bool{}, plErr: map[string]string{},
		artists: map[string]*artistData{}, albums: map[string]*albumData{}, users: map[string]*userData{},
		podcasts: map[int64]*podcastData{}, podcastTab: &podcastTab{}, searches: map[route]*searchData{},
		lyricAt: -1, queueAt: -1,
		focused: true, shown: true, // the active app, its window showing: what it draws moves on its own
	}
	a.art = newArtCache(func(func()) {})
	a.pl = player.New(func(func()) {})
	a.pl.SetVolume(0.5)
	return a
}

// openSettingsTab puts the settings on the tab that shows page, as a click
// on it does, so that a test of a group there finds it.
func openSettingsTab(a *app, page settingsPage) {
	for i, tab := range settingsTabs() {
		if tab.page == page {
			a.settingsTab = i
			return
		}
	}
	panic("no settings tab shows that page")
}

// textCount is how many elements show exactly s. A drawn menu's items are
// elements too, so this is how a test says a menu does or does not offer
// something the rest of the window shows as well.
func textCount(tt *ui.Tester, s string) int {
	n := 0
	for _, text := range tt.Texts() {
		if text == s {
			n++
		}
	}
	return n
}
