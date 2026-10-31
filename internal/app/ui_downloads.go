package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/music"
)

// The download page: the songs being saved, and those that were, under the
// tabs of where they are. It shows once the listener turns downloads on,
// after lx-music-desktop's download page: a row per song with what is left
// of it, and beside each the few things to do with it.
//
// The page is drawn to be scanned rather than read. Its head stays put and
// only the rows scroll, so that the tabs, and the buttons that act on all
// of them, are always where the pointer left them. A running task shows a
// bar under its name rather than a spinner: the listener wants to know how
// long, not that something is happening. A finished row says where its
// file went, in the quiet gray of a fact rather than the black of a title.

// The tabs of the download page, in the order lx-music-desktop has them.
var dlTabs = []struct {
	label string
	match func(download.Status) bool
}{
	{"所有任务", func(download.Status) bool { return true }},
	{"正在下载", func(s download.Status) bool { return s == download.Running || s == download.Waiting }},
	{"已暂停", func(s download.Status) bool { return s == download.Paused }},
	{"出错", func(s download.Status) bool { return s == download.Failed }},
	{"已完成", func(s download.Status) bool { return s == download.Done }},
}

// downloadEnabled reports whether downloads are on: the page, and every
// entry that starts one, show only then.
func (a *app) downloadEnabled() bool { return a.settings.DownloadOn }

// dlQualityAsk is the quality a batch is downloaded at, asked before it
// starts: the songs it is asked for, and the level chosen, which begins at
// the settings' default.
type dlQualityAsk struct {
	open  bool
	songs []music.Track
	level string
}

// askDownload asks the quality, then queues the songs at it. One song is
// asked as a batch of one: the answer is the same, and the dialog is where
// the listener learns what a download will be.
func (a *app) askDownload(songs []music.Track) {
	if a.dl == nil || len(songs) == 0 {
		return
	}
	level := a.settings.DownloadQuality
	if level == "" {
		level = a.settings.Quality
	}
	if level == "" {
		level = "exhigh"
	}
	a.dlAsk.open, a.dlAsk.songs, a.dlAsk.level = true, songs, level
}

// dlTask is the task of saving one song at one level: the song and its
// level name it, and the folder and the file's name are the settings'.
// The extension is left for the bytes to say; Name is filled in as the
// file is written, since only then is its format known.
func (a *app) dlTask(t music.Track, level string) download.Task {
	return download.Task{
		ID:     t.Ident() + "@" + level,
		Track:  t,
		Level:  level,
		Dir:    a.downloadDir(),
		Status: download.Waiting,
	}
}

// startDownloads queues the songs the dialog asked for, and says how many
// were new: asking again for a song already being saved adds nothing.
func (a *app) startDownloads(songs []music.Track, level string) {
	if a.dl == nil {
		return
	}
	tasks := make([]download.Task, 0, len(songs))
	for _, t := range songs {
		tasks = append(tasks, a.dlTask(t, level))
	}
	n := a.dl.Add(tasks)
	a.dlAsk.open = false
	switch {
	case n == 0:
		a.toast = "这些歌曲已经在下载或已下载完成"
	case n < len(songs):
		a.toast = fmt.Sprintf("已加入下载 %d 首，%d 首已经有了", n, len(songs)-n)
	default:
		a.toast = fmt.Sprintf("已加入下载 %d 首", n)
	}
}

