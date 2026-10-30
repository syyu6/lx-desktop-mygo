package app

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lxsource"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// The sources settings show the scripts the listener imported, and let
// them import more, from a file or the internet, turn them on and off, and
// take them away.
//
// None ships with the app: a script is code from the internet, and what it
// does is the listener's to choose. The dialog says so before one is
// imported.

// importDialog asks where a script comes from: an address on the internet,
// or a file on the computer.
type importDialog struct {
	open    bool
	addr    string
	busy    bool
	err     string
	fromURL bool // the field is an address; else a file is picked
}

// removeDialog names the script the settings ask about deleting: empty
// while nothing is asked.
type removeDialog struct {
	id   string
	name string
}

// The dialogs are mounted in the settings window, which has its own root.
func (a *app) settingsDialogs(c *ui.Context) {
	a.removeDialog(c)
	a.importSourceDialog(c)
}

// importSourceDialog asks where a script comes from, and imports it.
func (a *app) importSourceDialog(c *ui.Context) {
	p := a.pal
	d := &a.importing
	ui.DialogBase(c.Key("source-import"), &d.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(430).Padding(24).Radius(18).Gap(12).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Text(c, "导入音源").FontSize(17).FontWeight(700)
		ui.Text(c, "音源脚本来自网络，可在会员歌曲无法播放时提供它。脚本在沙箱中运行，只会访问网络，但请只导入你信任的来源。").
			FontSize(12.5).TextColor(p.muted).LineHeight(1.45)
		mode := 0
		if !d.fromURL {
			mode = 1
		}
		if segmented(c.Key("source-mode"), &mode, "网址", "本地文件") {
			d.fromURL = mode == 0
			d.err = ""
		}
		if d.fromURL {
			in := ui.TextInput(c.Key("source-url"), &d.addr).Placeholder("https://example.com/source.js").Label("音源网址").FontSize(13).AutoFocus()
			if in.Submitted() {
				a.importSource()
			}
		} else {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "从一个 .js 文件导入").FontSize(13).TextColor(p.muted).Grow(1)
				if ui.Button(c.Key("source-file"), "选择文件…").Clicked() {
					a.pickSourceFile()
				}
			})
		}
		if d.err != "" {
			ui.Text(c, d.err).FontSize(12.5).TextColor(p.accent).LineHeight(1.4)
		}
		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			if pillButton(c.Key("cancel"), nil, "取消", p.hover.Over(p.bg), p.text).Clicked() {
				d.open = false
			}
			ok := pillButton(c.Key("ok"), nil, "导入", p.accent, p.onAccent)
			if d.busy || (d.fromURL && strings.TrimSpace(d.addr) == "") {
				ok.Disabled(true).Opacity(0.5)
			}
			if ok.Clicked() {
				a.importSource()
			}
		})
	})
}

// importSource imports the script the dialog names: an address on the
// internet, or the file picked.
func (a *app) importSource() {
	d := &a.importing
	if d.busy || a.srcStore == nil || !d.fromURL {
		return
	}
	addr := strings.TrimSpace(d.addr)
	d.busy = true
	d.err = ""
	go func() {
		info, err := a.srcStore.ImportURL(addr)
		a.post(func() {
			d.busy = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.open = false
			d.addr = ""
			a.loadSourceList()
			a.changed()
			a.toast = "已导入音源「" + info.Name + "」"
		})
	}()
}

// pickSourceFile asks for a script file, and imports it.
//
// The dialog is shown off the frame that asked for it: showing one is a
// modal, which runs the main thread's own loop while it is up, and that
// loop cannot be run from within a build — a frame building a button would
// be waiting for the button that opened the panel. The goroutine asks, and
// what it finds is handed back to the main thread.
func (a *app) pickSourceFile() {
	d := &a.importing
	if d.busy || a.srcStore == nil {
		return
	}
	d.busy = true
	d.err = ""
	parent := a.settingsWin
	go func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  parent,
			Title:   "选择音源脚本",
			Filters: []mygo.FileFilter{{Name: "音源脚本", Extensions: []string{"js"}}},
		})
		if err != nil {
			a.post(func() { d.busy = false; d.err = "无法打开选择窗口：" + err.Error() })
			return
		}
		if len(files) == 0 {
			a.post(func() { d.busy = false })
			return
		}
		code, err := os.ReadFile(files[0])
		if err != nil {
			a.post(func() { d.busy = false; d.err = "读取文件失败：" + err.Error() })
			return
		}
		info, err := a.srcStore.Import(string(code))
		a.post(func() {
			d.busy = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.open = false
			a.loadSourceList()
			a.changed()
			a.toast = "已导入音源「" + info.Name + "」"
		})
	}()
}

