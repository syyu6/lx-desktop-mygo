package lxsource

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modernc.org/quickjs"
)

// An Engine runs one script.
//
// The script runs in QuickJS, the C library compiled to pure Go by
// modernc.org/libquickjs, so it has no syscall of its own, the app still
// builds with CGO_ENABLED=0, and nothing has to be compiled first: a
// runtime is up in a fraction of a millisecond. A bare runtime reaches no
// filesystem at all, and its module loader is left unset, so an import of
// anything — `qjs:std`, `fs`, a path — is refused by the engine rather than
// read from the computer.
//
// QuickJS is not goroutine-safe. Its runtime can be interrupted, though,
// from any goroutine, and the interrupt ends whatever the script is in, loop
// or no loop, leaving the runtime usable afterwards. So an Engine owns one
// goroutine, and every call into the runtime happens there. A call that
// does not come back in time interrupts the runtime and poisons the Engine
// with ErrHung; its owner makes another.
type Engine struct {
	// vm is the runtime. Only the engine's own goroutine touches it; Close
	// interrupts it from elsewhere, which the runtime allows.
	vm   *quickjs.VM
	http *http.Client
	jobs chan func()
	// info is what the script's header says about it, which lx hands the
	// script back as currentScriptInfo.
	info Info
	// script is the script's own text, which currentScriptInfo.rawScript is.
	script string

	closeOnce sync.Once
	closed    chan struct{}
	stopped   chan struct{} // the loop has ended, and freed the runtime
	// broken says a call hung: the runtime is somewhere in the script's
	// own loop, and must not be touched again.
	broken bool

	// caps is what the script said it does, once it has said it, and
	// ready closes then.
	capsMu sync.Mutex
	caps   map[Source]Capability
	ready  chan struct{}

	// seq numbers the callbacks the script gives and the calls made to it.
	seq int64
	// calls holds where each call's answer goes, by its id.
	callsMu sync.Mutex
	calls   map[int64]chan result
}

// result is what a call into the script came back with.
type result struct {
	value json.RawMessage
	err   error
}

// ErrHung is returned by a call the script did not answer in time. The
// Engine is unusable after it.
var ErrHung = errors.New("音源脚本没有响应")

// ErrClosed is returned for a call on an engine that was closed.
var ErrClosed = errors.New("音源脚本已停止")

// The limits one script runs under.
const (
	// engineMemory is the heap the script and what it holds share: a
	// source script needs a few megabytes.
	engineMemory = 48 << 20
	// engineStack is how deep its calls may go, counted in stack slots,
	// about one per Javascript call. Deeper than this the engine throws a
	// catchable "stack overflow" rather than overflowing Go's own stack.
	engineStack = 4000
	// engineEvalTimeout bounds a single entry into the script, whatever
	// it is doing. It is a backstop, well past the timeouts below, which
	// are what an engine is judged broken by: a script that runs this long
	// without them noticing is not answering anything anyway.
	engineEvalTimeout = 60 * time.Second
	// initTimeout is how long a script has to say what it does: a script
	// that reaches the network as it starts waits for it.
	initTimeout = 20 * time.Second
	// callTimeout is how long it has to answer one request. A script that
	// answers by asking a service of its own waits for that service.
	callTimeout = 12 * time.Second
	// requestTimeout is how long a request of a script's may take: it
	// leaves the answer time to be read and given back.
	requestTimeout = 8 * time.Second
	// maxResponse is the largest answer a request of the script's may
	// bring back. It crosses into the runtime as a string and is parsed
	// there, so an answer this large is already several copies of itself
	// in the heap. A source's playlists and its song URLs are far under
	// it; an answer over it is refused rather than cut, since half of a
	// JSON body is not one.
	maxResponse = 2 << 20
	// maxAnswer is the largest answer the script may give to a call.
	maxAnswer = 64 << 10
)

// userAgent is what the app's requests call themselves: the source APIs
// expect a browser.
const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

