package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// A downloaded file plays from where it is on a later play, which is the
// point of keeping it: the same song, without the network. The player asks
// the app for it as it opens the song.
func TestDownloadedFilePlaysNextTime(t *testing.T) {
	a, dir := downloadsApp(t)
	file := filepath.Join(dir, "晴天.flac")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.dl.Restore([]download.Task{{ID: "a@lossless", Track: song(1, "晴天", "周杰伦"), Level: "lossless", Served: "lossless",
		Status: download.Done, Path: file, Dir: dir}})

	path, level, ok := a.keptFile(song(1, "晴天", "周杰伦"))
	if !ok || path != file || level != "lossless" {
		t.Fatalf("keptFile = %q, %q, %v; want the downloaded file at its level", path, level, ok)
	}
	// A song that was not downloaded, or one whose file went, is nothing
	// to play from.
	if _, _, ok := a.keptFile(song(2, "夜曲", "周杰伦")); ok {
		t.Error("keptFile found a file for a song not downloaded")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := a.keptFile(song(1, "晴天", "周杰伦")); ok {
		t.Error("keptFile found a file the listener took away")
	}
}

// The row's 更多 opens a menu whose item must reach the row: mygo hands the
// choice back to the element on a later frame, and a menu takes the pointer
// off its row as it opens, so the row's buttons must stay built. They were
// built only while the pointer was on the row, and 「删除任务和文件」 did
// nothing.
func TestRowMenuChoiceDeletesTheTaskAndFile(t *testing.T) {
	trashed := ""
	real := trashFile
	trashFile = func(path string) error { trashed = path; return nil }
	defer func() { trashFile = real }()

	a, dir := downloadsApp(t)
	file := filepath.Join(dir, "晴天.flac")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.dl = download.New(func(ctx context.Context, tk *download.Task, p *download.Progress) error { return nil }, func() {}, func([]download.Task) {})
	a.dl.Restore([]download.Task{{ID: "a@exhigh", Track: song(1, "晴天", "周杰伦"), Level: "exhigh", Served: "exhigh",
		Status: download.Done, Path: file, Dir: dir}})
	a.route = route{page: pageDownloads}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()

	// The pointer goes on the row for its buttons, then off it, as it does
	// on the way to the menu item.
	r, ok := tt.Find("晴天")
	if !ok {
		t.Fatal("the row is not there")
	}
	tt.Move(r.X+r.W-200, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("更多"); err != nil {
		t.Fatalf("the row has no 更多: %v", err)
	}
	tt.Frame()
	tt.Move(60, 400)
	tt.Frame()

	if err := tt.ChooseMenuItem("删除任务和文件"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Frame()
	if len(a.dl.Tasks()) != 0 {
		t.Errorf("the task is still there: %+v", a.dl.Tasks())
	}
	if trashed != file {
		t.Errorf("trashed %q, want %q", trashed, file)
	}
}

// The 下载 pill's chevron turns a list into boxes: a click chose rather
// than played, so a hand on a mouse can download a few songs without
// knowing ⌘.
func TestChoosingSongsWithBoxes(t *testing.T) {
	a, _ := downloadsApp(t)
	tracks := []netease.Track{
		song(1, "一张相片", "林宥嘉"),
		song(2, "宝船", "DOUDOU"),
		song(3, "晴天", "周杰伦"),
	}
	a.playlists[playlistKey("", 10)] = &netease.Playlist{ID: 10, Name: "通勤", Tracks: tracks}
	a.route = route{page: pagePlaylist, id: 10}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()

	if err := tt.Click("批量下载"); err != nil {
		t.Fatalf("the 下载 pill has no chevron: %v", err)
	}
	if err := tt.ChooseMenuItem("批量下载…"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Frame()
	if !a.trkPick {
		t.Fatal("the chevron did not turn the list into boxes")
	}

	// A click chooses now, and does not play.
	if err := tt.Click("宝船"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.trkSelected(&tracks[1]) {
		t.Error("a click in choice mode did not choose the song")
	}
	if a.pl.Current() != nil {
		t.Error("a click in choice mode played the song")
	}
	if !tt.HasText("已选 1 首") {
		t.Errorf("the bar does not say one is chosen: %q", tt.Texts())
	}

	// 完成 leaves the boxes, keeping the choice: a click plays again.
	if err := tt.Click("完成"); err != nil {
		t.Fatalf("the bar has no 完成: %v", err)
	}
	tt.Frame()
	if a.trkPick {
		t.Error("完成 left the boxes")
	}
	if !a.trkSelected(&tracks[1]) {
		t.Error("完成 took the choice away")
	}
}

// 全选 chooses the whole list at once, which is what a listener means by
// downloading everything.
func TestChoosingEverySong(t *testing.T) {
	a, _ := downloadsApp(t)
	tracks := []netease.Track{song(1, "一张相片", "林宥嘉"), song(2, "宝船", "DOUDOU"), song(3, "晴天", "周杰伦")}
	a.playlists[playlistKey("", 10)] = &netease.Playlist{ID: 10, Name: "通勤", Tracks: tracks}
	a.route = route{page: pagePlaylist, id: 10}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	if err := tt.Click("批量下载"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("全选"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	tt.Frame()
	if got := len(a.trkChosenSongs(tracks)); got != len(tracks) {
		t.Errorf("%d songs chosen, want all %d", got, len(tracks))
	}
}