// qualityDialog asks the quality a batch downloads at.
func (a *app) qualityDialog(c *ui.Context) {
	p := a.pal
	d := &a.dlAsk
	ui.DialogBase(c.Key("dl-quality"), &d.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(380).Padding(24).Radius(18).Gap(14).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Text(c, "下载").FontSize(17).FontWeight(700)
		ui.Text(c, dlAskSummary(d.songs)).FontSize(12.5).TextColor(p.muted).LineHeight(1.4).MaxLines(2)
		ui.Column(c).Gap(0).Radius(10).Border(1, p.hairline).Dividers(1, p.hairline).Children(func() {
			for _, q := range qualityLevels {
				q := q
				on := d.level == q.level
				row := ui.ButtonBase(c.Key(q.level)).Height(36).PaddingX(12).Gap(10).AlignItems(ui.Center).
					FocusRing(false).Label(q.label)
				row.Background(row.AnimateColor("bg", pick(on, p.hover.Over(p.bg), ui.Transparent), ui.Animation{Duration: 100 * time.Millisecond}))
				row.Children(func() {
					ui.Text(c, q.label).FontSize(13).Grow(1).TextColor(pick(on, p.text, p.muted))
					if on {
						ui.Icon(c, iconCheck).Size(13, 13).TextColor(p.accent)
					}
				})
				if row.Clicked() {
					d.level = q.level
				}
			}
		})
		if d.level == "lossless" || d.level == "hires" {
			ui.Text(c, "需要黑胶 VIP；没有时下载来源能给出的最高音质").FontSize(12).TextColor(p.faint)
		}
		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			if pillButton(c.Key("cancel"), nil, "取消", p.hover.Over(p.bg), p.text).Clicked() {
				d.open = false
			}
			if pillButton(c.Key("ok"), nil, "开始下载", p.accent, p.onAccent).Clicked() {
				a.startDownloads(d.songs, d.level)
			}
		})
	})
}

// dlAskSummary says what is about to be downloaded.
func dlAskSummary(songs []music.Track) string {
	switch len(songs) {
	case 0:
		return ""
	case 1:
		t := songs[0]
		if names := t.ArtistNames(); names != "" {
			return t.Name + " - " + names
		}
		return t.Name
	}
	return fmt.Sprintf("%d 首歌曲", len(songs))
}

// pick returns a when on, else b.
func pick[T any](on bool, a, b T) T {
	if on {
		return a
	}
	return b
}

// dlTasks returns the tasks the page shows, sorted so that what moves is
// where the eye comes to first.
func (a *app) dlTasks(tab int) []download.Task {
	if a.dl == nil {
		return nil
	}
	tasks := download.SortStatus(a.dl.Tasks())
	match := dlTabs[min(tab, len(dlTabs)-1)].match
	out := make([]download.Task, 0, len(tasks))
	for _, t := range tasks {
		if match(t.Status) {
			out = append(out, t)
		}
	}
	return out
}

// downloadsPage is the page.
func (a *app) downloadsPage(c *ui.Context) {
	p := a.pal
	top, bottom := a.insets(c)
	tasks := a.dlTasks(a.dlTab)
	running := 0
	if a.dl != nil {
		running = a.dl.Running()
	}
	// The head and the tabs stay; only the rows scroll, so that the actions
	// over all of them are never scrolled away from.
	ui.Column(c).Fill().Padding(top+pageGap, pagePad-12, bottom, pagePad-12).Gap(14).Children(func() {
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, "下载管理").FontSize(24).FontWeight(700).LetterSpacing(-0.4)
				ui.Text(c, a.dlSummary(running)).FontSize(12.5).TextColor(p.muted).SingleLine()
			})
			a.dlActions(c)
		})
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			i := min(a.dlTab, len(dlTabs)-1)
			if segmented(c.Key("dl-tabs"), &i, a.dlTabLabels()...) {
				a.dlTab = i
				a.dlSel.Clear()
			}
			ui.Spacer(c)
			if n := a.dlSel.Len(); n > 0 {
				a.dlBulkBar(c, n)
			}
		})
		if len(tasks) == 0 {
			a.dlEmpty(c, a.dlTab)
			return
		}
		a.dlRows(c, tasks)
	})
}

