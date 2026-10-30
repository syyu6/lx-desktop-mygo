package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// palette holds the app's colors, for the light and the dark appearance.
type palette struct {
	dark      bool
	bg        ui.Color // the content's background
	sidebar   ui.Color // under the sidebar where no material shows
	text      ui.Color
	muted     ui.Color
	faint     ui.Color
	hairline  ui.Color
	hover     ui.Color
	press     ui.Color
	accent    ui.Color
	onAccent  ui.Color
	cardShade ui.Color
	// immersive says the window is painted with the playing song's backdrop
	// behind it, as Now Playing is: the page and the sidebar are shades of
	// its cover's color rather than the appearance's own, and what is drawn
	// over them is the light text of a dark window.
	immersive bool
}

// paletteFor returns the app's colors for the theme t: the light or the dark
// one, and the accent the appearance asks for.
func paletteFor(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			dark:      true,
			bg:        ui.Hex("#141416"),
			sidebar:   ui.Hex("#1c1c1f"),
			text:      ui.Hex("#f5f5f7"),
			muted:     ui.RGBA(235, 235, 245, 0.56),
			faint:     ui.RGBA(235, 235, 245, 0.3),
			hairline:  ui.RGBA(255, 255, 255, 0.07),
			hover:     ui.RGBA(255, 255, 255, 0.06),
			press:     ui.RGBA(255, 255, 255, 0.1),
			accent:    ui.Oklch(0.66, 0.2, 22),
			onAccent:  ui.Hex("#ffffff"),
			cardShade: ui.RGBA(0, 0, 0, 0.45),
		}
	}
	return palette{
		bg:        ui.Hex("#ffffff"),
		sidebar:   ui.Hex("#f4f4f6"),
		text:      ui.Hex("#1d1d1f"),
		muted:     ui.RGBA(60, 60, 67, 0.62),
		faint:     ui.RGBA(60, 60, 67, 0.32),
		hairline:  ui.RGBA(0, 0, 0, 0.07),
		hover:     ui.RGBA(0, 0, 0, 0.04),
		press:     ui.RGBA(0, 0, 0, 0.08),
		accent:    ui.Oklch(0.6, 0.21, 24),
		onAccent:  ui.Hex("#ffffff"),
		cardShade: ui.RGBA(0, 0, 0, 0.16),
	}
}

// paletteFor returns the app's colors for the theme t: the light or the dark
// one, and what the appearance asks for. Following the player, and with a
// song playing, the whole window is the song's: the backdrop Now Playing
// paints behind it, and a dark palette of its cover's color over that.
// Without a song there is nothing to follow, and the system's own colors
// show instead.
func (a *app) paletteFor(t *ui.Theme, c *ui.Context) palette {
	p := paletteFor(t)
	tr := a.followingTrack()
	if a.settings.Appearance != "cover" || tr == nil {
		// Nothing plays, or the appearance follows the system: the
		// player's palette is not on screen, and the next one snaps in
		// rather than fading from a window that was another's.
		a.themeActive = false
		return p
	}
	tint := a.coverTint()
	if !a.tintKnown {
		// Its cover is still loading: the dark Now Playing paints while
		// there is none, so that the window does not flash the system's
		// light colors on the way to the song's.
		tint = npTint
	}
	if !a.themeActive {
		// The first frame that follows the player: there is nothing on
		// screen to fade from, and the window is the song's at once.
		a.themeActive, a.themeTint, a.themeFrom = true, tint, immersivePalette(tint)
		return a.themeFrom
	}
	to := immersivePalette(tint)
	now := c.Now()
	if tint != a.themeTint {
		// Another song: the window goes from the colors it shows now to
		// the new one's, rather than cutting from one to the other.
		a.themeFrom = blendPalette(a.themeFrom, immersivePalette(a.themeTint), themeProgress(now, a.themeAt))
		a.themeTint, a.themeAt = tint, now
	}
	if k := themeProgress(now, a.themeAt); k < 1 {
		c.AnimationFrame()
		return blendPalette(a.themeFrom, to, k)
	}
	return to
}