// sourceSettings is the settings' group of built-in sources, and the group
// of the scripts the listener imported.
//
//   - 内置源: the services the app ships, in the listener's order. The
//     order is the order they are searched in, asked for a song's words
//     in, and played from. Each service is one row, which opens to its
//     own switches: 曲库 (its catalogue searched, its square and its own
//     pages shown), 单曲 (its songs played), 歌词 and 评论. They are
//     apart: a source may have a catalogue the app cannot stream from
//     (Kugou needs a signed address), and a source kept for playing is
//     still not asked for a search.
//   - 音源脚本: the scripts the listener imported. They are asked before
//     the built-in sources, in their own order, and can be imported,
//     updated and taken away.
//
// A service's songs were one switch before this, searched and played
// together: with them apart, turning off a source's 单曲 no longer takes
// its catalogue out of a search.
func (a *app) sourceSettings(group func(string, func()), row func(string, string, func()), rowWith func(string, string, func(), func()), c *ui.Context) {
	a.accountSettings(group, row, c)
	a.builtinSourceSettings(group, rowWith, c)
	a.scriptSourceSettings(group, row, rowWith, c)
}

// builtinSourceSettings is the group of built-in sources: the services the
// app ships, in the listener's order, each opening to what is asked of it.
func (a *app) builtinSourceSettings(group func(string, func()), rowWith func(string, string, func(), func()), c *ui.Context) {
	group("内置源", func() {
		if a.reg == nil {
			rowWith("内置源不可用", "内置源未能载入", func() {}, nil)
			return
		}
		ui.Column(c).Padding(12, 16).Gap(4).Children(func() {
			ui.Text(c, "顺序就是查找顺序：先搜到的排在前面，找不到歌词时也按这个顺序问下一个来源。").
				FontSize(12).TextColor(a.pal.muted).LineHeight(1.45)
			ui.Text(c, "曲库管搜索与歌单，单曲管播放，歌词与评论只管取用：关掉一个来源的单曲，它仍会被搜到；关掉曲库，它仍能放歌。").
				FontSize(12).TextColor(a.pal.muted).LineHeight(1.45)
		})
		order := a.sourceOrder()
		for i, id := range order {
			i, id := i, id
			name, desc := a.builtinSourceInfo(id)
			last := i == len(order)-1
			open := a.sourceOpen(id)
			rowWith(name, desc, func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					a.sourceArrows(c, id, i == 0, last, false)
					// Opening the row is what the arrow at its end does: the
					// switches are within. It turns to point the way they go,
					// down while closed and up while open, and is given the
					// room a control of the row's own takes rather than the
					// size of the pair beside it.
					arrow := iconDown
					if open {
						arrow = iconUp
					}
					openBtn := iconButton(c.Key("open-"+id), arrow, 15, a.pal.muted, "开关").Size(26, 26).Radius(6)
					if openBtn.Hovered() {
						openBtn.Background(a.pal.hover)
					}
					if openBtn.Clicked() {
						a.setSourceOpen(id, !open)
					}
				})
			}, func() {
				if !open {
					return
				}
				a.sourceCaps(c, id, name)
			})
		}
	})
}

// sourceCaps builds a built-in source's switches, under its name: what is
// asked of it, each with what it does and whether there is anything behind
// it.
func (a *app) sourceCaps(c *ui.Context, id, name string) {
	p := a.pal
	ui.Row(c).Gap(6).Margin(2, 0, 0, 0).Wrap().Children(func() {
		for _, cap := range source.Capabilities {
			cap := cap
			has := a.reg.Has(id, cap)
			on := a.reg.Asked(id, cap)
			chip := ui.ButtonBase(c.Key("cap-" + id + "-" + strconv.Itoa(int(cap)))).Height(26).PaddingX(10).Gap(6).Radius(999).
				AlignItems(ui.Center).FocusRing(false).Label(CapLabel(name, cap))
			switch {
			case !has:
				chip.Disabled(true).Opacity(0.35)
			case on:
				chip.Background(p.accent.Alpha(0.14)).TextColor(p.text)
			default:
				chip.Background(p.hover).TextColor(p.muted)
			}
			if has && chip.Hovered() {
				chip.Background(p.press)
			}
			chip.Children(func() {
				ui.Icon(c, capIcon(cap)).Size(11, 11).TextColor(capColor(on, has, p))
				ui.Text(c, source.CapName(cap)).FontSize(11.5).FontWeight(500).
					TextColor(capTextColor(on, has, p))
			})
			if has && chip.Clicked() {
				a.turnSourceCap(id, cap, !on)
			}
		}
	})
}