// newEngine starts a runtime, walls it in, runs the script, and waits for
// it to say what it does.
func newEngine(script string) (*Engine, error) {
	vm, err := quickjs.NewVM()
	if err != nil {
		return nil, fmt.Errorf("启动脚本运行时: %w", err)
	}
	vm.SetMemoryLimit(engineMemory)
	vm.SetMaxStackSize(engineStack)
	// The eval timeout bounds each entry into the script from Go, so a
	// script that loops as it starts ends in initTimeout rather than
	// spinning until the engine is closed. Once it has said what it does,
	// the longer backstop takes over.
	vm.SetEvalTimeout(initTimeout)

	e := &Engine{
		vm:      vm,
		http:    &http.Client{Timeout: requestTimeout},
		jobs:    make(chan func(), 32),
		closed:  make(chan struct{}),
		stopped: make(chan struct{}),
		ready:   make(chan struct{}),
		calls:   map[int64]chan result{},
		script:  script,
	}
	// The header is what the script says about itself; a script that does
	// not parse is left to say so when it is run.
	e.info, _ = ParseScript(script)
	started := make(chan error, 1)
	go e.loop(script, started)
	select {
	case err := <-started:
		if err != nil {
			e.Close()
			return nil, err
		}
		return e, nil
	case <-time.After(initTimeout):
		e.Close()
		return nil, fmt.Errorf("音源脚本 %v 没有就绪", initTimeout)
	}
}

// loop is the engine's goroutine: it sets the runtime up, runs the script,
// serves the calls made to it, and owns the runtime to its end. Nothing
// else touches the runtime, so nothing else frees it either.
func (e *Engine) loop(script string, started chan<- error) {
	defer close(e.stopped)
	defer func() {
		// A runtime panics out of a call it was abandoned in, which is no
		// reason to take the app down. The engine is done either way.
		if r := recover(); r != nil {
			e.markBroken()
			select {
			case started <- ErrHung:
			default:
			}
		}
		e.vm.Close()
	}()

	if err := e.setUp(); err != nil {
		started <- err
		return
	}
	if err := e.eval(script); err != nil {
		started <- fmt.Errorf("音源脚本运行出错: %w", err)
		return
	}
	// A script that says what it does without waiting has said it by now;
	// one that waits has its timers and its promises settle below.
	e.drain()
	if !e.isReady() {
		deadline := time.After(initTimeout)
		for !e.isReady() {
			select {
			case job := <-e.jobs:
				job()
				// A call that hung interrupted the script: it will never
				// say what it does now.
				if e.isBroken() {
					started <- ErrHung
					return
				}
				e.drain()
			case <-deadline:
				if e.isBroken() {
					started <- ErrHung
				} else {
					started <- errors.New("脚本没有说明它支持哪些音源")
				}
				return
			case <-e.closed:
				started <- ErrClosed
				return
			}
		}
	}
	started <- nil
	// It has said what it does: from now on the timeouts around each call
	// are what bound it, and this is only the backstop.
	e.vm.SetEvalTimeout(engineEvalTimeout)
	for {
		select {
		case job := <-e.jobs:
			job()
			// A call that hung interrupted the script from another
			// goroutine: what it was in has ended, and the engine is done.
			if e.isBroken() {
				return
			}
			e.drain()
		case <-e.closed:
			return
		}
	}
}

// drain settles what the script queued: the runtime's pending promises run
// to their end.
func (e *Engine) drain() {
	if e.isBroken() {
		return
	}
	// A script's rejected promise is its own business: it is reported
	// where it was awaited.
	_, _ = e.vm.ExecutePendingJobs()
}

