package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
)

// downloadsApp is an app with downloads on, its songs going in a folder of
// the test's, and a manager that runs nothing: a task's start is what the
// page shows, not a file it writes.
func downloadsApp(t *testing.T) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	// Nothing downloads during a test: a task waits where it was put, and
	// the page shows it so.
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.DownloadOn = true
	a.settings.DownloadPath = dir
	a.settings.DownloadMax = 1
	a.dl = download.New(func(ctx context.Context, task *download.Task, p *download.Progress) error {
		<-done
		return nil
	}, func() {}, func([]download.Task) {})
	return a, dir
}

// Asking for a song asks its quality first, and says whose it is; the whole
// batch is asked at one quality, which begins at the settings' default.
func TestDownloadAsksQuality(t *testing.T) {
	a, dir := downloadsApp(t)
	a.settings.DownloadQuality = "lossless"
	songs := []music.Track{
		{ID: 1, Name: "晴天", Artists: []music.Artist{{Name: "周杰伦"}}},
		{ID: 2, Name: "七里香", Artists: []music.Artist{{Name: "周杰伦"}}},
	}
	a.route = route{page: pageDownloads}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	a.askDownload(songs)
	if !a.dlAsk.open {
		t.Fatal("asking for a download did not ask its quality")
	}
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if a.dlAsk.level != "lossless" {
		t.Errorf("the level asked is %q, want the default", a.dlAsk.level)
	}
	if err := tt.Click("开始下载"); err != nil {
		t.Fatalf("the dialog has no download button: %v", err)
	}
	tt.Frame()

	tasks := a.dl.Tasks()
	if len(tasks) != 2 {
		t.Fatalf("%d tasks queued, want 2", len(tasks))
	}
	for _, tk := range tasks {
		if tk.Level != "lossless" {
			t.Errorf("%s queued at %q, want the level chosen", tk.Track.Name, tk.Level)
		}
		if tk.Dir != dir {
			t.Errorf("%s goes to %q, want the settings' folder", tk.Track.Name, tk.Dir)
		}
	}
	if a.dlAsk.open {
		t.Error("the dialog stayed open after it was confirmed")
	}

	// Asking for the same songs again adds nothing, and says so.
	a.askDownload(songs[:1])
	a.startDownloads(songs[:1], "lossless")
	if n := len(a.dl.Tasks()); n != 2 {
		t.Errorf("%d tasks after asking again, want 2", n)
	}
}

// The page shows the tasks of the tab chosen, and the tabs count what they
// hold.
func TestDownloadsPageTabs(t *testing.T) {
	a, _ := downloadsApp(t)
	a.settings.DownloadMax = 0
	a.dl.Restore([]download.Task{
		{ID: "a@exhigh", Track: netease.Track{ID: 1, Name: "晴天"}, Level: "exhigh", Status: download.Paused, Dir: a.settings.DownloadPath},
		{ID: "b@exhigh", Track: netease.Track{ID: 2, Name: "七里香"}, Level: "exhigh", Status: download.Failed, Reason: "地址打不开", Dir: a.settings.DownloadPath},
		{ID: "c@exhigh", Track: netease.Track{ID: 3, Name: "夜曲"}, Level: "exhigh", Status: download.Done, Dir: a.settings.DownloadPath, Path: "/tmp/none.flac"},
	})
	a.route = route{page: pageDownloads}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	for _, c := range []struct {
		tab   int
		shown []string
		gone  []string
	}{
		{0, []string{"晴天", "七里香", "夜曲"}, nil},
		{1, nil, []string{"晴天", "七里香", "夜曲"}},
		{3, []string{"七里香"}, []string{"晴天", "夜曲"}},
		{4, []string{"夜曲"}, []string{"晴天", "七里香"}},
	} {
		a.dlTab = c.tab
		tt.Frame()
		for _, name := range c.shown {
			if !tt.HasText(name) {
				t.Errorf("tab %d does not show %q", c.tab, name)
			}
		}
		for _, name := range c.gone {
			if tt.HasText(name) {
				t.Errorf("tab %d shows %q, which it should not", c.tab, name)
			}
		}
	}
	// The failed task says why, in the words the run gave.
	a.dlTab = 3
	tt.Frame()
	if !tt.HasText("地址打不开") {
		t.Error("the failed task does not say why")
	}
}

// The page with no tasks says what downloads do, and the sidebar has its
// entry only while downloads are on.
func TestDownloadsPageNeedsDownloadsOn(t *testing.T) {
	a, _ := downloadsApp(t)
	a.settings.DownloadOn = false
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if r, _ := tt.Find("下载管理"); r.W > 0 {
		t.Error("the sidebar shows the download page with downloads off")
	}
	a.settings.DownloadOn = true
	tt.Frame()
	if r, _ := tt.Find("下载管理"); r.W == 0 {
		t.Error("the sidebar has no download page with downloads on")
	}
	// Reached with downloads off, it says where to turn them on.
	a.route = route{page: pageDownloads}
	a.settings.DownloadOn = false
	tt.Frame()
	if !tt.HasText("下载已在设置中关闭，打开后可以继续下载") {
		t.Error("the page does not say downloads are off")
	}
}

