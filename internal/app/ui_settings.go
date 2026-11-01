package app

import (
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The settings window's own size: the tabs across the top, and a column of
// groups under them.
const (
	settingsWidth  = 620
	settingsHeight = 640
)

// openSettings shows the settings window, opening it the first time.
func (a *app) openSettings() {
	if a.settingsWin != nil {
		a.settingsWin.Show()
		a.settingsWin.Focus()
		a.measureCache()
		return
	}
	a.measureCache()
	a.settingsWin = mygo.NewWindow(mygo.WindowOptions{
		Title:           "设置",
		Width:           settingsWidth,
		Height:          settingsHeight,
		MinWidth:        520,
		MinHeight:       460,
		StateKey:        "settings",
		DisableMaximize: true,
		DisableMinimize: true,
		// Not a window that can go full screen: its state is restored with
		// the rest of it, and a settings window that was in full screen
		// once (a slip of the green button before this was off) would open
		// that way for good.
		DisableFullScreen: true,
		// The app's menu has nothing for this window: on Linux and
		// Windows it stays out of sight until Alt shows it.
		AutoHideMenuBar: true,
		BackgroundColor: "light-dark(#f4f4f6, #1c1c1f)",
		Content:         ui.View(a.settingsView),
	})
	a.settingsWindowSized()
	a.settingsWin.OnClosed(func() {
		a.settingsWin = nil
		a.syncDock()
	})
	a.syncDock()
}

// openSettingsOn shows the settings on the tab that shows page: what a page
// asks for when it says where to turn something on.
func (a *app) openSettingsOn(page settingsPage) {
	for i, tab := range settingsTabs() {
		if tab.page == page {
			a.settingsTab = i
			break
		}
	}
	a.openSettings()
}

// settingsWindowSized puts a settings window that came back in full screen
// or maximized back to its own size, around where it was: the state file
// keeps that flag, and the window is made with it before there is a view to
// say otherwise.
func (a *app) settingsWindowSized() {
	w := a.settingsWin
	if w == nil || !w.IsFullScreen() && !w.IsMaximized() {
		return
	}
	if w.IsFullScreen() {
		w.SetFullScreen(false)
	}
	b := w.Bounds()
	w.SetBounds(mygo.Rectangle{
		X:      b.X + (b.Width-settingsWidth)/2,
		Y:      b.Y + (b.Height-settingsHeight)/2,
		Width:  settingsWidth,
		Height: settingsHeight,
	})
}

// changed applies the settings everywhere and keeps them.
func (a *app) changed() {
	a.settings.save()
	a.settings.applyAppearance()
	if a.notch != nil {
		a.notch.setEnabled(a.settings.Notch)
	}
	a.redraw()
}

// settingsView is the settings: a row of tabs across the top, each with its
// icon over its name, and the groups of the chosen tab under them. The
// groups were one long scroll, nine of them: the tabs put a page's worth in
// reach without scrolling past the rest.
func (a *app) settingsView(c *ui.Context) {
	t := *c.Theme()
	p := paletteFor(&t)
	// The window's own palette, for what the groups draw from the app: this
	// window may be built with the main one shut, and the colors its rows ask
	// for are the ones of the theme it is drawn in.
	a.pal = p
	t.Accent, t.AccentText = p.accent, p.onAccent
	c.SetTheme(&t)
	c.Root().Background(p.sidebar)
	c.OnShortcut(ui.Cmd, ui.KeyW, func() { a.settingsWin.Close() })
	s := &a.settings

	// The groups, apart: the tabs choose which of them the column shows.
	group := func(title string, rows func()) {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.muted).Margin(14, 4, 4, 4)
		ui.Column(c).Radius(12).Background(p.bg).Border(1, p.hairline).Dividers(1, p.hairline).Children(rows)
	}
	// rowWith is a row whose description is followed by what extras draws
	// in the same column: the sources' tags, under what a script says of
	// itself.
	rowWith := func(label, desc string, control func(), extras func()) {
		ui.Row(c).Padding(12, 16).Gap(16).AlignItems(ui.Center).MinHeight(48).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, label).FontSize(13.5).FontWeight(500)
				if desc != "" {
					ui.Text(c, desc).FontSize(12).TextColor(p.muted).LineHeight(1.4)
				}
				if extras != nil {
					extras()
				}
			})
			control()
		})
	}
	row := func(label, desc string, control func()) {
		rowWith(label, desc, control, nil)
	}

	tabs := settingsTabs()
	ui.Column(c).Fill().Children(func() {
		settingsTabBar(c, &a.settingsTab, tabs, p)
		ui.Box(c).Height(1).Background(p.hairline)
		ui.Scroll(c).Grow(1).Padding(10, 28, 32, 28).Gap(8).Children(func() {
			switch tabs[a.settingsTab].page {
			case settingsGeneral:
				// Only on macOS: elsewhere the icon is always in the
				// notification area.
				if runtime.GOOS == "darwin" {
					group("通用", func() {
						row("应用图标", "显示在菜单栏时，关闭窗口后可从菜单栏图标打开", func() {
							labels := make([]string, len(appIcons))
							chosen := appIcons[0].label
							for i, ic := range appIcons {
								labels[i] = ic.label
								if ic.id == s.AppIcon {
									chosen = ic.label
								}
							}
							if ui.Select(c.Key("app-icon"), &chosen, labels).Label("应用图标").Width(150).Changed() {
								for _, ic := range appIcons {
									if ic.label == chosen {
										s.AppIcon = ic.id
									}
								}
								a.changed()
								a.changeAppIcon()
							}
						})
					})
				}
				// Only on macOS, the one with a notch player.
				if a.notch != nil {
					group("刘海播放器", func() {
						row("显示刘海播放器", "在屏幕顶部的刘海处显示迷你播放器，鼠标悬停即可展开", func() {
							if ui.Switch(c.Key("notch"), &s.Notch).Label("显示刘海播放器").Changed() {
								a.changed()
							}
						})
						row("播放时显示实时活动", "在刘海两侧显示封面与音频波形，可能遮挡刘海旁的菜单栏图标", func() {
							if ui.Switch(c.Key("activity"), &s.NotchActivity).Label("播放时显示实时活动").Disabled(!s.Notch).Changed() {
								a.changed()
							}
						})
						row("在无刘海的屏幕上常驻显示", "关闭时，将鼠标移至屏幕顶部中央即可唤出，不会遮挡菜单栏", func() {
							if ui.Switch(c.Key("virtual"), &s.NotchVirtual).Label("在无刘海的屏幕上常驻显示").Disabled(!s.Notch).Changed() {
								a.changed()
							}
						})
					})
				}
				group("缓存", func() {
					row("歌曲缓存", "听过的歌曲保存在本机，再次播放无需等待，也不耗流量；"+a.cacheUsage(), func() {
						labels := make([]string, len(cacheLimits))
						chosen := cacheLimits[2].label
						for i, l := range cacheLimits {
							labels[i] = l.label
							if l.mb == s.CacheMB {
								chosen = l.label
							}
						}
						if ui.Select(c.Key("cache"), &chosen, labels).Label("歌曲缓存").Width(150).Changed() {
							for _, l := range cacheLimits {
								if l.label == chosen {
									s.CacheMB = l.mb
								}
							}
							a.changed()
							a.applyCacheLimit()
						}
					})
					row("清除缓存", "删除保存在本机的全部歌曲", func() {
						if ui.Button(c.Key("clear-cache"), "清除").Disabled(a.cacheUsed <= 0).Clicked() {
							a.clearCache()
						}
					})
					a.cacheDirRow(c)
				})
				if a.updates {
					group("更新", func() {
						desc := "每天检查一次是否有新版本"
						if t := lastUpdateCheck(); !t.IsZero() {
							desc += "；上次检查：" + commentTime(t, time.Now())
						}
						row("自动检查更新", desc, func() {
							on := updateChecks()
							if ui.Switch(c.Key("update-checks"), &on).Label("自动检查更新").Changed() {
								setUpdateChecks(on)
							}
						})
						row("自动下载并安装", "在后台下载新版本，下次打开时生效", func() {
							on := updateDownloads()
							if ui.Switch(c.Key("update-downloads"), &on).Label("自动下载并安装").Disabled(!updateChecks()).Changed() {
								setUpdateDownloads(on)
							}
						})
						row("当前版本 "+mygo.App.Version(), "", func() {
							if ui.Button(c.Key("check-updates"), "检查更新").Clicked() {
								checkForUpdates()
							}
						})
					})
				}

			case settingsPlay:
				group("播放", func() {
					row("音质", "无损与 Hi-Res 需要黑胶 VIP，没有时播放所能播放的最高音质；下一首歌曲开始生效", func() {
						labels := make([]string, len(qualityLevels))
						chosen := qualityLevels[2].label
						for i, q := range qualityLevels {
							labels[i] = q.label
							if q.level == s.Quality {
								chosen = q.label
							}
						}
						if ui.Select(c.Key("quality"), &chosen, labels).Label("音质").Width(150).Changed() {
							for _, q := range qualityLevels {
								if q.label == chosen {
									s.Quality = q.level
								}
							}
							a.changed()
						}
					})
					row("间奏时显示热评", "前奏、间奏与尾奏时，歌词处换成这首歌的热门评论", func() {
						if ui.Switch(c.Key("hot-comments"), &s.HotComments).Label("间奏时显示热评").Changed() {
							a.changed()
						}
					})
					row("提前缓冲下一曲", "当前歌曲播放时预先取好下一首，切歌时不必再等来源给出地址", func() {
						if ui.Switch(c.Key("prefetch"), &s.Prefetch).Label("提前缓冲下一曲").Changed() {
							a.changed()
						}
					})
					row("音量均衡", "按网易云音乐测得的响度调整每首歌的音量，让歌曲听起来一样响", func() {
						if ui.Switch(c.Key("normalize"), &s.Normalize).Label("音量均衡").Changed() {
							a.pl.SetVolume(a.volume)
							a.changed()
						}
					})
				})
				if a.captionsShown() {
					group("播客", func() { a.captionSettings(row, c) })
				}
				group("实验性功能", func() {
					row("进度条烟雾特效", "播放时进度条化作拖着烟雾的光点，随音乐起伏；会多用一些电量", func() {
						if ui.Switch(c.Key("smoke"), &s.Smoke).Label("进度条烟雾特效").Changed() {
							a.changed()
						}
					})
					row("烟雾背景", "播放页的背景化作封面色调的烟雾，缓缓升起，随低音起伏；仍在打磨，会多用一些电量", func() {
						if ui.Switch(c.Key("fog"), &s.Fog).Label("烟雾背景").Changed() {
							a.changed()
						}
					})
				})

			case settingsDownload:
				a.downloadSettings(group, row, c)

			case settingsSources:
				a.sourceSettings(group, row, rowWith, c)

			case settingsLook:
				group("外观", func() {
					// The four appearances are a wide control: it takes a row
					// of its own under what it does, rather than squeezing
					// the words beside it into a column too narrow to read.
					ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
						ui.Column(c).Gap(3).Children(func() {
							ui.Text(c, "外观").FontSize(13.5).FontWeight(500)
							ui.Text(c, "跟随播放器时，主界面与正在播放页融为一体，随正在播放的歌曲封面着色（默认）；没有歌曲时跟随系统").
								FontSize(12).TextColor(p.muted).LineHeight(1.4)
						})
						modes := []string{"system", "light", "dark", "cover"}
						i := 0
						for k, m := range modes {
							if m == s.Appearance {
								i = k
							}
						}
						if segmented(c.Key("appearance"), &i, "跟随系统", "浅色", "深色", "跟随播放器") {
							s.Appearance = modes[i]
							a.changed()
						}
					})
				})
				group("歌词", func() {
					row("字号", "正在播放页的歌词与律动，全屏时看得更清楚；标准字号跟着窗口大小走", func() {
						labels := make([]string, len(lyricScales))
						chosen := lyricScales[1].label
						for k, z := range lyricScales {
							labels[k] = z.label
							if nearScale(s.LyricScale, z.value) {
								chosen = z.label
							}
						}
						if ui.Select(c.Key("lyric-scale"), &chosen, labels).Label("歌词字号").Width(120).Changed() {
							for _, z := range lyricScales {
								if z.label == chosen {
									s.LyricScale = z.value
									a.changed()
								}
							}
						}
					})
				})
			}
		})
	})
	// The dialogs sit over the settings, in the same window.
	a.settingsDialogs(c)
}