// windowColor is the window's own color, around the frame the app draws:
// the system's, or the playing song's while the appearance follows the
// player, so that the edge of a resize and the first frame after a change
// are the song's rather than a white edge around a dark window.
func (a *app) setWindowColor(p palette) {
	if a.win == nil {
		return
	}
	// Back to the system's, as the window was made: it changes only while
	// the appearance follows the player.
	if !p.immersive && a.settings.Appearance != "cover" {
		return
	}
	want := "light-dark(#ffffff, #141416)"
	if p.immersive {
		want = fmt.Sprintf("#%02x%02x%02x", p.bg.R, p.bg.G, p.bg.B)
	}
	if want == a.winColor {
		return
	}
	a.winColor = want
	_ = a.win.SetBackgroundColor(want)
}

// themeFade is how long the window takes to go from one song's colors to
// the next one's, about as long as the backdrop behind them.
const themeFade = 650 * time.Millisecond

// themeProgress returns how far the window has gone from the colors of the
// song before to the playing one's, eased so that it starts and ends
// gently.
func themeProgress(now, from time.Time) float32 {
	if from.IsZero() {
		return 1
	}
	k := min(max(float32(now.Sub(from))/float32(themeFade), 0), 1)
	return k * k * (3 - 2*k)
}

// blendPalette returns the palette k of the way from a to b, as the window
// fades between two songs' colors.
func blendPalette(a, b palette, k float32) palette {
	if k >= 1 {
		return b
	}
	if k <= 0 {
		return a
	}
	mix := func(x, y ui.Color) ui.Color { return x.Mix(y, k) }
	b.dark = a.dark || b.dark
	b.bg, b.sidebar = mix(a.bg, b.bg), mix(a.sidebar, b.sidebar)
	b.text, b.muted, b.faint = mix(a.text, b.text), mix(a.muted, b.muted), mix(a.faint, b.faint)
	b.hairline, b.hover, b.press = mix(a.hairline, b.hairline), mix(a.hover, b.hover), mix(a.press, b.press)
	b.accent, b.onAccent = mix(a.accent, b.accent), mix(a.onAccent, b.onAccent)
	b.cardShade = mix(a.cardShade, b.cardShade)
	return b
}

// immersivePalette returns the palette of a window painted with the
// playing song's backdrop: the dark appearance's text and hairlines, over
// a page and a sidebar of the cover's own hue, at the depths Now Playing
// paints its backdrop.
func immersivePalette(tint ui.Color) palette {
	p := paletteFor(&ui.Theme{Dark: true})
	h, s, _ := hsv(float64(tint.R)/255, float64(tint.G)/255, float64(tint.B)/255)
	if s > 0.12 {
		s = min(max(s*1.3, 0.45), 0.66)
	}
	// The page is the backdrop's own foot, a little deeper than it so that
	// the sidebar can stand off it; the sidebar a shade lighter, as a
	// raised surface.
	deep := hsvColor(h, s, 0.24)
	p.dark = true
	p.bg = deep
	// The sidebar is the backdrop darkened rather than a color of its own:
	// it shows through, so that the two read as one window, and it still
	// stands a little below the page.
	p.sidebar = ui.RGBA(0, 0, 0, 0.28)
	p.immersive = true
	// Text is white on it, and what lies over the page is light.
	p.text = ui.Hex("#f7f7fa")
	p.muted = ui.RGBA(255, 255, 255, 0.66)
	p.faint = ui.RGBA(255, 255, 255, 0.36)
	p.hairline = ui.RGBA(255, 255, 255, 0.1)
	p.hover = ui.RGBA(255, 255, 255, 0.08)
	p.press = ui.RGBA(255, 255, 255, 0.14)
	p.cardShade = ui.RGBA(0, 0, 0, 0.5)
	p.accent = coverAccent(tint, true)
	p.onAccent = ui.Hex("#ffffff")
	return p
}

