package app

import (
	"image"
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Following the player, the window takes the playing song's cover's color:
// its accent, and the page's own colors, which are the dark ones Now Playing
// paints over the same backdrop.
func TestAccentFollowsThePlayer(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{47, 168, 79, 255})
		}
	}
	url := "https://p1.music.126.net/cover"
	a.art.art[netease.Sized(url, 600)+"#backdrop"] = &artwork{bmp: ui.NewBitmap(img), tint: dominantColor(img)}
	a.pl.PlayQueue([]netease.Track{{ID: 1, Name: "歌", Album: netease.Album{PicURL: url}}}, 0)

	a.settings = defaultSettings()
	// The system's own colors first, which the appearance follows before it
	// follows the player.
	a.settings.Appearance = "system"
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	red := a.pal.accent

	a.settings.Appearance = "cover"
	tt.Frame()
	green := a.pal.accent
	if green == red {
		t.Fatalf("the accent is still NetEase's red #%02x%02x%02x", green.R, green.G, green.B)
	}
	// It is the cover's hue: more green than red, where the cover is.
	if green.G <= green.R || green.G <= green.B {
		t.Errorf("the accent #%02x%02x%02x is not the cover's green", green.R, green.G, green.B)
	}
	// And the page is the cover's too, dark enough for white on it, where
	// the light appearance it followed had a white page.
	page := a.pal.bg
	if !a.pal.immersive {
		t.Fatal("the window is not painted with the player's backdrop")
	}
	if page.G <= page.R || page.G <= page.B {
		t.Errorf("the page #%02x%02x%02x is not the cover's green", page.R, page.G, page.B)
	}
	if l := 0.2126*float32(page.R) + 0.7152*float32(page.G) + 0.0722*float32(page.B); l > 90 {
		t.Errorf("the page #%02x%02x%02x is too light for white text", page.R, page.G, page.B)
	}
}

// With nothing playing there is nothing to follow: the system's own colors
// show, not a window dimmed to gray.
func TestNothingPlayingKeepsTheSystem(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	a.settings = defaultSettings()
	a.settings.Appearance = "cover"
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if a.pal.immersive {
		t.Error("the window follows a player with nothing in it")
	}
	if a.pal.bg != paletteFor(&ui.Theme{}).bg {
		t.Errorf("the page is #%02x%02x%02x, want the system's", a.pal.bg.R, a.pal.bg.G, a.pal.bg.B)
	}
}

// A cover with no color of its own leaves the accent NetEase's red: the app
// is not tinted gray.
func TestAGrayCoverKeepsTheAccent(t *testing.T) {
	gray := coverAccent(ui.RGBA(128, 128, 128, 1), false)
	if gray != (ui.Color{R: 0xd7, G: 0x32, B: 0x40, A: 0xff}) {
		t.Errorf("a gray cover gives #%02x%02x%02x, want NetEase's red", gray.R, gray.G, gray.B)
	}
	// The dark appearance's own red, a little lighter to stand off the
	// window.
	dark := coverAccent(ui.Color{}, true)
	if dark.R <= 0xd7 || dark.G <= 0x32 || dark.B <= 0x40 {
		t.Errorf("the dark accent is #%02x%02x%02x, want a lighter red", dark.R, dark.G, dark.B)
	}
}

// Each of the four appearances is kept and read back: the three that set the
// system's appearance, and the one that takes the accent from the cover.
func TestTheFourAppearances(t *testing.T) {
	for _, mode := range []string{"system", "light", "dark", "cover"} {
		s := defaultSettings()
		s.Appearance = mode
		if got := s.Appearance; got != mode {
			t.Errorf("appearance = %q, want %q", got, mode)
		}
	}
	// The settings show all four, and only the chosen one starts chosen.
	a := chainTestApp(t)
	a.settings = defaultSettings()
	a.settings.Appearance = "cover"
	openSettingsTab(a, settingsLook)
	tt := ui.NewTester(a.settingsView, settingsWidth, 900)
	tt.Frame()
	for _, want := range []string{"跟随系统", "浅色", "深色", "跟随播放器"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("the settings do not offer %q", want)
		}
	}
}

