package app

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// likes are the songs and episodes the listener likes, which hearts show
// filled. A song is liked in the source it is from: NetEase keeps the list
// it was added to, and QQ Music its own 我喜欢.

// These like or unlike a song with NetEase, and ask which episodes the
// listener likes; tests replace them. A song of a source that streams is
// liked through that source's Library.
var (
	likeSong      = netease.Like
	likeProgram   = netease.LikeProgram
	programsLiked = netease.ProgramsLiked
)

// liker is a source that keeps the songs its listener likes, with the
// account that source is signed in to.
func (a *app) liker(src string) (source.Library, bool) {
	if src == "" || src == music.SourceNetease {
		// NetEase's list is what likeSong writes, and loadLiked reads.
		return nil, false
	}
	if a.reg == nil {
		return nil, false
	}
	p, ok := a.reg.Get(src)
	if !ok {
		return nil, false
	}
	l, ok := p.(source.Library)
	return l, ok
}

// loadLiked loads the liked songs of every source the listener is signed
// in to, each into its own keys: a song's Ident names its source, so two
// sources numbering a song alike are two songs.
func (a *app) loadLiked(uid int64) {
	go func() {
		ids, err := netease.LikedIDs(uid)
		a.post(func() {
			if err != nil || a.profile == nil || a.profile.UserID != uid {
				return
			}
			for k := range a.liked {
				if strings.HasPrefix(k, music.SourceNetease+"/") {
					delete(a.liked, k)
				}
			}
			for _, id := range ids {
				a.liked[music.SourceNetease+"/"+strconv.FormatInt(id, 10)] = true
			}
		})
	}()
}

// loadLikedOf loads what the listener likes on another source, once its
// account is signed in. Its songs are named by their mid there, which the
// source gives by number; the list is told by numbers otherwise.
func (a *app) loadLikedOf(src string, uid int64) {
	l, ok := a.liker(src)
	if !ok {
		return
	}
	go func() {
		ids, err := l.LikedIDs(uid)
		a.post(func() {
			if err != nil {
				return
			}
			// The source's songs are named by mid, and LikedIDs answers
			// with numbers: a heart of a song not seen since is filled in
			// when the song is drawn, from the numbers kept here.
			for k := range a.liked {
				if strings.HasPrefix(k, src+"/") {
					delete(a.liked, k)
				}
			}
			for _, id := range ids {
				a.liked[src+"/"+strconv.FormatInt(id, 10)] = true
			}
		})
	}()
}

// toggleLike likes a song or an episode, or unlikes it, at once in the app
// and then with its source, which may refuse: the heart goes back then.
// Signed out, it asks the listener to sign in.
func (a *app) toggleLike(tr *netease.Track) {
	if tr.ProgramID != 0 {
		if a.profile == nil {
			a.showMain()
			a.openLogin()
			return
		}
		a.toggleProgramLike(tr.ProgramID)
		return
	}
	src := tr.Src()
	l, other := a.liker(src)
	if src == music.SourceNetease {
		if !a.signedInTo(src) {
			a.showMain()
			a.openLogin()
			return
		}
	} else if !other {
		// A source that keeps no such list of its own: the heart is the
		// app's alone, and says why it cannot go anywhere.
		a.toast = a.sourceShortName(src) + "没有可同步的喜欢列表"
		return
	} else if !a.signedInTo(src) {
		// The source's own code, from the dialog that names it: it is
		// signed in where its row in the settings says.
		a.showMain()
		a.openLoginTo(src, "")
		return
	}
	if a.liked == nil {
		a.liked = map[string]bool{}
	}
	key, like := tr.Ident(), !a.liked[tr.Ident()]
	a.setLiked(key, like)
	go func() {
		var err error
		if src == music.SourceNetease {
			err = likeSong(tr.ID, like)
		} else {
			err = l.Like(tr.ID, like)
		}
		a.post(func() {
			if err != nil {
				a.setLiked(key, !like)
				a.toast = "操作失败，请稍后再试"
				return
			}
			// The liked songs' playlist changed: load it anew when shown.
			for _, pl := range a.mine {
				if pl.SpecialType == 5 {
					delete(a.playlists, playlistKey("", pl.ID))
					if a.route.page == pagePlaylist && a.route.id == pl.ID {
						a.loadPlaylist("", pl.ID, false)
					}
				}
			}
		})
	}()
}

