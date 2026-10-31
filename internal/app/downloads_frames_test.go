package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// With NM_FRAMES set, saves the download page with a task of every state,
// in the light and the dark, and the quality dialog that starts one.
func TestDownloadPageFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		a, _ := downloadsApp(t)
		dirPath := a.downloadDir()
		// A restored task comes back paused: what runs is added after, its
		// run reporting a part of the song and then waiting, as a download
		// in progress does.
		held := make(chan struct{})
		t.Cleanup(func() { close(held) })
		a.dl = download.New(func(ctx context.Context, task *download.Task, p *download.Progress) error {
			p.Total(41 << 20)
			p.Bytes(12 << 20)
			p.Bytes(13 << 20)
			<-held
			return ctx.Err()
		}, func() {}, func([]download.Task) {})
		a.dl.Restore([]download.Task{
			{ID: "b@exhigh", Track: song(2, "七里香", "周杰伦"), Level: "exhigh", Served: "exhigh",
				Status: download.Waiting, Dir: dirPath},
			{ID: "c@exhigh", Track: song(3, "夜曲", "周杰伦"), Level: "exhigh", Served: "exhigh",
				Status: download.Paused, Dir: dirPath},
			{ID: "d@exhigh", Track: song(4, "不能说的秘密", "周杰伦"), Level: "exhigh", Served: "exhigh",
				Status: download.Failed, Reason: "这首歌需要 VIP，其它来源也没有找到", Dir: dirPath},
			{ID: "e@exhigh", Track: song(5, "珊瑚海", "周杰伦, {{PERSON_18cynlyj}}"), Level: "exhigh", Served: "exhigh",
				Status: download.Done, Path: filepath.Join(dirPath, "珊瑚海 - 周杰伦, {{PERSON_18cynlyj}}.flac"), Dir: dirPath},
			{ID: "f@standard", Track: song(6, "一路向北", "周杰伦"), Level: "standard", Served: "standard",
				Status: download.Done, FromCache: true, Path: filepath.Join(dirPath, "一路向北.flac"), Dir: dirPath},
		})
		a.dl.SetMax(1)
		a.dl.Add([]download.Task{{ID: "a@lossless", Level: "lossless", Track: song(1, "晴天", "周杰伦")}})
		a.route = route{page: pageDownloads}
		tt := ui.NewTester(a.view, 1180, 780)
		tt.SetDark(dark)
		tt.Advance(time.Second)
		tt.Frame()
		name := "downloads.png"
		if dark {
			name = "downloads-dark.png"
		}
		saveFrame(t, tt, filepath.Join(dir, name))

		if !dark {
			a.askDownload([]netease.Track{song(7, "搁浅", "周杰伦"), song(8, "借口", "周杰伦")})
			tt.Frame()
			tt.Advance(400 * time.Millisecond)
			tt.Frame()
			saveFrame(t, tt, filepath.Join(dir, "downloads-quality.png"))

			a.dlAsk.open = false
			a.dlTab = 0
			a.dlSel.Add("b@exhigh")
			a.dlSel.Add("c@exhigh")
			tt.Frame()
			tt.Advance(200 * time.Millisecond)
			tt.Frame()
			saveFrame(t, tt, filepath.Join(dir, "downloads-chosen.png"))

			a.settings.DownloadOn = false
			openSettingsTab(a, settingsDownload)
			st := ui.NewTester(a.settingsView, settingsWidth, 1400)
			st.Advance(time.Second)
			st.Frame()
			saveFrame(t, st, filepath.Join(dir, "settings-download.png"))
		}
	}
}

func song(id int64, name, artists string) netease.Track {
	return netease.Track{ID: id, Name: name, Artists: []netease.Artist{{Name: artists}}, Duration: 200000}
}

func saveFrame(t *testing.T, tt *ui.Tester, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// With NM_FRAMES set, saves a playlist with two songs chosen, and the bar
// over the player that acts on them.
func TestSongChoiceBarFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	a, _ := downloadsApp(t)
	tracks := []netease.Track{
		song(1, "一张相片", "林宥嘉"),
		song(2, "宝船", "DOUDOU"),
		song(3, "晴天", "周杰伦"),
	}
	a.playlists[playlistKey("", 10)] = &netease.Playlist{ID: 10, Name: "通勤", Tracks: tracks}
	a.route = route{page: pagePlaylist, id: 10}
	a.trkSel = map[string]bool{tracks[0].Ident(): true, tracks[1].Ident(): true}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	saveFrame(t, tt, filepath.Join(dir, "songs-chosen.png"))

	// The 下载 pill's chevron turned the list into boxes, two chosen.
	a.trkPick = true
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	saveFrame(t, tt, filepath.Join(dir, "songs-boxes.png"))
}
