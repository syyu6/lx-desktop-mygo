package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/player"
)

// The songs kept on disk may be kept in a folder of the listener's own:
// another disk, which has the room the system's does not. Choosing one
// stages the move for the next start (PendingCacheDir), since the songs
// playing have the cache open: at the next start the window says the cache
// is moving, copies what is kept across, and only then saves the new folder
// and removes the old files. A copy that fails leaves the old folder the
// cache's, and what was copied is dropped by the next start's sweep.

// pickCacheDirectory asks for a folder; a test replaces it rather than
// opening a system dialog.
var pickCacheDirectory = mygo.Dialog.Open

// cacheDirRow is the settings' row for the folder the songs are kept in:
// where it is, whether a move waits for a restart, and the ways to change
// it.
func (a *app) cacheDirRow(c *ui.Context) {
	p := a.pal
	where := "默认目录"
	if dir := a.pl.Cache.Directory(); dir != "" {
		where = dir
	}
	if pending := a.settings.PendingCacheDir; pending != nil {
		where = cacheSongsDir(*pending)
		if *pending == "" {
			where = "默认目录"
		}
	}
	ui.Row(c).Padding(12, 16).Gap(16).AlignItems(ui.Center).MinHeight(48).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, "缓存目录").FontSize(13.5).FontWeight(500)
			ui.Text(c, where).FontSize(12).TextColor(p.muted).SingleLine().Tooltip(where)
			if a.settings.PendingCacheDir != nil {
				ui.Text(c, "重启后迁移并生效").FontSize(12).TextColor(p.muted)
			}
			if a.cacheDirErr != "" {
				ui.Text(c, a.cacheDirErr).FontSize(12).TextColor(p.muted).LineHeight(1.4)
			}
		})
		ui.Row(c).Gap(8).Children(func() {
			if a.settings.PendingCacheDir != nil {
				ui.Button(c.Key("cancel-cache-dir"), "取消更改").Label("取消缓存目录更改").
					Disabled(a.cacheMoving).OnClick(func() { a.changeCacheDirectory(a.settings.CacheDir) })
			} else if a.settings.CacheDir != "" {
				ui.Button(c.Key("default-cache-dir"), "恢复默认").Label("恢复默认缓存目录").
					Disabled(a.cacheMoving).OnClick(func() { a.changeCacheDirectory("") })
			}
			ui.Button(c.Key("cache-dir"), "更改").Label("更改缓存目录").
				Disabled(a.cacheMoving).OnClick(a.chooseCacheDirectory)
		})
	})
}

// chooseCacheDirectory asks for the folder the songs should be kept in.
//
// The dialog is shown off the frame that asked for it, as the download
// folder's is: showing one is a modal, and a modal cannot run from within a
// build.
func (a *app) chooseCacheDirectory() {
	start := a.settings.CacheDir
	if pending := a.settings.PendingCacheDir; pending != nil {
		start = *pending
	}
	if start == "" {
		if dir := a.pl.Cache.Directory(); dir != "" {
			start = filepath.Dir(dir)
		}
	}
	parent := a.settingsWin
	go func() {
		dirs, err := pickCacheDirectory(mygo.OpenDialogOptions{
			Parent:            parent,
			Title:             "选择缓存目录",
			ButtonLabel:       "选择",
			DefaultPath:       start,
			Directory:         true,
			CreateDirectories: true,
		})
		if err != nil {
			a.post(func() { a.cacheDirErr = "无法打开文件夹选择器" })
			return
		}
		if len(dirs) == 0 {
			return
		}
		a.post(func() { a.changeCacheDirectory(dirs[0]) })
	}()
}

func (a *app) changeCacheDirectory(dir string) {
	if err := a.setCacheDirectory(dir); err != nil {
		a.cacheDirErr = err.Error()
		a.redraw()
	}
}

// setCacheDirectory stages a folder for the next start, once it is proved
// writable: the cache does not find out it cannot write where it was sent
// after the old songs are gone.
func (a *app) setCacheDirectory(dir string) error {
	if a.cacheMoving {
		return fmt.Errorf("正在迁移缓存")
	}
	if dir != "" {
		if !filepath.IsAbs(dir) {
			return fmt.Errorf("请选择完整的文件夹路径")
		}
		dir = filepath.Clean(dir)
	}
	next := a.settings
	if dir == next.CacheDir {
		next.PendingCacheDir = nil
	} else {
		chosen := next
		chosen.CacheDir = dir
		if _, err := prepareSongCache(chosen); err != nil {
			return err
		}
		pending := dir
		next.PendingCacheDir = &pending
	}
	if err := next.save(); err != nil {
		return fmt.Errorf("无法保存缓存目录：%w", err)
	}
	a.settings = next
	a.cacheDirErr = ""
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
	return nil
}