func (a *app) toggleProgramLike(id int64) {
	like := !a.likedPrograms[id]
	a.setProgramLiked(id, like)
	go func() {
		err := likeProgram(id, like)
		a.post(func() {
			if err != nil {
				a.setProgramLiked(id, !like)
				a.toast = "操作失败，请稍后再试"
			}
		})
	}()
}

// isLiked reports whether the listener likes tr, a song or an episode.
func (a *app) isLiked(tr *netease.Track) bool {
	if tr.ProgramID != 0 {
		return a.likedPrograms[tr.ProgramID]
	}
	return a.isLikedKey(tr)
}

// isLikedKey is isLiked for a song. Both of the app's sources list the
// songs a listener likes by number, not by the key a song has there, so a
// song is known either by its Ident — once a like or an unlike named it —
// or by the number its source listed.
func (a *app) isLikedKey(tr *netease.Track) bool {
	return a.liked[tr.Ident()] || a.liked[tr.Src()+"/"+strconv.FormatInt(tr.ID, 10)]
}

// likeCurrent likes the song or episode playing, or unlikes it, from the
// keyboard (⌘D), which shows no heart: a toast says which it did.
func (a *app) likeCurrent() {
	tr := a.pl.Current()
	if tr == nil {
		return
	}
	a.toggleLike(tr)
	if tr.ProgramID != 0 {
		if !a.isLiked(tr) {
			a.toast = "已取消喜欢"
		} else {
			a.toast = "已喜欢这期节目"
		}
		return
	}
	if !a.signedInTo(tr.Src()) {
		return // asked to sign in, or said so
	}
	if a.isLiked(tr) {
		if tr.Src() == music.SourceNetease {
			a.toast = "已添加到我喜欢的音乐"
		} else {
			a.toast = "已添加到我的喜欢"
		}
	} else {
		a.toast = "已取消喜欢"
	}
}

func (a *app) setLiked(key string, like bool) {
	if a.liked == nil {
		a.liked = map[string]bool{}
	}
	if like {
		a.liked[key] = true
	} else {
		delete(a.liked, key)
	}
}

// setProgramLiked says the listener likes an episode, or doesn't, over
// what NetEase may still answer of it.
func (a *app) setProgramLiked(id int64, like bool) {
	if a.likedPrograms == nil {
		a.likedPrograms = map[int64]bool{}
	}
	a.likedPrograms[id] = like
	delete(a.askedPrograms, id)
}

// askProgramLikes asks NetEase whether the listener likes the episodes
// ids, those not asked about yet: NetEase lists no episodes they like.
// An episode it doesn't answer of shows not liked, and isn't asked again.
func (a *app) askProgramLikes(ids []int64) {
	if a.profile == nil {
		return
	}
	var ask []int64
	for _, id := range ids {
		if _, known := a.likedPrograms[id]; !known && !a.askedPrograms[id] {
			ask = append(ask, id)
		}
	}
	if len(ask) == 0 {
		return
	}
	if a.askedPrograms == nil {
		a.askedPrograms = map[int64]bool{}
	}
	for _, id := range ask {
		a.askedPrograms[id] = true
	}
	uid := a.profile.UserID
	go func() {
		liked, _ := programsLiked(ask)
		a.post(func() { a.programLikesAnswered(uid, ask, liked) })
	}()
}

// programLikesAnswered takes NetEase's answer of whether uid likes the
// episodes asked about.
func (a *app) programLikesAnswered(uid int64, asked []int64, liked map[int64]bool) {
	if a.profile == nil || a.profile.UserID != uid {
		return
	}
	for _, id := range asked {
		// A heart clicked meanwhile knows better.
		if a.askedPrograms[id] {
			a.setProgramLiked(id, liked[id])
		}
	}
}

