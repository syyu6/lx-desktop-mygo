package app

import (
	"errors"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"rsc.io/qr"

	"netease-music-mygo/internal/account"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
)

// loginState is a QR code login under way.
type loginState struct {
	open bool
	gen  int // counts logins, to stop the polls of closed ones
	// src is the source being signed in to, and via the way in where the
	// source has more than one (QQ's app, or WeChat). A source is signed in
	// on its own: the dialog is one source's at a time.
	src  string
	via  string
	key  string
	code *qr.Code
	// png is the code where the source drew it rather than named a page to
	// open, as QQ does: it is shown as it came, since a code drawn again
	// from an address is not the same code.
	png []byte
	// bmp is png decoded, once, for the frame that shows it.
	bmp      *ui.Bitmap
	status   int // a QR state, or 0 while the code loads
	scanner  string
	avatar   string
	err      string
	finished bool
	// restoring says the login of a release before 0.1.26 is being read
	// from the keychain, which may have the system ask first, and
	// restoreErr what came of it while the dialog stays open.
	restoring  bool
	restoreErr string
}

// adoptOldLogin reads the login an older release kept in the keychain. A
// test replaces it, as the Keychain would ask the listener of a test.
var adoptOldLogin = netease.AdoptKeychainSession

// accountOf asks NetEase who is signed in through the session it was given.
var accountOf = netease.Account

// boot signs in to every source that kept a session, then loads the home
// page for whoever is listening. NetEase is asked first, as what it answers
// is what the app's own pages show without a source named.
func (a *app) boot() {
	go func() {
		profs := a.restoreAccounts()
		a.post(func() {
			a.signedIn(profs)
		})
	}()
}

// restoreAccounts reads back every source's session, and asks each who is
// signed in through it. It is run off the main thread, as each answer is a
// call.
func (a *app) restoreAccounts() map[string]*account.Profile {
	profs := map[string]*account.Profile{}
	for _, src := range account.Services() {
		s, ok := account.Get(src)
		if !ok || !s.Restore() {
			continue
		}
		p, err := s.Profile()
		switch {
		case err == nil && p == nil:
			s.Logout() // the source no longer knows the session
		case err == nil:
			profs[src] = p
		}
	}
	return profs
}

// signedIn takes the accounts that are signed in, and shows what is
// personal to them: nobody, when profs is empty.
func (a *app) signedIn(profs map[string]*account.Profile) {
	a.accounts = profs
	a.profile = nil
	var qq *account.Profile
	if p := profs[music.SourceNetease]; p != nil {
		a.profile = &netease.Profile{UserID: p.UserID, Nickname: p.Nickname, AvatarURL: p.AvatarURL}
	}
	qq = profs[music.SourceQQ]
	// Playback picks up where it was left, once the session that lets
	// the listener play their songs is back.
	a.restoreSession()
	a.mine, a.saved, a.liked = nil, nil, nil
	a.likedPrograms, a.askedPrograms = nil, nil
	// What the listener may play, and follows, changed: load the pages
	// again.
	a.playlists = map[string]*netease.Playlist{}
	a.plErr = map[string]string{}
	a.artists, a.albums, a.users, a.podcasts = map[string]*artistData{}, map[string]*albumData{}, map[string]*userData{}, map[int64]*podcastData{}
	a.podcastTab = &podcastTab{}
	a.searches = map[route]*searchData{}
	// The playlists followed from other services are not NetEase's, and are
	// still at hand.
	a.seedSubscriptions()
	a.load(a.route)
	// The home pages of every source are asked again: what the listener is
	// given depends on who is signed in.
	a.homes = map[string]*homeState{}
	a.loadHome()
	if a.profile != nil {
		a.loadLiked(a.profile.UserID)
		a.loadMine(a.profile.UserID)
	} else if a.liked != nil {
		// NetEase is signed out: its hearts are gone, the other sources'
		// are their own (loadLikedOf).
		for k := range a.liked {
			if strings.HasPrefix(k, music.SourceNetease+"/") {
				delete(a.liked, k)
			}
		}
	}
	if qq != nil {
		a.loadLikedOf(music.SourceQQ, qq.UserID)
	}
}

