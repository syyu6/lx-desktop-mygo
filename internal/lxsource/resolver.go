package lxsource

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// A Resolver asks the listener's scripts for what the app cannot get
// itself: the stream of a song, its lyrics, its cover.
//
// Scripts run one at a time, each in an Engine of its own, made the first
// time it is asked something and kept while it is used: a runtime takes a
// moment to start, and a script is imported to be asked for many songs.
// A script that hangs is marked broken, and is not asked again until it is
// imported again.
//
// A runtime costs its script's heap, which is the megabytes QuickJS holds
// for the script's own objects, so the ones not asked for a while are let
// go (idleFor): a script asked again is started again, which with QuickJS
// compiled to pure Go takes a fraction of a millisecond. What is left is
// the runtime of the script playing.
type Resolver struct {
	store *Store
	// Order is the scripts to ask, by ID, first first. It is read when a
	// script is asked for, so a change takes effect at the next request.
	Order func() []string

	mu      sync.Mutex
	engines map[string]*Engine
	// used is when each engine was last asked something, by ID, which the
	// idle sweep reads.
	used   map[string]time.Time
	broken map[string]bool
	closed bool
	// starting holds the scripts being started, so that two requests at
	// once wait for one runtime rather than making two.
	starting map[string]chan struct{}
}

// NewResolver returns a resolver of the scripts in store. It asks the
// scripts in Order, and none where Order is nil.
func NewResolver(store *Store) *Resolver {
	return &Resolver{
		store:    store,
		engines:  map[string]*Engine{},
		used:     map[string]time.Time{},
		broken:   map[string]bool{},
		starting: map[string]chan struct{}{},
	}
}

// ErrNoSource is returned when no script can answer.
var ErrNoSource = errors.New("没有音源能提供这首歌")

// engine returns the engine of the script id, started if it is not.
func (r *Resolver) engine(id string) (*Engine, error) {
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return nil, ErrClosed
		}
		if r.broken[id] {
			r.mu.Unlock()
			return nil, ErrHung
		}
		if e, ok := r.engines[id]; ok {
			r.used[id] = time.Now()
			r.mu.Unlock()
			return e, nil
		}
		if wait, ok := r.starting[id]; ok {
			// Another request is starting it: wait for that one, rather
			// than make a second runtime of the same script.
			r.mu.Unlock()
			<-wait
			continue
		}
		wait := make(chan struct{})
		r.starting[id] = wait
		r.mu.Unlock()

		e, err := r.start(id)

		r.mu.Lock()
		delete(r.starting, id)
		if err != nil {
			r.broken[id] = true
		} else {
			r.engines[id] = e
			r.used[id] = time.Now()
		}
		r.mu.Unlock()
		close(wait)
		return e, err
	}
}

// start reads a script and starts its runtime. It is called outside the
// resolver's lock: starting one takes as long as a script needs to say
// what it does.
func (r *Resolver) start(id string) (*Engine, error) {
	info, ok := r.store.get(id)
	if !ok {
		return nil, ErrNotFound
	}
	code, err := r.store.Source(info)
	if err != nil {
		return nil, err
	}
	return newEngine(code)
}

// scriptFor returns the engine of the first script in Order that does
// action for source, with the ID it is kept under, in the order the
// listener put them.
func (r *Resolver) scriptFor(source Source, action Action) (string, *Engine, error) {
	if r.Order == nil {
		return "", nil, ErrNoSource
	}
	for _, id := range r.Order() {
		e, err := r.engine(id)
		if err != nil {
			continue
		}
		if c, ok := e.Capabilities()[source]; ok && c.Has(action) {
			return id, e, nil
		}
	}
	return "", nil, ErrNoSource
}

// failed notes that a script hung, so that it is not asked again: the
// runtime of a script in an endless loop cannot be stopped, and is left to
// the engine that owns it.
func (r *Resolver) failed(id string, err error) {
	if !errors.Is(err, ErrHung) {
		return
	}
	r.mu.Lock()
	r.broken[id] = true
	delete(r.used, id)
	r.mu.Unlock()
}

// idleFor is how long a runtime is kept after the last time it was asked
// something. A song asks a script for its stream, its lyrics and its cover
// as it loads, and the next song asks again: ninety seconds of quiet is
// longer than the gap between two songs, and short enough that the
// megabytes behind a script the listener moved away from are given up while
// they still have the memory to give.
const idleFor = 90 * time.Second

// Sweep lets go of the runtimes not asked for in idleFor, and returns how
// many it let go. A script asked again starts a runtime again, which takes
// as long as its script does to say what it does — a fraction of a second
// — and only on the song that needs it.
func (r *Resolver) Sweep() int {
	cutoff := time.Now().Add(-idleFor)
	var drop []*Engine
	r.mu.Lock()
	for id, e := range r.engines {
		if !r.used[id].Before(cutoff) {
			continue
		}
		drop = append(drop, e)
		delete(r.engines, id)
		delete(r.used, id)
	}
	r.mu.Unlock()
	// Closing an engine waits for its goroutine, which is the script's time
	// to stop: do it outside the lock, and one at a time.
	for _, e := range drop {
		e.Close()
	}
	return len(drop)
}

