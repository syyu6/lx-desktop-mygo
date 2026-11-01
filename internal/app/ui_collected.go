package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// The liked playlist of a source is the songs the listener likes there, and
// on a source that keeps albums too, the albums they collected are its
// second tab: QQ Music collects both, and shows them as a list's songs and
// its albums.

// collectedData is what a source's collected albums have loaded.
type collectedData struct {
	albums []music.AlbumProfile
	page   paged
}

// collectedOf is a source's album collection, when it has one.
func (a *app) collectedOf(src string) (source.Collection, bool) {
	if a.reg == nil || src == "" {
		return nil, false
	}
	p, ok := a.reg.Get(src)
	if !ok {
		return nil, false
	}
	c, ok := p.(source.Collection)
	return c, ok
}

// loadCollected loads the albums the listener collected on a source, once
// its account is signed in and the page shows them. It asks again when
// asked to, as an album collected on the listener's phone shows then.
func (a *app) loadCollected(src string, again bool) {
	if a.collected == nil {
		a.collected = map[string]*collectedData{}
	}
	d := a.collected[src]
	if d != nil && !again {
		return
	}
	d = &collectedData{}
	a.collected[src] = d
	a.moreCollected(src, d)
}

// moreCollected asks a source for the next page of the albums collected.
func (a *app) moreCollected(src string, d *collectedData) {
	if d.page.loading || !a.signedInTo(src) {
		return
	}
	c, ok := a.collectedOf(src)
	if !ok {
		return
	}
	uid, offset := a.accountProfile(src).UserID, len(d.albums)
	d.page.loading, d.page.err = true, ""
	go func() {
		albums, more, err := c.CollectedAlbums(30, offset)
		a.post(func() { a.collectedAnswered(src, d, uid, albums, more, err) })
	}()
}

// collectedAnswered takes a page of collected albums, once the source is
// done with it.
func (a *app) collectedAnswered(src string, d *collectedData, uid int64,
	albums []music.AlbumProfile, more bool, err error) {
	if a.collected[src] != d {
		return
	}
	// Signed out, or signed in as someone else, while the page was on its
	// way: what came back is not this listener's to show.
	if p := a.accountProfile(src); p == nil || p.UserID != uid {
		return
	}
	d.page.loading = false
	if err != nil {
		d.page.err = a.pageError(src, "收藏专辑加载失败", err)
		return
	}
	d.albums = append(d.albums, albums...)
	d.page.loaded = true
	d.page.more = more && len(albums) > 0
}

// collectedPage is the albums tab of a liked playlist's page: the albums
// the source's account collected, under the page's header.
func (a *app) collectedPage(c *ui.Context, src string, header func(c *ui.Context)) {
	d := a.collected[src]
	if d == nil {
		a.loadCollected(src, false)
		d = a.collected[src]
	}
	if d == nil {
		d = &collectedData{}
	}
	a.scrollPage(c, func() {
		header(c)
		a.albumGrid(c, d.albums)
		if !d.page.loaded && d.page.loading {
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 156), 156, false)
			return
		}
		a.pageFoot(c, &d.page, func() { a.moreCollected(src, d) })
		if d.page.loaded && len(d.albums) == 0 {
			a.empty(c, "暂无收藏专辑")
		}
	})
}