// signedInTo is whether the listener is signed in to a source.
//
// NetEase's is also kept in profile, which is what its own pages read: a
// test that signs in by setting profile alone is signed in to it here too.
func (a *app) signedInTo(src string) bool { return a.accountProfile(src) != nil }

// accountProfile is a source's account, or nil.
func (a *app) accountProfile(src string) *account.Profile {
	if src == "" {
		src = music.SourceNetease
	}
	if p := a.accounts[src]; p != nil {
		return p
	}
	if src == music.SourceNetease && a.profile != nil {
		return &account.Profile{
			Source: music.SourceNetease, UserID: a.profile.UserID,
			Nickname: a.profile.Nickname, AvatarURL: a.profile.AvatarURL,
		}
	}
	return nil
}

// openLoginTo opens the dialog on a source's sign-in, the way in named
// where that source has more than one.
func (a *app) openLoginTo(src, via string) {
	if src == "" {
		src = music.SourceNetease
	}
	a.login = loginState{open: true, gen: a.login.gen + 1, src: src, via: via}
	gen := a.login.gen
	if _, ok := account.LoginOf(src); !ok {
		a.login.err = account.ShortName(src) + "暂不支持在此登录"
		return
	}
	go func() {
		key, code, pngBytes, err := a.beginCode(src, via)
		a.post(func() {
			if gen != a.login.gen {
				return
			}
			if err != nil {
				a.login.err = "无法获取二维码，请检查网络"
				return
			}
			a.login.key, a.login.code, a.login.png = key, code, pngBytes
			a.login.bmp = nil
			a.login.status = account.Waiting
			a.pollLogin(gen, key)
		})
	}()
}

// openLogin opens the dialog on NetEase's sign-in, which is what the app's
// own pages ask for.
func (a *app) openLogin() { a.openLoginTo(music.SourceNetease, "") }

// beginCode asks a source for a code, as an image to draw: the one the
// source named a page for, drawn here, or the one it drew itself.
func (a *app) beginCode(src, via string) (key string, code *qr.Code, pngBytes []byte, err error) {
	l, ok := account.LoginOf(src)
	if !ok {
		return "", nil, nil, account.ErrNoLogin
	}
	c, err := l.Code(via)
	if err != nil {
		return "", nil, nil, err
	}
	if len(c.PNG) > 0 {
		return c.Key, nil, c.PNG, nil
	}
	code, err = qr.Encode(c.URL, qr.M)
	if err != nil {
		l.Cancel(c.Key)
		return "", nil, nil, err
	}
	return c.Key, code, nil, nil
}

// pollLogin asks every two seconds whether the code was scanned, until the
// login ends or the dialog closes.
func (a *app) pollLogin(gen int, key string) {
	src := a.login.src
	l, ok := account.LoginOf(src)
	if !ok {
		return
	}
	go func() {
		for {
			time.Sleep(2 * time.Second)
			code, msg, name, avatar, err := l.Check(key)
			var prof *account.Profile
			if code == account.Confirmed {
				l.Save()
				if s, ok := account.Get(src); ok {
					prof, _ = s.Profile()
				}
			}
			done := make(chan bool, 1)
			a.post(func() {
				lg := &a.login
				if gen != lg.gen || !lg.open {
					done <- true
					return
				}
				switch {
				case err != nil:
					// A call that may be made again is: the network may come
					// back. One that may not ended the login, and says why.
					msg, ended := l.Failed(err)
					if ended {
						lg.err = msg
					}
				case code == account.Confirmed:
					if prof == nil {
						prof = &account.Profile{Source: src, Nickname: name, AvatarURL: avatar}
					}
					lg.status, lg.finished = code, true
					a.adopt(src, prof)
					a.closeLoginSoon(gen)
				case code == account.Expired || code == account.Waiting || code == account.Scanned:
					lg.status = code
					if name != "" {
						lg.scanner, lg.avatar = name, avatar
					}
				default:
					lg.err = msg
					if lg.err == "" {
						lg.err = "登录失败，请稍后再试"
					}
				}
				done <- lg.status == account.Expired || lg.finished || lg.err != ""
			})
			if <-done {
				return
			}
		}
	}()
}

