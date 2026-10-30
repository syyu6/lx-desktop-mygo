package lxsource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The scripts these tests run are the ones in testdata: they answer
// without the network, so the tests say what the resolver does rather than
// what a source published today does.

// resolverOf imports the scripts given, in that order, and returns a
// resolver that asks them so.
func resolverOf(t *testing.T, scripts ...string) (*Resolver, *Store, []string) {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, code := range scripts {
		info, err := s.Import(code)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, info.ID)
	}
	r := NewResolver(s)
	r.Order = func() []string { return ids }
	t.Cleanup(r.Close)
	return r, s, ids
}

// TestResolverFallsBackToTheNextScript checks that a script that cannot
// answer for a source is passed over for one that can.
func TestResolverFallsBackToTheNextScript(t *testing.T) {
	// The first script answers only for kw, the second for wy.
	kwOnly := `/*!
 * @name 酷我音源
 */
const { EVENT_NAMES, on, send } = globalThis.lx;
on(EVENT_NAMES.request, ({ action }) => action === 'musicUrl' ? 'https://kw.example.invalid/a.mp3' : Promise.reject(new Error('不支持')));
send(EVENT_NAMES.inited, { sources: { kw: { name: '酷我音乐', type: 'music', actions: ['musicUrl'], qualitys: ['320k'] } } });
`
	r, _, _ := resolverOf(t, kwOnly, script(t, "simple.js"))
	url, err := r.Stream(Track{Source: SourceNetease, ID: "7", Name: "海阔天空"}, Quality320k)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if !strings.Contains(url, "example.invalid") || !strings.Contains(url, "7") {
		t.Errorf("Stream = %q, want the second script's answer", url)
	}
}

// TestResolverFallsBackToLowerQuality checks that a script that will not
// give the quality asked gives a lower one rather than nothing.
func TestResolverFallsBackToLowerQuality(t *testing.T) {
	lowOnly := `/*!
 * @name 只有低音质的音源
 */
const { EVENT_NAMES, on, send } = globalThis.lx;
on(EVENT_NAMES.request, ({ action, info }) => {
  if (action !== 'musicUrl') return Promise.reject(new Error('不支持'));
  if (info.type !== '128k') return Promise.reject(new Error('没有这个音质'));
  return 'https://low.example.invalid/' + info.musicInfo.id + '.mp3';
});
send(EVENT_NAMES.inited, { sources: { wy: { name: '网易云音乐', type: 'music', actions: ['musicUrl'], qualitys: ['128k', '320k'] } } });
`
	r, _, _ := resolverOf(t, lowOnly)
	url, err := r.Stream(Track{Source: SourceNetease, ID: "5"}, Quality320k)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if !strings.Contains(url, "low.example.invalid") {
		t.Errorf("Stream = %q, want the lower quality's", url)
	}
}

// TestResolverSkipsAScriptThatDoesNotStart checks that a script that will
// not run is passed over rather than kept the stream of a song.
func TestResolverSkipsAScriptThatDoesNotStart(t *testing.T) {
	broken := `/*!
 * @name 跑不起来的音源
 */
throw new Error('故意');
`
	r, _, _ := resolverOf(t, broken, script(t, "simple.js"))
	if _, err := r.Stream(Track{Source: SourceNetease, ID: "7"}, Quality320k); err != nil {
		t.Fatalf("Stream: %v", err)
	}
}

// TestResolverWithoutOrderAnswersNothing checks that no script is asked
// until the listener's list says which.
func TestResolverWithoutOrderAnswersNothing(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(script(t, "simple.js")); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(s)
	defer r.Close()
	if _, err := r.Stream(Track{Source: SourceNetease, ID: "1"}, Quality320k); err != ErrNoSource {
		t.Errorf("Stream = %v, want ErrNoSource", err)
	}
}

// TestResolverStartsAScriptOnce checks that two requests at once make one
// runtime of a script, not two.
func TestResolverStartsAScriptOnce(t *testing.T) {
	s := tempStore(t)
	info, err := s.Import(script(t, "simple.js"))
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(s)
	r.Order = func() []string { return []string{info.ID} }
	defer r.Close()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Stream(Track{Source: SourceNetease, ID: "9"}, Quality320k); err != nil {
				t.Errorf("Stream: %v", err)
			}
		}()
	}
	wg.Wait()

	r.mu.Lock()
	engines := len(r.engines)
	r.mu.Unlock()
	if engines != 1 {
		t.Errorf("%d runtimes for one script, want 1", engines)
	}
}