// dlTabLabels returns the tabs' names, each with how many tasks it holds: a
// count beside the name says where to look without opening each.
func (a *app) dlTabLabels() []string {
	var all []download.Task
	if a.dl != nil {
		all = a.dl.Tasks()
	}
	labels := make([]string, len(dlTabs))
	for i, tab := range dlTabs {
		n := 0
		for _, t := range all {
			if tab.match(t.Status) {
				n++
			}
		}
		labels[i] = tab.label
		if n > 0 {
			labels[i] = fmt.Sprintf("%s %d", tab.label, n)
		}
	}
	return labels
}

// dlSummary says what downloads are doing, in one line: what runs, what
// waits, and what finished.
func (a *app) dlSummary(running int) string {
	var all []download.Task
	if a.dl != nil {
		all = a.dl.Tasks()
	}
	if len(all) == 0 {
		return "歌曲会保存在你设置的文件夹里"
	}
	var done, waiting, paused, failed int
	for _, t := range all {
		switch t.Status {
		case download.Done:
			done++
		case download.Waiting:
			waiting++
		case download.Paused:
			paused++
		case download.Failed:
			failed++
		}
	}
	var parts []string
	if running > 0 {
		parts = append(parts, fmt.Sprintf("正在下载 %d 首", running))
	}
	if waiting > 0 {
		parts = append(parts, fmt.Sprintf("等待 %d 首", waiting))
	}
	if paused > 0 {
		parts = append(parts, fmt.Sprintf("已暂停 %d 首", paused))
	}
	if done > 0 {
		parts = append(parts, fmt.Sprintf("已完成 %d 首", done))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("出错 %d 首", failed))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("共 %d 首", len(all))
	}
	return strings.Join(parts, " · ")
}

// dlActions are the page's own buttons: the folder the songs go in, pause
// or resume all, retry what failed, and clear the finished ones.
func (a *app) dlActions(c *ui.Context) {
	p := a.pal
	var all []download.Task
	if a.dl != nil {
		all = a.dl.Tasks()
	}
	live, done, failed := 0, 0, 0
	for _, t := range all {
		switch t.Status {
		case download.Running, download.Waiting, download.Paused:
			live++
		case download.Done:
			done++
		case download.Failed:
			failed++
		}
	}
	if live > 0 {
		// While anything waits or runs the button pauses; with every task
		// paused, it starts them again.
		running := false
		for _, t := range all {
			if t.Status == download.Running || t.Status == download.Waiting {
				running = true
			}
		}
		label, icon := "全部暂停", iconPause
		if !running {
			label, icon = "全部继续", iconPlay
		}
		if pillButton(c.Key("dl-pause-all"), icon, label, p.hover.Over(p.bg), p.text).Clicked() {
			if running {
				a.dl.Pause("")
			} else {
				a.dl.Resume("")
			}
		}
	}
	if failed > 0 {
		if pillButton(c.Key("dl-retry-all"), iconReload, "全部重试", p.hover.Over(p.bg), p.text).Clicked() {
			a.dl.Retry("")
		}
	}
	if done > 0 {
		if pillButton(c.Key("dl-clear"), nil, "清除已完成", p.hover.Over(p.bg), p.text).Clicked() {
			// The files are the listener's, saved where they asked: only
			// the tasks go.
			a.dl.ClearDone()
			a.dlSel.Clear()
		}
	}
	if dir := a.downloadDir(); dir != "" && len(all) > 0 {
		if iconButton(c.Key("dl-folder"), iconFolder, 15, p.muted, "在文件夹中显示").Clicked() {
			mygo.Shell.OpenPath(dir)
		}
	}
}