// adopt signs a source in, in the dialog, without the net's being asked
// again: what only a source's own pages show is loaded for it.
func (a *app) adopt(src string, prof *account.Profile) {
	if a.accounts == nil {
		a.accounts = map[string]*account.Profile{}
	}
	a.accounts[src] = prof
	if src == music.SourceNetease {
		a.signedIn(a.accounts)
		a.pickUp(prof.UserID)
		return
	}
	// Another source's pages are what it gives signed in: its home, its
	// own lists, and its hearts.
	if a.homes != nil {
		delete(a.homes, src)
	}
	a.loadHome()
	a.loadLikedOf(src, prof.UserID)
}

// signOut ends one source's session, and leaves the others as they were.
func (a *app) signOut(src string) {
	if src == "" {
		src = music.SourceNetease
	}
	a.login.gen++
	if s, ok := account.Get(src); ok {
		s.Logout()
	}
	delete(a.accounts, src)
	if src == music.SourceNetease {
		// NetEase's own pages are most of the app: what was personal to it
		// goes, and what the other sources give stays.
		a.signedIn(a.accounts)
		return
	}
	// Another source's own: its home is asked again signed out, its hearts
	// are gone, and a search asks it with no account.
	if a.homes != nil {
		delete(a.homes, src)
	}
	if a.liked != nil {
		for k := range a.liked {
			if strings.HasPrefix(k, src+"/") {
				delete(a.liked, k)
			}
		}
	}
	if a.route.page == pageSearch {
		a.searches = map[route]*searchData{}
	}
	a.loadHome()
	a.load(a.route)
}

// closeLoginSoon closes the dialog a moment after it showed the welcome.
func (a *app) closeLoginSoon(gen int) {
	go func() {
		time.Sleep(1200 * time.Millisecond)
		a.post(func() {
			if gen == a.login.gen {
				a.login.open = false
			}
		})
	}()
}

// restoreOldLogin takes the login a release before 0.1.26 left in the
// keychain, once the listener asks for it: the Keychain asks them to let
// this binary read it, and that question is theirs to answer rather than
// one the app brings up on its own after an update.
func (a *app) restoreOldLogin() {
	l := &a.login
	if l.restoring {
		return
	}
	l.restoring, l.restoreErr = true, ""
	gen := l.gen
	go func() {
		ok, err := adoptOldLogin()
		var prof *netease.Profile
		var acctErr error
		if err == nil && ok {
			prof, acctErr = accountOf()
		}
		a.post(func() { a.oldLoginRead(gen, err, prof, acctErr) })
	}()
}

// oldLoginRead takes what reading the old login came of.
func (a *app) oldLoginRead(gen int, err error, prof *netease.Profile, acctErr error) {
	l := &a.login
	l.restoring = false
	if gen != l.gen {
		return
	}
	switch {
	case errors.Is(err, netease.ErrNoOldLogin):
		l.restoreErr = "没有找到旧版本的登录，请扫码登录"
	case err != nil:
		// The system would not let this binary read it: the listener
		// chose not to allow it, or never said.
		l.restoreErr = "没有取得旧版本的登录，请扫码登录"
	case acctErr != nil:
		// The login is kept: the network may come back.
		l.restoreErr = "无法确认旧版本的登录，请稍后再试或扫码登录"
	case prof == nil:
		// The login was there, but NetEase no longer knows it.
		netease.ClearSession()
		l.restoreErr = "旧版本的登录已失效，请扫码登录"
	default:
		l.finished, l.status = true, account.Confirmed
		a.adopt(music.SourceNetease, &account.Profile{
			Source: music.SourceNetease, UserID: prof.UserID,
			Nickname: prof.Nickname, AvatarURL: prof.AvatarURL,
		})
		a.closeLoginSoon(gen)
	}
}

