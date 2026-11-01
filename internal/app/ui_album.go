package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// albumDetail loads an album from NetEase; tests replace it.
var albumDetail = netease.AlbumDetail

// albumData is what the app has of an album.
type albumData struct {
	album   *netease.AlbumProfile
	loading bool
	err     string
	// collected is whether the source's account has the album in its
	// collection, asked with the album and once per account: the answer is
	// the account's, and another account's answer is not this one's. A
	// source that would not say is asked once too — the page is built every
	// frame, and asking again there would be a request a frame.
	collected      bool
	collectedFor   int64 // the account the answer is of
	collectedAsked bool  // the collection was read, or the read failed
	asking         bool  // the collection is being read or written
	collection     bool  // the collection was read: the button shows what it says
}

func (a *app) loadAlbum(src string, id int64) {
	key := sourceKey(src, id)
	d := a.albums[key]
	if d == nil {
		d = &albumData{}
		a.albums[key] = d
	}
	if d.album != nil || d.loading {
		return
	}
	d.loading, d.err = true, ""
	fetch := albumDetail
	if p := a.albumPages(src); p != nil {
		fetch = p.Album
	}
	go func() {
		al, err := fetch(id)
		a.post(func() {
			d.loading = false
			if err != nil {
				d.err = a.pageError(src, "专辑加载失败", err)
				return
			}
			d.album = al
		})
	}()
}

// albumPages is a source's album pages, or nil for NetEase's, which are the
// app's own call.
func (a *app) albumPages(src string) source.AlbumPages {
	if src == "" || src == music.SourceNetease || a.reg == nil {
		return nil
	}
	p, ok := a.reg.Get(src)
	if !ok {
		return nil
	}
	ap, _ := p.(source.AlbumPages)
	return ap
}

func (a *app) albumPage(c *ui.Context) {
	src, id := a.route.src, a.route.id
	d := a.albums[sourceKey(src, id)]
	if d == nil || d.album == nil {
		if d == nil || !d.loading {
			err := ""
			if d != nil {
				err = d.err
			}
			a.centered(c, err, func() { a.loadAlbum(src, id) })
			return
		}
		s := a.pageShimmer(c)
		a.trackSkeleton(c, s, false, func(c *ui.Context) { a.playlistHeaderSkeleton(c, s) })
		return
	}
	a.trackRows(c, d.album.Songs, false, func(c *ui.Context) { a.albumHeader(c, src, id, d) })
	a.loadAlbumCollected(src, id, d)
}

// loadAlbumCollected asks the source whether the listener's account keeps
// an album, once per account: the answer is the account's, and it changes
// only when the listener changes it here or on their phone, which is what
// opening the page again shows.
func (a *app) loadAlbumCollected(src string, id int64, d *albumData) {
	c, ok := a.collectedOf(src)
	if !ok || d.asking {
		return
	}
	prof := a.accountProfile(src)
	if prof == nil {
		return
	}
	if d.collectedAsked && d.collectedFor == prof.UserID {
		return
	}
	uid := prof.UserID
	d.asking, d.collectedAsked, d.collectedFor = true, true, uid
	go func() {
		kept, err := c.AlbumCollected(id)
		a.post(func() { a.albumCollectedAnswered(src, d, uid, kept, err) })
	}()
}

// albumCollectedAnswered takes whether the account keeps the album, or why
// it did not say. A source that would not say leaves the button as it was:
// 收藏专辑 is right for most albums, and clicking it says the truth.
func (a *app) albumCollectedAnswered(src string, d *albumData, uid int64, kept bool, err error) {
	d.asking = false
	if p := a.accountProfile(src); p == nil || p.UserID != uid {
		return
	}
	if err == nil {
		d.collected, d.collection = kept, true
	}
}

// toggleCollectedAlbum collects an album the listener's account does not
// keep, or drops it from the collection: at once on the page, and then with
// the source, which may refuse — the button goes back then, as a heart does.
func (a *app) toggleCollectedAlbum(src string, id int64, d *albumData) {
	c, ok := a.collectedOf(src)
	if !ok || d.asking {
		return
	}
	if !a.signedInTo(src) {
		// The source's own code, from the dialog that names it: it is
		// signed in where its row in the settings says.
		a.showMain()
		a.openLoginTo(src, "")
		return
	}
	collect := !d.collected
	uid := a.accountProfile(src).UserID
	d.collected, d.asking = collect, true
	go func() {
		err := c.CollectAlbum(id, collect)
		a.post(func() { a.albumCollectedToggled(src, d, uid, collect, err) })
	}()
}

