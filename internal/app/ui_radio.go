package app

import (
	"math/rand/v2"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// Radios play on without end: the personal FM, in one of its modes, as
// "fm:<source>:<mode>" — NetEase's without the source, as "fm:<mode>" —
// and the heart mode of the liked songs, as "heart".

const heartRadio = "heart"

var fmModes = []struct{ mode, label string }{
	{netease.FMDefault, "推荐"},
	{netease.FMFamiliar, "熟悉"},
	{netease.FMExplore, "探索"},
}

// fmRadio names the FM of a source in a mode. NetEase's keep the name they
// had, without the source, and another source's say which it is: two
// sources may name a mode alike and play different songs.
func fmRadio(src, mode string) string {
	if src == "" || src == music.SourceNetease {
		return "fm:" + mode
	}
	return "fm:" + src + ":" + mode
}

// fmParts reads a radio's name as the source and mode it was started with.
func fmParts(name string) (src, mode string, ok bool) {
	rest, found := strings.CutPrefix(name, "fm:")
	if !found {
		return "", "", false
	}
	if s, m, found := strings.Cut(rest, ":"); found {
		return s, m, true
	}
	return "", rest, true
}

// radioLabel names a radio as the listener knows it.
func radioLabel(name string) string {
	if name == heartRadio {
		return "心动模式"
	}
	src, mode, ok := fmParts(name)
	if !ok {
		return "私人 FM"
	}
	for _, m := range fmModes {
		if mode == m.mode {
			return "私人 FM · " + m.label
		}
	}
	if src != "" && src != music.SourceNetease {
		return "私人 FM · " + src
	}
	return "私人 FM"
}

func playable(tracks []netease.Track) []netease.Track {
	var out []netease.Track
	for _, t := range tracks {
		if t.Playable() {
			out = append(out, t)
		}
	}
	return out
}

// likedPlaylist returns the ID of the listener's liked songs, or 0.
func (a *app) likedPlaylist() int64 {
	for _, pl := range a.mine {
		if pl.SpecialType == 5 {
			return pl.ID
		}
	}
	return 0
}

// radioFeed returns what gives the radio name more tracks: it loads them
// on a goroutine, and gives them to got on the main thread, none if it
// cannot.
func (a *app) radioFeed(name string) func(got func([]netease.Track)) {
	return func(got func([]netease.Track)) {
		var fetch func() ([]netease.Track, error)
		if src, mode, ok := fmParts(name); ok {
			fetch = a.fmFetcher(src, mode)
		} else if name == heartRadio {
			// More like the last song queued, from the song it began with.
			q, pid := a.pl.Queue(), a.likedPlaylist()
			if len(q) == 0 || pid == 0 {
				got(nil)
				return
			}
			start, last := q[0].ID, q[len(q)-1].ID
			fetch = func() ([]netease.Track, error) { return netease.HeartMode(last, start, pid) }
		}
		if fetch == nil {
			got(nil)
			return
		}
		go func() {
			// The FM gives a song or three at a time, some of them VIP
			// songs the listener may not play: ask again for those.
			var tracks []netease.Track
			for tries := 0; tries < 3 && len(tracks) == 0; tries++ {
				more, err := fetch()
				if err != nil {
					break
				}
				tracks = playable(more)
			}
			a.post(func() { got(tracks) })
		}()
	}
}

// fmFetcher returns what gives a source's station its next songs, in a
// mode. NetEase's is the app's own call, which a test replaces; a
// service's is asked through the registry.
func (a *app) fmFetcher(src, mode string) func() ([]netease.Track, error) {
	if src == "" || src == music.SourceNetease {
		return func() ([]netease.Track, error) { return netease.PersonalFM(mode) }
	}
	p := a.providerOf(src)
	if p == nil {
		return nil
	}
	r, ok := any(p).(source.Radio)
	if !ok {
		return nil
	}
	return func() ([]netease.Track, error) { return r.Radio(mode) }
}

// playFM plays the personal FM in the mode chosen, or goes on playing it.
func (a *app) playFM() {
	src := a.fmSource()
	modes := a.fmModesOf(src)
	mode := ""
	if len(modes) > 0 {
		mode = modes[min(a.fmMode, len(modes)-1)].ID
	}
	name := fmRadio(src, mode)
	if a.pl.Radio() == name {
		if !a.pl.Playing() {
			a.pl.TogglePlay()
		}
		return
	}
	a.radioStarting = name
	feed := a.radioFeed(name)
	feed(func(tracks []netease.Track) {
		if a.radioStarting != name {
			return
		}
		a.radioStarting = ""
		if len(tracks) == 0 {
			a.toast = "私人 FM 暂时无法播放，请稍后再试"
			return
		}
		a.pl.PlayRadio(name, tracks, feed)
	})
}

// exitRadio stops the radio playing, but not the song it plays.
func (a *app) exitRadio() {
	name := a.pl.Radio()
	if name == "" {
		return
	}
	a.pl.EndRadio()
	label := radioLabel(name)
	if name != heartRadio {
		label = "私人 FM"
	}
	a.toast = "已退出" + label
}

// trashFM skips the song playing on the FM, which plays it no more.
//
// "No more" is NetEase's to keep: a service's station is skipped and told
// nothing, since neither source has a way to say it, and to say so would be
// to lie about what the next song is picked from.
func (a *app) trashFM() {
	tr := a.pl.Current()
	if tr == nil {
		return
	}
	id := tr.ID
	a.pl.Next()
	if src, _, _ := fmParts(a.pl.Radio()); src == "" || src == music.SourceNetease {
		go netease.FMTrash(id)
	}
}

// startHeartMode plays songs like one the listener likes: the one playing,
// if they like it, or else one of their liked songs at random.
func (a *app) startHeartMode() {
	if a.profile == nil {
		a.showMain()
		a.openLogin()
		return
	}
	pid := a.likedPlaylist()
	if pid == 0 {
		a.toast = "喜欢一些歌曲后，即可开启心动模式"
		return
	}
	if a.pl.Radio() == heartRadio {
		a.nowPlaying = true
		return
	}
	var seed *netease.Track
	fromPlaying := false
	// The heart radio is NetEase's: it plays from the songs it likes.
	if cur := a.pl.Current(); cur != nil && cur.Src() == music.SourceNetease && a.isLiked(cur) {
		s := *cur
		seed, fromPlaying = &s, true
	}
	liked := a.playlists[playlistKey("", pid)]
	a.radioStarting = heartRadio
	go func() {
		if seed == nil {
			if liked == nil {
				liked, _ = netease.PlaylistDetail(pid)
			}
			if liked != nil {
				if ok := playable(liked.Tracks); len(ok) > 0 {
					seed = &ok[rand.IntN(len(ok))]
				}
			}
		}
		var more []netease.Track
		var err error
		if seed != nil {
			more, err = netease.HeartMode(seed.ID, seed.ID, pid)
		}
		a.post(func() {
			if a.radioStarting != heartRadio {
				return
			}
			a.radioStarting = ""
			if seed == nil || err != nil {
				a.toast = "心动模式暂时无法开启，请稍后再试"
				return
			}
			more = playable(more)
			if fromPlaying && a.pl.Current() != nil && a.pl.Current().ID == seed.ID {
				a.pl.RadioFrom(heartRadio, more, a.radioFeed(heartRadio))
				return
			}
			a.pl.PlayRadio(heartRadio, append([]netease.Track{*seed}, more...), a.radioFeed(heartRadio))
		})
	}()
}

// fmSource is the source the FM page plays from: the one the home pages
// show while it has a station, else the first that has one.
//
// A registry that is not there, or none with a station, is NetEase's, as
// the page was before there were others.
func (a *app) fmSource() string {
	ids, at := sourcesWith[source.Radio](a, homeTabPage(a.route.page), a.homeSource())
	if len(ids) == 0 {
		return music.SourceNetease
	}
	return ids[at]
}

// fmModesOf are the modes a source's station plays in: NetEase's three,
// and a source's own as it says. A source with none plays one it picks
// alone, and the page offers no mode to choose.
func (a *app) fmModesOf(src string) []music.RadioMode {
	if src == "" || src == music.SourceNetease {
		var out []music.RadioMode
		for _, m := range fmModes {
			out = append(out, music.RadioMode{ID: m.mode, Name: m.label})
		}
		return out
	}
	if p := a.providerOf(src); p != nil {
		if r, ok := any(p).(source.Radio); ok {
			return r.RadioModes()
		}
	}
	return nil
}

// fmPage is the personal FM: what it plays, large, over its cover's
// colors, with the FM's own controls, and heart mode below.
func (a *app) fmPage(c *ui.Context) {
	p := a.pal
	src := a.fmSource()
	modes := a.fmModesOf(src)
	top, bottom := a.insets(c)
	ui.Scroll(c.Key("fm")).TrackScroll(&a.pageScroll).Grow(1).Padding(top+pageGap, pagePad, bottom+48, pagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.startMark(c)
		defer a.endMark(c)
		// The mode the FM plays in, if it plays.
		for i, m := range modes {
			if a.pl.Radio() == fmRadio(src, m.ID) {
				a.fmMode = i
			}
		}
		// The name stands alone, the source and the mode below it: the
		// heading names the page, and what it plays is a setting of it,
		// rather than three things sharing one line that the narrowest
		// window cramps.
		a.heading(c, "私人 FM", "无尽推荐")
		ui.Row(c).AlignItems(ui.Center).Gap(16).Margin(0, 0, 18, 0).Children(func() {
			a.homeSourceTabsIn(c)
			// The tabs keep their own bottom margin, which the row would
			// otherwise double: the row is where the mode goes beside them.
			ui.Box(c).Grow(1)
			if len(modes) < 2 {
				// A station with one mode has nothing to choose: the page
				// says which it plays instead of offering a control that
				// does nothing.
				return
			}
			mode := min(a.fmMode, len(modes)-1)
			labels := make([]string, len(modes))
			for i, m := range modes {
				labels[i] = m.Name
			}
			if segmented(c.Key("fm-mode"), &mode, labels...) {
				a.fmMode = mode
				// Playing the FM, it changes to the mode at once.
				if _, _, ok := fmParts(a.pl.Radio()); ok {
					a.playFM()
				}
			}
		})
		a.fmCard(c, src)
		// Heart mode plays the songs NetEase's account likes: another
		// source's station has none to play from.
		if src == "" || src == music.SourceNetease {
			a.section(c, "心动模式")
			a.heartCard(c, p)
		}
	})
}

