package app

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/subscription"
)

// The playlists the listener follows from another service, as lx-music-
// desktop's 收藏歌单: a playlist of a source that is not NetEase's is
// copied into the app, songs and all, and fetched again as the app opens
// (when the listener keeps it so), or when they ask.
//
// The copy is what shows first, so a subscription opens at once, even with
// its service unreachable or turned off in the settings: what it had then
// plays if the source still serves it.

// subscriptionsPath is where the followed playlists are kept, in the user
// data.
func subscriptionsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "subscriptions.json"), nil
}

// loadFollowed opens the playlists the listener follows, and puts their
// songs where the pages find them.
func (a *app) loadFollowed() {
	path, err := subscriptionsPath()
	if err != nil {
		a.toast = "订阅的歌单未能载入"
		return
	}
	a.openSubscriptions(path)
}

// openSubscriptions reads the followed playlists kept at path, and puts
// their songs where the pages find them.
func (a *app) openSubscriptions(path string) {
	subs, err := subscription.NewStore(path)
	if err != nil {
		a.toast = "订阅的歌单未能载入"
		return
	}
	a.subs = subs
	a.seedSubscriptions()
}

// seedSubscriptions gives the pages the songs the subscriptions had when
// they were last fetched, unless the pages already have them fresher.
func (a *app) seedSubscriptions() {
	if a.subs == nil || a.playlists == nil {
		return
	}
	for _, sub := range a.subs.List() {
		id, ok := subID(sub)
		if !ok {
			continue
		}
		key := playlistKey(sub.Source, id)
		if a.playlists[key] == nil {
			a.playlists[key] = subPlaylist(sub, id)
		}
	}
}

// autoSyncSubscriptions fetches the followed playlists again as the app
// opens, the ones autoSyncAsk names.
func (a *app) autoSyncSubscriptions() {
	for _, sub := range a.autoSyncAsk() {
		a.syncSubscription(sub)
	}
}

// autoSyncAsk is what the app fetches again as it opens: the subscriptions
// the listener keeps up to date, of the sources they still ask. A source
// turned off is not asked behind their back.
func (a *app) autoSyncAsk() []*subscription.Subscription {
	if a.subs == nil {
		return nil
	}
	var out []*subscription.Subscription
	for _, sub := range a.subs.List() {
		if sub.AutoUpdate && a.sourceOn(sub.Source) {
			out = append(out, sub)
		}
	}
	return out
}

// subID is a subscription's id as a route carries it. The services the app
// knows number their playlists, so the id is a number: one named otherwise
// has no page to show.
func subID(sub *subscription.Subscription) (int64, bool) {
	n, err := strconv.ParseInt(sub.ID, 10, 64)
	return n, err == nil
}

// subPlaylist is a subscription as a playlist the pages show: its songs as
// last fetched.
func subPlaylist(sub *subscription.Subscription, id int64) *netease.Playlist {
	pl := &netease.Playlist{
		Source:      sub.Source,
		ID:          id,
		Name:        sub.Name,
		CoverURL:    sub.Cover,
		Description: sub.Desc,
		TrackCount:  sub.Count,
		Tracks:      sub.Tracks,
	}
	pl.Creator.Nickname = sub.Author
	return pl
}

// subscribedLists is the followed playlists, as the sidebar lists them.
func (a *app) subscribedLists() []netease.Playlist {
	if a.subs == nil {
		return nil
	}
	var out []netease.Playlist
	for _, sub := range a.subs.List() {
		if id, ok := subID(sub); ok {
			out = append(out, *subPlaylist(sub, id))
		}
	}
	return out
}

// followed returns the subscription a playlist is, or nil: NetEase's
// playlists are saved natively, and are not followed here.
func (a *app) followed(pl *netease.Playlist) *subscription.Subscription {
	if a.subs == nil || pl.Src() == music.SourceNetease {
		return nil
	}
	return a.subs.Get(pl.Src() + "/" + pl.KeyOf())
}

// syncSubscription fetches a subscription's songs again, and keeps them.
func (a *app) syncSubscription(sub *subscription.Subscription) {
	id, ok := subID(sub)
	if !ok || a.subs == nil {
		return
	}
	key := sub.Key()
	if a.subSyncing == nil {
		a.subSyncing = map[string]bool{}
	}
	if a.subSyncing[key] {
		return
	}
	a.subSyncing[key] = true
	src := sub.Source
	go func() {
		pl, err := a.fetchPlaylist(src, id)
		a.post(func() { a.subscriptionArrived(sub, id, pl, err) })
	}()
}