// likeButton is a heart that likes tr: filled in red once liked. Liking
// pops it in on a spring while a ring swells around it and confetti flies
// out; unliking lets it give a little.
func (a *app) likeButton(c *ui.Context, tr *netease.Track, size float32, color ui.Color) ui.Element {
	if tr.ProgramID != 0 {
		a.askProgramLikes([]int64{tr.ProgramID})
	}
	liked := a.isLiked(tr)
	icon, label := iconHeart, "喜欢"
	if liked {
		icon, color, label = iconHeartFill, a.pal.accent, "取消喜欢"
	}
	// The burst goes on a wrapper, which the heart's pop does not scale.
	wrap := ui.Box(c).Center()
	var b ui.Element
	wrap.Children(func() { b = iconButton(c, icon, size, color, label) })
	scale := b.AnimateTo("pop", 1, ui.Animation{Spring: ui.SpringOptions{Duration: 550 * time.Millisecond, Bounce: 0.55}})
	b.Scale(scale, scale)
	burst := ui.Local(wrap, "burst", func() time.Time { return time.Time{} })
	if b.Clicked() {
		switch {
		case a.profile == nil:
		case liked:
			b.Jump("pop", 0.8)
		default:
			b.Jump("pop", 0.2)
			*burst = c.Now()
		}
		a.toggleLike(tr)
	}
	if !burst.IsZero() {
		start, accent := *burst, a.pal.accent
		wrap.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if drawBurst(p, r, size, p.Now().Sub(start), accent) {
				if a.showing() {
					p.AnimationFrame()
				}
			}
		})
	}
	return wrap
}

// burstColors are the confetti's colors, around the heart's red.
var burstColors = []ui.Color{
	ui.Hex("#ff4d6d"), ui.Hex("#ff9f1c"), ui.Hex("#ffd166"),
	ui.Hex("#c77dff"), ui.Hex("#4cc9f0"), ui.Hex("#ff70a6"),
}

const burstTime = 750 * time.Millisecond

// drawBurst draws the burst of a heart of size in r, at elapsed since the
// like, and reports whether it goes on.
func drawBurst(p *ui.Painter, r ui.Rect, size float32, elapsed time.Duration, accent ui.Color) bool {
	t := float32(elapsed) / float32(burstTime)
	if t >= 1 || t < 0 {
		return false
	}
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	out := func(x float32) float32 { return 1 - (1-x)*(1-x)*(1-x) } // ease out

	// The ring swells from the heart and thins away, in the first half.
	if rt := t * 2; rt < 1 {
		radius := size * (0.35 + 0.65*out(rt))
		width := size * 0.22 * (1 - rt)
		ring := (&ui.Path{}).Circle(cx, cy, radius)
		p.StrokePath(ring, width, accent.Alpha(0.9*(1-rt)))
	}

	// Two rings of confetti fly out, slowing, shrinking and fading; the
	// inner one a little behind and turned, as if thrown at once.
	for ring := range 2 {
		lag := float32(ring) * 0.08
		pt := (t - 0.12 - lag) / (0.88 - lag)
		if pt <= 0 {
			continue
		}
		pt = min(pt, 1)
		const n = 7
		dist := size * (0.55 + (1.15-0.25*float32(ring))*out(pt))
		dot := size * (0.16 - 0.04*float32(ring)) * (1 - pt*pt)
		alpha := 1 - pt*pt
		for i := range n {
			angle := (float64(i)/n + float64(ring)*0.5/n - 0.25) * 2 * math.Pi
			x := cx + dist*float32(math.Cos(angle))
			y := cy + dist*float32(math.Sin(angle))
			color := burstColors[(i+ring*3)%len(burstColors)].Alpha(alpha)
			p.Fill(ui.Rect{X: x - dot, Y: y - dot, W: 2 * dot, H: 2 * dot}, color, dot)
		}
	}
	return true
}
