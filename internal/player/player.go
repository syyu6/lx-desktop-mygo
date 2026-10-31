package player

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
)

type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

// songURL asks NetEase where a track streams from; tests replace it.
var songURL = netease.SongURL

// lyricsOf asks NetEase for a track's lyrics; tests replace it.
var lyricsOf = netease.Lyrics

// Player plays a queue of tracks. Its methods run on the main thread;
// loading happens on goroutines, whose results post brings back there.
type Player struct {
	post func(func())
	// Quality returns the stream level to load, as the settings say.
	Quality func() string
	// Normalize reports whether to even out the loudness of tracks, as
	// the settings say.
	Normalize func() bool
	// Cache keeps the songs played, if set.
	Cache *Cache
	// Resolve returns where a track streams from at level, and the id of
	// the source that serves it: the app tries the sources in the
	// listener's order, its own first and then the others, which may have
	// the song the first will not stream.
	Resolve func(t netease.Track, level string) (*netease.Stream, string, error)
	// ResolveExcept is Resolve, also returning the id of the link that gave
	// the address and leaving out the link skip names. The app sets it: a
	// source may name an address and then fail to serve it — an imported
	// script whose upstream is down answers with a URL that is a 404 — and
	// the next link that has the song must get its turn rather than the
	// song failing outright.
	ResolveExcept func(t netease.Track, level, skip string) (*netease.Stream, string, string, error)
	// ResolveLyrics returns a track's lyrics, from whichever source has
	// them.
	ResolveLyrics func(t netease.Track) (lrc, translation string, ok bool)
	// ResolveLyricsElsewhere is ResolveLyrics for the sources other than
	// the song's own: what is asked once its own has answered with none,
	// rather than asking it the same thing twice.
	ResolveLyricsElsewhere func(t netease.Track) (lrc, translation string, ok bool)
	// ResolveCover returns a track's cover, from whichever source knows
	// it.
	ResolveCover func(t netease.Track) (url string, ok bool)
	// Changed, if set, hears that what plays, where it is or whether it
	// plays changed.
	Changed func()
	// Prefetch, when it returns true, fetches the next track's song while
	// the one playing plays: the network is idle between the two, and a
	// song still waiting for its first bytes when it takes over starts
	// late. The song is kept waiting, not played, until its turn.
	Prefetch func() bool
	// KeptFile returns a song's file, when the listener saved it and it
	// is still there: it plays from there rather than from the sources, as
	// the file they chose to keep is the one they hear — no traffic, and
	// the level they picked when they asked for the download. The cache is
	// still looked at first, which keeps every level it was played at.
	// The app sets it; a test without it plays from the cache and sources,
	// as one without downloads does.
	KeptFile func(t netease.Track) (path, level string, ok bool)

	queue   []netease.Track
	order   []int // the queue's indices in play order: shuffled or not
	at      int   // where in order the current track is, -1 for none
	shuffle bool
	repeat  RepeatMode
	volume  float64

	// A radio goes on with the tracks its feed gives, as the queue nears
	// its end, for as long as nothing else is played.
	radio    string
	feed     func(got func([]netease.Track))
	radioGen int  // counts radios, to drop what an old one gives
	feeding  bool // the feed is asked
	awaiting bool // at the queue's end, for what the feed gives

	track   *netease.Track
	served  string // the id of the source the track streams from
	out     *audio.Player
	stream  stream
	info    *netease.Stream // what the current track streams
	fade    fader
	gain    float64 // the current track's normalizing gain
	loading bool
	playing bool // what the listener wants: false while paused
	err     string
	gen     int // counts loads, to drop the results of stale ones

	// ahead is the next track's song, fetching while the one playing
	// plays, and kept for its turn: see startAhead. Only the main thread
	// makes or takes one.
	ahead *aheadTrack

	// local is a downloaded file to play instead of the source's stream,
	// once: the song the download page's 播放已下载的文件 asks for, which
	// names the file even where the task's own record is gone. It is taken
	// as that song loads and then dropped; a song downloaded and still on
	// the disk plays from its file regardless, which is KeptFile's doing.
	// open reads it off the main thread, hence the lock.
	localMu sync.Mutex
	local   struct {
		ident, path, level string
	}

	lyrics       []lyrics.Line
	lyricsLoaded bool

	// pending holds a seek asked for while the track loads.
	pending time.Duration
	// A seek playing fades out, then lands, then fades in: meanwhile the
	// position is where it goes.
	seeking bool
	seekTo  time.Duration
	seekGen int
}

