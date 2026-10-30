package player

import (
	"time"

	"netease-music-mygo/internal/netease"
)

// aheadTrack is the next track's song, fetched while the one playing
// plays: its stream, waiting to be played, and where it was found. The
// request to the sources, and the download's first bytes, are what a song
// waits for as it starts; the network is idle between two songs, so they
// are paid there rather than between the two.
//
// It holds a download, which keeps fetching the song as it waits, part of
// it in memory or on disk: what comes in while the last one plays is a
// song's first seconds, the rest follows as it plays.
type aheadTrack struct {
	track  netease.Track
	level  string
	done   chan struct{}
	info   *netease.Stream
	s      stream
	served string
	err    error
}

// aheadResult is what a fetched song opened as.
type aheadResult struct {
	info   *netease.Stream
	s      stream
	served string
	err    error
}

// startAhead fetches the next track's song, if there is one and the
// listener asked for it: one ahead at a time, kept for its turn.
func (p *Player) startAhead() {
	if p.Prefetch == nil || !p.Prefetch() || p.at < 0 || p.at+1 >= len(p.order) {
		return
	}
	next := p.queue[p.order[p.at+1]]
	if p.ahead != nil && p.ahead.track.ID == next.ID && p.ahead.track.Src() == next.Src() {
		return
	}
	p.dropAhead()
	level := "exhigh"
	if p.Quality != nil {
		level = p.Quality()
	}
	a := &aheadTrack{track: next, level: level, done: make(chan struct{})}
	p.ahead = a
	go func() {
		defer close(a.done)
		a.info, a.s, a.served, a.err = p.open(next, level, true)
	}()
}

// takeAhead returns the next song's song, still waiting, if it is the one
// started for t at level: what the load of t plays without asking again.
func (p *Player) takeAhead(t netease.Track, level string) *aheadTrack {
	a := p.ahead
	p.ahead = nil
	if a == nil || a.track.ID != t.ID || a.track.Src() != t.Src() || a.level != level {
		if a != nil {
			a.abandon()
		}
		return nil
	}
	return a
}

// dropAhead lets go of the next song's song, if one was fetched.
func (p *Player) dropAhead() {
	if a := p.ahead; a != nil {
		p.ahead = nil
		a.abandon()
	}
}

// abandon lets go of a song fetched ahead, once whatever opened it has
// come back: it is not played, and its download is no use.
func (a *aheadTrack) abandon() {
	go func() {
		<-a.done
		if a.s != nil {
			a.s.Close()
		}
	}()
}

// wait waits for the song to be fetched. It returns false if the source
// has not answered within the time a song plays: the song is asked for
// again at its turn, rather than the listener waiting for a source that is
// slow now.
func (a *aheadTrack) wait() (aheadResult, bool) {
	select {
	case <-a.done:
		return aheadResult{a.info, a.s, a.served, a.err}, true
	case <-time.After(prefetchTimeout):
		return aheadResult{}, false
	}
}

// prefetchTimeout is the longest the next song's song is waited for once
// its turn comes: what was fetched while the last one played is ready by
// then, and one still fetching is asked again.
const prefetchTimeout = 5 * time.Second