// Turning downloads off from the settings pauses what runs, and does not
// drop it: the switch is the only place the listener meets this, so the
// test takes the same road.
func TestTurningDownloadsOffPauses(t *testing.T) {
	a, _ := downloadsApp(t)
	a.settings.DownloadMax = 2
	songs := []music.Track{{ID: 1, Name: "晴天"}}
	a.startDownloads(songs, "exhigh")
	tasks := a.dl.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("%d tasks, want 1", len(tasks))
	}
	a.route = route{page: pageDownloads}
	// The switch's own handler: what turning downloads off does to what
	// was already asked for.
	a.settings.DownloadOn = false
	a.downloadToggled()
	tk, ok := a.dl.Task(tasks[0].ID)
	if !ok {
		t.Fatal("turning downloads off dropped the task")
	}
	if tk.Status != download.Paused {
		t.Errorf("the task is %q, want paused", tk.Status)
	}
	// The page it was on is left, since its entry is about to go.
	if a.route.page == pageDownloads {
		t.Error("the download page stayed open after downloads were turned off")
	}
}

// The page turns downloads on and off, and a task keeps the folder it was
// queued for even if the settings change under it.
func TestDownloadSettings(t *testing.T) {
	a, dir := downloadsApp(t)
	a.settings.DownloadOn = false
	openSettingsTab(a, settingsDownload)
	tt := ui.NewTester(a.settingsView, settingsWidth, 1600)
	tt.Frame()
	for _, want := range []string{"启用下载", "保存目录", "默认音质", "同时下载", "命名方式", "已存在时跳过", "其它来源", "写入封面", "写入歌词", "另存歌词文件"} {
		if !tt.HasText(want) {
			t.Errorf("the download settings do not show %q", want)
		}
	}
	// A newer folder does not move a task already queued: the listener
	// asked for that one when they asked for the song.
	task := a.dlTask(music.Track{ID: 1, Name: "晴天"}, "exhigh")
	if task.Dir != dir {
		t.Errorf("a new task goes to %q, want %q", task.Dir, dir)
	}
	a.settings.DownloadPath = t.TempDir()
	if got := a.dlTask(music.Track{ID: 2, Name: "夜曲"}, "exhigh").Dir; got != a.settings.DownloadPath {
		t.Errorf("a task queued after the change goes to %q, want the new folder", got)
	}
}

// A downloaded file plays from itself, without asking a source.
func TestDownloadedFilePlays(t *testing.T) {
	a, dir := downloadsApp(t)
	path := filepath.Join(dir, "晴天 - 周杰伦.flac")
	if err := os.WriteFile(path, []byte("fLaC"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := &download.Task{
		Track:  netease.Track{ID: 1, Name: "晴天", Artists: []netease.Artist{{Name: "周杰伦"}}},
		Served: "lossless",
		Path:   path,
	}
	// The player's own rule: the file is taken for the song it is of, once.
	a.dlPlay(task)
	if cur := a.pl.Current(); cur == nil || cur.Ident() != task.Track.Ident() {
		t.Fatalf("playing the file put %v in the player", cur)
	}
	// The file has gone since: the page says so rather than failing to
	// play it in silence.
	os.Remove(path)
	a.dlPlay(task)
	if a.toast != "文件已经不在了，可以重新下载" {
		t.Errorf("a missing file says %q", a.toast)
	}
}

// The download page asks for no frames of its own: a list of tasks waits
// still, as a page of songs does.
// A list in choice mode is still: its boxes are not an animation, and the
// row of actions that every row now builds is built hidden, not drawn.
func TestSongListInChoiceModeFrameBudget(t *testing.T) {
	a, _ := downloadsApp(t)
	tracks := []netease.Track{song(1, "一张相片", "林宥嘉"), song(2, "宝船", "DOUDOU"), song(3, "晴天", "周杰伦")}
	a.playlists[playlistKey("", 10)] = &netease.Playlist{ID: 10, Name: "通勤", Tracks: tracks}
	a.route = route{page: pagePlaylist, id: 10}
	a.trkPick = true
	a.trkSel = map[string]bool{tracks[0].Ident(): true}
	tt := ui.NewTester(a.view, 1200, 800)
	if n := framesAt(a, tt, 12*time.Second); n > 3 {
		t.Errorf("a list with boxes, still: %d frames a second", n)
	}
}

func TestDownloadsPageFrameBudget(t *testing.T) {
	a, _ := downloadsApp(t)
	a.dl.Restore([]download.Task{
		{ID: "a@exhigh", Track: netease.Track{ID: 1, Name: "晴天"}, Level: "exhigh", Status: download.Paused, Dir: a.settings.DownloadPath},
	})
	a.route = route{page: pageDownloads}
	tt := ui.NewTester(a.view, 1200, 800)
	if n := framesAt(a, tt, 12*time.Second); n > 3 {
		t.Errorf("the download page, still: %d frames a second", n)
	}
}