// dlEmpty is the page with no task of the kind shown: the first time, what
// downloads do and where to start one.
func (a *app) dlEmpty(c *ui.Context, tab int) {
	p := a.pal
	ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
		ui.Box(c).Size(56, 56).Radius(16).Center().Background(p.hover).Children(func() {
			ui.Icon(c, iconDownload).Size(24, 24).TextColor(p.faint)
		})
		switch {
		case a.dl == nil || len(a.dl.Tasks()) == 0:
			ui.Text(c, "还没有下载").FontSize(15).FontWeight(600)
			ui.Text(c, "在歌曲、专辑或歌单的「…」里下载，也可以选中多首一起下载").FontSize(12.5).TextColor(p.muted)
		case tab == 1:
			ui.Text(c, "没有正在下载的歌曲").FontSize(14).TextColor(p.muted)
		case tab == 2:
			ui.Text(c, "没有暂停的歌曲").FontSize(14).TextColor(p.muted)
		case tab == 3:
			ui.Text(c, "没有出错的歌曲").FontSize(14).TextColor(p.muted)
		default:
			ui.Text(c, "还没有下载完成的歌曲").FontSize(14).TextColor(p.muted)
		}
	})
}

// dlRows lists the tasks: chosen by ⌘-click and Shift-click, as the songs
// of a list are, rather than by the list's own selection, whose accent
// would paint over the row's own colors.
func (a *app) dlRows(c *ui.Context, tasks []download.Task) {
	// The rows are keyed by task, so that the list keeps its place as the
	// sort moves the tasks under it.
	a.dlList.Key = func(i int) any {
		if i < len(tasks) {
			return tasks[i].ID
		}
		return i
	}
	a.dlList.KeepOffset = true
	a.dlList.Label = func(i int) string {
		if i < len(tasks) {
			return tasks[i].Track.Name
		}
		return ""
	}
	ui.List(c.Key("dl-list"), &a.dlList, len(tasks), func(i int) {
		t := &tasks[i]
		a.dlRow(c, t, tasks, i)
	}).Grow(1)
}

// dlRow is one task: its place, its cover, its name, where it is, what it
// is doing, and what may be done with it.
func (a *app) dlRow(c *ui.Context, t *download.Task, tasks []download.Task, i int) {
	p := a.pal
	cur := a.pl.Current()
	playing := cur != nil && cur.Ident() == t.Track.Ident() && t.Status == download.Done
	chosen := a.dlSel.Has(t.ID)
	row := ui.ButtonBase(c.Key(t.ID)).Height(64).PaddingX(10).Gap(12).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(t.Track.Name).
		ContextMenu(a.dlItems(t))
	hovered := row.Hovered()
	bg := ui.Transparent
	switch {
	case chosen:
		bg = p.accent.Alpha(0.12)
	case hovered:
		bg = p.hover
	}
	row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond}))
	row.Children(func() {
		// Where it is in the list, and what it is doing: the number is the
		// row's place, replaced by the play mark of one playing from its
		// file, or a downward arrow while it comes.
		ui.Box(c).Width(22).Center().Children(func() {
			switch {
			case playing:
				ui.Icon(c, iconPlay).Size(12, 12).TextColor(p.accent)
			case t.Status == download.Running:
				ui.Icon(c, iconDownload).Size(14, 14).TextColor(p.accent)
			default:
				ui.Textf(c, "%d", i+1).FontSize(12.5).FontFeatures("tnum").TextColor(p.faint).TextAlign(ui.End)
			}
		})
		a.cover(c, t.Track.Cover(), 96, 40, 7)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			name := p.text
			if playing {
				name = p.accent
			}
			ui.Text(c, t.Track.Name).FontSize(13.5).FontWeight(500).TextColor(name).SingleLine()
			if t.Status == download.Running && t.Size > 0 {
				a.dlProgress(c, t)
				return
			}
			ui.Text(c, a.dlLine(t)).FontSize(12).TextColor(a.dlLineColor(t)).SingleLine()
		})
		// What it is: the level it came at, in the quiet of a fact beside
		// the song's name rather than a badge that shouts.
		ui.Box(c).Width(56).Margin(0, 10, 0, 0).Children(func() {
			if t.Served != "" {
				ui.Text(c, dlLevelLabel(t.Served)).FontSize(11.5).TextColor(p.muted).
					TextAlign(ui.End).SingleLine()
			}
		})
		// What may be done with it, under the pointer or once chosen, so
		// that quiet rows stay quiet. Built either way, and only hidden:
		// mygo hands the item chosen from a menu back to the element that
		// opened it on a later frame, and a menu takes the pointer off its
		// row as it opens — an element built only under the pointer has
		// gone by then, and its choice is dropped, which left 「删除任务和
		// 文件」 doing nothing at all.
		actions := ui.Row(c).Width(80).Gap(2).Justify(ui.End)
		if !hovered && !chosen {
			actions.Invisible()
		}
		actions.Children(func() {
			a.dlRowActions(c, t)
		})
	})
	// What a click does: play a saved song from its own file, which is
	// what the page is for once a download is done, and choose and
	// unchoose with ⌘ and Shift, as the songs of a list are.
	if row.Clicked() {
		mods := row.ClickModifiers()
		switch {
		case mods&ui.Shift != 0:
			a.dlExtend(tasks, i)
		case mods&ui.Cmd != 0:
			a.dlToggle(t.ID)
			a.dlLast = i
		case t.Status == download.Done && t.Path != "":
			a.dlPlay(t)
		}
	}
}