// capIcon is what a capability's switch shows: its catalogue's a shelf of
// records, its songs' a note, its words' and its remarks' their own.
func capIcon(cap source.Cap) *ui.SVG {
	switch cap {
	case source.CapLibrary:
		return iconLibrary
	case source.CapLyrics:
		return iconLyrics
	case source.CapComments:
		return iconComment
	}
	return iconNote
}

func capColor(on, has bool, p palette) ui.Color {
	switch {
	case !has:
		return p.faint
	case on:
		return p.accent
	}
	return p.muted
}

func capTextColor(on, has bool, p palette) ui.Color {
	if on && has {
		return p.text
	}
	return p.muted
}

// CapLabel is what a capability's tooltip says: the source's name and the
// thing asked of it, as one sentence.
func CapLabel(name string, cap source.Cap) string {
	return name + " · " + source.CapName(cap)
}

// builtinSourceInfo says what a built-in source is: its name, and what its
// catalogue has.
func (a *app) builtinSourceInfo(id string) (name, desc string) {
	if a.reg == nil {
		return id, ""
	}
	c, ok := a.reg.Catalog(id)
	if !ok {
		return id, ""
	}
	name = c.Name()
	switch id {
	case music.SourceNetease:
		return name, "网易云的曲库，单曲、歌单、歌手、专辑、用户与播客"
	case music.SourceKuwo:
		return name, "酷我的曲库，单曲与歌单"
	case music.SourceKugou:
		return name, "酷狗的曲库，单曲；播放要导入支持酷狗的音源脚本"
	case music.SourceQQ:
		return name, "QQ 音乐的曲库，单曲与歌单"
	case music.SourceMigu:
		return name, "咪咕的曲库，单曲"
	}
	return name, "内置源"
}

// accountDescription says who listens, for the NetEase row: what the
// music source's account gives.
func (a *app) accountDescription() string {
	if a.profile == nil {
		return "未登录 · 登录后可按会员身份播放会员歌曲，每日推荐与歌单也依你的口味生成"
	}
	return "已登录 · " + a.profile.Nickname
}

// accountSettings is the group of NetEase's account: the one account the
// app has, and the only place it is signed in to. It was at the foot of the
// sidebar, where it was a button the listener did not ask for; the sidebar
// keeps none of it.
func (a *app) accountSettings(group func(string, func()), row func(string, string, func()), c *ui.Context) {
	group("网易云音乐账号", func() {
		row("登录状态", a.accountDescription(), func() {
			if a.profile == nil {
				if ui.PrimaryButton(c.Key("source-login"), "扫码登录").Clicked() {
					a.showMain()
					a.openLogin()
					a.win.Invalidate()
				}
				return
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				a.avatar(c, a.profile.AvatarURL, 26, a.pal.press)
				if ui.Button(c.Key("source-logout"), "退出登录").Clicked() {
					a.signOut()
				}
			})
		})
	})
}

