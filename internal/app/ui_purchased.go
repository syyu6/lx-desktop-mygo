package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/account"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// The purchased page is what the listener bought from a source: the songs
// and the albums, as its account names them. It belongs to whichever
// signed-in source has a shop — NetEase sells nothing — and says which in
// its header.
//
// What it shows is the account's, so it is asked each time the page opens
// rather than kept: a purchase made on the listener's phone shows the next
// time the page does. Its two tabs load apart, each from where it left.

const (
	purchasedAlbums = iota
	purchasedSongs
	purchasedLimit = 30
)

// purchasedData is what one tab of the page has loaded.
type purchasedData struct {
	songs  []music.Track
	albums []music.AlbumProfile
	page   paged
}

func (a *app) loadPurchased(tab int, again bool) {
	src, ok := a.purchaseSource()
	if !ok {
		return
	}
	if a.purchased[tab] != nil && !again {
		return
	}
	d := &purchasedData{}
	a.purchased[tab] = d
	a.morePurchased(src, tab, d)
}

// morePurchased asks the source for the next page of what was bought. The
// two lists page apart: a song's offset is how many songs loaded, an
// album's how many albums.
func (a *app) morePurchased(src string, tab int, d *purchasedData) {
	if d.page.loading || !a.signedInTo(src) {
		return
	}
	ext, ok := a.extras(src)
	if !ok {
		return
	}
	offset := len(d.songs)
	if tab == purchasedAlbums {
		offset = len(d.albums)
	}
	uid := a.accountProfile(src).UserID
	d.page.loading, d.page.err = true, ""
	go func() {
		var songs []music.Track
		var albums []music.AlbumProfile
		var more bool
		var err error
		if tab == purchasedSongs {
			songs, more, err = ext.PurchasedSongs(purchasedLimit, offset)
		} else {
			albums, more, err = ext.PurchasedAlbums(purchasedLimit, offset)
		}
		a.post(func() { a.purchasedAnswered(src, tab, d, uid, songs, albums, more, err) })
	}()
}

// purchasedAnswered takes a page of what was bought, once the shop is done
// with it.
func (a *app) purchasedAnswered(src string, tab int, d *purchasedData, uid int64,
	songs []music.Track, albums []music.AlbumProfile, more bool, err error) {
	if a.purchased[tab] != d {
		return
	}
	// Signed out, or signed in as someone else, while the page was on its
	// way: what came back is not this listener's to show.
	if p := a.accountProfile(src); p == nil || p.UserID != uid {
		return
	}
	d.page.loading = false
	if err != nil {
		d.page.err = a.pageError(src, "已购音乐加载失败", err)
		return
	}
	d.songs = append(d.songs, songs...)
	d.albums = append(d.albums, albums...)
	d.page.loaded = true
	d.page.more = more && len(songs)+len(albums) > 0
}

// purchaseSource is the signed-in source whose shop the page shows: the one
// chosen while it has a shop and is signed in, else the first that has one.
func (a *app) purchaseSource() (string, bool) {
	if a.purchasePick != "" && a.signedInTo(a.purchasePick) {
		if _, ok := a.extras(a.purchasePick); ok {
			return a.purchasePick, true
		}
	}
	for _, src := range account.Services() {
		if !a.signedInTo(src) {
			continue
		}
		if _, ok := a.extras(src); ok {
			return src, true
		}
	}
	return "", false
}

// extras is a source's shop, for the page that shows what it sells.
func (a *app) extras(src string) (source.Extras, bool) {
	if a.reg == nil || src == "" {
		return nil, false
	}
	p, ok := a.reg.Get(src)
	if !ok {
		return nil, false
	}
	ext, ok := p.(source.Extras)
	return ext, ok
}

// anyShop names a source that has a shop, signed in or not: what the page
// asks the listener to sign in to when nobody is.
func (a *app) anyShop() (string, bool) {
	if a.reg == nil {
		return "", false
	}
	for _, src := range a.reg.SearchedSources() {
		if _, ok := a.extras(src); ok {
			return src, true
		}
	}
	return "", false
}

