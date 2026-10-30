package app

import (
	"fmt"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/lxsource"
)

// The sources are the scripts the listener imported, run by lxsource: the
// app asks them for a song NetEase will not play.
//
// Nothing of the app ships with a script: an imported one is code from the
// internet, run in a sandbox that reaches an empty directory and the
// network, and nothing else of the computer. See package lxsource.

// lxDir is where the imported scripts are kept, in the user data.
func lxDir() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sources"), nil
}

// sourceState holds what the resolver reads while a song loads, which the
// settings may change meanwhile: the scripts to ask, first first. It is a
// pointer in app so that app stays copyable.
type sourceState struct {
	mu    sync.Mutex
	order []string
}

// loadSources opens the store of the listener's scripts, and the resolver
// that runs them. It returns no error if they cannot be opened: the app
// plays what NetEase serves, and the settings say why there are no sources.
func (a *app) loadSources() {
	dir, err := lxDir()
	if err != nil {
		a.srcErr = err.Error()
		return
	}
	store, err := lxsource.NewStore(dir)
	if err != nil {
		a.srcErr = err.Error()
		return
	}
	a.srcStore = store
	a.srcState = &sourceState{}
	a.src = lxsource.NewResolver(store)
	// The scripts are asked in the order the settings keep them, first
	// first. The resolver asks off the main thread, so the order is read
	// through the lock: the settings may change as a song loads.
	a.src.Order = func() []string {
		a.srcState.mu.Lock()
		defer a.srcState.mu.Unlock()
		return append([]string(nil), a.srcState.order...)
	}
}

// sourceByID returns the script with id, as the settings list it.
func (a *app) sourceByID(id string) (lxsource.Info, bool) {
	for _, info := range a.sources {
		if info.ID == id {
			return info, true
		}
	}
	return lxsource.Info{}, false
}

// loadSourceList reads the scripts the app has, for the settings to show,
// and orders the ones turned on as the settings say.
func (a *app) loadSourceList() {
	if a.srcStore == nil {
		return
	}
	list, err := a.srcStore.List()
	if err != nil {
		a.srcErr = err.Error()
		return
	}
	a.sources = list
	// The order the scripts are asked in, kept as the settings have it, and
	// completed for a script they do not name yet.
	known := map[string]bool{}
	for _, info := range list {
		known[info.ID] = true
	}
	a.settings.ScriptOrder = a.scriptOrder()
	// What a script no longer kept had said about itself goes with it.
	for id := range a.srcCaps {
		if !known[id] {
			delete(a.srcCaps, id)
			delete(a.srcCapsErr, id)
			delete(a.srcCapsAsked, id)
		}
	}
	// The chain holds every link, the scripts among them: it is told what
	// the list now has, and it in turn tells the resolver what to ask.
	a.rebuildChain()
}

// askSourceCaps has each script say what it plays, once, in the background:
// only a running script knows, and what it says tags it in the settings.
// The platforms it said are kept beside the script, so a script asked
// before is tagged without a runtime: a runtime costs its script's heap,
// and listing the scripts should not pay that for a tag. A script new, or
// one updated since it was asked, is started — in the background, since
// starting it runs the script — and asked once.
func (a *app) askSourceCaps() {
	if a.srcCapsAsked == nil {
		a.srcCapsAsked = map[string]bool{}
	}
	if a.srcCaps == nil {
		a.srcCaps = map[string][]lxsource.Source{}
	}
	for _, info := range a.sources {
		id := info.ID
		if a.srcCapsAsked[id] {
			continue
		}
		// The platforms the store kept from the last time the script was
		// asked are the tags: a runtime is started to ask only a script
		// the store does not know yet, or the one updated since.
		if len(info.Platforms) > 0 {
			a.srcCapsAsked[id] = true
			a.srcCaps[id] = append([]lxsource.Source(nil), info.Platforms...)
			continue
		}
		// Starting a script is what a test has no loop for, and no window
		// means no test: the scripts are left unasked.
		if a.src == nil || a.srcStore == nil || a.win == nil {
			continue
		}
		a.srcCapsAsked[id] = true
		go func() {
			caps, err := a.src.Probe(id)
			a.post(func() { a.setSourceCaps(id, caps, err) })
		}()
	}
}