// dlToggle adds a task to the choice, or takes it out.
func (a *app) dlToggle(id string) {
	if a.dlSel.Has(id) {
		a.dlSel.Remove(id)
		return
	}
	a.dlSel.Add(id)
}

// dlExtend chooses the tasks from the one last chosen to i, as a
// Shift-click does.
func (a *app) dlExtend(tasks []download.Task, i int) {
	lo, hi := min(a.dlLast, i), max(a.dlLast, i)
	for k := lo; k <= hi && k < len(tasks); k++ {
		a.dlSel.Add(tasks[k].ID)
	}
	a.dlLast = i
}

// dlProgress is a running task's bar, and the bytes under it.
func (a *app) dlProgress(c *ui.Context, t *download.Task) {
	p := a.pal
	share := min(1, float64(t.Done)/float64(t.Size))
	ui.Column(c).Gap(5).Margin(4, 0, 0, 0).Children(func() {
		ui.Progress(c, share).Height(3).Radius(2).Width(min(300, max(120, a.pageWidth(c)*0.26)))
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("%s / %s · %d%%", download.Size(t.Done), download.Size(t.Size), int(share*100))).
				FontSize(11.5).TextColor(p.muted).FontFeatures("tnum")
			if s := download.Speed(t.Speed); s != "" {
				ui.Text(c, s).FontSize(11.5).TextColor(p.faint).FontFeatures("tnum")
			}
			if t.FromCache {
				ui.Text(c, "来自缓存").FontSize(11.5).TextColor(p.faint)
			}
		})
	})
}

// dlLine says where a task is, in a line under its name.
func (a *app) dlLine(t *download.Task) string {
	switch t.Status {
	case download.Waiting:
		return "等待下载"
	case download.Paused:
		return "已暂停 · " + t.Track.ArtistNames()
	case download.Done:
		if t.Note != "" {
			return t.Note
		}
		line := "已保存"
		if names := t.Track.ArtistNames(); names != "" {
			line = names + " · " + line
		}
		if t.FromCache {
			line += "，来自缓存"
		}
		return line
	case download.Failed:
		return t.Reason
	}
	return ""
}

// dlLineColor is the color of the line under a task's name: the accent, the
// app's red, for one that failed, and muted for the rest.
func (a *app) dlLineColor(t *download.Task) ui.Color {
	if t.Status == download.Failed {
		return a.pal.accent
	}
	return a.pal.muted
}

// dlLevelLabel names a level as the settings do, without its rate.
func dlLevelLabel(level string) string {
	switch level {
	case "standard":
		return "标准"
	case "higher":
		return "较高"
	case "exhigh":
		return "极高"
	case "lossless":
		return "无损"
	case "hires":
		return "Hi-Res"
	}
	return level
}