// npTint is the gray Now Playing paints while its song's cover has not
// loaded, which the window behind it paints too rather than the system's
// light colors.
var npTint = ui.RGB(120, 120, 130)

// followingTrack returns the song whose cover the window follows while the
// appearance is the player's, or nil when there is none to follow: nothing
// plays, or what plays has no cover.
func (a *app) followingTrack() *netease.Track {
	tr := a.pl.Current()
	if tr != nil {
		a.seed = nil
	} else {
		tr = a.seed
	}
	if tr == nil || tr.Album.PicURL == "" {
		return nil
	}
	return tr
}

// coverTint returns the playing song's cover's color: the one the window
// takes while the appearance follows the player. It is asked for here, so
// that it loads while the frame is built, and kept for the frames after it,
// which paint from it as they draw.
func (a *app) coverTint() ui.Color {
	tr := a.followingTrack()
	if tr == nil {
		a.tintKnown, a.coverArt = false, nil
		return ui.Color{}
	}
	// The size the backdrop needs: it is drawn across the window, and a
	// cover that small would show its pixels once blurred.
	art := a.art.get(netease.Sized(tr.Album.PicURL, 600), true)
	if art == nil || art.failed {
		if !a.tintKnown {
			// Its cover has not loaded, and no song's had: the shade Now
			// Playing paints while it waits, which the window is drawn in
			// rather than the system's light.
			return npTint
		}
		// Not loaded yet: the last song's colors, so that the window does
		// not go back to the system's between two songs.
		return a.tint
	}
	// Kept for the frames after this one: the cover's own load posts the
	// redraw that shows it. A new song's cover, loaded, is the window's at
	// once rather than the last one's.
	a.tint, a.tintKnown, a.coverArt = art.tint, true, art
	a.keepCoverTint(tr.Album.PicURL, art.tint)
	return a.tint
}

// keepCoverTint keeps the color the cover at url gave the window, for the
// next time the app opens on that song (seedTint). It is kept once, as the
// song's cover has loaded, rather than on every frame.
func (a *app) keepCoverTint(url string, tint ui.Color) {
	if a.settings.Appearance != "cover" || a.settings.CoverTintCover == url {
		return
	}
	a.settings.CoverTintCover = url
	a.settings.CoverTint = fmt.Sprintf("#%02x%02x%02x", tint.R, tint.G, tint.B)
	_ = a.settings.save()
}

// hexColor reads "#rrggbb", as keepCoverTint writes it.
func hexColor(s string) (ui.Color, bool) {
	if len(s) != 7 || s[0] != '#' {
		return ui.Color{}, false
	}
	var v [3]uint8
	for i := range v {
		b, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return ui.Color{}, false
		}
		v[i] = uint8(b)
	}
	return ui.RGB(v[0], v[1], v[2]), true
}

// seedTint takes the color the window was last open in, if the song the
// session kept is the one it was taken from: the first frame is then drawn
// in the song's colors, rather than in the system's until its cover has
// loaded.
func (a *app) seedTint() {
	s := a.seed
	if s == nil || a.settings.CoverTintCover == "" || a.settings.CoverTintCover != s.Album.PicURL {
		return
	}
	c, ok := hexColor(a.settings.CoverTint)
	if !ok {
		return
	}
	a.tint, a.tintKnown = c, true
}

// withAccent returns p with the accent taken from the playing song's cover,
// as the immersive appearance asks: the cover's most vivid color, deepened
// or lightened to read on p's own background, in place of NetEase's red.
func (p palette) withAccent(tint ui.Color) palette {
	a := coverAccent(tint, p.dark)
	p.accent = a
	p.onAccent = ui.Hex("#ffffff")
	return p
}