// setSourceCaps keeps what the script id said it plays, for the settings'
// tags, and shows it.
func (a *app) setSourceCaps(id string, caps map[lxsource.Source]lxsource.Capability, err error) {
	if a.srcCaps == nil {
		a.srcCaps = map[string][]lxsource.Source{}
	}
	if a.srcCapsErr == nil {
		a.srcCapsErr = map[string]string{}
	}
	if err != nil {
		a.srcCapsErr[id] = err.Error()
		delete(a.srcCaps, id)
	} else {
		delete(a.srcCapsErr, id)
		tags := lxsource.CapabilitySources(caps)
		a.srcCaps[id] = tags
		// Kept beside the script, so that listing them asks no runtime
		// next time: a runtime costs its script's heap, and the tags are
		// only what it can play.
		if a.srcStore != nil {
			_ = a.srcStore.SetPlatforms(id, tags)
		}
	}
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

// sourceTags are the platforms the script id plays, as the settings tag it:
// what its runtime said, or that it has not said yet.
func (a *app) sourceTags(id string) (tags []lxsource.Source, asked, failed bool) {
	_, failed = a.srcCapsErr[id]
	tags = a.srcCaps[id]
	return tags, a.srcCapsAsked[id], failed
}

// setScriptOrder gives the resolver the scripts to ask, in the order the
// settings keep them.
func (a *app) setScriptOrder(ask []string) {
	if a.srcState == nil {
		return
	}
	a.srcState.mu.Lock()
	a.srcState.order = ask
	a.srcState.mu.Unlock()
}

// closeSources stops the scripts' runtimes, as the app closes.
func (a *app) closeSources() {
	if a.src != nil {
		a.src.Close()
	}
}

// liveSources is how many script runtimes are up, which the settings show.
func (a *app) liveSources() int {
	if a.src == nil {
		return 0
	}
	return a.src.Live()
}

// sweepSources lets go of the scripts' runtimes not asked for in a while,
// as the app sits idle: a runtime holds its script's heap, which is mostly
// what the scripts cost, and a listener playing another source's songs has
// no use for them. A script asked again is started again, in a few
// milliseconds, by the chain as it plays a song.
func (a *app) sweepSources() {
	if a.src == nil {
		return
	}
	go func() {
		if a.src.Sweep() == 0 {
			return
		}
		// A runtime let go leaves its script's heap behind in the Go heap,
		// which the runtime keeps for the next allocation rather than
		// handing back: a sweep that freed a runtime gives the memory to
		// the system, so that an idle app is the size it looks. The
		// settings then count the runtimes that are left, and are drawn
		// again.
		debug.FreeOSMemory()
		a.post(func() {
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
		})
	}()
}

// sweepEvery is how often the scripts' idle runtimes are looked over: the
// sweep keeps a runtime for ninety seconds of quiet, so a pass every ten
// seconds lets go of one soon after that, rather than up to a minute later.
const sweepEvery = 10 * time.Second

// keepSweeping sweeps the scripts' runtimes now and then, until the app
// quits. It walks no window, so it runs on a timer rather than a frame,
// and it is the skin of the app's own goroutine: the sweep stops an idle
// script, which waits for that script's own goroutine to end.
func (a *app) keepSweeping() {
	go func() {
		t := time.NewTicker(sweepEvery)
		defer t.Stop()
		for range t.C {
			a.sweepSources()
		}
	}()
}

// formatInterval writes a length as mm:ss, as source scripts read it.
func formatInterval(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	secs := ms / 1000
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}