// cacheSongsDir is where songs are kept under a chosen folder: the folder
// itself holds the listener's other things, and a folder of songs in it is
// plain to see.
func cacheSongsDir(root string) string {
	if root == "" {
		return ""
	}
	return filepath.Join(root, "songs")
}

// prepareSongCache makes the cache the settings name, once the folder it
// keeps its songs in is proved writable: a file is written there and
// removed, as a folder that lists but does not take a file is no use.
func prepareSongCache(s settings) (*player.Cache, error) {
	cache := newSongCache(s)
	if cache == nil {
		return nil, fmt.Errorf("无法获取默认缓存目录")
	}
	dir := cache.Directory()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("无法使用此目录：%w", err)
	}
	probe, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return nil, fmt.Errorf("此目录不可写：%w", err)
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return nil, fmt.Errorf("无法写入此目录：%w", closeErr)
	}
	if removeErr != nil {
		return nil, fmt.Errorf("无法清理此目录：%w", removeErr)
	}
	return cache, nil
}

// startCacheMigration moves the songs kept into the folder the settings
// staged, and then makes it the cache's. It runs before playback comes
// back, so that no song of the cache is open: a file still being read from
// would be removed under the stream reading it.
func (a *app) startCacheMigration() {
	pending := a.settings.PendingCacheDir
	if pending == nil {
		a.boot()
		return
	}
	root := *pending
	chosen := a.settings
	chosen.CacheDir = root
	source := a.pl.Cache
	go func() {
		target, err := prepareSongCache(chosen)
		cleanup := func() error { return nil }
		if err == nil && source != nil {
			var last int64
			cleanup, err = source.CopyTo(target, func(done, total int64) {
				// A frame a few tens of megabytes is plenty for a bar.
				if done != 0 && done != total && done-last < 16<<20 {
					return
				}
				last = done
				a.post(func() { a.cacheMoveDone, a.cacheMoveTotal = done, total })
			})
		}
		a.post(func() {
			if err == nil {
				err = a.commitCacheDirectory(root, target)
			}
			a.cacheMoving = false
			if err != nil {
				a.cacheDirErr = "缓存迁移失败：" + err.Error()
				a.toast = "缓存迁移失败，继续使用原目录"
			} else {
				// The originals go only once the new folder is saved: were
				// the app to quit in between, the old folder would still be
				// the one asked for.
				go func() {
					if err := cleanup(); err != nil {
						a.post(func() {
							a.cacheDirErr = "已切换目录，部分原缓存未能删除：" + err.Error()
							a.toast = "已切换缓存目录，部分原缓存未能删除"
						})
					}
				}()
			}
			a.boot()
		})
	}()
}

// commitCacheDirectory makes the moved-to folder the cache's, once the
// songs are across: the setting is saved before the player is given the new
// folder, so that a quit right after still opens the new one next time.
func (a *app) commitCacheDirectory(root string, cache *player.Cache) error {
	if cache == nil {
		return fmt.Errorf("新目录不可用")
	}
	next := a.settings
	next.CacheDir, next.PendingCacheDir = root, nil
	if err := next.save(); err != nil {
		return err
	}
	a.settings, a.pl.Cache = next, cache
	a.cacheDirErr, a.cacheUsed = "", -1
	a.applyCacheLimit()
	return nil
}

// cacheMigrationView is the window while the cache moves: the song that was
// playing is not back until the move ends, since it may be one of the files
// moving.
func (a *app) cacheMigrationView(c *ui.Context) {
	p := a.pal
	c.Root().Background(p.bg)
	ui.Column(c).Fill().Center().Padding(40).Children(func() {
		ui.Column(c).Width(360).Gap(16).Children(func() {
			ui.Text(c, "正在迁移缓存").FontSize(22).FontWeight(600)
			progress := float32(0)
			if a.cacheMoveTotal > 0 {
				progress = float32(a.cacheMoveDone) / float32(a.cacheMoveTotal)
			}
			ui.Box(c).Height(6).Radius(3).Background(p.hairline).Children(func() {
				ui.Box(c).Height(6).Radius(3).WidthPercent(progress * 100).Background(p.accent)
			})
			ui.Text(c, megabytes(a.cacheMoveDone)+" / "+megabytes(a.cacheMoveTotal)).FontSize(13).TextColor(p.muted)
		})
	})
	// The window's own controls are gone while it shows this, so the strip
	// at the top stays there to be dragged by.
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(topBar(c)).DragWindow()
}

// The copies a migration did not finish are dropped by the cache's own
// pruning (player.dropParts): a folder the listener chose and never saved is
// not the app's to clean, but one the cache keeps in is.