// loginDialog shows the QR code to scan with the NetEase app on a phone.
func (a *app) loginDialog(c *ui.Context) {
	p := a.pal
	l := &a.login
	ui.DialogBase(c.Key("login"), &l.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(340).Padding(28, 28, 26, 28).Radius(22).Gap(6).AlignItems(ui.Center).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		title, sub := "扫码登录", "打开"+loginAppName(l.source())+" App，扫一扫登录"
		if l.source() == music.SourceQQ {
			if l.via == "wechat" {
				sub = "打开微信，扫一扫登录"
			} else {
				sub = "打开手机 QQ，扫一扫登录"
			}
		}
		switch {
		case l.finished:
			title, sub = "登录成功", "欢迎回来，"+a.profileName()
		case l.restoring:
			sub = "正在读取旧版本的登录…"
		case l.restoreErr != "":
			sub = l.restoreErr
		case l.err != "":
			sub = l.err
		case l.status == account.Scanned:
			title, sub = "扫描成功", "请在手机上确认登录"
		case l.status == account.Expired:
			sub = "二维码已失效"
		}
		ui.Text(c, title).FontSize(20).FontWeight(700)
		ui.Text(c, sub).FontSize(13).TextColor(p.muted).TextAlign(ui.Center).Margin(0, 0, 14, 0)
		if a.loginChoices(c) {
			// Another source, or another way into this one: the code is
			// asked for again, as the sign-in it replaces has not been
			// scanned.
			return
		}

		ui.Box(c).Size(212, 212).Radius(16).Background(ui.RGB(255, 255, 255)).Border(1, p.hairline).Children(func() {
			dimmed := l.status == account.Scanned || l.status == account.Expired || l.finished || l.err != "" || l.restoring
			opacity := float32(1)
			if dimmed {
				opacity = 0.12
			}
			switch {
			case l.code != nil:
				code := l.code
				ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Opacity(opacity).
					Draw(func(pt *ui.Painter, r ui.Rect) { drawQR(pt, r, code, 14) })
			case len(l.png) > 0:
				// The source's own drawing, as it came: a code drawn again from
				// the address behind it is not the code it signed. Kept as a
				// bitmap on the state, which is decoded once.
				if l.bmp == nil {
					if bmp, err := ui.DecodeBitmap(l.png); err == nil {
						l.bmp = bmp
					}
				}
				if l.bmp != nil {
					ui.Image(c, l.bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Contain).Opacity(opacity)
				}
			case l.err == "":
				// On white whatever the appearance, as the code is.
				s := shimmer{base: ui.RGBA(0, 0, 0, 0.08), sheen: ui.RGBA(0, 0, 0, 0.03)}.in(c)
				qrSkeleton(c, s, 14).Absolute().Top(0).Left(0).Right(0).Bottom(0)
			}
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(10).Children(func() {
				switch {
				case l.finished || l.status == account.Scanned:
					if bmp := bitmap(a.art.get(netease.Sized(l.avatar, 160), false)); bmp != nil {
						pic := ui.Box(c).Size(72, 72).Radius(36).Shadow(0, 6, 16, -4, ui.RGBA(0, 0, 0, 0.3))
						in := artIn(pic, bmp)
						pic.Children(func() { artImage(c, bmp, 36, in) })
					}
					if l.scanner != "" {
						ui.Text(c, l.scanner).FontSize(14).FontWeight(600).TextColor(ui.Hex("#1d1d1f"))
					}
				case l.status == account.Expired || l.err != "":
					if pillButton(c.Key("refresh"), iconRepeat, "刷新二维码", p.accent, p.onAccent).Clicked() {
						a.openLoginTo(l.source(), l.via)
					}
				}
			})
		})
		ui.Text(c, a.loginNote(l.source())).FontSize(11.5).TextColor(p.faint).Margin(16, 0, 0, 0)
		if !l.finished && l.source() == music.SourceNetease {
			a.restoreOldLoginLink(c, p)
		}
	})
}