// fmCard shows the song the FM plays, or how to start it.
func (a *app) fmCard(c *ui.Context, src string) {
	pl := a.pl
	_, _, onFM := fmParts(pl.Radio())
	var tr *netease.Track
	if onFM {
		tr = pl.Current()
	}
	var art *artwork
	if tr != nil {
		art = a.art.get(netease.Sized(tr.Album.PicURL, 600), true)
	}
	card := ui.Box(c.Key("fm-card")).Height(320).Radius(24).Background(ui.Hex("#232328")).TextColor(white).
		Shadow(0, 18, 44, -16, a.pal.cardShade)
	in := artIn(card, backdrop(art))
	card.Children(func() {
		if in < 1 {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(24).
				LinearGradient(ui.LinearGradient{Angle: 135, Oklab: true, From: ui.Oklch(0.42, 0.14, 20), To: ui.Oklch(0.3, 0.1, 300)})
		}
		artImage(c, backdrop(art), 24, in)
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(24).
			LinearGradient(ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
				{Color: ui.RGBA(0, 0, 0, 0.35), At: 0},
				{Color: ui.RGBA(0, 0, 0, 0.1), At: 1},
			}})
		ui.Row(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Padding(36, 40).Gap(40).AlignItems(ui.Center).Children(func() {
			a.fmCovers(c, tr)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				label := "私人 FM"
				if modes := a.fmModesOf(src); len(modes) > 0 {
					label += " · " + modes[min(a.fmMode, len(modes)-1)].Name
				}
				if onFM {
					label = radioLabel(pl.Radio())
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconRadio).Size(14, 14).TextColor(white.Alpha(0.75))
					ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(white.Alpha(0.75))
				})
				if tr == nil {
					ui.Text(c, "听见更多好音乐").FontSize(30).FontWeight(800).LetterSpacing(-0.6)
					ui.Text(c, "根据你的口味，一首接一首，不停推荐").FontSize(13.5).TextColor(white.Alpha(0.75))
					ui.Row(c).Margin(18, 0, 0, 0).Children(func() {
						label := "开始收听"
						if a.radioStarting != "" && strings.HasPrefix(a.radioStarting, "fm:") {
							label = "正在调频…"
						}
						if pillButton(c.Key("start"), iconPlay, label, white, ui.Hex("#1d1d1f")).Clicked() {
							a.playFM()
						}
					})
					return
				}
				ui.Text(c, tr.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
				switch {
				case pl.Loading():
					ui.Text(c, "正在缓冲…").FontSize(14).TextColor(white.Alpha(0.7)).SingleLine()
				case pl.Err() != "":
					ui.Text(c, pl.Err()).FontSize(14).TextColor(white).SingleLine()
				default:
					a.artistLinks(c, tr, white.Alpha(0.7), white, true).FontSize(14).SingleLine()
				}
				ui.Row(c).Gap(14).AlignItems(ui.Center).Margin(20, 0, 0, -8).Children(func() {
					if a.profile != nil {
						if iconButton(c.Key("trash"), iconTrash, 20, white.Alpha(0.8), "不喜欢").Clicked() {
							a.trashFM()
						}
					}
					a.likeButton(c.Key("like"), tr, 20, white.Alpha(0.8))
					play := ui.ButtonBase(c.Key("play")).Size(56, 56).Center().Radius(999).Background(white).TextColor(ui.Hex("#1d1d1f")).Label("播放/暂停")
					pose := ui.Pose{}
					if play.Pressed() {
						pose.Scale = 0.92
					} else if play.Hovered() {
						pose.Scale = 1.05
					}
					play.AnimatePose(pose, ui.Snappy)
					play.Children(func() { playPause(c, play, pl.Playing(), 22) })
					if play.Clicked() {
						pl.TogglePlay()
					}
					if iconButton(c.Key("next"), iconNext, 20, white.Alpha(0.8), "下一首").Clicked() {
						pl.Next()
					}
				})
			})
		})
	})
}