// albumCollectedToggled takes what the source said of the write: where it
// refused, the button goes back to what it was.
func (a *app) albumCollectedToggled(src string, d *albumData, uid int64, collect bool, err error) {
	d.asking = false
	if p := a.accountProfile(src); p == nil || p.UserID != uid {
		return
	}
	if err != nil {
		d.collected = !collect
		a.toast = "操作失败，请稍后再试"
		return
	}
	d.collectedFor = uid
	switch {
	case collect:
		a.toast = "已收藏专辑"
	default:
		a.toast = "已取消收藏专辑"
	}
	// The list the album is on is the source's, and this page changed it.
	if a.collected[src] != nil {
		a.loadCollected(src, true)
	}
}

func (a *app) albumHeader(c *ui.Context, src string, id int64, d *albumData) {
	al := d.album
	p := a.pal
	ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 26, 0).Children(func() {
		ui.Box(c).Size(212, 212).Radius(14).Shadow(0, 18, 40, -10, p.cardShade).Children(func() {
			a.coverFill(c, al.PicURL, 600, 14)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "专辑").FontSize(12).FontWeight(600).TextColor(p.accent)
			ui.Text(c, al.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
			ui.RichText(c).FontSize(13).TextColor(p.muted).Children(func() {
				for i, ar := range al.Artists {
					if i > 0 {
						ui.Text(c, ", ")
					}
					// The artist is the album's source's, as the album is the
					// page's.
					a.link(c.Key(i), ar.Name, p.muted, p.text, route{page: pageArtist, src: pageSrc(ar.Source), id: ar.ID})
				}
				meta := fmt.Sprintf(" · %d 首", len(al.Songs))
				if al.PublishTime > 0 {
					meta = " · " + time.UnixMilli(al.PublishTime).Format("2006-01-02") + meta
				}
				ui.Text(c, meta)
			})
			if d := strings.TrimSpace(al.Description); d != "" {
				a.collectionDescription(c, d)
			}
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				if pillButton(c.Key("play"), iconPlay, "播放", p.accent, p.onAccent).Clicked() {
					if a.pl.Shuffle() {
						a.pl.ToggleShuffle()
					}
					a.pl.PlayQueue(al.Songs, firstPlayable(al.Songs))
				}
				if a.downloadEnabled() && len(al.Songs) > 0 {
					a.trkDownloadButton(c, al.Songs)
				}
				// Collecting an album is a source's own list, and only a
				// source that keeps one has the button: NetEase's albums
				// are saved as playlists, elsewhere.
				if _, ok := a.collectedOf(src); ok {
					icon, label := iconPlus, "收藏专辑"
					if d.collected {
						icon, label = iconCheck, "已收藏"
					}
					if pillButton(c.Key("collect"), icon, label, p.hover.Over(p.bg), p.text).Clicked() {
						a.toggleCollectedAlbum(src, id, d)
					}
				}
			})
		})
	})
}

// albumGrid shows albums as cards, each opening its page.
func (a *app) albumGrid(c *ui.Context, albums []netease.AlbumProfile) {
	ui.Grid(c).Columns(a.columns(c, 156)).GapX(20).GapY(28).Children(func() {
		for i := range albums {
			a.albumCard(c.Key(albums[i].ID), &albums[i])
		}
	})
}

// albumCard is an album's cover, lifting under the pointer, over its name
// and the year it came out.
func (a *app) albumCard(c *ui.Context, al *netease.AlbumProfile) {
	p := a.pal
	card := ui.ButtonBase(c).Column().Gap(10).AlignItems(ui.Stretch).FocusRing(false).Label(al.Name)
	hovered := card.Hovered()
	card.Children(func() {
		art := ui.Box(c).AspectRatio(1).Radius(10)
		lift := ui.Pose{}
		if hovered {
			lift = ui.Pose{Y: -3}
			art.Shadow(0, 12, 28, -6, p.cardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.cardShade)
		}
		art.AnimatePose(lift, ui.Snappy)
		art.Children(func() { a.coverFill(c, al.PicURL, 400, 10) })
		ui.Text(c, al.Name).FontSize(13).FontWeight(500).MaxLines(2).LineHeight(1.35)
		var meta []string
		if a.route.page != pageArtist {
			meta = append(meta, al.ArtistNames())
		}
		if al.PublishTime > 0 {
			meta = append(meta, time.UnixMilli(al.PublishTime).Format("2006"))
		}
		ui.Text(c, strings.Join(meta, " · ")).FontSize(12).TextColor(p.muted).SingleLine().Margin(-6, 0, 0, 0)
	})
	if card.Clicked() {
		// The album's page is the one of the source it came from: a card
		// drawn on an artist's page opens the same source as that page.
		a.open(route{page: pageAlbum, src: pageSrc(a.route.src), id: al.ID})
	}
}