func New(post func(func())) *Player {
	p := &Player{post: post, at: -1, volume: 0.8, gain: 1}
	// Music, mixed far enough ahead to play on while macOS keeps the app
	// waiting, as it does while its appearance changes.
	audio.SetLatency(audio.LatencyPlayback)
	// The media keys, and the controls of Control Center.
	mygo.NowPlaying.OnCommand(func(cmd mygo.MediaCommand, pos time.Duration) {
		switch cmd {
		case mygo.MediaPlay, mygo.MediaPause:
			if (cmd == mygo.MediaPlay) != p.playing {
				p.TogglePlay()
			}
		case mygo.MediaNext:
			p.Next()
		case mygo.MediaPrevious:
			p.Prev()
		case mygo.MediaSeek:
			p.Seek(pos)
		}
		p.post(func() {})
	})
	return p
}

// sync shows the system, and Changed, where playback is.
func (p *Player) sync() {
	if p.Changed != nil {
		defer p.Changed()
	}
	if p.track == nil {
		mygo.NowPlaying.Clear()
		return
	}
	state := mygo.PlaybackPaused
	if p.playing {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.SetPlayback(state, p.Position())
}

// announce shows the system the track, and its cover once loaded.
func (p *Player) announce(t *netease.Track, artwork []byte) {
	state := mygo.PlaybackPaused
	if p.playing {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.Set(mygo.NowPlayingInfo{
		Title:    t.Name,
		Artist:   t.ArtistNames(),
		Album:    t.Album.Name,
		Artwork:  artwork,
		Duration: t.Length(),
		Position: p.Position(),
		State:    state,
		Commands: mygo.MediaPlay | mygo.MediaPause | mygo.MediaNext | mygo.MediaPrevious | mygo.MediaSeek,
	})
}

func (p *Player) ended() {
	if p.repeat == RepeatOne {
		p.out.Seek(0)
		p.out.Play()
		return
	}
	if p.at+1 >= len(p.order) && p.feed != nil {
		p.awaitFeed()
		return
	}
	if p.at+1 >= len(p.order) && p.repeat == RepeatOff {
		p.playing = false
		p.sync()
		return
	}
	p.Next()
}

// PlayQueue replaces the queue with tracks and plays the one at start.
func (p *Player) PlayQueue(tracks []netease.Track, start int) {
	if len(tracks) == 0 {
		return
	}
	p.endRadio()
	start = min(max(start, 0), len(tracks)-1)
	p.queue = append([]netease.Track(nil), tracks...)
	p.order = make([]int, len(p.queue))
	for i := range p.order {
		p.order[i] = i
	}
	if p.shuffle {
		p.shuffleOrder(start)
		p.load(0)
		return
	}
	p.load(start)
}

// PlayRadio plays tracks in their order, then goes on with those feed
// gives as the queue nears its end, until something else is played. The
// radio's name says which it is. feed calls got on the main thread.
func (p *Player) PlayRadio(name string, tracks []netease.Track, feed func(got func([]netease.Track))) {
	if len(tracks) == 0 {
		return
	}
	shuffle := p.shuffle
	p.shuffle = false
	p.PlayQueue(tracks, 0)
	p.shuffle = shuffle
	p.AttachRadio(name, feed)
}

// RadioFrom makes the track playing the first of a radio's queue, which
// goes on with tracks, then with those feed gives: without starting it
// over.
func (p *Player) RadioFrom(name string, tracks []netease.Track, feed func(got func([]netease.Track))) {
	if p.track == nil {
		return
	}
	p.queue, p.order, p.at = []netease.Track{*p.track}, []int{0}, 0
	p.appendTracks(tracks)
	p.AttachRadio(name, feed)
}

// AttachRadio makes the queue the radio name's, which feed goes on with,
// as after restoring a session that played it.
func (p *Player) AttachRadio(name string, feed func(got func([]netease.Track))) {
	p.endRadio()
	p.radio, p.feed = name, feed
	p.refill()
}

// EndRadio stops the radio playing: the song playing plays on, but the
// songs the radio queued after it go, and it adds no more.
func (p *Player) EndRadio() {
	if p.radio == "" && p.feed == nil {
		return
	}
	awaiting := p.awaiting
	p.endRadio()
	if p.at >= 0 && p.at+1 < len(p.order) {
		kept := make([]netease.Track, p.at+1)
		for i, k := range p.order[:p.at+1] {
			kept[i] = p.queue[k]
		}
		p.queue, p.order = kept, make([]int, len(kept))
		for i := range p.order {
			p.order[i] = i
		}
	}
	if awaiting {
		// At its end, waiting for more: nothing plays on.
		p.playing = false
		p.sync()
	}
}

func (p *Player) endRadio() {
	p.radioGen++
	p.radio, p.feed, p.feeding, p.awaiting = "", nil, false, false
}

// Radio returns the name of the radio playing, or "".
func (p *Player) Radio() string { return p.radio }

// refill asks the radio for more once the queue is near its end.
func (p *Player) refill() {
	if p.feed == nil || p.feeding || p.at < len(p.order)-2 {
		return
	}
	p.feeding = true
	gen := p.radioGen
	p.feed(func(more []netease.Track) {
		if gen != p.radioGen {
			return
		}
		p.feeding = false
		p.appendTracks(more)
		switch {
		case !p.awaiting:
		case p.at+1 < len(p.order):
			p.awaiting = false
			p.load(p.at + 1)
		default:
			// The radio has nothing more: it stops at its end.
			p.awaiting, p.playing = false, false
			p.err = "电台暂时没有更多歌曲"
			p.sync()
		}
	})
}

// awaitFeed waits at the end of a radio's queue for what its feed gives.
func (p *Player) awaitFeed() {
	p.awaiting = true
	p.refill()
}

// Append adds tracks to the end of the queue, those it does not have: what
// a list of songs chosen asks for, without playing them now.
func (p *Player) Append(tracks []netease.Track) {
	if len(tracks) == 0 {
		return
	}
	p.appendTracks(tracks)
	if p.post != nil {
		p.post(func() {})
	}
}

// appendTracks adds tracks to the end of the queue, but those in it.
func (p *Player) appendTracks(tracks []netease.Track) {
	in := make(map[string]bool, len(p.queue))
	for _, t := range p.queue {
		in[t.Ident()] = true
	}
	for _, t := range tracks {
		if !in[t.Ident()] {
			in[t.Ident()] = true
			p.order = append(p.order, len(p.queue))
			p.queue = append(p.queue, t)
		}
	}
}

// shuffleOrder shuffles the order, with first first.
func (p *Player) shuffleOrder(first int) {
	rand.Shuffle(len(p.order), func(i, j int) { p.order[i], p.order[j] = p.order[j], p.order[i] })
	for i, v := range p.order {
		if v == first {
			p.order[0], p.order[i] = p.order[i], p.order[0]
		}
	}
}

func (p *Player) ToggleShuffle() {
	p.shuffle = !p.shuffle
	if len(p.order) == 0 {
		return
	}
	cur := p.order[max(p.at, 0)]
	if p.shuffle {
		p.shuffleOrder(cur)
		p.at = 0
	} else {
		for i := range p.order {
			p.order[i] = i
		}
		p.at = cur
	}
}

func (p *Player) CycleRepeat() { p.repeat = (p.repeat + 1) % 3 }

// Current returns the track playing or loading, or nil.
func (p *Player) Current() *netease.Track { return p.track }

// Queue returns the tracks queued, in the order they were given.
func (p *Player) Queue() []netease.Track { return p.queue }

// Playing reports what the listener wants: false while paused.
func (p *Player) Playing() bool { return p.playing }

// Loading reports whether the current track is still loading.
func (p *Player) Loading() bool { return p.loading || p.awaiting }

// Ready reports whether the current track has loaded, to play.
func (p *Player) Ready() bool { return p.out != nil }

// Err returns why the current track did not play, or "".
func (p *Player) Err() string { return p.err }

// Stream returns what the current track streams, once loaded, or nil.
func (p *Player) Stream() *netease.Stream { return p.info }

func (p *Player) Shuffle() bool      { return p.shuffle }
func (p *Player) Repeat() RepeatMode { return p.repeat }

// Lyrics returns the current track's lyrics, and whether they have loaded.
func (p *Player) Lyrics() ([]lyrics.Line, bool) { return p.lyrics, p.lyricsLoaded }

// Downloaded returns the share of the current track downloaded so far, or
// false before it has started to.
func (p *Player) Downloaded() (float64, bool) {
	if p.stream == nil {
		return 0, false
	}
	dl, _ := p.stream.source()
	return dl.Progress(), true
}

// Audio is the current track's sound decoded apart from what plays, as
// OpenAudio opens it: interleaved stereo at its rate, read as it
// downloads.
type Audio interface {
	SampleRate() int
	Channels() int
	Read(dst []float32) (int, error)
	// SetPosition goes to a frame, of SampleRate a second.
	SetPosition(frame int64) error
	Close() error
}

// OpenAudio opens the current track's sound again, for decoding apart
// from what plays, as for transcribing an episode. It reads the same
// download, waiting for what has not come in, and fails once the track
// changes. It works on any goroutine once opened; close it when done.
func (p *Player) OpenAudio() (Audio, error) {
	if p.stream == nil || p.track == nil {
		return nil, errors.New("player: nothing loaded")
	}
	dl, _ := p.stream.source()
	return openShared(dl, p.track.Duration)
}

// PlayOrder returns the queue's indices in play order, and where the
// current track is among them, -1 for none.
func (p *Player) PlayOrder() ([]int, int) { return p.order, p.at }

func (p *Player) Next() {
	if len(p.order) == 0 {
		return
	}
	at := p.at + 1
	if at >= len(p.order) {
		if p.feed != nil {
			p.stop()
			p.awaitFeed()
			return
		}
		at = 0
	}
	p.load(at)
}

// Prev plays the track before this one, however far this one has played:
// once a few seconds in, going back to its start only kept the listener
// pressing twice.
func (p *Player) Prev() {
	if len(p.order) == 0 {
		return
	}
	at := p.at - 1
	if at < 0 {
		at = len(p.order) - 1
	}
	p.load(at)
}

// Jump plays the queue's track i.
func (p *Player) Jump(i int) {
	for k, v := range p.order {
		if v == i {
			p.load(k)
			return
		}
	}
}

// TogglePlay pauses, fading out, or plays, fading in.
func (p *Player) TogglePlay() {
	if p.out == nil {
		switch {
		case p.loading:
			// It plays once loaded, or not.
			p.playing = !p.playing
			p.sync()
		case len(p.order) > 0:
			p.loadSkipping(max(p.at, 0), 0, true, p.pending)
		}
		return
	}
	out := p.out
	if p.playing {
		p.playing = false
		p.fade.fade(0, fadeOut, func() {
			p.post(func() {
				if p.out == out && !p.playing {
					out.Pause()
					p.sync()
				}
			})
		})
		p.sync()
		return
	}
	p.playing = true
	if !p.seeking && !out.Playing() {
		if out.Position() >= out.Duration()-time.Second {
			out.Seek(0)
		}
		out.Play()
	}
	p.fade.fade(1, fadeIn, nil)
	p.sync()
}

// seekGap is how long a seek waits for what plays to fade out, which the
// audio player does over 5 ms, as it pauses: within the next buffer or
// two it sends the system.
const seekGap = 30 * time.Millisecond

// Seek moves to t. Playing, it pauses and plays again around the jump,
// which the audio player fades over a few milliseconds, rather than
// cutting from one sound to the other with a click.
func (p *Player) Seek(t time.Duration) {
	if p.out == nil {
		p.pending = t
		return
	}
	out := p.out
	// Paused, it jumps at once; but a seek on its way paused it only for
	// the jump, and this one takes its place, playing on from there.
	if !out.Playing() && !p.seeking {
		out.Seek(t)
		p.sync()
		return
	}
	p.seekGen++
	gen := p.seekGen
	p.seeking, p.seekTo = true, t
	out.Pause()
	time.AfterFunc(seekGap, func() {
		p.post(func() {
			if gen != p.seekGen || p.out != out {
				return
			}
			p.seeking = false
			out.Seek(t)
			if p.playing || !p.fade.silent() {
				out.Play()
			}
			p.sync()
		})
	})
	p.sync()
}

func (p *Player) SetVolume(v float64) {
	p.volume = v
	p.applyGain()
}

// applyGain sets the volume of what plays: the listener's, which the ear
// hears in powers, and the track's gain, if normalizing.
func (p *Player) applyGain() {
	g := p.volume * p.volume
	if p.Normalize != nil && p.Normalize() {
		g *= p.gain
	}
	p.fade.setGain(g)
}

// normPreamp lifts normalized tracks, which NetEase brings to the quiet
// loudness of ReplayGain, nearer to how loud music plays elsewhere.
const normPreamp = 4

// normGain is the gain that brings a track as loud as others, as NetEase
// measured it, but never so loud that its peaks would clip.
//
// A track whose peak is unknown is only ever made quieter: the preamp lifts
// tracks to the loudness music plays elsewhere, and without the peak there
// is nothing to say the lift would not run past full scale, which the mixer
// clips hard. Songs already at 0 dBFS are common, and a source that tells
// nothing of a track's loudness (a script's, an older cache entry) would
// otherwise be lifted five decibels over everything else.
func normGain(st *netease.Stream) float64 {
	if st == nil || st.Gain == 0 && st.Peak == 0 {
		return 1
	}
	g := math.Pow(10, (st.Gain+normPreamp)/20)
	if st.Peak > 0 {
		g = min(g, 1/st.Peak)
	} else {
		g = min(g, 1)
	}
	return g
}

func (p *Player) Position() time.Duration {
	switch {
	case p.out == nil:
		return p.pending
	case p.seeking:
		return p.seekTo
	}
	return p.out.Position()
}

func (p *Player) Duration() time.Duration {
	if p.track == nil {
		return 0
	}
	return p.track.Length()
}

// Levels returns the spectrum of what plays now.
func (p *Player) Levels() ([Bands]float32, bool) {
	if p.stream == nil || p.out == nil || !p.playing {
		return [Bands]float32{}, false
	}
	_, spec := p.stream.source()
	frame := int64(p.Position().Seconds() * float64(p.stream.SampleRate()))
	return spec.at(frame)
}

func (p *Player) stop() {
	p.dropAhead()
	if out := p.out; out != nil {
		// Paused first, it fades out over a few milliseconds, rather than
		// stopping with a click.
		out.Pause()
		time.AfterFunc(100*time.Millisecond, out.Close)
	}
	p.out, p.stream, p.info = nil, nil, nil
	p.seeking = false
	p.fade.attach(nil, 1)
}

// fetchLyrics returns a track's lyrics: NetEase's, where the track is
// NetEase's and it has them, and else whichever source has them, so that a
// song the app found on another source shows its words — and so does a
// NetEase song NetEase has no words for, an instrumental's, which the
// other services often have words for all the same.
func (p *Player) fetchLyrics(t netease.Track) (lrc, trans, yrc string, err error) {
	if t.Src() == music.SourceNetease {
		lrc, trans, yrc, err = lyricsOf(t.ID)
		if err == nil && strings.TrimSpace(lrc) != "" {
			return lrc, trans, yrc, nil
		}
		// NetEase answered with none: the other sources are asked for the
		// song as they have it, before the song is called an instrumental.
		if p.ResolveLyricsElsewhere != nil {
			if l, tr, ok := p.ResolveLyricsElsewhere(t); ok {
				return l, tr, "", nil
			}
		}
	} else if p.ResolveLyrics != nil {
		// A song of another source knows its words by its own numbering.
		if l, tr, ok := p.ResolveLyrics(t); ok {
			return l, tr, "", nil
		}
	}
	if lrc == "" && err == nil {
		err = errors.New("player: no lyrics")
	}
	return lrc, trans, yrc, err
}

// fetchCover returns a track's cover as an image: NetEase's own, where the
// track is NetEase's and it has one, and else whichever source names one.
func (p *Player) fetchCover(t netease.Track) ([]byte, error) {
	if t.Src() == music.SourceNetease {
		if cover, err := netease.Fetch(netease.Sized(t.Album.PicURL, 600)); err == nil {
			return cover, nil
		}
	}
	if p.ResolveCover != nil {
		if url, ok := p.ResolveCover(t); ok {
			if b, err := netease.Fetch(url); err == nil {
				return b, nil
			}
		}
	}
	if pic := t.Cover(); pic != "" {
		return netease.Fetch(pic)
	}
	return nil, errors.New("player: no cover")
}

// load plays the track at order[at], skipping those that cannot play.
func (p *Player) load(at int) {
	p.loadSkipping(at, 0, true, 0)
}

// loadSkipping loads the track at order[at], to play or not, from from,
// skipping those that cannot play: skipped of them so far.
func (p *Player) loadSkipping(at, skipped int, play bool, from time.Duration) {
	t := p.queue[p.order[at]]
	level := "exhigh"
	if p.Quality != nil {
		level = p.Quality()
	}
	// The next song was fetched while the last one played: what it opened
	// is taken as it is, still waiting, rather than asked for again. One
	// fetched for any other song goes.
	ahead := p.takeAhead(t, level)
	p.stop()
	p.gen++
	gen := p.gen
	p.at = at
	p.track = &t
	p.served = ""
	p.loading, p.playing, p.err = true, play, ""
	p.awaiting = false
	p.lyrics, p.lyricsLoaded = nil, false
	p.pending = from
	p.announce(&t, nil)
	p.refill()

	go func() {
		cover, err := p.fetchCover(t)
		if err == nil {
			p.post(func() {
				if gen == p.gen {
					p.announce(&t, cover)
				}
			})
		}
	}()

	go func() {
		lrc, trans, yrc, err := p.fetchLyrics(t)
		var lines []lyrics.Line
		if err == nil {
			lines = lyrics.Build(lrc, trans, yrc)
		}
		p.post(func() {
			if gen == p.gen {
				p.lyrics = lines
				p.lyricsLoaded = true
			}
		})
	}()
	go func() {
		var info *netease.Stream
		var s stream
		var served string
		var err error
		if ahead == nil {
			info, s, served, err = p.open(t, level, false)
		} else if got, ok := ahead.wait(); ok {
			info, s, served, err = got.info, got.s, got.served, got.err
		} else {
			// Still fetching: what it opens is let go, and the song asked
			// for now, which waits for the sources as it would have.
			ahead.abandon()
			info, s, served, err = p.open(t, level, false)
		}
		p.post(func() {
			if gen != p.gen {
				if s != nil {
					s.Close()
				}
				return
			}
			p.loading = false
			if err != nil {
				p.err = err.Error()
				switch {
				case errors.Is(err, ErrUnsupportedFormat), errors.Is(err, ErrNotAudio):
					// What a source gave is not a song the player can play,
					// and says why: keep its words.
				case t.ProgramID != 0:
					p.err = "这期节目需要购买才能收听"
				case t.VIP():
					// Its own source wanted a subscription, and the others
					// were tried for it: say so, as Music does.
					p.err = "这首歌需要 VIP，其它来源也没有找到"
				}
				// Skip what cannot play, unless nothing in the queue can.
				if skipped+1 < len(p.order) {
					next := at + 1
					if next >= len(p.order) {
						next = 0
					}
					p.loadSkipping(next, skipped+1, p.playing, 0)
				} else {
					p.playing = false
					p.sync()
				}
				return
			}
			p.stream, p.info, p.served = s, info, served
			p.gain = normGain(info)
			p.out = audio.NewPlayer(s)
			// From its middle, as a session picked up again, it fades in.
			level := 1.0
			if p.pending > 0 {
				level = 0
			}
			p.fade.attach(p.out, level)
			p.applyGain()
			p.out.OnEnd(func() {
				p.post(func() {
					if gen == p.gen {
						p.ended()
					}
				})
			})
			if p.pending > 0 {
				p.out.Seek(p.pending)
				p.pending = 0
			}
			if p.playing {
				p.out.Play()
				p.fade.fade(1, fadeIn, nil)
			}
			p.startAhead()
			p.sync()
		})
	}()
}

// PlayFile plays t from path, the file a download made, at the level kept:
// the download page's 播放已下载的文件. It is how the page names its file,
// whatever the tasks say of it — one whose record went, or whose file the
// listener moved. The file plays in place of the queue, and the song's
// lyrics and cover come from the sources as they would: only its sound is
// the file's. A file that will not open falls back to the sources at the
// level the settings say.
func (p *Player) PlayFile(t netease.Track, path, level string) {
	p.localMu.Lock()
	p.local.ident, p.local.path, p.local.level = t.Ident(), path, level
	p.localMu.Unlock()
	p.PlayQueue([]netease.Track{t}, 0)
}

// takeLocal returns the downloaded file waiting for t, if it is the one
// asked for, and takes it: it plays once.
func (p *Player) takeLocal(t netease.Track) (path, level string) {
	p.localMu.Lock()
	defer p.localMu.Unlock()
	if p.local.ident != t.Ident() {
		return "", ""
	}
	path, level = p.local.path, p.local.level
	p.local.ident = ""
	return path, level
}

// openFile opens the file at path as t's song: an MP3, FLAC or AAC, by its
// own bytes, which is what the player asks of any file. Its level is the
// extension's, which says nothing of the level it was kept at: the caller
// sets that.
func openFile(path string, t netease.Track) (*netease.Stream, stream, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	s, err := openStream(opened(f, st.Size()), t.Duration)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return &netease.Stream{Size: st.Size(), Type: strings.TrimPrefix(filepath.Ext(path), ".")}, s, nil
}

// open opens track t's audio at level: kept in the cache, or else
// streamed into it, and kept once it has all come in.
//
// A song streams from its own source where that source will, and else from
// another that has it: what the app's Resolve does, which tries the
// sources in the listener's order and looks the song up on the others
// when its own will not stream it.
//
// A link that names an address which does not open gives way to the next:
// a script whose upstream is down answers with a URL that is a 404, and
// that is its own failing, not the song's. Only the links that come back
// saying they have no address at all end the song's chances, as they did
// before.
func (p *Player) open(t netease.Track, level string, ahead bool) (*netease.Stream, stream, string, error) {
	// A downloaded song plays from its own file, which the listener asked
	// for by name: it is the song as they chose it, at the level they
	// chose, and needs no source. A file that does not open gives way to
	// the sources below.
	if path, kept := p.takeLocal(t); path != "" {
		if info, s, err := openFile(path, t); err == nil {
			info.Level = kept
			return info, s, t.Src(), nil
		}
	}
	// The cache names a song by its source and its id there, so that two
	// sources' songs of one number stay apart.
	name := cacheName(t)
	if name != "" {
		if f, info, ok := p.Cache.lookup(name, level); ok {
			s, err := openStream(f, t.Duration)
			if err == nil {
				// What the cache keeps is the song's own, from its own
				// source.
				return info, s, t.Src(), nil
			}
			f.Close()
		}
	}
	// A song the listener downloaded plays from its file, which is the
	// level they kept rather than the one playing asks for: they chose it
	// when they asked. It needs no source, and no network. The cache is
	// asked first, as it is the song's own bytes at the level playing
	// wants; a file that will not open gives way to the sources below.
	if p.KeptFile != nil {
		if path, kept, ok := p.KeptFile(t); ok {
			if info, s, err := openFile(path, t); err == nil {
				info.Level = kept
				return info, s, t.Src(), nil
			}
		}
	}
	// skip is the link whose address did not open: the next try leaves it
	// out. It stays "" until one fails, so the common case is one look.
	var skip string
	var firstErr error
	for {
		info, served, link, err := p.resolveLink(t, level, skip)
		if err != nil {
			if firstErr != nil {
				// A source named an address that was no song, and no other
				// source has it: say both, as only the pair explains it.
				return nil, nil, "", fmt.Errorf("音源给出的地址无法播放（%v），其它来源也没有这首歌", firstErr)
			}
			return nil, nil, "", err
		}
		// What a source other than the song's own gives is not kept: the
		// cache's name is the song's own, and a stream found elsewhere may
		// be a different recording. Either way the song comes into a file
		// rather than into memory, and the one the cache keeps comes into
		// the cache's own.
		var dl *download
		switch {
		case served == t.Src() && name != "":
			dl = p.Cache.download(name, info)
		case ahead:
			// Waiting for its turn behind a song that plays: in a file,
			// which goes when the song does.
			dl = p.Cache.downloadAhead(info)
		default:
			dl = p.Cache.download("", info)
		}
		s, err := openStream(dl, t.Duration)
		if err == nil {
			return info, s, served, nil
		}
		dl.Close()
		if firstErr == nil {
			firstErr = err
		}
		// The address this link named is no song: ask the next link that
		// has the song. Without a link to leave out, as in a test with
		// Resolve alone, there is nothing more to ask.
		if link == "" || link == skip {
			return nil, nil, "", firstErr
		}
		skip = link
	}
}

// resolveLink asks where the track streams from, leaving out skip, and
// returns the link that answered.
func (p *Player) resolveLink(t netease.Track, level, skip string) (*netease.Stream, string, string, error) {
	if p.ResolveExcept != nil {
		return p.ResolveExcept(t, level, skip)
	}
	info, served, err := p.resolve(t, level)
	return info, served, "", err
}

// resolve asks Resolution where the track streams from. Without one, as in
// a test, NetEase answers for its own songs, and a song of another source
// is not played.
func (p *Player) resolve(t netease.Track, level string) (*netease.Stream, string, error) {
	if p.Resolve != nil {
		return p.Resolve(t, level)
	}
	if t.Src() != music.SourceNetease {
		return nil, "", ErrUnavailableFrom(t.Src())
	}
	info, err := songURL(t.ID, level)
	return info, music.SourceNetease, err
}

// Served returns the id of the source the track playing streams from, or
// "" before one plays: its own source, or the one the app found it on.
func (p *Player) Served() string { return p.served }

// ErrUnavailableFrom is returned for a song of a source the app has no
// provider for, and no script to ask: its source is turned off, or its
// provider is not built in.
func ErrUnavailableFrom(src string) error {
	return fmt.Errorf("这个来源（%s）没有提供这首歌", src)
}

// Session is where playback was left, to pick up again.
type Session struct {
	Queue    []netease.Track `json:"queue"`
	Order    []int           `json:"order"`
	At       int             `json:"at"`
	Shuffle  bool            `json:"shuffle"`
	Repeat   RepeatMode      `json:"repeat"`
	Position int64           `json:"position"` // milliseconds
	Radio    string          `json:"radio,omitempty"`
}

// Session returns where playback is, or false with nothing queued.
func (p *Player) Session() (Session, bool) {
	if p.track == nil || p.at < 0 {
		return Session{}, false
	}
	return Session{
		Queue:    p.queue,
		Order:    p.order,
		At:       p.at,
		Shuffle:  p.shuffle,
		Repeat:   p.repeat,
		Position: p.Position().Milliseconds(),
		Radio:    p.radio,
	}, true
}

// Restore picks up a session where it was left, paused, and loads its
// track to play at once when asked.
func (p *Player) Restore(s Session) {
	n := len(s.Queue)
	if n == 0 || len(s.Order) != n || s.At < 0 || s.At >= n {
		return
	}
	seen := make([]bool, n)
	for _, i := range s.Order {
		if i < 0 || i >= n || seen[i] {
			return
		}
		seen[i] = true
	}
	p.queue, p.order = s.Queue, s.Order
	p.shuffle, p.repeat = s.Shuffle, s.Repeat%3
	p.loadSkipping(s.At, 0, false, time.Duration(s.Position)*time.Millisecond)
}
