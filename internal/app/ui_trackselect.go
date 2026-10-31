package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Choosing several songs in a list, to download them together.
//
// lx-music-desktop's list has a box beside every row; this app's lists are
// chosen the way the Finder's are, with ⌘ and Shift, so that the rows stay
// a list of songs rather than a form. The choice lives on the app rather
// than the list, since the bar that acts on it sits over the player bar,
// outside the list, and the list's own keys are its rows.

// trkSelected reports whether a song is chosen.
func (a *app) trkSelected(tr *netease.Track) bool { return a.trkSel[tr.Ident()] }

// trkToggle adds a song to the choice, or takes it out.
func (a *app) trkToggle(tr *netease.Track) {
	if a.trkSel == nil {
		a.trkSel = map[string]bool{}
	}
	id := tr.Ident()
	if a.trkSel[id] {
		delete(a.trkSel, id)
		return
	}
	a.trkSel[id] = true
}

// trkExtend chooses the songs from the one last chosen to i, as a
// Shift-click does: the run between them, in the list's order.
func (a *app) trkExtend(tracks []netease.Track, i int) {
	if a.trkSel == nil {
		a.trkSel = map[string]bool{}
	}
	from := min(a.trkLast, len(tracks)-1)
	lo, hi := min(from, i), max(from, i)
	for k := lo; k <= hi && k < len(tracks); k++ {
		a.trkSel[tracks[k].Ident()] = true
	}
	a.trkLast = i
}

// trkClear takes the choice away, and ends the list's choice mode with it:
// the boxes go once there is nothing they act on.
func (a *app) trkClear() {
	clear(a.trkSel)
	a.trkPick = false
}

// trkPickOn enters the list's choice mode: a box beside every song, and a
// click choosing rather than playing. A list is read by the pointer as
// often as by ⌘, and a box says what a modifier does not.
func (a *app) trkPickOn() {
	a.trkPick = true
}

// trkPickOff leaves it, keeping what was chosen.
func (a *app) trkPickOff() { a.trkPick = false }

// trkChosenSongs returns the songs chosen, in the order the page lists
// them, and keeps them for the bar to draw: the list is built again each
// frame, and the bar is not in it.
func (a *app) trkChosenSongs(tracks []netease.Track) []netease.Track {
	out := a.trkChosen[:0]
	if len(a.trkSel) == 0 {
		a.trkChosen = out
		return out
	}
	for i := range tracks {
		if a.trkSel[tracks[i].Ident()] {
			out = append(out, tracks[i])
		}
	}
	a.trkChosen = out
	return out
}

// trkDownloadButton is a list's 下载 pill, with a chevron beside it: the
// pill downloads the whole list at once, and the chevron opens what else a
// list of songs can do, which is to be read with boxes beside them.
//
// The two are one shape with a hairline between, as a split button on a
// tool bar: the pill is what a click usually wants, and the chevron is
// there when it is not.
func (a *app) trkDownloadButton(c *ui.Context, tracks []netease.Track) {
	p := a.pal
	bg := p.hover.Over(p.bg)
	// One shape of two halves: the pill's own background is the row's, so
	// the halves share it, and the hairline is what tells the click that
	// downloads from the one that opens the menu. Its own size, not the
	// row's to shrink: squeezed, the halves overlap the controls beside
	// them, and the chevron lands under the neighbouring 「更多」.
	ui.Row(c).Height(36).Radius(999).Background(bg).Gap(0).AlignItems(ui.Center).Shrink(0).Children(func() {
		half := func(key any, label string) ui.Element {
			b := ui.ButtonBase(c.Key(key)).Height(36).Padding(0, 16).Radius(999).FocusRing(false).Shrink(0).
				Label(label).AlignItems(ui.Center).Gap(8).TextColor(p.text)
			if b.Hovered() {
				b.Background(bg.Mix(ui.RGB(255, 255, 255), 0.12))
			}
			return b
		}
		d := half("download", "下载")
		d.Children(func() {
			ui.Icon(c, iconDownload).Size(14, 14)
			ui.Text(c, "下载").FontSize(14).FontWeight(600).SingleLine()
		})
		if d.Clicked() {
			// In choice mode the pill does what the bar does: the songs
			// chosen, or the whole list when none is.
			if chosen := a.trkChosenSongs(tracks); a.trkPick && len(chosen) > 0 {
				a.askDownload(chosen)
			} else {
				a.askDownload(tracks)
			}
		}
		ui.Box(c).Width(1).Height(16).Background(p.hairline)
		// The chevron is the menu button, its own size at the pill's end.
		more := half("download-more", "批量下载")
		more.Padding(0, 10)
		more.Children(func() { ui.Icon(c, iconChevron).Size(13, 13).TextColor(p.muted) })
		more.PopoverMenuButton(func(m *ui.Menu) {
			if m.Item("批量下载…").Chosen() {
				a.trkPickOn()
			}
			if m.Item("全选").Chosen() {
				a.trkPickOn()
				if a.trkSel == nil {
					a.trkSel = map[string]bool{}
				}
				for i := range tracks {
					a.trkSel[tracks[i].Ident()] = true
				}
				a.trkLast = max(len(tracks)-1, 0)
			}
			m.Separator()
			if m.Item("下载整个列表").Chosen() {
				a.askDownload(tracks)
			}
		})
	})
}