// A settingsPage is a group of the settings, which the tabs choose among.
type settingsPage int

const (
	settingsGeneral settingsPage = iota
	settingsPlay
	settingsDownload
	settingsSources
	settingsLook
)

// A settingsTab is a tab of the settings: what it is called, the icon over
// its name, and the groups it shows.
type settingsTab struct {
	label string
	icon  *ui.SVG
	page  settingsPage
}

// settingsTabs are the tabs across the top of the settings, in the order
// they show: the app's own things, what plays, where saved songs go, where
// songs come from, and how it looks.
func settingsTabs() []settingsTab {
	return []settingsTab{
		{label: "通用", icon: iconSettings, page: settingsGeneral},
		{label: "播放", icon: iconHeadset, page: settingsPlay},
		{label: "下载", icon: iconDownload, page: settingsDownload},
		{label: "来源", icon: iconUpDown, page: settingsSources},
		{label: "外观", icon: iconPalette, page: settingsLook},
	}
}

// settingsTabBar is the row of tabs: the icon over the name, the chosen one
// in the accent over a fill of its own, the rest muted, and all of them
// centred in the window.
func settingsTabBar(c *ui.Context, at *int, tabs []settingsTab, p palette) {
	ui.Row(c).Padding(14, 16, 10, 16).Gap(6).Justify(ui.Center).Children(func() {
		for i, tab := range tabs {
			i, tab := i, tab
			on := *at == i
			b := ui.ButtonBase(c.Key("tab-" + tab.label)).FocusRing(false).Column().
				Width(84).Height(60).Radius(10).Gap(5).Justify(ui.Center).Label(tab.label)
			bg := ui.Transparent
			switch {
			case on:
				bg = p.press
			case b.Hovered():
				bg = p.hover
			}
			b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
			color := p.muted
			if on {
				color = p.accent
			}
			b.Children(func() {
				ui.Icon(c, tab.icon).Size(19, 19).TextColor(color)
				ui.Text(c, tab.label).FontSize(11.5).FontWeight(500).TextColor(color).SingleLine()
			})
			if b.Clicked() && !on {
				*at = i
			}
		}
	})
}