// A song change takes the window from the colors of the one before to the
// new one's over a moment, rather than cutting from one to the other.
func TestTheWindowEasesBetweenSongs(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	url := func(id string) string { return "https://p1.music.126.net/" + id }
	put := func(id string, c color.RGBA) {
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		for y := range 64 {
			for x := range 64 {
				img.Set(x, y, c)
			}
		}
		a.art.art[netease.Sized(url(id), 600)+"#backdrop"] = &artwork{bmp: ui.NewBitmap(img), tint: dominantColor(img)}
	}
	put("green", color.RGBA{47, 168, 79, 255})
	put("blue", color.RGBA{28, 76, 190, 255})

	a.settings = defaultSettings()
	a.settings.Appearance = "cover"
	a.pl.PlayQueue([]netease.Track{{ID: 1, Name: "绿", Album: netease.Album{PicURL: url("green")}}}, 0)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	green := a.pal.bg

	a.pl.PlayQueue([]netease.Track{{ID: 2, Name: "蓝", Album: netease.Album{PicURL: url("blue")}}}, 0)
	tt.Frame()
	// The frame the new song takes over on still shows the last one's
	// colors: the change begins there, it does not cut.
	if a.pal.bg != green {
		t.Error("the window cut to the new song's colors on the frame it took over")
	}
	if !tt.FramePending() {
		t.Error("the window is not asking for the frames of the change")
	}
	// Halfway, the page is between the two: its green going, the blue
	// coming.
	tt.Advance(themeFade / 2)
	tt.Frame()
	if a.pal.bg.G >= green.G || a.pal.bg.B <= green.B {
		t.Errorf("halfway, the page #%02x%02x%02x is not between the two songs'", a.pal.bg.R, a.pal.bg.G, a.pal.bg.B)
	}
	tt.Advance(themeFade)
	tt.Frame()
	if a.pal.bg.B <= a.pal.bg.G {
		t.Errorf("after the change, the page #%02x%02x%02x is not the new song's blue", a.pal.bg.R, a.pal.bg.G, a.pal.bg.B)
	}
}