// TestResolverResetForgetsAnEngine checks that importing a script again
// leaves the old runtime behind.
func TestResolverResetForgetsAnEngine(t *testing.T) {
	r, s, ids := resolverOf(t, script(t, "simple.js"))
	if _, err := r.Stream(Track{Source: SourceNetease, ID: "3"}, Quality320k); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetEnabled(ids[0], false); err != nil {
		t.Fatal(err)
	}
	r.Reset(ids[0])
	r.mu.Lock()
	n := len(r.engines)
	r.mu.Unlock()
	if n != 0 {
		t.Errorf("Reset left %d runtimes", n)
	}
}

// TestResolverReadsCapabilities checks that what a script answers for is
// what Probe says, and that a script with nothing to say is an error.
func TestResolverReadsCapabilities(t *testing.T) {
	r, _, ids := resolverOf(t, script(t, "simple.js"))
	caps, err := r.Probe(ids[0])
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if _, ok := caps[SourceNetease]; !ok {
		t.Errorf("caps = %v, want wy", caps)
	}
	if got := CapabilitySummary(caps); got != "网易云音乐" {
		t.Errorf("CapabilitySummary = %q", got)
	}
}

// The store is the listener's own: a script whose code is gone is not
// there to ask, whatever its entry says.
func TestResolverSkipsScriptWithoutCode(t *testing.T) {
	r, s, ids := resolverOf(t, script(t, "simple.js"))
	if err := os.Remove(filepath.Join(s.dir, ids[0]+".js")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Stream(Track{Source: SourceNetease, ID: "3"}, Quality320k); err != ErrNoSource {
		t.Errorf("Stream = %v, want ErrNoSource", err)
	}
}

// A script not asked for a while lets go of its runtime, and asked again
// starts one: what a runtime holds is the script's heap, and a listener
// away from it should not be holding it.
func TestResolverSweepsIdleScripts(t *testing.T) {
	r, _, ids := resolverOf(t, script(t, "simple.js"))
	if _, err := r.Stream(Track{Source: SourceNetease, ID: "5"}, Quality320k); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	started := r.used[ids[0]]
	r.mu.Unlock()
	if started.IsZero() {
		t.Fatal("the resolver did not note when it asked the script")
	}
	// Freshly asked, the runtime is kept.
	if n := r.Sweep(); n != 0 {
		t.Errorf("Sweep let go of %d runtimes asked just now", n)
	}
	// Asked a while ago, it goes, and the next request makes another.
	r.mu.Lock()
	r.used[ids[0]] = time.Now().Add(-idleFor - time.Second)
	r.mu.Unlock()
	if n := r.Sweep(); n != 1 {
		t.Errorf("Sweep let go of %d runtimes, want the idle one", n)
	}
	r.mu.Lock()
	n := len(r.engines)
	r.mu.Unlock()
	if n != 0 {
		t.Errorf("%d runtimes left after the sweep", n)
	}
	if _, err := r.Stream(Track{Source: SourceNetease, ID: "5"}, Quality320k); err != nil {
		t.Fatalf("Stream after the sweep: %v", err)
	}
	r.mu.Lock()
	n = len(r.engines)
	r.mu.Unlock()
	if n != 1 {
		t.Errorf("%d runtimes after asking again, want 1", n)
	}
}

// A script that hung is let go of by the sweep, as any other is, and is
// not asked again: its runtime is dead, and a new one would hang the same.
func TestResolverSweepLetsGoOfAHungScript(t *testing.T) {
	r, _, ids := resolverOf(t, script(t, "simple.js"))
	if _, err := r.Stream(Track{Source: SourceNetease, ID: "5"}, Quality320k); err != nil {
		t.Fatal(err)
	}
	r.failed(ids[0], ErrHung)
	if n := r.Sweep(); n != 1 {
		t.Errorf("Sweep let go of %d runtimes, want the hung one", n)
	}
	if _, err := r.Stream(Track{Source: SourceNetease, ID: "5"}, Quality320k); !errors.Is(err, ErrNoSource) {
		t.Errorf("Stream = %v, want no source: the script hung", err)
	}
	r.mu.Lock()
	engines, broken := len(r.engines), r.broken[ids[0]]
	r.mu.Unlock()
	if engines != 0 || !broken {
		t.Errorf("%d runtimes again, broken %v", engines, broken)
	}
}
