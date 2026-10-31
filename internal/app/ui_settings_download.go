package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// The download settings, after lx-music-desktop's: where saved songs go,
// how their files are named, how many download at once, what goes inside
// them, and whether the words come beside them. Downloads are off until
// the listener turns them on here, since a folder filled with music they
// did not ask for is their own business.

// downloadSettings is the 下载 tab.
func (a *app) downloadSettings(group func(string, func()), row func(string, string, func()), c *ui.Context) {
	s := &a.settings
	// The groups show whether downloads are on or off: the switch is what
	// a listener comes to this tab for, and what it would give them reads
	// better beside it than as a blank page.
	group("下载", func() {
		row("启用下载", "打开后，侧边栏出现「下载管理」，歌曲与专辑可以下载到本机", func() {
			if ui.Switch(c.Key("download-on"), &s.DownloadOn).Label("启用下载").Changed() {
				a.downloadToggled()
				a.changed()
			}
		})
		row("保存目录", a.downloadDirLabel(), func() {
			label := "选择…"
			if s.DownloadPath != "" {
				label = "更改…"
			}
			if pillButton(c.Key("dl-dir"), nil, label, a.pal.hover.Over(a.pal.bg), a.pal.text).Clicked() {
				a.pickDownloadDir()
			}
		})
		row("默认音质", "下载时先按这个音质取，也可以在每次下载时另选", func() {
			labels := make([]string, len(qualityLevels)+1)
			labels[0] = "跟随播放音质"
			chosen := labels[0]
			for i, q := range qualityLevels {
				labels[i+1] = q.label
				if q.level == s.DownloadQuality {
					chosen = q.label
				}
			}
			if ui.Select(c.Key("dl-quality-default"), &chosen, labels).Label("默认音质").Width(150).Changed() {
				s.DownloadQuality = ""
				for _, q := range qualityLevels {
					if q.label == chosen {
						s.DownloadQuality = q.level
					}
				}
				a.changed()
			}
		})
		row("同时下载", "一次下载几首，多了更快，也更容易被来源限速", func() {
			labels := make([]string, 6)
			chosen := fmt.Sprintf("%d 首", s.DownloadMax)
			for i := range labels {
				labels[i] = fmt.Sprintf("%d 首", i+1)
			}
			for _, l := range labels {
				if l == chosen {
					chosen = l
				}
			}
			if ui.Select(c.Key("dl-max"), &chosen, labels).Label("同时下载").Width(150).Changed() {
				for i, l := range labels {
					if l == chosen {
						s.DownloadMax = i + 1
					}
				}
				a.setDownloadMax()
				a.changed()
			}
		})
	})
	// The files' names, with a song's own shown under the scheme, so that
	// the listener sees the name before the folder is full of them.
	group("文件名", func() {
		row("命名方式", a.downloadNamePreview(), func() {
			labels := make([]string, len(download.NameFormats))
			chosen := download.NameFormats[0].Label
			for i, f := range download.NameFormats {
				labels[i] = f.Label
				if f.Format == s.DownloadName {
					chosen = f.Label
				}
			}
			if ui.Select(c.Key("dl-name"), &chosen, labels).Label("命名方式").Width(150).Changed() {
				for _, f := range download.NameFormats {
					if f.Label == chosen {
						s.DownloadName = f.Format
					}
				}
				a.changed()
			}
		})
		row("已存在时跳过", "同一首歌已经保存过时不再下载，留着你手上那个文件", func() {
			if ui.Switch(c.Key("dl-skip"), &s.DownloadSkip).Label("已存在时跳过").Changed() {
				a.changed()
			}
		})
		row("其它来源", "自己的来源给不出这首歌时，到别的来源找一找，如 lx-music-desktop 的「使用其它源」", func() {
			if ui.Switch(c.Key("dl-other"), &s.DownloadOtherSource).Label("其它来源").Changed() {
				a.changed()
			}
		})
	})
	group("歌曲信息", func() {
		row("写入封面", "把歌曲封面写进文件，播放器里能看到", func() {
			if ui.Switch(c.Key("dl-cover"), &s.EmbedCover).Label("写入封面").Changed() {
				a.changed()
			}
		})
		row("写入歌词", "把歌词写进文件，歌词随文件走，不必另存一份", func() {
			if ui.Switch(c.Key("dl-lyric"), &s.EmbedLyric).Label("写入歌词").Changed() {
				a.changed()
			}
		})
		row("歌词含翻译", "写入歌词时，把翻译一并写进去；需要先打开「写入歌词」", func() {
			sw := ui.Switch(c.Key("dl-translation"), &s.EmbedTranslation).Label("歌词含翻译")
			if !s.EmbedLyric {
				sw.Disabled(true).Opacity(0.5)
			}
			if sw.Changed() {
				a.changed()
			}
		})
	})
	group("歌词文件", func() {
		row("另存歌词文件", "在歌曲旁边写一个同名的 .lrc，一些播放器要读它", func() {
			if ui.Switch(c.Key("dl-lrc"), &s.DownloadLrc).Label("另存歌词文件").Changed() {
				a.changed()
			}
		})
		row("歌词含翻译", "另存的歌词文件里，翻译行跟在原文下", func() {
			sw := ui.Switch(c.Key("dl-lrc-translation"), &s.DownloadLrcTranslation).Label("歌词含翻译")
			if !s.DownloadLrc {
				sw.Disabled(true).Opacity(0.5)
			}
			if sw.Changed() {
				a.changed()
			}
		})
		row("编码", "UTF-8 带 BOM，多数播放器都读；GBK 给只认它的旧播放器", func() {
			labels := []string{"UTF-8", "GBK"}
			chosen := labels[0]
			if s.DownloadLrcFormat == "gbk" {
				chosen = labels[1]
			}
			sw := ui.Select(c.Key("dl-lrc-format"), &chosen, labels).Label("编码").Width(150)
			if !s.DownloadLrc {
				sw.Disabled(true).Opacity(0.5)
			}
			if sw.Changed() {
				if chosen == labels[1] {
					s.DownloadLrcFormat = "gbk"
				} else {
					s.DownloadLrcFormat = "utf8"
				}
				a.changed()
			}
		})
	})
}