// Stream returns where t streams from, as a script has it. quality is the
// quality the app asked for; a script that does not serve it serves what
// it has.
func (r *Resolver) Stream(t Track, quality Quality) (string, error) {
	id, e, err := r.scriptFor(t.Source, ActionMusicURL)
	if err != nil {
		return "", err
	}
	url, err := e.StreamURL(t, quality)
	if err != nil {
		// A script that cannot serve the quality asked may serve a lower
		// one: what a source offers for a song is not always what the
		// settings ask for.
		if lower := below(quality); lower != quality {
			if url, lerr := e.StreamURL(t, lower); lerr == nil {
				return url, nil
			}
		}
		r.failed(id, err)
		return "", err
	}
	return url, nil
}

// below returns the quality under q, or q for the lowest.
func below(q Quality) Quality {
	for i, x := range Qualities {
		if x == q && i > 0 {
			return Qualities[i-1]
		}
	}
	return q
}

// Supports reports whether the script id answers action for source, as it
// said when it started. A script that does not start supports nothing.
func (r *Resolver) Supports(id string, source Source, action Action) bool {
	e, err := r.engine(id)
	if err != nil {
		return false
	}
	c, ok := e.Capabilities()[source]
	return ok && c.Has(action)
}

// StreamWith returns where t streams from, as the script id has it, for a
// track the listener's order put that script's turn at: a script that does
// not do t's source, or cannot serve the quality asked for, serves the
// best below it, and otherwise says so.
func (r *Resolver) StreamWith(id string, t Track, quality Quality) (string, error) {
	e, err := r.engine(id)
	if err != nil {
		return "", err
	}
	if c, ok := e.Capabilities()[t.Source]; !ok || !c.Has(ActionMusicURL) {
		return "", ErrNoSource
	}
	url, err := e.StreamURL(t, quality)
	if err != nil {
		// What a source offers for a song is not always what the settings
		// ask for: a lower one may come.
		if lower := below(quality); lower != quality {
			if url, lerr := e.StreamURL(t, lower); lerr == nil {
				return url, nil
			}
		}
		r.failed(id, err)
		return "", err
	}
	return url, nil
}

// LyricsWith returns t's lyrics, as the script id has them.
func (r *Resolver) LyricsWith(id string, t Track) (Lyrics, error) {
	e, err := r.engine(id)
	if err != nil {
		return Lyrics{}, err
	}
	if c, ok := e.Capabilities()[t.Source]; !ok || !c.Has(ActionLyric) {
		return Lyrics{}, ErrNoSource
	}
	l, err := e.Lyrics(t)
	r.failed(id, err)
	return l, err
}

// PicWith returns t's cover, as the script id knows it.
func (r *Resolver) PicWith(id string, t Track) (string, error) {
	e, err := r.engine(id)
	if err != nil {
		return "", err
	}
	if c, ok := e.Capabilities()[t.Source]; !ok || !c.Has(ActionPic) {
		return "", ErrNoSource
	}
	url, err := e.Pic(t)
	r.failed(id, err)
	return url, err
}

// Lyrics returns t's lyrics, as a script has them.
func (r *Resolver) Lyrics(t Track) (Lyrics, error) {
	id, e, err := r.scriptFor(t.Source, ActionLyric)
	if err != nil {
		return Lyrics{}, err
	}
	l, err := e.Lyrics(t)
	r.failed(id, err)
	return l, err
}

// Pic returns the cover of t, as a script knows it.
func (r *Resolver) Pic(t Track) (string, error) {
	id, e, err := r.scriptFor(t.Source, ActionPic)
	if err != nil {
		return "", err
	}
	url, err := e.Pic(t)
	r.failed(id, err)
	return url, err
}

// Close stops every engine the resolver started.
func (r *Resolver) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	engines := make([]*Engine, 0, len(r.engines))
	for _, e := range r.engines {
		engines = append(engines, e)
	}
	r.engines = map[string]*Engine{}
	r.used = map[string]time.Time{}
	r.mu.Unlock()
	// Closing an engine waits for its goroutine, which is another script's
	// time: do it outside the lock, and one at a time.
	for _, e := range engines {
		e.Close()
	}
}

// Reset drops the engines of a script, as after it was imported again.
func (r *Resolver) Reset(id string) {
	r.mu.Lock()
	e, ok := r.engines[id]
	delete(r.engines, id)
	delete(r.used, id)
	delete(r.broken, id)
	r.mu.Unlock()
	if ok {
		e.Close()
	}
}

// Live returns how many runtimes the resolver holds: the scripts asked
// lately, each of which keeps its script's heap. The settings show it, so
// that a listener can see the scripts let go.
func (r *Resolver) Live() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.engines)
}

// Probe starts a script and returns what it says it does, for the settings
// to say what it can answer for. A script that does not start in time
// returns its error, and is left for dead.
func (r *Resolver) Probe(id string) (map[Source]Capability, error) {
	e, err := r.engine(id)
	if err != nil {
		return nil, err
	}
	caps := e.Capabilities()
	if len(caps) == 0 {
		return nil, errors.New("音源脚本没有说明它支持哪些音源")
	}
	return caps, nil
}

// CapabilitySources returns the sources caps stream, in the order the app
// names them: what a script can play a song from, which is what the
// settings tag it with.
func CapabilitySources(caps map[Source]Capability) []Source {
	var out []Source
	for _, s := range Sources {
		if c, ok := caps[s]; ok && c.Has(ActionMusicURL) {
			out = append(out, s)
		}
	}
	return out
}

// CapabilitySummary writes what caps cover, for a list: the sources it
// answers for, in the order the app names them.
func CapabilitySummary(caps map[Source]Capability) string {
	var names []string
	for _, s := range CapabilitySources(caps) {
		names = append(names, s.Name())
	}
	return strings.Join(names, "、")
}