// fmCovers stacks the cover playing over the next one's, which peeks out
// behind it, tilted, as the FM's own player shows them.
func (a *app) fmCovers(c *ui.Context, tr *netease.Track) {
	const size = 232
	ui.Box(c).Size(size+36, size).Shrink(0).Children(func() {
		var next *netease.Track
		if tr != nil {
			if order, at := a.pl.PlayOrder(); at >= 0 && at+1 < len(order) {
				next = &a.pl.Queue()[order[at+1]]
			}
		}
		if next != nil {
			back := ui.Box(c.Key(next.ID)).Absolute().Top(16).Left(36).Size(size-32, size-32).Radius(14).Opacity(0.55).Background(white.Alpha(0.12)).
				Shadow(0, 10, 24, -8, ui.RGBA(0, 0, 0, 0.4))
			back.AnimatePose(ui.Pose{Rotate: 6, X: 14}, ui.Animation{Spring: ui.SpringOptions{Duration: 500 * time.Millisecond, Bounce: 0.2}})
			back.Children(func() { a.coverFill(c, next.Album.PicURL, 400, 14) })
		}
		front := ui.Box(c.Key("front")).Absolute().Top(0).Left(0).Size(size, size).Radius(16).
			Background(white.Alpha(0.12)).Shadow(0, 20, 44, -10, ui.RGBA(0, 0, 0, 0.55))
		if tr == nil {
			front.Center().Children(func() {
				ui.Icon(c, iconRadio).Size(64, 64).TextColor(white.Alpha(0.85))
			})
			return
		}
		scale := float32(1)
		if !a.pl.Playing() {
			scale = 0.94
		}
		front.AnimatePose(ui.Pose{Scale: scale}, ui.Animation{Spring: ui.SpringOptions{Duration: 600 * time.Millisecond, Bounce: 0.25}})
		front.Children(func() {
			ui.Box(c.Key(tr.ID)).Fill().Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.92}}).
				Children(func() { a.coverFill(c, tr.Album.PicURL, 600, 16) })
		})
	})
}