// scriptSourceSettings is the group of the scripts the listener imported:
// the sources asked before the built-in ones, in their own order, which can
// be turned on and off, moved, updated and taken away.
func (a *app) scriptSourceSettings(group func(string, func()), row func(string, string, func()), rowWith func(string, string, func(), func()), c *ui.Context) {
	group("音源脚本", func() {
		if a.srcStore == nil {
			msg := "音源脚本未能载入"
			if a.srcErr != "" {
				msg = a.srcErr
			}
			row("音源脚本不可用", msg, func() {})
			return
		}
		// What a script is, before one is imported: the listener imports
		// what they trust.
		ui.Column(c).Padding(12, 16).Gap(4).Children(func() {
			ui.Text(c, "脚本排在所有内置源之前：内置源放不了会员歌曲时，先由脚本提供。").
				FontSize(12).TextColor(a.pal.muted).LineHeight(1.45)
			ui.Text(c, "脚本在沙箱中运行，只能访问网络，不会接触本机的文件；请只导入你信任的来源。").
				FontSize(12).TextColor(a.pal.muted).LineHeight(1.45)
			// A runtime holds its script's heap, so the settings say how
			// many are up: the rest were let go after a while without a
			// request, and start again when a song needs one.
			if live := a.liveSources(); live > 0 {
				ui.Text(c, fmt.Sprintf("正在运行 %d 个脚本运行时：一分半不用就会释放，下次用到再启动。", live)).
					FontSize(12).TextColor(a.pal.faint).LineHeight(1.45)
			}
		})
		list := a.scriptOrder()
		if len(list) == 0 {
			row("还没有音源脚本", "导入一个落雪音源脚本，用它补内置源放不了的歌", func() {})
		}
		// A script says what it plays only once its runtime is up: the
		// settings ask each of the ones listed, here, where the tags show.
		a.askSourceCaps()
		for i, id := range list {
			i, id := i, id
			info, ok := a.sourceByID(id)
			if !ok {
				continue
			}
			name := info.Name
			on := info.Enabled
			tags := func() { a.sourceTagsRow(c, id) }
			controls := func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					a.scriptArrows(c, id, i == 0, i == len(list)-1)
					sw := on
					if ui.Switch(c.Key("on-"+id), &sw).Label(name).Changed() {
						a.turnSource(id, sw)
					}
					// Updating fetches the script again from the address it
					// came from. A script imported from a file has no
					// address: its button is not shown, rather than shown
					// and doing nothing.
					if info.URL != "" {
						updating := a.srcUpdating[id]
						upd := ui.Button(c.Key("update-"+id), "更新")
						if updating {
							upd = ui.Button(c.Key("update-"+id), "更新中…").Disabled(true).Opacity(0.6)
						}
						if upd.Clicked() {
							a.updateSource(id)
						}
					}
					if ui.Button(c.Key("del-"+id), "删除").Clicked() {
						a.askRemoveSource(id, name)
					}
				})
			}
			rowWith(name, sourceDescription(&info), controls, tags)
		}
		row("导入音源", "从网址或文件导入落雪音源脚本", func() {
			if ui.Button(c.Key("source-import-open"), "导入").Clicked() {
				a.importing.addr = ""
				a.importing.err = ""
				a.importing.fromURL = true
				a.importing.open = true
			}
		})
	})
}

// sourceArrows builds a built-in source's up and down buttons: the order
// it is searched in, asked for words in, and played from. They are stacked,
// one over the other, so that the pair reads as one control for the row's
// place rather than as two more of the row's buttons.
func (a *app) sourceArrows(c *ui.Context, id string, first, last, script bool) {
	move := a.moveSource
	if script {
		move = a.moveScript
	}
	ui.Column(c).Gap(1).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		up := iconButton(c.Key("up-"+id), iconUp, 12, a.pal.muted, "上移").Size(24, 19).Radius(5).Disabled(first)
		if first {
			up.Opacity(0.3)
		}
		if up.Clicked() {
			move(id, true)
		}
		down := iconButton(c.Key("down-"+id), iconDown, 12, a.pal.muted, "下移").Size(24, 19).Radius(5).Disabled(last)
		if last {
			down.Opacity(0.3)
		}
		if down.Clicked() {
			move(id, false)
		}
	})
}

// scriptArrows is sourceArrows for an imported script, whose order is its
// own.
func (a *app) scriptArrows(c *ui.Context, id string, first, last bool) {
	a.sourceArrows(c, id, first, last, true)
}

// sourceOpen reports whether a built-in source's row shows its switches.
func (a *app) sourceOpen(id string) bool {
	if a.sourceExpanded == nil {
		a.sourceExpanded = map[string]bool{}
	}
	open, ok := a.sourceExpanded[id]
	return !ok || open
}

// setSourceOpen opens or closes a built-in source's switches.
func (a *app) setSourceOpen(id string, open bool) {
	if a.sourceExpanded == nil {
		a.sourceExpanded = map[string]bool{}
	}
	a.sourceExpanded[id] = open
	a.redraw()
}

// turnSourceCap turns what is asked of a built-in source for a capability
// on or off, keeps it, and redraws: what a source's switch does.
func (a *app) turnSourceCap(id string, cap source.Cap, on bool) {
	if !a.setSourceCap(id, cap, on) {
		return
	}
	a.changed()
	a.redraw()
}