// loginChoices draws the sources the dialog may sign in to, and the ways
// into the one it shows, and reports whether the listener chose another:
// the code under it is one of the sign-in being replaced, and is not drawn.
//
// A source is signed in on its own, one at a time: two codes side by side
// would be two logins to mind, and the listener minds one.
func (a *app) loginChoices(c *ui.Context) bool {
	l := &a.login
	var srcs []string
	for _, src := range account.Services() {
		if _, ok := account.LoginOf(src); ok {
			srcs = append(srcs, src)
		}
	}
	if len(srcs) > 1 {
		at := 0
		for i, src := range srcs {
			if src == l.src {
				at = i
			}
		}
		labels := make([]string, len(srcs))
		for i, src := range srcs {
			labels[i] = account.ShortName(src)
		}
		chosen := at
		if segmented(c, &chosen, labels...) {
			a.openLoginTo(srcs[chosen], "")
			return true
		}
	}
	if l.source() == music.SourceQQ && l.err == "" && !l.finished {
		via := 0
		if l.via == "wechat" {
			via = 1
		}
		chosen := via
		ui.Row(c).Margin(0, 0, 10, 0).Children(func() {
			segmented(c, &chosen, "QQ 扫码", "微信扫码")
		})
		if chosen != via {
			if chosen == 1 {
				a.openLoginTo(l.source(), "wechat")
			} else {
				a.openLoginTo(l.source(), "")
			}
			return true
		}
	}
	return false
}

// src is the source being signed in to, or NetEase's where none is named:
// a login of the app's own pages is NetEase's.
func (l *loginState) source() string {
	if l.src == "" {
		return music.SourceNetease
	}
	return l.src
}

// loginAppName is the app that reads a source's code.
func loginAppName(src string) string {
	switch src {
	case music.SourceQQ:
		return "手机 QQ"
	case music.SourceNetease:
		return "网易云音乐"
	}
	return account.ShortName(src)
}

// loginNote is what a source's sign-in gives the listener, under its code.
func (a *app) loginNote(src string) string {
	switch src {
	case music.SourceQQ:
		return "登录后，QQ 音乐的每日推荐、我的歌单与喜欢才可用；微信登录的凭据过期前会自动续期"
	}
	return "登录后，每日推荐与歌单将根据你的口味生成"
}

// restoreOldLoginLink offers the login a release before 0.1.26 kept in the
// keychain. It is a quiet line under the dialog rather than a button beside
// the code: few listeners have one, and the ones who do look for it after an
// update signed them out. Not the accent, which the app uses for what is
// wrong or worth warning about.
func (a *app) restoreOldLoginLink(c *ui.Context, p palette) {
	l := &a.login
	b := ui.ButtonBase(c.Key("restore-login")).Height(24).PaddingX(8).Radius(6).FocusRing(false).
		Label("从旧版本恢复登录").Tooltip("读取更新前留在钥匙串中的登录，系统会先询问是否允许")
	bg := ui.Transparent
	label := p.muted
	if b.Hovered() {
		bg, label = p.hover, p.text
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() {
		ui.Text(c, "从旧版本恢复登录").FontSize(12).FontWeight(500).TextColor(label)
	})
	switch {
	case l.restoring:
		b.Disabled(true).Opacity(0.5)
	case b.Clicked():
		a.restoreOldLogin()
	}
}

// drawQR draws a QR code in r, as crisp squares on white, inside a quiet
// zone of pad.
func drawQR(p *ui.Painter, r ui.Rect, code *qr.Code, pad float32) {
	n := code.Size
	cell := (min(r.W, r.H) - 2*pad) / float32(n)
	x0 := r.X + (r.W-cell*float32(n))/2
	y0 := r.Y + (r.H-cell*float32(n))/2
	ink := ui.Hex("#141416")
	for y := range n {
		for x := range n {
			if code.Black(x, y) {
				// A hair larger, so neighbors join without seams.
				p.Fill(ui.Rect{X: x0 + float32(x)*cell, Y: y0 + float32(y)*cell, W: cell + 0.3, H: cell + 0.3}, ink, 0)
			}
		}
	}
}