// subscriptionArrived keeps a subscription's songs as they are now, and
// shows them where it did before. Failing, what it had stays; the listener
// hears of it only if there was nothing to keep.
func (a *app) subscriptionArrived(sub *subscription.Subscription, id int64, pl *netease.Playlist, err error) {
	if a.subSyncing != nil {
		delete(a.subSyncing, sub.Key())
	}
	if err != nil {
		if len(sub.Tracks) == 0 {
			a.toast = "「" + sub.Name + "」同步失败"
			a.redraw()
		}
		return
	}
	if a.subs != nil {
		a.subs.Update(sub.Key(), pl)
	}
	// Where the page reads it: open, it shows the songs that just came.
	a.playlists[playlistKey(sub.Source, id)] = subPlaylist(sub, id)
	a.redraw()
}

// subscribe follows a playlist, keeping its songs as just fetched.
func (a *app) subscribe(pl *netease.Playlist) {
	if a.subs == nil {
		a.toast = "订阅的歌单未能保存"
		return
	}
	sub, err := a.subs.Add(pl)
	if err != nil {
		a.toast = "订阅失败：" + err.Error()
		return
	}
	// Its page shows the songs just fetched, without asking again.
	if id, ok := subID(sub); ok {
		a.playlists[playlistKey(sub.Source, id)] = subPlaylist(sub, id)
	}
	a.toast = "已订阅「" + sub.Name + "」"
	a.redraw()
}

// unsubscribe forgets a followed playlist. Its page stays open, loading the
// playlist from its source again.
func (a *app) unsubscribe(sub *subscription.Subscription) {
	if a.subs == nil {
		return
	}
	if err := a.subs.Remove(sub.Key()); err != nil {
		a.toast = "取消订阅失败：" + err.Error()
		return
	}
	if id, ok := subID(sub); ok {
		key := playlistKey(sub.Source, id)
		delete(a.playlists, key)
		if a.route.page == pagePlaylist && playlistKey(a.route.src, a.route.id) == key {
			a.loadPlaylist(a.route.src, a.route.id, false)
		}
	}
	a.toast = "已取消订阅「" + sub.Name + "」"
	a.redraw()
}

// setSubAuto says whether a subscription is fetched again as the app opens.
func (a *app) setSubAuto(sub *subscription.Subscription, on bool) {
	if a.subs == nil {
		return
	}
	if err := a.subs.SetAutoUpdate(sub.Key(), on); err != nil {
		a.toast = "订阅设置失败：" + err.Error()
		return
	}
	a.redraw()
}

// subscribeDialog asks for a playlist to follow: a link to it on its
// service, or its id there, and which service that is where the link does
// not say.
type subscribeDialog struct {
	open bool
	text string
	src  string // the source chosen; "" for the one the link names
	busy bool
	err  string
}

// openSubscribeDialog asks for a playlist to follow.
func (a *app) openSubscribeDialog() {
	a.subscribeDlg = subscribeDialog{open: true}
}

// subscribeSources are what the dialog offers to choose from: the link's
// own service, and the sources the listener asks.
func (a *app) subscribeSources() []string {
	srcs := []string{""}
	if a.reg != nil {
		// A playlist is read from a catalogue: its 单曲 switch says
		// nothing about whether its lists can be followed.
		srcs = append(srcs, a.reg.SearchedSources()...)
	}
	return srcs
}

// platformChips chooses one of srcs, ids of sources with "" for the link's
// own, in a row that wraps: the one chosen is filled with the accent, as
// the square's categories are, and the others wait in the hover's gray.
//
// They wrap rather than sit in one fixed-width row as a segmented control
// does: the sources are as many as the listener asks, and their names do
// not fit six across a dialog. Each is named shortly, as the search bar's
// badges name them, so that the row stays a row or two of chips rather than
// a wall of service names.
func (a *app) platformChips(c *ui.Context, key any, srcs []string, chosen *string, choose func(src string)) {
	p := a.pal
	ui.Row(c.Key(key)).Wrap().Gap(8).GapY(8).Children(func() {
		for _, id := range srcs {
			label := "按链接识别"
			if id != "" {
				label = shortSource(a.reg.Name(id))
			}
			on := id == *chosen
			// Each chip a key of its own: a dialog built twice, as it is
			// redrawn, must find the same ones.
			b := ui.ButtonBase(c.Key(struct {
				key any
				src string
			}{key, id})).Height(28).PaddingX(12).Radius(14).FocusRing(false).Label(label)
			color := p.muted
			switch {
			case on:
				b.Background(p.accent)
				color = p.onAccent
			case b.Hovered():
				b.Background(p.hover)
				color = p.text
			}
			b.Children(func() {
				ui.Text(c, label).FontSize(12.5).FontWeight(500).TextColor(color).SingleLine()
			})
			if b.Clicked() && !on {
				choose(id)
			}
		}
	})
}