// trkBox is a song row's box in choice mode: filled with the accent and a
// check once chosen. The row itself is the button, which is what makes the
// whole row a target the pointer can hit.
func (a *app) trkBox(c *ui.Context, chosen bool) {
	p := a.pal
	box := ui.Box(c).Size(17, 17).Radius(5).Center()
	if chosen {
		box.Background(p.accent)
	} else {
		box.Border(1.5, p.muted.Alpha(0.55))
	}
	box.Children(func() {
		if chosen {
			ui.Icon(c, iconCheck).Size(11, 11).TextColor(p.onAccent)
		}
	})
}

// trkBar is the bar over the player bar while songs are chosen: how many,
// and the few things to do with them. It is mounted over the page rather
// than in it, so that it stays where the pointer can reach it as the list
// under it scrolls.
func (a *app) trkBar(c *ui.Context) {
	chosen := a.trkChosen
	// In choice mode the bar shows with nothing chosen yet: it is what
	// says the boxes are there and how to be done with them.
	if (len(chosen) == 0 && !a.trkPick) || !a.downloadEnabled() {
		return
	}
	p := a.pal
	_, bottom := a.insets(c)
	bottom += 18
	// A pill floating over the player rather than a bar across the page:
	// it is about the songs chosen, not the page, and it is the page's
	// center it takes so that it reads as over the list.
	ui.Box(c).Absolute().Left(0).Right(0).Bottom(bottom).Height(48).Children(func() {
		ui.Row(c).AlignSelf(ui.Center).Height(48).Gap(6).Padding(6, 8).Radius(24).Background(p.bg).
			Border(1, p.hairline).Shadow(0, 12, 34, -10, ui.RGBA(0, 0, 0, 0.32)).
			Label(trkBarLine(chosen)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("已选 %d 首", len(chosen))).FontSize(12.5).FontWeight(600).Margin(0, 10, 0, 6)
			// The three things chosen songs are for, each only once there
			// is a song to do it with.
			if len(chosen) > 0 {
				if pillButton(c.Key("trk-download"), iconDownload, "下载…", p.accent, p.onAccent).Clicked() {
					a.askDownload(chosen)
				}
				if pillButton(c.Key("trk-play"), iconPlay, "播放", p.hover.Over(p.bg), p.text).Clicked() {
					a.pl.PlayQueue(chosen, 0)
					a.trkClear()
				}
				if pillButton(c.Key("trk-add"), iconPlus, "加入队列", p.hover.Over(p.bg), p.text).Clicked() {
					a.pl.Append(chosen)
					a.trkClear()
				}
			}
			// 完成 ends the choice mode but keeps what was chosen, so that
			// the list goes back to playing on a click and the bar stays:
			// ending both at once would throw the choice away with the mode.
			label := "取消选择"
			if a.trkPick {
				label = "完成"
			}
			if iconButton(c.Key("trk-clear"), iconX, 15, p.muted, label).Clicked() {
				if a.trkPick {
					a.trkPickOff()
					return
				}
				a.trkClear()
			}
		})
	})
}

// trkBarLine says which songs are chosen, for the label assistive
// technology reads.
func trkBarLine(tracks []netease.Track) string {
	names := make([]string, 0, min(len(tracks), 3))
	for i := range tracks {
		if i == 3 {
			names = append(names, "…")
			break
		}
		names = append(names, tracks[i].Name)
	}
	return strings.Join(names, "、")
}
