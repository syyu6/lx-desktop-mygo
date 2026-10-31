package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// playingApp is an app playing a track with lyrics, at a position the test
// sets with pl.Seek, and the number of times its view was built.
func playingApp() (*app, *ui.Tester, *int) {
	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 7, Name: "宝船", Artists: []netease.Artist{{Name: "DOUDOU"}}, Duration: 200000}
	a.pl.Cue([]netease.Track{tr})
	// A line every 10 seconds, sung over the first 5 of them.
	var lrc string
	for i := range 12 {
		lrc += "[0" + string(rune('0'+i*10/60)) + ":" + string(rune('0'+i*10%60/10)) + "0.00]一二三四五六\n"
	}
	lines := lyrics.Build(lrc, "", "")
	for i := range lines {
		l := &lines[i]
		l.End = l.At + 5*time.Second
		l.Words = lyrics.SpreadWords(l.Text, l.At, l.End)
	}
	a.pl.SetLyrics(lines)
	a.pl.Hold(0)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1200, 800)
	return a, tt, &builds
}

// framesAt counts the frames the view asks for over a second, at 60 a
// second, with playback held at pos.
func framesAt(a *app, tt *ui.Tester, pos time.Duration) int {
	a.pl.Seek(pos)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	asked := 0
	for range 60 {
		tt.Advance(time.Second / 60)
		if tt.FramePending() {
			asked++
			tt.Frame()
		}
	}
	return asked
}

// A window hidden or minimized asks for no frames of its own: a frame drawn
// then is one nobody sees, which the window only gives back once it has
// drawn nothing for a while. One still on screen keeps drawing whether or
// not the app is the active one, since a window left in front of another
// app's is a window somebody reads; a window covered by another's is
// stopped by MyGo itself, which holds the frames of an occluded window
// before they are drawn.
func TestWindowHiddenAsksForNoFrames(t *testing.T) {
	a, tt, _ := playingApp()
	a.settings.Smoke = true
	a.nowPlaying, a.npPane = true, paneStage
	for _, c := range []struct {
		name           string
		focused, shown bool
		least, most    int
	}{
		{"in front of another app", false, true, 2, 60},
		{"active", true, true, 2, 60},
		{"hidden", false, false, 0, 1},
	} {
		a.focused, a.shown = c.focused, c.shown
		if n := framesAt(a, tt, 12*time.Second); n < c.least || n > c.most {
			t.Errorf("the stage, %s: %d frames a second, want %d to %d", c.name, n, c.least, c.most)
		}
	}
}

// Now Playing asks for frames only as fast as what it shows changes. The
// Tester runs timers as it settles a frame, so this counts only what does
// not change for a while: a frame log of the app (MYGO_FRAME_STATS) measures
// the rest.
func TestNowPlayingFrameBudget(t *testing.T) {
	a, tt, _ := playingApp()
	if n := framesAt(a, tt, 12*time.Second); n > 3 {
		t.Errorf("a page under the player: %d frames a second", n)
	}
	// Its rows still, the wall asks for no more than the lyrics beside it
	// do: it moves only as it is moved.
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.nowPlaying, a.npPane = true, paneLyrics
	lyricsFrames := framesAt(a, tt, 12*time.Second)
	a.npPane = paneWall
	if n := framesAt(a, tt, 12*time.Second); n > lyricsFrames+3 {
		t.Errorf("the wall, still: %d frames a second, the lyrics %d", n, lyricsFrames)
	}
	tt.SetPreferences(ui.Preferences{})
	a.npPane = paneStage
	a.settings.StageSparks = true
	for _, s := range stageStyles {
		a.settings.StageStyle = s.id
		if n := framesAt(a, tt, 18*time.Second); n > 12 {
			t.Errorf("the stage between lines, in %s: %d frames a second", s.label, n)
		}
	}
}

// Another song's backdrop fades in over the last one's, which is then let
// go, so that the backdrop stops asking for frames.
func TestBackdropFadeEnds(t *testing.T) {
	a, tt, _ := coverApp(t)
	a.art.art[netease.Sized("a", 600)+"#backdrop"] = &artwork{tint: ui.RGB(200, 40, 40)}
	a.art.art[netease.Sized("b", 600)+"#backdrop"] = &artwork{tint: ui.RGB(40, 40, 200)}
	q := a.pl.Queue()
	q[1].Album.PicURL, q[2].Album.PicURL = "a", "b"
	tt.Frame()
	a.pl.Next()
	tt.Frame()
	if a.backdropCache.before == nil {
		t.Fatal("the backdrop changed songs without fading")
	}
	tt.Advance(backdropFade + 100*time.Millisecond)
	tt.Frame()
	if a.backdropCache.before != nil {
		t.Error("the last song's backdrop still fades after the fade")
	}
}

// Following the player, the pages ask for no frames of their own: the
// backdrop behind them is painted once a song, not thirty times a second as
// Now Playing's fog is, and the pages keep the frames they had.
func TestThePageBackdropAsksForNoFrames(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Appearance = "cover"
	a.art.art[netease.Sized("a", 600)+"#backdrop"] = &artwork{tint: ui.RGB(200, 40, 40)}
	a.pl.Cue([]netease.Track{{ID: 1, Name: "一", Album: netease.Album{PicURL: "a"}}})
	a.pl.Next()
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	if !a.pal.immersive {
		t.Fatal("the window is not painted with the player's backdrop")
	}
	for range 4 {
		tt.Advance(time.Second)
		if tt.FramePending() {
			t.Error("the page asks for frames with the backdrop still")
		}
	}
}