// The day's card takes the colors the window's palette is blending, and
// draws nothing of its own: it used to run a 600ms animation beside the
// window's 650ms fade, so mid-change it was somewhere else entirely from
// the page beside it.
func TestTheDailyCardTakesTheWindowColors(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	url := func(id string) string { return "https://p1.music.126.net/" + id }
	put := func(id string, c color.RGBA) {
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		for y := range 64 {
			for x := range 64 {
				img.Set(x, y, c)
			}
		}
		a.art.art[netease.Sized(url(id), 600)+"#backdrop"] = &artwork{bmp: ui.NewBitmap(img), tint: dominantColor(img)}
	}
	put("green", color.RGBA{47, 168, 79, 255})
	put("blue", color.RGBA{28, 76, 190, 255})

	a.settings = defaultSettings()
	a.settings.Appearance = "cover"
	a.daily = []netease.Track{{ID: 9, Name: "一", Album: netease.Album{PicURL: url("green")}}}
	a.pl.PlayQueue([]netease.Track{{ID: 1, Name: "绿", Album: netease.Album{PicURL: url("green")}}}, 0)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	greenDeep, greenRich, greenGlow := a.pal.dailyDeep, a.pal.dailyRich, a.pal.dailyGlow

	a.pl.PlayQueue([]netease.Track{{ID: 2, Name: "蓝", Album: netease.Album{PicURL: url("blue")}}}, 0)
	// The frame the change is noticed on still shows the last song's.
	tt.Frame()
	tt.Advance(themeFade / 3)
	tt.Frame()
	if !a.pal.dailyKnown || !a.pal.immersive {
		t.Fatal("the window's palette carries no card colors")
	}
	if a.pal.dailyDeep == greenDeep {
		t.Error("the card waited for the page: it is still the last song's")
	}
	// The card is exactly as far along as the page: the same fraction of
	// the way from the song before's colors to this one's, over the
	// palette's own two ends.
	from, to := a.themeFrom, immersivePalette(a.tint).withDaily()
	k := blendFraction(from.dailyDeep, to.dailyDeep, a.pal.dailyDeep)
	if kb := blendFraction(from.bg, to.bg, a.pal.bg); abs32(k-kb) > 0.06 {
		t.Errorf("the card is %.2f of the way, the page %.2f", k, kb)
	}
	// And the card draws from the palette rather than a color of its own:
	// however it is asked, mid-change, it answers with the window's.
	deep, rich, glow := a.dailyColors(nil)
	if deep != a.pal.dailyDeep || rich != a.pal.dailyRich || glow != a.pal.dailyGlow {
		t.Errorf("the card draws %v, the window's %v", deep, a.pal.dailyDeep)
	}
	// Arrived, both are the new song's, and neither kept the old one's.
	tt.Advance(themeFade)
	tt.Frame()
	want := immersivePalette(a.tint).withDaily()
	if a.pal.dailyDeep != want.dailyDeep || a.pal.dailyRich != want.dailyRich || a.pal.dailyGlow != want.dailyGlow {
		t.Errorf("the card is %v, the window's own %v", a.pal.dailyDeep, want.dailyDeep)
	}
	if a.pal.dailyDeep == greenDeep || a.pal.dailyRich == greenRich || a.pal.dailyGlow == greenGlow {
		t.Error("the card kept a color of the song before")
	}
}

// imageOf is a square of one color, for the colors the window blends from.
func imageOf(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, c)
		}
	}
	return img
}

// blendFraction returns how far shown is from a to b, by the channel that
// moved most.
func blendFraction(a, b, shown ui.Color) float32 {
	d := func(x, y uint8) float32 { return float32(x) - float32(y) }
	ch := [][3]uint8{{a.R, b.R, shown.R}, {a.G, b.G, shown.G}, {a.B, b.B, shown.B}}
	best, k := float32(0), float32(0)
	for _, c := range ch {
		total := d(c[1], c[0])
		if total == 0 || abs32(total) < best {
			continue
		}
		best = abs32(total)
		k = d(c[2], c[0]) / total
	}
	return k
}

// On the day of the recommendations, the card takes the playing song's
// colors as the rest of the window does, rather than the day's first
// cover's.
func TestTheDailyCardFollowsThePlayer(t *testing.T) {
	a := chainTestApp(t)
	a.art = newArtCache(func(func()) {})
	a.settings = defaultSettings()
	a.settings.Appearance = "cover"
	a.daily = []netease.Track{{ID: 9, Name: "一", Album: netease.Album{PicURL: "https://p1.music.126.net/day"}}}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	// Nothing plays: the card is its own cover's.
	a.pal = paletteFor(&ui.Theme{Dark: true})
	alone, _, _ := dailyColors(ui.Color{})

	a.pl.PlayQueue([]netease.Track{{ID: 1, Name: "歌", Album: netease.Album{PicURL: "https://p1.music.126.net/song"}}}, 0)
	a.art.art[netease.Sized("https://p1.music.126.net/song", 600)+"#backdrop"] = &artwork{tint: ui.RGB(47, 168, 79)}
	tt.Frame()
	deep, _, _ := a.dailyColors(nil)
	if !a.pal.immersive {
		t.Fatal("the window does not follow the player")
	}
	if deep.G <= deep.R || deep.G <= deep.B {
		t.Errorf("the card #%02x%02x%02x is not the playing song's green", deep.R, deep.G, deep.B)
	}
	if deep == alone {
		t.Error("the card kept its own colors while the window follows the player")
	}
}