// downloadDirLabel says where saved songs go, as the row shows it: the
// folder the settings name, or the Desktop they begin at.
func (a *app) downloadDirLabel() string {
	if a.settings.DownloadPath != "" {
		return a.settings.DownloadPath
	}
	if dir := a.downloadDir(); dir != "" {
		return "默认：" + dir
	}
	return "还没有设置下载目录"
}

// downloadNamePreview says what a song's file will be called, with a song's
// own name as the example: the listener reads the name before the folder
// fills with them.
func (a *app) downloadNamePreview() string {
	// The song played last is the nearest to a name the listener will see;
	// before any plays, the usual shape of one.
	name, artist := "晴天", "周杰伦"
	if cur := a.pl.Current(); cur != nil {
		if cur.Name != "" {
			name = cur.Name
		}
		if names := cur.ArtistNames(); names != "" {
			artist = names
		}
	}
	example := download.FileName(a.settings.DownloadName, *curOr(a.pl.Current(), name, artist))
	if example == "" {
		return ""
	}
	return "例如：" + example + ".flac"
}

// curOr is the track playing, or a stand-in with the name and artists the
// preview shows.
func curOr(cur *netease.Track, name, artist string) *netease.Track {
	if cur != nil {
		return cur
	}
	return &netease.Track{Name: name, Artists: []netease.Artist{{Name: artist}}}
}

// pickDownloadDir asks for the folder saved songs go in.
//
// The dialog is shown off the frame that asked for it, as pickSourceFile's
// is: showing one is a modal, and a modal cannot be run from within a
// build.
func (a *app) pickDownloadDir() {
	s := &a.settings
	parent := a.settingsWin
	start := a.downloadDir()
	go func() {
		dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:            parent,
			Title:             "选择保存歌曲的文件夹",
			DefaultPath:       start,
			Directory:         true,
			CreateDirectories: true,
			ButtonLabel:       "选择",
		})
		if err != nil {
			a.post(func() { a.toast = "无法打开选择窗口" })
			return
		}
		if len(dirs) == 0 {
			return
		}
		dir := strings.TrimSpace(dirs[0])
		if dir == "" {
			return
		}
		a.post(func() {
			s.DownloadPath = filepath.Clean(dir)
			a.changed()
		})
	}()
}

// downloadToggled keeps the tasks the listener already asked for as the
// switch changes: turned off, what is downloading is paused rather than
// dropped, and the page it was on goes back, since its entry is about to
// be gone. Turned on, the paused ones stay paused: nothing starts on its
// own.
func (a *app) downloadToggled() {
	if a.settings.DownloadOn {
		a.toast = "下载已打开，侧边栏出现了「下载管理」"
		return
	}
	if a.dl != nil {
		a.dl.Pause("")
	}
	// A list left in choice mode would keep choosing with a click, and the
	// bar that says so is gone with downloads: the choice goes with it.
	a.trkClear()
	if a.route.page == pageDownloads {
		a.open(route{page: pageHome})
	}
	a.toast = "下载已关闭，正在下载的任务已暂停"
}