// coverAccent returns the accent a cover's tint gives: the cover's own hue,
// at a saturation and a lightness that read as a fill, white text upon it,
// on a window of the given appearance.
func coverAccent(tint ui.Color, dark bool) ui.Color {
	h, s, _ := hsv(float64(tint.R)/255, float64(tint.G)/255, float64(tint.B)/255)
	if s < 0.12 {
		// A gray cover has no color to give: the accent stays NetEase's.
		return ui.Oklch(accentLightness(dark), 0.2, 22)
	}
	// Bright enough to be a color rather than a shadow, deep enough for
	// white on it: quite saturated, whatever the cover's own was.
	s = min(max(s, 0.55), 0.85)
	v := 0.72
	if dark {
		v = 0.8
	}
	return hsvColor(h, s, v)
}

// accentLightness is how light the accent is on a background of the given
// appearance: deep enough for white text on it, and bright enough to stand
// off a dark window.
func accentLightness(dark bool) float32 {
	if dark {
		return 0.68
	}
	return 0.58
}

// iconButton is a borderless button showing an icon, which a soft circle
// marks under the pointer and which gives a little as it is pressed.
func iconButton(c *ui.Context, icon *ui.SVG, size float32, color ui.Color, label string) ui.Element {
	b := roundButton(c, size, color, label)
	b.Children(func() { ui.Icon(c, icon).Size(size, size) })
	return b
}

// playButton is the icon button that plays and pauses, its icon turning
// from one into the other.
func playButton(c *ui.Context, playing bool, size float32, color ui.Color) ui.Element {
	b := roundButton(c, size, color, "播放/暂停")
	b.Children(func() { playPause(c, b, playing, size) })
	return b
}

// playPause draws the icon of b, a button that plays and pauses: as it
// goes from one to the other, the icon going shrinks and fades as the one
// coming grows in, with a little spring.
func playPause(c *ui.Context, b ui.Element, playing bool, size float32) {
	to := float32(0)
	if playing {
		to = 1
	}
	v := b.AnimateTo("playing", to, ui.Animation{Spring: ui.SpringOptions{Duration: 340 * time.Millisecond, Bounce: 0.3}})
	ui.Box(c).Size(size, size).Children(func() {
		for _, icon := range []struct {
			svg   *ui.SVG
			shown float32
		}{{iconPlay, 1 - v}, {iconPause, v}} {
			if icon.shown <= 0.01 {
				continue
			}
			s := 0.4 + 0.6*icon.shown
			ui.Icon(c, icon.svg).Absolute().Top(0).Left(0).Size(size, size).Scale(s, s).Opacity(min(icon.shown, 1))
		}
	})
}

// roundButton is the button of iconButton, without its icon.
func roundButton(c *ui.Context, size float32, color ui.Color, label string) ui.Element {
	b := ui.ButtonBase(c).Size(size+16, size+16).Center().Radius(999).Label(label).TextColor(color)
	bg := ui.Transparent
	if b.Hovered() {
		bg = ui.RGBA(128, 128, 128, 0.14)
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	pose := ui.Pose{}
	if b.Pressed() {
		pose.Scale = 0.88
	}
	b.AnimatePose(pose, ui.Snappy)
	return b
}

// pillButton is a rounded button with an icon and a label.
func pillButton(c *ui.Context, icon *ui.SVG, label string, bg, fg ui.Color) ui.Element {
	b := ui.ButtonBase(c).Height(36).PaddingX(18).Gap(8).Radius(999).AlignItems(ui.Center).TextColor(fg)
	fill := bg
	if b.Hovered() {
		fill = bg.Mix(ui.RGB(255, 255, 255), 0.12)
	}
	b.Background(b.AnimateColor("bg", fill, ui.Animation{Duration: 120 * time.Millisecond}))
	pose := ui.Pose{}
	if b.Pressed() {
		pose.Scale = 0.96
	}
	b.AnimatePose(pose, ui.Snappy)
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14)
		}
		ui.Text(c, label).FontSize(14).FontWeight(600).SingleLine()
	})
	return b
}