func (a *app) profileName() string {
	if a.profile == nil {
		return ""
	}
	return a.profile.Nickname
}

// The account is the sidebar's foot: who listens, by their picture alone,
// with a dropdown to open their own page, the settings, or to sign out.
// The settings keep a sign-in of their own (设置 → 来源), since the foot is
// folded away with the rail's names.

// accountButton is the listener's picture at the foot of the sidebar: the
// way in when nobody is signed in, and a menu when somebody is. It says who
// it is by the picture rather than a name, so that it is one width folded
// and unfolded, and the row beside it has room.
//
// The picture is the main account's — NetEase, the app's own — or, when
// only another source is signed in, that one's, with the source said under
// it in the menu rather than on the button.
func (a *app) accountButton(c *ui.Context) {
	p := a.pal
	main := a.mainAccount()
	b := ui.ButtonBase(c.Key("account")).Size(33, 33).Radius(999).Center().FocusRing(false)
	if b.Hovered() || a.accountOpen && main != nil {
		b.Background(p.hover)
	}
	if main == nil {
		a.accountOpen = false
		b.Label("登录")
		b.Tooltip("登录")
		b.Children(func() {
			ui.Box(c).Size(26, 26).Radius(13).Background(p.press).Center().Children(func() {
				ui.Icon(c, iconUser).Size(14, 14).TextColor(p.muted)
			})
		})
		if b.Clicked() {
			a.openLogin()
		}
		return
	}
	b.Label(main.Nickname).Expanded(a.accountOpen)
	b.Tooltip(main.Nickname + " · " + account.ShortName(main.Source))
	b.Children(func() {
		a.avatar(c, main.AvatarURL, 26, p.press)
	})
	if b.Clicked() {
		a.accountOpen = !a.accountOpen
	}
	a.accountMenu(c, b)
}

// mainAccount is the account the foot shows: NetEase's, as the app's own
// pages are its, and else whichever source is signed in.
func (a *app) mainAccount() *account.Profile {
	if p := a.accountProfile(music.SourceNetease); p != nil {
		return p
	}
	for _, src := range account.Services() {
		if p := a.accountProfile(src); p != nil {
			return p
		}
	}
	return nil
}

// accountMenu drops up from the listener's picture, in the colors of the
// volume popover: an account per source signed in, the way into the ones
// that are not, the settings, and signing out.
func (a *app) accountMenu(c *ui.Context, row ui.Element) {
	pop := popoverColors(a.pal.dark)
	ui.PopoverBase(c.Key("account-menu"), row, &a.accountOpen, func(panel ui.Element) {
		panel.AttachTo(row, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 6, 0).Padding(5).Gap(1).
			Radius(12).Background(pop.fill).Border(0.5, pop.edge).Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.2)).
			Role(ui.RoleMenu).FocusGroup(ui.Vertical).Label("账户").TransformOrigin(0.5, 1).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.96, Y: 4}, Exit: &ui.Motion{Scale: 0.98}})
		panel.Width(200)
		shortcut := "Ctrl+,"
		if runtime.GOOS == "darwin" {
			shortcut = "⌘,"
		}
		// Each source is a row of its own: who is signed in to it, and the
		// way out of that one alone.
		for _, src := range account.Services() {
			src := src
			prof := a.accounts[src]
			name := account.ShortName(src)
			if prof == nil {
				if a.menuItemKeyed(c, "account-"+src, iconUser, "登录"+name, "").Clicked() {
					a.accountOpen = false
					a.openLoginTo(src, "")
				}
				continue
			}
			if a.menuItemKeyed(c, "account-"+src, iconUser, prof.Nickname, name).Clicked() {
				a.accountOpen = false
				if src == music.SourceNetease {
					a.open(route{page: pageUser, id: prof.UserID})
					continue
				}
				// Another source's page is its own: opened by its id.
				a.open(route{page: pageUser, src: pageSrc(src), id: prof.UserID})
			}
		}
		if a.menuItem(c, iconSettings, "设置…", shortcut).Clicked() {
			a.accountOpen = false
			a.openSettings()
		}
		if main := a.mainAccount(); main != nil {
			ui.Box(c).Height(0.5).Margin(4, 8).Background(pop.edge)
			if a.menuItem(c, iconSignOut, "退出"+account.ShortName(main.Source), "").Clicked() {
				a.accountOpen = false
				a.signOut(main.Source)
			}
		}
	})
}

