package app

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/player"
)

// newSongCache returns the cache of songs played, in the folder the
// listener chose or, while they chose none, the app's cache directory. It
// returns nil without either.
//
// The folder is the listener's, so the cache keeps its songs in it as
// <folder>/songs: the folder is theirs to hold other things in, and a
// folder that has a folder of songs in it is plain to see.
func newSongCache(s settings) *player.Cache {
	dir := s.CacheDir
	if dir == "" {
		app, err := mygo.App.Path(mygo.PathCache)
		if err != nil {
			return nil
		}
		dir = app
	}
	limit := int64(max(s.CacheMB, 0)) << 20
	return player.NewCache(filepath.Join(dir, "songs"), limit)
}

// applyCacheLimit gives the cache the limit the settings say.
func (a *app) applyCacheLimit() {
	if cache := a.pl.Cache; cache != nil {
		limit := int64(a.settings.CacheMB) << 20
		go func() {
			cache.SetLimit(limit)
			a.measureCache()
		}()
	}
}

// measureCache finds how much the cache takes, on a goroutine, for the
// settings to show.
func (a *app) measureCache() {
	cache := a.pl.Cache
	if cache == nil {
		return
	}
	go func() {
		n := cache.Size()
		a.post(func() {
			a.cacheUsed = n
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
		})
	}()
}

func (a *app) clearCache() {
	if cache := a.pl.Cache; cache != nil {
		a.cacheUsed = 0
		go func() {
			cache.Clear()
			a.measureCache()
		}()
	}
}

// cacheUsage says how much the cache takes, once measured.
func (a *app) cacheUsage() string {
	switch n := a.cacheUsed; {
	case n < 0:
		return "正在计算已用空间…"
	case n >= 1<<30:
		return fmt.Sprintf("已用 %.1f GB", float64(n)/(1<<30))
	default:
		return fmt.Sprintf("已用 %d MB", n>>20)
	}
}