// cover shows an artwork, or a placeholder of the same size while it loads.
func (a *app) cover(c *ui.Context, url string, px int, size, radius float32) ui.Element {
	p := a.pal
	box := ui.Box(c).Size(size, size).Radius(radius).Center()
	bmp := bitmap(a.art.get(netease.Sized(url, px), false))
	in := artIn(box, bmp)
	if in < 1 {
		box.Background(p.hover)
	}
	box.Children(func() {
		if in < 1 {
			ui.Icon(c, iconNote).Size(size*0.3, size*0.3).TextColor(p.faint).Opacity(1 - in)
		}
		artImage(c, bmp, radius, in)
	})
	return box
}

// bitmap returns the cover of art, nil while it loads.
func bitmap(art *artwork) *ui.Bitmap {
	if art == nil {
		return nil
	}
	return art.bmp
}

// backdrop returns the blurred cover of art, nil while it loads.
func backdrop(art *artwork) *ui.Bitmap {
	if art == nil {
		return nil
	}
	return art.backdrop
}

// artFade is how an artwork fades in over its placeholder.
var artFade = ui.Animation{Duration: 320 * time.Millisecond}

// artIn returns how far bmp has faded in on el, the element that shows it
// or its placeholder: over a moment once it loads while el shows, and at
// once if it was ready as el first showed.
func artIn(el ui.Element, bmp *ui.Bitmap) float32 {
	if bmp == nil {
		return el.Jump("art", 0)
	}
	return el.AnimateTo("art", 1, artFade)
}

// artImage fills its parent with bmp, its corners rounded by radius, as
// far faded in as in.
func artImage(c *ui.Context, bmp *ui.Bitmap, radius, in float32) {
	if bmp == nil || in <= 0 {
		return
	}
	img := ui.Image(c, bmp).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover).Radius(radius)
	if in < 1 {
		img.Opacity(in)
	}
}