// setSourceCap turns a capability on or off and keeps it, without
// redrawing: what a test, and the switch, change.
func (a *app) setSourceCap(id string, cap source.Cap, on bool) bool {
	if a.reg == nil || !a.reg.SetAsked(id, cap, on) {
		return false
	}
	switch cap {
	case source.CapLibrary:
		// The catalogue: what is searched, what the square shows, and the
		// pages NetEase alone has. The squares are read again from what is
		// left, and a page of NetEase's own goes elsewhere once its
		// catalogue is off.
		a.settings.LibrariesOff = setOff(a.settings.LibrariesOff, id, !on)
		a.square = nil
		a.rebuildChain()
		if neteaseOnly(a.route.page) && !a.searchesOn(music.SourceNetease) {
			a.open(route{page: pageSonglists})
		}
	case source.CapStream:
		// The songs: what the chain tries a song on, apart from where it
		// was found.
		a.settings.SourcesOff = setOff(a.settings.SourcesOff, id, !on)
		a.rebuildChain()
	case source.CapLyrics:
		a.settings.LyricsOff = setOff(a.settings.LyricsOff, id, !on)
		a.rebuildChain()
	case source.CapComments:
		a.settings.CommentsOff = setOff(a.settings.CommentsOff, id, !on)
	}
	a.settings.save()
	return true
}

// setSourceOn turns a source's catalogue and its songs both on or off, and
// keeps it: the switch a source had before they were apart, which the tests
// and a few pages use.
func (a *app) setSourceOn(id string, on bool) bool {
	lib := a.setSourceCap(id, source.CapLibrary, on)
	stream := a.setSourceCap(id, source.CapStream, on)
	return lib || stream
}

// setSourceSearched turns a source's catalogue on or off, and keeps it.
func (a *app) setSourceSearched(id string, on bool) bool {
	return a.setSourceCap(id, source.CapLibrary, on)
}

// setOff is off with id added when it is not on, or without it when it is:
// a list of the ids turned off, as the settings keep them.
func setOff(off []string, id string, add bool) []string {
	out := slices.DeleteFunc(slices.Clone(off), func(s string) bool { return s == id })
	if add {
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sourceRowInfo says what the list says of a source: its name, what it is,
// whether the listener asks it, and whether the app ships it.
func (a *app) sourceRowInfo(id string) (name, desc string, on, builtin bool) {
	if a.reg != nil {
		if p, ok := a.reg.Get(id); ok {
			return p.Name(), builtinSourceDescription(id), a.reg.Enabled(id), true
		}
	}
	if info, ok := a.sourceByID(id); ok {
		return info.Name, sourceDescription(&info), info.Enabled, false
	}
	return id, "", false, false
}

// builtinSourceDescription says what a source does: whether the app
// streams its songs itself, or asks a script for them.
func builtinSourceDescription(id string) string {
	switch id {
	case music.SourceNetease:
		return "内置，登录后可听会员歌曲；也可关闭，只用其他来源"
	case music.SourceKuwo:
		return "内置，搜索、歌单与播放都无需脚本"
	case music.SourceQQ:
		return "内置，搜索、歌单、歌词与播放都无需脚本；会员歌曲仍要有账号才能听到完整版"
	case music.SourceMigu:
		return "内置，搜索、歌词与播放都无需脚本；会员歌曲只能听到试听"
	}
	return "内置来源"
}

// turnBuiltinSource turns a source's catalogue and its songs both on or
// off, keeping it in the settings, and reloads the pages that list the
// sources' things.
func (a *app) turnBuiltinSource(id string, on bool) {
	lib := a.setSourceCap(id, source.CapLibrary, on)
	stream := a.setSourceCap(id, source.CapStream, on)
	if lib || stream {
		a.changed()
		a.redraw()
	}
}

// sourceDescription says what a script is: what its header says about it.
func sourceDescription(info *lxsource.Info) string {
	var parts []string
	if info.Version != "" {
		parts = append(parts, "版本 "+info.Version)
	}
	if info.Author != "" {
		parts = append(parts, info.Author)
	}
	if info.URL != "" {
		parts = append(parts, "从网络导入")
	}
	return strings.Join(parts, " · ")
}

// sourceTagsRow is the platforms a script plays, under what it says of
// itself: the abbreviations of lx-music-desktop's sources, which the
// listener who imported the script knows it by — kw, kg, tx, wy, mg.
// A script that has not said yet says so, and one that would not start
// says what it answered.
func (a *app) sourceTagsRow(c *ui.Context, id string) {
	p := a.pal
	tags, asked, failed := a.sourceTags(id)
	switch {
	case failed:
		ui.Text(c, "未能启动，无法得知它支持哪些平台").FontSize(11.5).TextColor(p.faint)
		return
	case !asked:
		ui.Text(c, "正在查看它支持哪些平台…").FontSize(11.5).TextColor(p.faint)
		return
	case len(tags) == 0:
		ui.Text(c, "没有它可播放的平台").FontSize(11.5).TextColor(p.faint)
		return
	}
	ui.Row(c).Gap(4).Margin(2, 0, 0, 0).Children(func() {
		for _, s := range tags {
			ui.Row(c).Key("tag-"+id+"-"+string(s)).Padding(2, 7).Radius(5).AlignItems(ui.Center).
				Background(p.hover).Tooltip(s.Name()).Children(func() {
				ui.Text(c, string(s)).FontSize(11).FontWeight(600).TextColor(p.muted)
			})
		}
	})
}

// turnSource turns a script on or off, and has the resolver ask it or not.
func (a *app) turnSource(id string, on bool) {
	if _, err := a.srcStore.SetEnabled(id, on); err != nil {
		a.toast = "音源设置失败：" + err.Error()
		return
	}
	a.afterSourceChange()
}

// afterSourceChange has the list and the chain see what the store now
// keeps, and redraws: what a change to the sources does.
func (a *app) afterSourceChange() {
	a.loadSourceList()
	a.changed()
}

// removeSource deletes a script, once the listener says so: the entry, and
// what was written of it. The runtime, if it is up, is let go with it.
func (a *app) removeSource(id, name string) {
	if a.srcStore == nil {
		return
	}
	if err := a.srcStore.Remove(id); err != nil {
		a.toast = "删除音源失败：" + err.Error()
		return
	}
	if a.src != nil {
		a.src.Reset(id)
	}
	a.removing.id = ""
	a.loadSourceList()
	a.changed()
	a.redraw()
}

// askRemoveSource asks before deleting a script, in a dialog of the app's
// own rather than a native alert: an alert shown from within the frame
// that built the button runs the main thread's loop under it, and the
// frame is waiting for that button. The app's own dialog is a part of the
// frame, and nothing waits.
func (a *app) askRemoveSource(id, name string) {
	a.removing.id, a.removing.name = id, name
	a.redraw()
}

// removeDialog asks whether a script should be deleted, and deletes it if
// the listener says so.
func (a *app) removeDialog(c *ui.Context) {
	p := a.pal
	r := &a.removing
	open := r.id != ""
	ui.DialogBase(c.Key("source-remove"), &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(400).Padding(24).Radius(18).Gap(10).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Text(c, "删除音源").FontSize(17).FontWeight(700)
		ui.Text(c, "确定删除音源「"+r.name+"」吗？删除后可以重新导入。").
			FontSize(12.5).TextColor(p.muted).LineHeight(1.45)
		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			if pillButton(c.Key("cancel"), nil, "取消", p.hover.Over(p.bg), p.text).Clicked() {
				r.id = ""
			}
			del := pillButton(c.Key("remove"), nil, "删除", p.accent, p.onAccent)
			if del.Clicked() {
				a.removeSource(r.id, r.name)
			}
		})
	})
	if !open && r.id != "" {
		// Dismissed by the backdrop or Escape.
		r.id = ""
	}
}

