package app

import (
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

func newTestApp() *app {
	a := &app{
		playlists: map[string]*netease.Playlist{}, loadingPl: map[string]bool{}, plErr: map[string]string{},
		artists: map[int64]*artistData{}, albums: map[int64]*albumData{}, users: map[int64]*userData{},
		podcasts: map[int64]*podcastData{}, podcastTab: &podcastTab{}, searches: map[route]*searchData{},
		lyricAt: -1, queueAt: -1,
		focused: true, // the active app: what it draws moves on its own
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