// clock formats a duration as minutes and seconds.
func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// plays formats a play count as people in China read it.
func plays(n float64) string {
	switch {
	case n >= 1e8:
		return fmt.Sprintf("%.1f亿", n/1e8)
	case n >= 1e4:
		return fmt.Sprintf("%.0f万", n/1e4)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}

// greeting returns what to say at this hour.
func greeting(now time.Time) string {
	switch h := now.Hour(); {
	case h < 5:
		return "夜深了"
	case h < 11:
		return "早上好"
	case h < 13:
		return "中午好"
	case h < 18:
		return "下午好"
	default:
		return "晚上好"
	}
}

// bars draws n bars of an equalizer in r, from the levels of what plays,
// smoothed in st between frames; idle bars rest low.
type barState struct {
	h    [player.Bands]float32
	last time.Time
}

func (st *barState) step(now time.Time, target [player.Bands]float32, live bool) {
	dt := float32(now.Sub(st.last).Seconds())
	if st.last.IsZero() || dt > 0.2 {
		dt = 0.016
	}
	st.last = now
	for i := range st.h {
		tg := target[i]
		if !live {
			tg = 0
		}
		k := float32(18) // rises fast
		if tg < st.h[i] {
			k = 6 // falls slowly
		}
		st.h[i] += (tg - st.h[i]) * min(1, k*dt)
	}
}

// group returns the mean of bands [from, to).
func (st *barState) group(from, to int) float32 {
	var s float32
	for i := from; i < to; i++ {
		s += st.h[i]
	}
	return s / float32(to-from)
}

func drawBars(p *ui.Painter, r ui.Rect, st *barState, n int, color ui.Color, minH float32) {
	gap := r.W / float32(n) * 0.38
	w := (r.W - gap*float32(n-1)) / float32(n)
	per := player.Bands / n
	for i := range n {
		// Shape the response: low bands are loud already.
		v := st.group(i*per, (i+1)*per)
		v = v * v * 1.15
		h := max(minH, min(r.H, r.H*v))
		x := r.X + float32(i)*(w+gap)
		p.Fill(ui.Rect{X: x, Y: r.Y + (r.H-h)/2, W: w, H: h}, color, w/2)
	}
}

// segmented is a segmented control as AppKit draws one: segments of one
// width in a small gray track, divided by hairlines, the one chosen raised
// on a capsule that slides to the next as it changes. It reports whether
// the user chose another.
func segmented(c *ui.Context, selected *int, labels ...string) bool {
	t := c.Theme()
	const height, inset, radius = 24, 2, 6
	width := float32(0)
	for _, l := range labels {
		w, _ := c.MeasureText(0, ui.Span{Text: l, Size: 13})
		width = max(width, w+26)
	}
	track, capsule, divider := ui.RGBA(0, 0, 0, 0.06), ui.RGB(255, 255, 255), ui.RGBA(0, 0, 0, 0.13)
	if t.Dark {
		track, capsule, divider = ui.RGBA(255, 255, 255, 0.08), ui.RGBA(255, 255, 255, 0.24), ui.RGBA(255, 255, 255, 0.13)
	}
	before := *selected
	parts := ui.SegmentedBase(c, selected, len(labels))
	at := parts.Track.AnimateTo("at", float32(*selected), ui.Animation{Spring: ui.SpringOptions{Duration: 280 * time.Millisecond, Bounce: 0.08}})
	n := len(labels)
	dark := t.Dark
	parts.Track.Height(height).Padding(inset).Radius(radius).Background(track).Draw(func(p *ui.Painter, r ui.Rect) {
		// Hairlines between segments, but beside the capsule.
		for i := 1; i < n; i++ {
			if d := float32(i) - at; d > -1 && d < 2 {
				continue
			}
			x := r.X + inset + float32(i)*width
			p.Fill(ui.Rect{X: x - 0.5, Y: r.Y + 6, W: 1, H: r.H - 12}, divider, 0)
		}
		pill := ui.Rect{X: r.X + inset + at*width, Y: r.Y + inset, W: width, H: r.H - 2*inset}
		if !dark {
			p.Shadow(pill, radius-1, 0, 0.5, 1, 0, ui.RGBA(0, 0, 0, 0.2))
			p.Shadow(pill, radius-1, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.06))
		}
		p.Fill(pill, capsule, radius-1)
	}).Children(func() {
		for i, l := range labels {
			seg := parts.Segment(i).Width(width).Height(height - 2*inset).Center().Radius(radius - 1)
			if seg.Pressed() && i != *selected {
				seg.Background(divider)
			}
			seg.Children(func() { ui.Text(c, l).FontSize(13).TextColor(t.Text).SingleLine() })
		}
	})
	return *selected != before
}

// paged is how a list loaded a page at a time is coming along.
type paged struct {
	loaded  bool // whether the first page came
	loading bool
	more    bool   // whether there is a page after those loaded
	err     string // why the last page didn't load
}

// pageFoot ends a list loaded a page at a time, asking next for the next
// page once it is within a screen of coming into view: where the last
// failed, it says so, with a button to try again.
func (a *app) pageFoot(c *ui.Context, pg *paged, next func()) {
	foot := ui.Row(c.Key("foot")).Height(56).Gap(12).Center()
	foot.Children(func() {
		switch {
		case pg.loading:
			ui.Text(c, "正在加载…").FontSize(12.5).TextColor(a.pal.muted)
		case pg.err != "":
			ui.Text(c, pg.err).FontSize(12.5).TextColor(a.pal.muted)
			if ui.Button(c.Key("retry"), "重试").Clicked() {
				next()
			}
		}
	})
	_, h := c.Size()
	if b := foot.Bounds(); pg.more && !pg.loading && pg.err == "" && b.W > 0 && b.Y < 2*h {
		next()
	}
}
