package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// userDetail and userPlaylists load a user and their playlists from
// NetEase, and followedArtists and subscribedPodcasts what the listener
// follows; tests replace them.
var (
	userDetail         = netease.UserDetail
	userPlaylists      = netease.UserPlaylists
	followedArtists    = netease.FollowedArtists
	subscribedPodcasts = netease.SubscribedPodcasts
)

// userData is what the app has of a user: who they are, and the playlists
// they made that anyone may see. Of the listener's own page, it has the
// artists they follow and the podcasts they subscribe to too, which
// NetEase shows no one else.
type userData struct {
	user      *netease.User
	playlists []netease.Playlist
	loading   int // how many of the two are on their way
	err       string

	artists       []netease.ArtistProfile
	podcasts      []netease.Podcast
	artistsShown  bool // whether artists has loaded once
	podcastsShown bool
	subsLoading   int

	// membership is what the source says of the listener's account, which
	// only their own page shows: an account's is not another listener's.
	membership    music.Membership
	memberUID     int64
	memberLoading bool
	memberErr     bool
}

func (a *app) loadUser(src string, id int64) {
	key := sourceKey(src, id)
	d := a.users[key]
	if d == nil {
		d = &userData{}
		a.users[key] = d
	}
	// What the listener follows and subscribes to is NetEase's own page of
	// them, which the other sources do not have.
	netease := src == "" || src == music.SourceNetease
	if netease && a.profile != nil && a.profile.UserID == id {
		a.loadSubscriptions(d)
	}
	if a.signedInTo(src) && a.accountProfile(src).UserID == id {
		a.loadMembership(src, d)
	}
	if d.user != nil || d.loading > 0 {
		return
	}
	d.loading, d.err = 2, ""
	fetch := userDetail
	lists := userPlaylists
	var pages source.UserPages
	if !netease {
		pages = a.userPages(src)
	}
	go func() {
		var u *music.User
		var err error
		if pages != nil {
			u, err = pages.User(id)
		} else {
			u, err = fetch(id)
		}
		a.post(func() {
			d.loading--
			if err != nil {
				d.err = a.pageError(src, "用户加载失败", err)
				return
			}
			d.user = u
		})
	}()
	go func() {
		var made []music.Playlist
		var err error
		if pages != nil {
			made, _, err = pages.UserPlaylists(id)
		} else {
			made, _, err = lists(id)
		}
		a.post(func() {
			d.loading--
			if err == nil {
				d.playlists = publicPlaylists(made, id)
			}
		})
	}()
}

// userPages is a source's listener pages, or nil for NetEase's, which are
// the app's own calls.
func (a *app) userPages(src string) source.UserPages {
	if src == "" || src == music.SourceNetease || a.reg == nil {
		return nil
	}
	p, ok := a.reg.Get(src)
	if !ok {
		return nil
	}
	up, _ := p.(source.UserPages)
	return up
}

// loadSubscriptions loads the artists the listener follows and the
// podcasts they subscribe to, each time their page shows: they follow and
// subscribe on other pages, and these keep up. What they had shows while
// the lists are on their way.
func (a *app) loadSubscriptions(d *userData) {
	if d.subsLoading > 0 {
		return
	}
	d.subsLoading = 2
	go func() {
		ars, err := followedArtists()
		a.post(func() {
			d.subsLoading--
			if err == nil {
				d.artists, d.artistsShown = ars, true
			}
		})
	}()
	go func() {
		pods, err := subscribedPodcasts()
		a.post(func() {
			d.subsLoading--
			if err == nil {
				d.podcasts, d.podcastsShown = pods, true
			}
		})
	}()
}

// publicPlaylists are the playlists of lists a user made that anyone may
// see: their own, as made lists those they saved too, but not their
// private ones.
func publicPlaylists(lists []netease.Playlist, uid int64) []netease.Playlist {
	var out []netease.Playlist
	for _, pl := range lists {
		if pl.Creator.UserID == uid && pl.Privacy != 10 {
			out = append(out, pl)
		}
	}
	return out
}

// loadMembership asks a source what the listener's account is, and asks
// once per sign-in: the account does not change while it is signed in.
func (a *app) loadMembership(src string, d *userData) {
	prof := a.accountProfile(src)
	ext, ok := a.extras(src)
	if prof == nil || !ok || d.memberLoading {
		return
	}
	if d.memberUID == prof.UserID && !d.memberErr {
		return
	}
	uid := prof.UserID
	d.memberLoading, d.memberErr, d.memberUID = true, false, uid
	go func() {
		level, err := ext.Membership()
		a.post(func() {
			if p := a.accountProfile(src); p == nil || p.UserID != uid {
				return
			}
			d.memberLoading, d.memberErr = false, err != nil
			if err == nil {
				d.membership = level
			}
		})
	}()
}