// setUp gives the script what a page running one would have: timers, the
// network through this app, and lx's own object.
//
// A bare runtime has none of QuickJS's demo globals — no `std`, `os`,
// `print` or `console` — and no module loader, so there is nothing of the
// computer's to take away. The console is put back as a no-op: scripts log
// what they are doing, and a missing console would end them instead.
func (e *Engine) setUp() error {
	if err := e.eval(`
		globalThis.console = new Proxy({}, { get: function () { return function () {} } });
		globalThis.__lx_timers = {};
		globalThis.__lx_httpCallbacks = {};
		globalThis.__lx_nextTimer = 1;
		globalThis.__lx_nextHttp = 1;
		// The engine leaves queueMicrotask out of a bare runtime; a promise
		// reaction is the same thing.
		if (typeof globalThis.queueMicrotask !== 'function') {
			globalThis.queueMicrotask = function (fn) { Promise.resolve().then(fn) };
		}
	`); err != nil {
		return err
	}

	// Timers run on the app's clock: a script that waits must not keep
	// its runtime from answering. The function to call stays on the JS
	// side, in `__lx_timers`, so Go holds nothing of the runtime.
	if err := e.vm.RegisterHostFunc("__lx_scheduleTimer", func(args []any) (any, error) {
		id := argInt(args, 0)
		ms := argInt(args, 1)
		if ms < 0 || ms > 60_000 {
			ms = 60_000
		}
		time.AfterFunc(time.Duration(ms)*time.Millisecond, func() {
			e.post(func() {
				e.eval("__lx_fireTimer(" + itoa(id) + ")")
			})
		})
		return nil, nil
	}); err != nil {
		return err
	}

	// The script's requests go out from Go: it reaches the network no
	// other way.
	if err := e.vm.RegisterHostFunc("__lx_fetch", func(args []any) (any, error) {
		go e.fetch(argInt(args, 0), argString(args, 1), argString(args, 2))
		return nil, nil
	}); err != nil {
		return err
	}

	if err := e.vm.RegisterHostFunc("__lx_md5", func(args []any) (any, error) {
		sum := md5.Sum([]byte(argString(args, 0)))
		return hex.EncodeToString(sum[:]), nil
	}); err != nil {
		return err
	}

	// A call's answer comes back here, from the promise the script gave.
	if err := e.vm.RegisterHostFunc("__lx_settle", func(args []any) (any, error) {
		e.settle(argInt(args, 0), result{value: json.RawMessage(argString(args, 1))})
		return nil, nil
	}); err != nil {
		return err
	}
	if err := e.vm.RegisterHostFunc("__lx_fail", func(args []any) (any, error) {
		e.settle(argInt(args, 0), result{err: errors.New(argString(args, 1))})
		return nil, nil
	}); err != nil {
		return err
	}

	// And what the script says it does, which is what makes it ready.
	if err := e.vm.RegisterHostFunc("__lx_inited", func(args []any) (any, error) {
		caps := readCapabilities(argString(args, 0))
		if caps == nil {
			return nil, nil
		}
		e.capsMu.Lock()
		if e.caps == nil {
			e.caps = caps
			close(e.ready)
		}
		e.capsMu.Unlock()
		return nil, nil
	}); err != nil {
		return err
	}

	return e.eval(`
		var __lx_info = ` + e.infoLiteral() + `;
		globalThis.setTimeout = function (fn, ms) {
			if (typeof fn !== 'function') return 0;
			var id = globalThis.__lx_nextTimer++;
			__lx_timers[id] = fn;
			__lx_scheduleTimer(id, ms || 0);
			return id;
		};
		globalThis.clearTimeout = function (id) { delete __lx_timers[id] };
		globalThis.setInterval = function () { return 0 };
		globalThis.clearInterval = function () {};

		globalThis.__lx_fireTimer = function (id) {
			var fn = __lx_timers[id];
			delete __lx_timers[id];
			if (typeof fn === 'function') { try { fn() } catch (e) {} }
		};
		// Everything crossing into Go crosses as a string.
		globalThis.__lx_json = function (v) {
			try { return JSON.stringify(v === undefined ? null : v) } catch (e) { return 'null' }
		};
		globalThis.__lx_deliver = function (id, errMsg, status, raw) {
			var cb = __lx_httpCallbacks[id];
			delete __lx_httpCallbacks[id];
			if (typeof cb !== 'function') return;
			if (errMsg) { try { cb(new Error(errMsg), null, null) } catch (e) {} return }
			var body = raw;
			try { body = JSON.parse(raw) } catch (e) {}
			try { cb(null, { statusCode: status, statusMessage: '', headers: {}, body: body, raw: raw }, body) } catch (e) {}
		};

		// A call into the script: it gets lx's shape of arguments, and its
		// promise's outcome goes back as JSON.
		globalThis.__lx_call = function (id, reqJSON) {
			var req;
			try { req = JSON.parse(reqJSON) } catch (e) { __lx_fail(id, '请求格式错误'); return }
			if (typeof __lx_handler !== 'function') { __lx_fail(id, '音源脚本没有处理请求'); return }
			var out;
			try { out = __lx_handler({ source: req.source, action: req.action, info: req.info }) }
			catch (e) { __lx_fail(id, (e && e.message) || String(e)); return }
			Promise.resolve(out).then(function (v) {
				var text;
				try { text = JSON.stringify(v === undefined ? null : v) }
				catch (e) { __lx_fail(id, '返回值无法序列化'); return }
				if (typeof text !== 'string') { __lx_fail(id, '返回值无法序列化'); return }
				__lx_settle(id, text);
			}, function (e) {
				__lx_fail(id, (e && e.message) || String(e));
			});
		};

		// The buffer a script gets is a string: what a source's requests
		// bring back is text. Its encodings are utf-8 and base64, which
		// scripts use to sign and to read what a service sends.
		globalThis.__lx_fromBuffer = function (v, enc) {
			if (typeof v !== 'string') { return v }
			if (enc === 'base64') { return __lx_b64decode(v) }
			return v;
		};
		globalThis.__lx_bufToString = function (b, enc) {
			var s = typeof b === 'string' ? b : String(b);
			if (enc === 'base64') { return __lx_b64encode(s) }
			if (enc === 'hex') {
				var out = '';
				for (var i = 0; i < s.length; i++) {
					var c = s.charCodeAt(i).toString(16);
					out += c.length === 1 ? '0' + c : c;
				}
				return out;
			}
			return s;
		};
		globalThis.__lx_b64encode = function (s) {
			var chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
			var out = '', i;
			for (i = 0; i + 2 < s.length; i += 3) {
				var n = (s.charCodeAt(i) << 16) | (s.charCodeAt(i + 1) << 8) | s.charCodeAt(i + 2);
				out += chars[(n >> 18) & 63] + chars[(n >> 12) & 63] + chars[(n >> 6) & 63] + chars[n & 63];
			}
			var rest = s.length - i;
			if (rest === 1) {
				var a = s.charCodeAt(i) << 16;
				out += chars[(a >> 18) & 63] + chars[(a >> 12) & 63] + '==';
			} else if (rest === 2) {
				var b = (s.charCodeAt(i) << 16) | (s.charCodeAt(i + 1) << 8);
				out += chars[(b >> 18) & 63] + chars[(b >> 12) & 63] + chars[(b >> 6) & 63] + '=';
			}
			return out;
		};
		globalThis.__lx_b64decode = function (s) {
			var chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
			var clean = String(s).replace(/[^A-Za-z0-9+/]/g, '');
			var out = '', i;
			for (i = 0; i + 3 < clean.length; i += 4) {
				var n = (chars.indexOf(clean[i]) << 18) | (chars.indexOf(clean[i + 1]) << 12) |
					(chars.indexOf(clean[i + 2]) << 6) | chars.indexOf(clean[i + 3]);
				out += String.fromCharCode((n >> 16) & 255, (n >> 8) & 255, n & 255);
			}
			var rest = clean.length - i;
			if (rest === 2) {
				var a = (chars.indexOf(clean[i]) << 18) | (chars.indexOf(clean[i + 1]) << 12);
				out += String.fromCharCode((a >> 16) & 255);
			} else if (rest === 3) {
				var b = (chars.indexOf(clean[i]) << 18) | (chars.indexOf(clean[i + 1]) << 12) | (chars.indexOf(clean[i + 2]) << 6);
				out += String.fromCharCode((b >> 16) & 255, (b >> 8) & 255);
			}
			return out;
		};

		globalThis.lx = {
			EVENT_NAMES: { request: 'request', inited: 'inited', updateAlert: 'updateAlert' },
			version: '2.0.0',
			env: 'desktop',
			currentScriptInfo: __lx_info,
			on: function (name, fn) {
				if (name === 'request' && typeof fn === 'function') { globalThis.__lx_handler = fn }
				return Promise.resolve()
			},
			send: function (name, data) {
				if (name === 'inited') { __lx_inited(__lx_json(data)) }
				return Promise.resolve();
			},
			request: function (url, opts, cb) {
				var id = globalThis.__lx_nextHttp++;
				__lx_httpCallbacks[id] = cb;
				__lx_fetch(id, String(url), __lx_json(opts || {}));
				return function () { delete __lx_httpCallbacks[id] };
			},
			utils: {
				crypto: { md5: function (s) { return __lx_md5(String(s)) } },
				buffer: {
					// A source's requests come back as text, so this buffer
					// holds a string. Its encodings are the ones scripts
					// use: utf-8 and base64.
					from: function (v, enc) { return __lx_fromBuffer(v, enc) },
					bufToString: function (b, enc) { return __lx_bufToString(b, enc) }
				},
				zlib: {
					inflate: function (b) { return Promise.reject(new Error('不支持')) },
					deflate: function (b) { return Promise.reject(new Error('不支持')) }
				}
			}
		};
	`)
}