// updateSource fetches a script again from where it was imported, and
// keeps it in its place: its id, its order and whether it is on stay as
// they were, and its code is replaced by what the address now serves. A
// script imported from a file has no address to fetch from, and says so.
func (a *app) updateSource(id string) {
	info, ok := a.sourceByID(id)
	if !ok || a.srcStore == nil {
		return
	}
	if info.URL == "" {
		a.toast = "这个音源是从本地文件导入的，请重新选择文件"
		return
	}
	if a.removing.id == id {
		return
	}
	if a.srcUpdating == nil {
		a.srcUpdating = map[string]bool{}
	}
	if a.srcUpdating[id] {
		return
	}
	a.srcUpdating[id] = true
	a.redraw()
	go func() {
		next, err := a.srcStore.Update(id)
		a.post(func() {
			delete(a.srcUpdating, id)
			if err != nil {
				a.toast = "更新音源失败：" + err.Error()
				a.redraw()
				return
			}
			if a.src != nil {
				// The code changed under the same id: the runtime the
				// resolver holds is the old script's, and is let go.
				a.src.Reset(id)
			}
			// The new code may play other platforms: the tags kept of the
			// old one go with it, and the settings ask it again.
			delete(a.srcCaps, id)
			delete(a.srcCapsErr, id)
			delete(a.srcCapsAsked, id)
			a.loadSourceList()
			a.changed()
			ver := ""
			if next.Version != "" {
				ver = " " + next.Version
			}
			a.toast = "已更新音源「" + next.Name + "」" + ver
			a.redraw()
		})
	}()
}