// dlRowActions are the buttons at the end of a row: play the file, show it
// in the folder, pause, resume or retry, and the row's menu.
func (a *app) dlRowActions(c *ui.Context, t *download.Task) {
	p := a.pal
	switch t.Status {
	case download.Done:
		if t.Path != "" {
			if iconButton(c.Key("play"), iconPlay, 14, p.muted, "播放已下载的文件").Clicked() {
				a.dlPlay(t)
			}
			if iconButton(c.Key("reveal"), iconFolder, 14, p.muted, "在文件夹中显示").Clicked() {
				a.dlReveal(t)
			}
		}
	case download.Running, download.Waiting:
		if iconButton(c.Key("pause"), iconPause, 14, p.muted, "暂停").Clicked() {
			a.dl.Pause(t.ID)
		}
	case download.Paused:
		if iconButton(c.Key("resume"), iconPlay, 14, p.muted, "继续").Clicked() {
			a.dl.Resume(t.ID)
		}
	case download.Failed:
		if iconButton(c.Key("retry"), iconReload, 14, p.muted, "重试").Clicked() {
			a.dl.Retry(t.ID)
		}
	}
	iconButton(c.Key("more"), iconMore, 14, p.muted, "更多").Menu(a.dlItems(t))
}

// dlItems builds the menu of a task: what its row's actions do, and the
// ones that start it again or take it away.
func (a *app) dlItems(t *download.Task) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		switch t.Status {
		case download.Done:
			if t.Path != "" {
				if m.Item("播放已下载的文件").Chosen() {
					a.dlPlay(t)
				}
				if m.Item("在文件夹中显示").Chosen() {
					a.dlReveal(t)
				}
				m.Separator()
			}
		case download.Running, download.Waiting:
			if m.Item("暂停").Chosen() {
				a.dl.Pause(t.ID)
			}
			m.Separator()
		case download.Paused:
			if m.Item("继续").Chosen() {
				a.dl.Resume(t.ID)
			}
			m.Separator()
		case download.Failed:
			if m.Item("重试").Chosen() {
				a.dl.Retry(t.ID)
			}
			m.Separator()
		}
		if m.Item("重新下载").Chosen() {
			a.dl.Add([]download.Task{a.dlTask(t.Track, t.Level)})
		}
		if t.Path != "" {
			if m.Item("删除任务和文件").Chosen() {
				a.dlDelete([]*download.Task{t})
			}
			return
		}
		if m.Item("删除任务").Chosen() {
			a.dl.Delete([]string{t.ID})
		}
	}
}

// dlBulkBar acts on the tasks chosen: pause, resume, retry, or delete them
// with their files.
func (a *app) dlBulkBar(c *ui.Context, n int) {
	p := a.pal
	chosen := a.dlChosen()
	ui.Text(c, fmt.Sprintf("已选 %d 首", n)).FontSize(12).TextColor(p.muted)
	anyLive, anyPaused, anyFailed := false, false, false
	for _, t := range chosen {
		switch t.Status {
		case download.Running, download.Waiting:
			anyLive = true
		case download.Paused:
			anyPaused = true
		case download.Failed:
			anyFailed = true
		}
	}
	if anyLive {
		if pillButton(c.Key("bulk-pause"), nil, "暂停", p.hover.Over(p.bg), p.text).Clicked() {
			for _, t := range chosen {
				if t.Status == download.Running || t.Status == download.Waiting {
					a.dl.Pause(t.ID)
				}
			}
		}
	}
	if anyPaused || anyFailed {
		if pillButton(c.Key("bulk-resume"), nil, "继续", p.hover.Over(p.bg), p.text).Clicked() {
			for _, t := range chosen {
				switch t.Status {
				case download.Paused:
					a.dl.Resume(t.ID)
				case download.Failed:
					a.dl.Retry(t.ID)
				}
			}
		}
	}
	if pillButton(c.Key("bulk-delete"), nil, "删除", p.hover.Over(p.bg), p.text).Clicked() {
		ptrs := make([]*download.Task, len(chosen))
		for i := range chosen {
			ptrs[i] = &chosen[i]
		}
		a.dlDelete(ptrs)
		a.dlSel.Clear()
	}
}