// fetch makes the script's request and hands what it says back to it.
func (e *Engine) fetch(id int64, url string, optsJSON string) {
	var opts struct {
		Method  string            `json:"method"`
		Timeout int               `json:"timeout"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	json.Unmarshal([]byte(optsJSON), &opts)
	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	switch method {
	case "":
		method = http.MethodGet
	case http.MethodGet, http.MethodPost:
	default:
		e.deliver(id, "不支持的请求方法", 0, "")
		return
	}
	timeout := time.Duration(opts.Timeout) * time.Millisecond
	if timeout <= 0 || timeout > requestTimeout {
		timeout = requestTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var payload io.Reader
	if opts.Body != "" {
		payload = strings.NewReader(opts.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, payload)
	if err != nil {
		e.deliver(id, err.Error(), 0, "")
		return
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	res, err := e.http.Do(req)
	if err != nil {
		e.deliver(id, err.Error(), 0, "")
		return
	}
	defer res.Body.Close()
	// The body is read straight into the string that crosses into the
	// runtime, rather than into bytes and copied: a response of a
	// megabyte or two is the largest thing a script's call holds at once.
	var body strings.Builder
	if _, err := io.Copy(&body, io.LimitReader(res.Body, maxResponse+1)); err != nil {
		e.deliver(id, err.Error(), 0, "")
		return
	}
	if body.Len() > maxResponse {
		// A body cut short would reach the script as a string that does
		// not parse, and the script would read a song's answer as text.
		e.deliver(id, "响应超过 2 MB，没有交给脚本", 0, "")
		return
	}
	e.deliver(id, "", res.StatusCode, body.String())
}

// deliver hands a request's outcome back to the script.
//
// The body is passed as a string the engine converts itself, rather than
// written into JavaScript source: a response of a megabyte or two would
// otherwise be escaped into a literal and parsed again, which is two more
// copies of it in the heap at the moment it arrives.
func (e *Engine) deliver(id int64, errMsg string, status int, raw string) {
	e.post(func() {
		_, _ = e.vm.Call("__lx_deliver", id, errMsg, status, raw)
	})
}

// call asks the script to do action for source, and waits for its answer.
func (e *Engine) call(action Action, req map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	id := e.nextID()
	ch := make(chan result, 1)
	e.callsMu.Lock()
	e.calls[id] = ch
	e.callsMu.Unlock()
	defer func() {
		e.callsMu.Lock()
		delete(e.calls, id)
		e.callsMu.Unlock()
	}()
	// The request goes in as a string the engine converts, not as source:
	// what a song's request carries is small, but every copy of it counts
	// at the moment a song loads.
	if err := e.postWait(func() {
		_, _ = e.vm.Call("__lx_call", id, string(body))
	}, callTimeout); err != nil {
		return nil, err
	}
	select {
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		if len(res.value) > maxAnswer {
			return nil, errors.New("音源脚本的返回值太大")
		}
		return res.value, nil
	case <-time.After(callTimeout):
		e.hang()
		return nil, ErrHung
	case <-e.closed:
		return nil, ErrClosed
	}
}

// hang stops a script that did not answer in time: an interrupt ends
// whatever it is in, loop or no loop, at the next check the engine makes,
// which is a moment away. The engine is done with it either way, and its
// goroutine frees the runtime.
func (e *Engine) hang() {
	e.markBroken()
	e.vm.Interrupt()
}

// settle gives a call its answer.
func (e *Engine) settle(id int64, res result) {
	e.callsMu.Lock()
	ch := e.calls[id]
	e.callsMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- res:
	default:
	}
}

// post runs fn on the engine's goroutine, unless the engine is closed or its
// goroutine has ended, as it does after a call hangs.
func (e *Engine) post(fn func()) {
	select {
	case e.jobs <- fn:
	case <-e.closed:
	case <-e.stopped:
	}
}

// postWait waits for the engine's goroutine to take fn, so that a call
// made before the loop started is not lost.
func (e *Engine) postWait(fn func(), within time.Duration) error {
	select {
	case e.jobs <- fn:
		return nil
	case <-e.closed:
		return ErrClosed
	case <-e.stopped:
		return ErrHung
	case <-time.After(within):
		return ErrHung
	}
}

// eval runs code in the runtime. It is called only on the engine's
// goroutine.
func (e *Engine) eval(code string) error {
	_, err := e.vm.Eval(code, quickjs.EvalGlobal)
	return err
}

// evalSync runs code in the runtime from another goroutine, and returns
// what it came to as a string. Tests use it to look into a script's own
// state; nothing of the app does.
func (e *Engine) evalSync(code string) (string, error) {
	var out string
	err := e.onMain(func() error {
		v, err := e.vm.Eval(code, quickjs.EvalGlobal)
		if err != nil {
			return err
		}
		out = goText(v)
		return nil
	}, 5*time.Second)
	return out, err
}

// onMain runs fn on the engine's goroutine and waits for it.
func (e *Engine) onMain(fn func() error, within time.Duration) error {
	if e.isBroken() {
		return ErrHung
	}
	done := make(chan error, 1)
	select {
	case e.jobs <- func() { done <- fn() }:
	case <-e.closed:
		return ErrClosed
	case <-e.stopped:
		return ErrHung
	case <-time.After(within):
		return ErrHung
	}
	select {
	case err := <-done:
		return err
	case <-e.closed:
		return ErrClosed
	case <-e.stopped:
		return ErrHung
	case <-time.After(within):
		e.hang()
		return ErrHung
	}
}

// nextID numbers the callbacks and calls an engine keeps. Requests are made
// from several goroutines at once — a song asks for its stream, its words
// and its cover together — so the count is atomic.
func (e *Engine) nextID() int64 {
	return atomic.AddInt64(&e.seq, 1)
}

// isReady reports whether the script has said what it does.
func (e *Engine) isReady() bool {
	e.capsMu.Lock()
	defer e.capsMu.Unlock()
	return e.caps != nil
}

// Capabilities returns what the script does, by source.
func (e *Engine) Capabilities() map[Source]Capability {
	e.capsMu.Lock()
	defer e.capsMu.Unlock()
	out := make(map[Source]Capability, len(e.caps))
	for s, c := range e.caps {
		out[s] = c
	}
	return out
}

// Close stops the engine: its runtime, its goroutine and what it holds. A
// script in an endless loop does not notice the engine being closed, so it
// is interrupted, which the runtime answers at once.
func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		close(e.closed)
		select {
		case <-e.stopped:
		case <-time.After(2 * time.Second):
			// The script is in a call that has not come back: stop what it
			// is in, and let its goroutine end where it can.
			e.vm.Interrupt()
			select {
			case <-e.stopped:
			case <-time.After(2 * time.Second):
			}
		}
	})
}

// isBroken reports whether a call hung.
func (e *Engine) isBroken() bool {
	e.capsMu.Lock()
	defer e.capsMu.Unlock()
	return e.broken
}

// markBroken records that a call hung.
func (e *Engine) markBroken() {
	e.capsMu.Lock()
	e.broken = true
	e.capsMu.Unlock()
}

// infoLiteral writes the script's header as a JavaScript object, as lx
// hands it back as `currentScriptInfo`. Scripts read their own version from
// it, and some send it as a header.
func (e *Engine) infoLiteral() string {
	b, err := json.Marshal(map[string]string{
		"name":        e.info.Name,
		"description": e.info.Description,
		"version":     e.info.Version,
		"author":      e.info.Author,
		"homepage":    e.info.Homepage,
		"rawScript":   e.script,
	})
	if err != nil {
		return "{}"
	}
	return string(b)
}

// argString reads arguments[i] as a string, whatever the script passed.
func argString(args []any, i int) string {
	if i < 0 || i >= len(args) {
		return ""
	}
	switch v := args[i].(type) {
	case string:
		return v
	case nil, quickjs.Undefined:
		return ""
	case float64:
		return goNumber(v)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}

// argInt reads arguments[i] as a number, or 0.
func argInt(args []any, i int) int64 {
	if i < 0 || i >= len(args) {
		return 0
	}
	switch v := args[i].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	}
	return 0
}

// goText writes a value the runtime gave as the text a test can compare.
func goText(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case float64:
		return goNumber(x)
	}
	return fmt.Sprint(v)
}

// goNumber writes a number as Javascript would print it: whole numbers
// without a fraction.
func goNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1<<53 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// itoa writes a number for JavaScript.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