// heartCard starts heart mode: songs like those the listener likes.
func (a *app) heartCard(c *ui.Context, p palette) {
	on := a.pl.Radio() == heartRadio
	ui.Row(c).Padding(22, 26).Gap(20).Radius(18).AlignItems(ui.Center).Background(p.hover).Children(func() {
		ui.Box(c).Size(56, 56).Radius(16).Center().Shrink(0).
			LinearGradient(ui.LinearGradient{Angle: 135, Oklab: true, From: ui.Oklch(0.72, 0.17, 10), To: ui.Oklch(0.58, 0.22, 350)}).
			TextColor(white).Children(func() { ui.Icon(c, iconHeartFill).Size(26, 26) })
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, "从你喜欢的音乐出发").FontSize(15).FontWeight(700)
			ui.Text(c, "根据你红心过的歌曲，智能推荐风格相似的音乐，与你的喜欢穿插播放").FontSize(12.5).TextColor(p.muted)
		})
		if on {
			if pillButton(c.Key("exit-heart"), iconX, "退出心动模式", p.press.Over(p.bg), p.text).Clicked() {
				a.exitRadio()
			}
		}
		label := "开启心动模式"
		switch {
		case a.profile == nil:
			label = "登录后开启"
		case on:
			label = "正在播放"
		case a.radioStarting == heartRadio:
			label = "正在开启…"
		}
		if pillButton(c.Key("heart"), iconHeartFill, label, p.accent, p.onAccent).Clicked() {
			a.startHeartMode()
		}
	})
}