func (a *app) purchasedPage(c *ui.Context) {
	src, ok := a.purchaseSource()
	if !ok {
		a.purchasedSignedOut(c)
		return
	}
	if a.purchasePick != src {
		// The shop the page is of changed — a source signed in, or out: what
		// was loaded was another shop's.
		a.purchased, a.purchasePick = [2]*purchasedData{}, src
	}
	tab := purchasedAlbums
	if a.route.tab == purchasedSongs {
		tab = purchasedSongs
	}
	d := a.purchased[tab]
	if d == nil {
		a.loadPurchased(tab, false)
		d = a.purchased[tab]
	}
	if d == nil {
		d = &purchasedData{}
	}
	// The tab's list is the page's: asking again builds the header as a
	// row of it, so that switching them keeps each tab where it was.
	header := func(c *ui.Context) { a.purchasedHeader(c, src, d) }
	if tab == purchasedSongs {
		if !d.page.loaded && d.page.loading {
			a.purchasedSongsSkeleton(c, header)
			return
		}
		foot := func(c *ui.Context) {
			a.pageFoot(c, &d.page, func() { a.morePurchased(src, tab, d) })
			if d.page.loaded && len(d.songs) == 0 {
				a.empty(c, "暂无已购单曲")
			}
		}
		a.trackRows(c, d.songs, true, header, foot)
		return
	}
	a.scrollPage(c, func() {
		header(c)
		// An album of the shop is the source's: its page is asked by the
		// number its own answer gave it, as any album's is.
		a.albumGrid(c, d.albums)
		if !d.page.loaded && d.page.loading {
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 156), 156, false)
			return
		}
		a.pageFoot(c, &d.page, func() { a.morePurchased(src, tab, d) })
		if d.page.loaded && len(d.albums) == 0 {
			a.empty(c, "暂无已购专辑")
		}
	})
}

// purchasedSignedOut is the page for a listener whose account, or whose
// source's shop, is not there: it says what to do, and signs in to the
// source that has one if any is enabled.
func (a *app) purchasedSignedOut(c *ui.Context) {
	src, ok := a.anyShop()
	if !ok {
		a.centered(c, "没有可查看的已购音乐：开启一个有商店的内置源后才能登录", nil)
		return
	}
	top, bottom := a.insets(c)
	ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(12).Children(func() {
		ui.Text(c, "登录"+a.sourceShortName(src)+"后查看已购音乐").FontSize(13).TextColor(a.pal.muted)
		if ui.Button(c.Key("purchased-login"), "登录"+a.sourceShortName(src)).Clicked() {
			a.showMain()
			a.openLoginTo(src, "")
		}
	})
}

// purchasedSongsSkeleton is the songs' tab while its first page is on its
// way: the header, and rows of bones under it.
func (a *app) purchasedSongsSkeleton(c *ui.Context, header func(c *ui.Context)) {
	a.trackSkeleton(c, a.pageShimmer(c), true, header)
}

func (a *app) purchasedHeader(c *ui.Context, src string, d *purchasedData) {
	p := a.pal
	a.heading(c, "已购音乐", a.sourceShortName(src))
	tab := purchasedAlbums
	if a.route.tab == purchasedSongs {
		tab = purchasedSongs
	}
	ui.Row(c).AlignItems(ui.Center).Gap(12).Margin(0, 0, 18, 0).Children(func() {
		if segmented(c.Key("purchased-kind"), &tab, "专辑", "单曲") {
			r := a.route
			r.tab = tab
			a.show(r)
			// The listing selected is the new tab's, not the one drawn.
			return
		}
		ui.Box(c).Grow(1)
		// The albums are to open; the songs are to play, as any list of
		// songs is.
		if tab == purchasedSongs && len(d.songs) > 0 {
			if pillButton(c.Key("play-purchased"), iconPlay, "播放", p.accent, p.onAccent).Clicked() {
				if a.pl.Shuffle() {
					a.pl.ToggleShuffle()
				}
				a.pl.PlayQueue(d.songs, firstPlayable(d.songs))
			}
		}
		iconButton(c.Key("refresh-purchased"), iconReload, 17, p.muted, "刷新已购音乐").
			Disabled(d.page.loading).
			OnClick(func() { a.loadPurchased(tab, true) })
	})
	// Whose purchases these are: an account's are the account's, and the
	// header names it while the page is open.
	if prof := a.accountProfile(src); prof != nil {
		if note := purchasedNote(prof.Nickname, prof.VIP); note != "" {
			ui.Text(c, note).FontSize(12.5).TextColor(p.muted).Margin(-8, 0, 18, 0)
		}
	}
}

// purchasedNote is who the purchases are of, and what their membership is
// in the source's own words: what the header says under the page.
func purchasedNote(nickname, vip string) string {
	var parts []string
	if nickname != "" {
		parts = append(parts, nickname)
	}
	if vip != "" {
		parts = append(parts, vip)
	}
	return strings.Join(parts, " · ")
}