// subscribeDialogView is the dialog that follows a playlist by its link.
func (a *app) subscribeDialogView(c *ui.Context) {
	p := a.pal
	d := &a.subscribeDlg
	ui.DialogBase(c.Key("subscribe"), &d.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(430).Padding(24).Radius(18).Gap(12).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Text(c, "订阅歌单").FontSize(17).FontWeight(700)
		ui.Text(c, "粘贴歌单的链接或编号，歌单会保存在本机，之后打开不必再等它加载。").
			FontSize(12.5).TextColor(p.muted).LineHeight(1.45)
		srcs := a.subscribeSources()
		if len(srcs) > 1 {
			// The platforms wrap rather than run in one row: there are as
			// many as there are sources the listener asks, and their names
			// are longer than a segmented control of fixed segments leaves
			// room for — six of them came to half again the dialog's width
			// and drew out past its edge.
			a.platformChips(c, c.Key("subscribe-src"), srcs, &d.src, func(src string) {
				d.src, d.err = src, ""
			})
		}
		in := ui.TextInput(c.Key("subscribe-link"), &d.text).Placeholder("https://… 或歌单编号").Label("歌单链接").FontSize(13).AutoFocus()
		if in.Submitted() {
			a.submitSubscribe()
		}
		if d.err != "" {
			ui.Text(c, d.err).FontSize(12.5).TextColor(p.accent).LineHeight(1.4)
		}
		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			if pillButton(c.Key("cancel"), nil, "取消", p.hover.Over(p.bg), p.text).Clicked() {
				d.open = false
			}
			ok := pillButton(c.Key("ok"), nil, "订阅", p.accent, p.onAccent)
			if d.busy || strings.TrimSpace(d.text) == "" {
				ok.Disabled(true).Opacity(0.5)
			}
			if ok.Clicked() {
				a.submitSubscribe()
			}
		})
	})
}

// submitSubscribe reads the playlist the dialog names, fetches it, and
// follows it.
func (a *app) submitSubscribe() {
	d := &a.subscribeDlg
	text := strings.TrimSpace(d.text)
	if text == "" || d.busy || a.subs == nil {
		return
	}
	var enabled []string
	if a.reg != nil {
		enabled = a.reg.SearchedSources()
	}
	src := d.src
	if src == "" {
		if s, ok := subscription.SourceOf(text); ok {
			src = s
		} else if len(enabled) == 1 && !strings.Contains(text, "/") {
			// An id alone names a service only where the listener asks but
			// one. A link names its own, and one of no service known is not
			// another's.
			src = enabled[0]
		}
	}
	if src == "" {
		d.err = "无法识别歌单来自哪个来源，请在上面选择"
		return
	}
	if src == music.SourceNetease {
		// NetEase's own playlists are the app's already, saved with the
		// account: following one here would only keep a stale copy.
		d.err = "网易云音乐的歌单登录后会出现在「收藏的歌单」里，无需订阅"
		return
	}
	idText, ok := subscription.ParseLink(src, text)
	if !ok {
		d.err = "无法从这个链接找到歌单，请检查后再试"
		return
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		d.err = "这个来源的歌单编号不是数字"
		return
	}
	d.busy, d.err = true, ""
	go func() {
		pl, err := a.fetchPlaylist(src, id)
		a.post(func() { a.subscribeArrived(d, pl, err) })
	}()
}

// subscribeArrived follows the playlist the dialog fetched, and shows it.
func (a *app) subscribeArrived(d *subscribeDialog, pl *netease.Playlist, err error) {
	d.busy = false
	if err != nil {
		d.err = "歌单加载失败，请检查链接或来源"
		return
	}
	sub, err := a.subs.Add(pl)
	if err != nil {
		d.err = "订阅失败：" + err.Error()
		return
	}
	d.open, d.text = false, ""
	id := subIDOf(sub)
	a.playlists[playlistKey(sub.Source, id)] = subPlaylist(sub, id)
	a.toast = "已订阅「" + sub.Name + "」"
	a.open(playlistRoute(sub.Source, id))
}

// subIDOf is a subscription's id as a number, for a page it opens: 0
// where it is named otherwise, which the app has no page for.
func subIDOf(sub *subscription.Subscription) int64 {
	id, _ := subID(sub)
	return id
}

// subscribeControls is the button of a playlist's page that follows it, or
// shows it followed: refresh it, keep it up to date, or stop following it.
// NetEase's own playlists are saved in the app when signed in, so they have
// none.
func (a *app) subscribeControls(c *ui.Context, pl *netease.Playlist) {
	if pl.Src() == music.SourceNetease {
		return
	}
	p := a.pal
	sub := a.followed(pl)
	if sub == nil {
		if pillButton(c.Key("subscribe"), iconPlus, "订阅", p.hover.Over(p.bg), p.text).Clicked() {
			a.subscribe(pl)
		}
		return
	}
	b := pillButton(c.Key("subscribe"), iconCheck, "已订阅", p.hover.Over(p.bg), p.text)
	b.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item("刷新歌单").Chosen() {
			a.syncSubscription(sub)
		}
		if m.Item("自动更新").Checked(sub.AutoUpdate).Chosen() {
			a.setSubAuto(sub, !sub.AutoUpdate)
		}
		m.Separator()
		if m.Item("取消订阅").Chosen() {
			a.unsubscribe(sub)
		}
	})
}