// dlChosen returns the tasks chosen.
func (a *app) dlChosen() []download.Task {
	var out []download.Task
	if a.dl == nil {
		return out
	}
	for id := range a.dlSel.All() {
		if t, ok := a.dl.Task(id); ok {
			out = append(out, t)
		}
	}
	return out
}

// dlPlay plays a downloaded song from its own file, rather than fetching
// it from a source: the same bytes, without the network.
func (a *app) dlPlay(t *download.Task) {
	path, ok := a.dlFile(t)
	if !ok {
		return
	}
	a.pl.PlayFile(t.Track, path, t.Served)
}

// revealFile shows a file in the file manager. Tests replace it: mygo's
// shell runs the call on the main thread and waits for it, which a test
// with no main loop never gives it.
var revealFile = func(path string) { mygo.Shell.ShowItemInFolder(path) }

// trashFile moves a file to the trash, the one thing this page cannot
// undo and the trash can. Tests replace it for the same reason.
var trashFile = func(path string) error { return mygo.Shell.TrashItem(path) }

// dlReveal shows a downloaded song in the file manager.
func (a *app) dlReveal(t *download.Task) {
	path, ok := a.dlFile(t)
	if !ok {
		return
	}
	revealFile(path)
}

// dlFile returns a task's file, if it is still where it said it put it.
func (a *app) dlFile(t *download.Task) (string, bool) {
	if t.Path == "" {
		return "", false
	}
	if _, err := os.Stat(t.Path); err != nil {
		a.toast = "文件已经不在了，可以重新下载"
		return "", false
	}
	return t.Path, true
}

// keptFile is the player's KeptFile: the file a song was downloaded to, at
// the level it was kept at, so that playing it again hears it from there
// rather than fetching it a second time. A file the listener moved or took
// away is no file, and the song plays as any other.
//
// The player asks this off the main thread, as it opens a song, so it only
// reads what the manager holds: the tasks, which it reads under its own
// lock.
func (a *app) keptFile(t music.Track) (path, level string, ok bool) {
	if a.dl == nil {
		return "", "", false
	}
	id := t.Ident()
	for _, task := range a.dl.Tasks() {
		if task.Status != download.Done || task.Path == "" || task.Track.Ident() != id {
			continue
		}
		if _, err := os.Stat(task.Path); err != nil {
			continue
		}
		return task.Path, task.Served, true
	}
	return "", "", false
}

// downloadDir is the folder saved songs go in: the one the listener picked,
// and the Desktop until they do, as lx-music-desktop begins. An empty one
// is nothing to show.
func (a *app) downloadDir() string {
	if dir := a.settings.DownloadPath; dir != "" {
		return dir
	}
	if dir, err := mygo.App.Path(mygo.PathDesktop); err == nil {
		return dir
	}
	return ""
}

// dlDelete takes the tasks out of the list, and moves their files to the
// trash: the one thing on this page a click cannot undo, and the one the
// trash can.
func (a *app) dlDelete(tasks []*download.Task) {
	if a.dl == nil {
		return
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	a.dl.Delete(ids)
	var failed bool
	trashed := 0
	for _, t := range tasks {
		if t.Path == "" {
			continue
		}
		if err := trashFile(t.Path); err != nil {
			failed = true
			continue
		}
		trashed++
	}
	switch {
	case len(ids) == 1 && trashed == 1:
		a.toast = "已删除任务，文件移到了废纸篓"
	case trashed > 0:
		a.toast = fmt.Sprintf("已删除 %d 个任务，文件移到了废纸篓", len(ids))
	case failed:
		a.toast = "任务已删除，但文件没能移到废纸篓"
	default:
		a.toast = "已删除任务"
	}
	if failed && trashed > 0 {
		a.toast = fmt.Sprintf("已删除 %d 个任务，有些文件没能移到废纸篓", len(ids))
	}
}