// menuItem is an item of a dropdown: an icon, its label, and the shortcut
// that does the same, if any.
func (a *app) menuItem(c *ui.Context, icon *ui.SVG, label, shortcut string) ui.Element {
	return a.menuItemKeyed(c, label, icon, label, shortcut)
}

// menuItemKeyed is menuItem keyed by something other than its label, for two
// items that read alike: an account per source, or two of them with one name.
func (a *app) menuItemKeyed(c *ui.Context, key string, icon *ui.SVG, label, shortcut string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(key)).Role(ui.RoleMenuItem).Label(label).Height(30).PaddingX(8).Gap(9).Radius(7).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || b.FocusVisible() {
		b.Background(p.press)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(p.muted)
		ui.Text(c, label).FontSize(13).Grow(1)
		if shortcut != "" {
			ui.Text(c, shortcut).FontSize(12).TextColor(p.faint)
		}
	})
	return b
}

// myPlaylists lists the listener's playlists under a header that opens
// and closes them: those they made, if mine, with a button to make one
// and a menu to rename them.
func (a *app) myPlaylists(c *ui.Context, title string, lists []netease.Playlist, mine bool, collapsed *bool) {
	if len(lists) == 0 {
		return
	}
	p := a.pal
	ui.Row(c.Key(title)).AlignItems(ui.Center).Margin(18, 4, 6, 6).Children(func() {
		b := ui.ButtonBase(c.Key("toggle")).Grow(1).Height(26).PaddingX(4).Gap(6).Radius(6).
			AlignItems(ui.Center).Justify(ui.Start).Role(ui.RoleDisclosure).Label(title)
		if b.Clicked() {
			*collapsed = !*collapsed
		}
		b.Expanded(!*collapsed)
		if b.Hovered() {
			b.Background(p.hover)
		}
		b.Children(func() {
			ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(p.faint).Grow(1)
			arrow := iconDown
			if *collapsed {
				arrow = iconForward
			}
			ui.Icon(c, arrow).Size(12, 12).TextColor(p.faint)
		})
		if mine && iconButton(c.Key("new"), iconPlus, 13, p.faint, "新建歌单").Size(26, 26).Radius(6).Clicked() {
			a.newPlaylist(nil)
		}
	})
	if *collapsed {
		return
	}
	for i := range lists {
		pl := &lists[i]
		selected := a.route.page == pagePlaylist && a.route.src == pl.Source && a.route.id == pl.ID && !a.nowPlaying
		b := ui.ButtonBase(c.Key(playlistKey(pl.Source, pl.ID))).Height(34).PaddingX(8).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(pl.Name)
		switch {
		case selected:
			b.Background(p.press)
		case b.Hovered():
			b.Background(p.hover)
		}
		b.Children(func() {
			a.cover(c, pl.Cover(), 80, 22, 5)
			ui.Text(c, pl.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
		})
		if mine && pl.SpecialType != 5 {
			b.PopoverMenu(func(m *ui.Menu) {
				if m.Item("重命名…").Chosen() {
					a.startRename(pl)
				}
				m.Separator()
				if m.Item("删除歌单…").Chosen() {
					a.confirmDelete(pl)
				}
			})
		}
		if sub := a.followed(pl); sub != nil {
			b.PopoverMenu(func(m *ui.Menu) {
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
		if b.Clicked() {
			a.open(playlistRoute(pl.Source, pl.ID))
		}
	}
}