// membershipBadge is what the listener's account is, as their source says:
// an account that can hear more than a free one can, and the word for it.
// NetEase says nothing of it, and its own page shows none.
func (a *app) membershipBadge(c *ui.Context, d *userData) {
	p := a.pal
	label, ink, bg := "会员状态未知", p.muted, p.hover
	switch d.membership {
	case music.MembershipSuper:
		label, ink, bg = "超级会员", p.onAccent, p.accent
	case music.MembershipVIP:
		label, ink, bg = "会员", p.onAccent, p.accent
	case music.MembershipFree:
		label, ink, bg = "免费账号", p.muted, p.hover
	}
	switch {
	case d.memberErr:
		label, ink, bg = "会员状态暂不可用", p.muted, p.hover
	case d.memberLoading && d.membership == music.MembershipUnknown:
		label = "正在查询会员状态"
	}
	ui.Box(c).Padding(3, 8).Radius(4).Background(bg).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(500).TextColor(ink).SingleLine()
	})
}

func (a *app) userPage(c *ui.Context) {
	p := a.pal
	src, id := a.route.src, a.route.id
	d := a.users[sourceKey(src, id)]
	if d == nil || d.user == nil {
		if d == nil || d.loading == 0 {
			err := ""
			if d != nil {
				err = d.err
			}
			a.centered(c, err, func() { a.loadUser(src, id) })
			return
		}
		s := a.pageShimmer(c)
		a.scrollPage(c, func() {
			a.playlistHeaderSkeleton(c, s)
			a.playlistCardsSkeleton(c, s, a.columns(c, 168), 168, false)
		})
		return
	}
	u := d.user
	// Own is the listener's page in the source it belongs to: what they
	// follow and subscribe to is NetEase's alone, so only its page shows
	// those.
	own := a.signedInTo(src) && a.accountProfile(src).UserID == id
	neteaseOwn := own && (src == "" || src == music.SourceNetease)
	a.scrollPage(c, func() {
		ui.Row(c).Gap(28).AlignItems(ui.End).Children(func() {
			ui.Box(c).Size(artistPhotoSize, artistPhotoSize).Radius(artistPhotoSize/2).Shadow(0, 18, 40, -10, p.cardShade).Children(func() {
				a.coverFill(c, u.AvatarURL, 600, artistPhotoSize/2)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				ui.Text(c, "用户").FontSize(12).FontWeight(600).TextColor(p.accent)
				ui.Text(c, u.Nickname).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, fmt.Sprintf("%s 关注 · %s 粉丝", count(u.Follows), count(u.Followeds))).FontSize(13).TextColor(p.muted)
					// What the account is, where the source says: the badge
					// belongs to the listener's own page, as the account does.
					if own {
						if _, ok := a.extras(src); ok {
							a.membershipBadge(c, d)
						}
					}
				})
				if u.Signature != "" {
					ui.Text(c, u.Signature).FontSize(12.5).TextColor(p.muted).MaxLines(2).LineHeight(1.45).Margin(2, 0, 0, 0)
				}
			})
		})
		a.section(c, "创建的歌单")
		switch {
		case d.loading > 0:
			a.playlistCardsSkeleton(c, a.pageShimmer(c), a.columns(c, 168), 168, false)
		case len(d.playlists) == 0:
			a.empty(c, "暂无公开歌单")
		default:
			ui.Grid(c).Columns(a.columns(c, 168)).GapX(20).GapY(28).Children(func() {
				for i := range d.playlists {
					a.playlistCard(c.Key(d.playlists[i].ID), &d.playlists[i])
				}
			})
		}
		if neteaseOwn && d.podcastsShown {
			a.section(c, "订阅的播客")
			if len(d.podcasts) == 0 {
				a.empty(c, "还没有订阅播客")
			}
			ui.Grid(c).Columns(a.columns(c, 340)).GapX(8).GapY(4).Margin(0, -12).Children(func() {
				for i := range d.podcasts {
					a.podcastRow(c.Key(d.podcasts[i].ID), &d.podcasts[i])
				}
			})
		}
		if neteaseOwn && d.artistsShown {
			a.section(c, "关注的歌手")
			if len(d.artists) == 0 {
				a.empty(c, "还没有关注歌手")
			}
			ui.Grid(c).Columns(a.columns(c, 132)).GapX(20).GapY(28).Children(func() {
				for i := range d.artists {
					a.artistCard(c.Key(d.artists[i].ID), &d.artists[i])
				}
			})
		}
	})
}

// userRow is a user in a list, opening their page.
func (a *app) userRow(c *ui.Context, u *netease.User) {
	p := a.pal
	row := ui.ButtonBase(c).Padding(10, 12).Gap(14).Radius(10).AlignItems(ui.Center).FocusRing(false).Label(u.Nickname)
	if row.Hovered() {
		row.Background(p.hover)
	}
	row.Children(func() {
		a.cover(c, u.AvatarURL, 120, 48, 24)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, u.Nickname).FontSize(14).FontWeight(600).SingleLine()
			if u.Signature != "" {
				ui.Text(c, u.Signature).FontSize(12.5).TextColor(p.muted).SingleLine()
			}
		})
	})
	if row.Clicked() {
		a.open(route{page: pageUser, id: u.UserID})
	}
}
