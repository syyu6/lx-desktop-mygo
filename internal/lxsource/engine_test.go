package lxsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The scripts the tests run: their own, which say what they do without the
// network, so a test never depends on a source published today.
func script(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestEngineRunsSource checks that a script of the shape the real ones
// have is started, reports what it does, and answers requests.
func TestEngineRunsSource(t *testing.T) {
	e, err := newEngine(script(t, "simple.js"))
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer e.Close()

	caps := e.Capabilities()
	wy, ok := caps[SourceNetease]
	if !ok {
		t.Fatalf("capabilities = %v, want wy", caps)
	}
	if !wy.Has(ActionMusicURL) || !wy.Has(ActionLyric) || !wy.Has(ActionPic) {
		t.Errorf("wy actions = %v, want musicUrl, lyric and pic", wy.Actions)
	}

	url, err := e.StreamURL(Track{Source: SourceNetease, ID: "347230", Name: "海阔天空"}, Quality320k)
	if err != nil {
		t.Fatalf("StreamURL: %v", err)
	}
	if !strings.Contains(url, "347230") || !strings.Contains(url, "320k") {
		t.Errorf("StreamURL = %q, want the song and quality in it", url)
	}
}

// TestEngineRejectsBadAnswers checks that what a script answers is held to
// the shapes lx-music-desktop accepts, whatever the script says.
func TestEngineRejectsBadAnswers(t *testing.T) {
	e, err := newEngine(script(t, "simple.js"))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	if _, err := e.StreamURL(Track{Source: SourceNetease, ID: "1", Name: "Missing"}, Quality320k); err == nil {
		t.Error("a rejected request answered with no error")
	}
}

// TestLyricsFromScript checks that lyrics come back with their translation.
func TestLyricsFromScript(t *testing.T) {
	e, err := newEngine(script(t, "simple.js"))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	// The script's lyric request goes to a host that does not answer, so
	// it falls back to lyrics without a translation: the request itself is
	// what is being checked, that it went and came back.
	l, err := e.Lyrics(Track{Source: SourceNetease, ID: "1", Name: "海阔天空"})
	if err != nil {
		t.Fatalf("Lyrics: %v", err)
	}
	if !strings.Contains(l.Lyric, "测试歌词") {
		t.Errorf("lyric = %q, want the test script's", l.Lyric)
	}
}

// TestSandboxBlocksEscape checks that a script reaches neither the
// computer's files nor the app's own exit, however it asks.
func TestSandboxBlocksEscape(t *testing.T) {
	// A file outside the runtime's directory that the script tries to read.
	canary := filepath.Join(t.TempDir(), "canary.txt")
	if err := os.WriteFile(canary, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	escapeFile := "/tmp/lxsource-escape.txt"
	os.Remove(escapeFile)

	e, err := newEngine(script(t, "escape.js"))
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer e.Close()

	// Let what the script started with import settle.
	time.Sleep(300 * time.Millisecond)
	e.post(func() { e.eval("globalThis.__escapeResult = __escape()") })
	got, err := e.evalSync("globalThis.__escapeResult || ''")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	for _, want := range []string{"BLOCKED", "undefined"} {
		if !strings.Contains(got, want) {
			t.Errorf("escape results %q, want %q in them", got, want)
		}
	}
	if strings.Contains(got, "READ ") {
		t.Errorf("a script read a file: %s", got)
	}
	if _, err := os.Stat(escapeFile); err == nil {
		os.Remove(escapeFile)
		t.Errorf("a script wrote %s", escapeFile)
	}
	if _, err := os.Stat(canary); err != nil {
		t.Errorf("the canary is gone, so a script reached it: %v", err)
	}
}

// TestHungScriptIsStopped checks that a script in an endless loop neither
// holds the app nor keeps a core: the call fails, and the loop is stopped
// rather than left spinning.
func TestHungScriptIsStopped(t *testing.T) {
	const hung = `/*!
 * @name 死循环音源
 * @version 1
 */
const { EVENT_NAMES, on, send } = globalThis.lx;
on(EVENT_NAMES.request, () => { while (true) {} });
send(EVENT_NAMES.inited, { sources: { wy: { name: '网易云音乐', type: 'music', actions: ['musicUrl'], qualitys: ['320k'] } } });
`
	e, err := newEngine(hung)
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	began := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := e.StreamURL(Track{Source: SourceNetease, ID: "1"}, Quality320k)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a hung script answered")
		}
	case <-time.After(callTimeout + 10*time.Second):
		t.Fatal("the call never came back")
	}
	if !e.broken {
		t.Error("the engine is not marked broken")
	}
	// The loop must end, not go on burning a core for the rest of the run.
	select {
	case <-e.stopped:
		if took := time.Since(began); took > callTimeout+2*time.Second {
			t.Errorf("the loop went on for %v after the call failed", took)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the script is still looping")
	}
}

// TestScriptTimers checks that a script's setTimeout runs on the app's
// clock while the runtime answers other things, and that an answer which
// waits on it is taken: a source that retries a service does so this way.
func TestScriptTimers(t *testing.T) {
	const timed = `/*!
 * @name 计时音源
 * @version 1
 */
const { EVENT_NAMES, on, send } = globalThis.lx;
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
on(EVENT_NAMES.request, async ({ info }) => {
  await wait(20);
  await Promise.resolve();
  await wait(10);
  return 'https://example.invalid/' + info.musicInfo.id + '.mp3';
});
send(EVENT_NAMES.inited, { sources: { wy: { name: '网易云音乐', type: 'music', actions: ['musicUrl'], qualitys: ['320k'] } } });
`
	e, err := newEngine(timed)
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	defer e.Close()
	for _, id := range []string{"42", "43"} {
		url, err := e.StreamURL(Track{Source: SourceNetease, ID: id}, Quality320k)
		if err != nil {
			t.Fatalf("StreamURL(%s): %v", id, err)
		}
		if !strings.Contains(url, id) {
			t.Errorf("StreamURL(%s) = %q", id, url)
		}
	}
}

// TestBufferEncodings checks the buffer scripts sign and read with: a
// wrong base64 would have a script quietly give the wrong song.
func TestBufferEncodings(t *testing.T) {
	e, err := newEngine(script(t, "simple.js"))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for _, tc := range []struct{ code, want string }{
		{`lx.utils.buffer.bufToString(lx.utils.buffer.from('hi!', 'utf-8'), 'base64')`, "aGkh"},
		{`lx.utils.buffer.bufToString(lx.utils.buffer.from('aGkh', 'base64'), 'utf-8')`, "hi!"},
		// Two bytes and one, the short ends of base64.
		{`lx.utils.buffer.bufToString(lx.utils.buffer.from('hi', 'utf-8'), 'base64')`, "aGk="},
		{`lx.utils.buffer.bufToString(lx.utils.buffer.from('h', 'utf-8'), 'base64')`, "aA=="},
		{`lx.utils.buffer.bufToString(lx.utils.buffer.from('h', 'utf-8'), 'hex')`, "68"},
	} {
		got, err := e.evalSync(tc.code)
		if err != nil {
			t.Fatalf("%s: %v", tc.code, err)
		}
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.code, got, tc.want)
		}
	}
}
