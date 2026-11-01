package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The login is a walk over QQ's own pages, and these tests walk it against a
// server that answers the way those pages do: each step is checked where it
// happens, so a step whose shape changes is noticed here rather than as a
// login that silently fails for the listener.

// fakeLogin points the login at a server of the test's own, and gives it a
// clean slate: no code under way, nobody signed in. The server's address is
// returned for a test that also answers the app's own calls from it.
func fakeLogin(t *testing.T, h http.HandlerFunc) *url.URL {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)
	old := authHTTP
	authHTTP = &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			clone := r.Clone(r.Context())
			u := *r.URL
			clone.URL = &u
			clone.URL.Scheme, clone.URL.Host = base.Scheme, base.Host
			return http.DefaultTransport.RoundTrip(clone)
		}),
	}
	sessionStore.Lock()
	prior, priorQR := sessionStore.session, loginState.qr
	sessionStore.session, loginState.qr = Session{}, nil
	sessionStore.Unlock()
	t.Cleanup(func() {
		authHTTP = old
		sessionStore.Lock()
		sessionStore.session, loginState.qr = prior, priorQR
		sessionStore.Unlock()
	})
	return base
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 37, 37))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The code QQ draws is shown as QQ drew it, and the login walks from waiting
// to scanned to signed in, with the grant the phone gave going to the one
// call that turns it into a key.
func TestQQLoginWalksThePages(t *testing.T) {
	pngBytes := testPNG(t)
	const oauthCode = "0123456789ABCDEF0123456789ABCDEF"
	polls, exchanges := 0, 0
	fakeLogin(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Host + r.URL.Path {
		case "xui.ptlogin2.qq.com/ssl/ptqrshow":
			if r.URL.Query().Get("pt_3rd_aid") != qqApp {
				t.Errorf("code asked for by another app: %s", r.URL.RawQuery)
			}
			http.SetCookie(w, &http.Cookie{Name: "qrsig", Value: "sig-1", Domain: "ptlogin2.qq.com", Path: "/"})
			w.Write(pngBytes)
		case "xui.ptlogin2.qq.com/ssl/ptqrlogin":
			if !strings.Contains(r.Header.Get("Cookie"), "qrsig=sig-1") {
				t.Error("poll lost the code's signature")
			}
			if r.URL.Query().Get("ptqrtoken") != strconv.FormatUint(uint64(hash33("sig-1", 0)), 10) {
				t.Error("poll lost the code's token")
			}
			polls++
			switch polls {
			case 1:
				io.WriteString(w, `ptuiCB('66','0','','0','二维码未失效','')`)
			case 2:
				io.WriteString(w, `ptuiCB('67','0','','0','二维码认证中','')`)
			default:
				io.WriteString(w, `ptuiCB('0','0','https://ssl.ptlogin2.graph.qq.com/check_sig?uin=1&ptredirect=1','0','登录成功','')`)
			}
		case "ssl.ptlogin2.graph.qq.com/check_sig":
			for name, value := range map[string]string{"p_uin": "o1x", "p_skey": "skey-1", "pt_oauth_token": "tok-1"} {
				http.SetCookie(w, &http.Cookie{Name: name, Value: value, Domain: "graph.qq.com", Path: "/"})
			}
			w.Header().Set("Location", loginJump)
			w.WriteHeader(http.StatusFound)
		case "graph.qq.com/oauth2.0/login_jump":
			if !strings.Contains(r.Header.Get("Cookie"), "p_skey=skey-1") {
				t.Error("the jump lost the authorization")
			}
			w.WriteHeader(http.StatusOK)
		case "graph.qq.com/oauth2.0/authorize":
			if r.Method != http.MethodPost {
				t.Error("the consent is not a form post")
			}
			_ = r.ParseForm()
			if r.Form.Get("g_tk") != strconv.FormatUint(uint64(hash33("skey-1", 5381)), 10) {
				t.Error("the consent lost its token")
			}
			if len(r.Form.Get("state")) != 32 {
				t.Error("the consent has no state")
			}
			w.Header().Set("Location", "https://y.qq.com/portal/wx_redirect.html?code="+oauthCode+"&state="+r.Form.Get("state"))
			w.WriteHeader(http.StatusFound)
		case "u.y.qq.com/cgi-bin/musicu.fcg":
			exchanges++
			if r.Header.Get("Cookie") != "" {
				t.Error("the app's own cookies went to a page that decides who signs in")
			}
			var body struct {
				Req struct {
					Module, Method string
					Param          struct{ Code string }
				} `json:"req"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Req.Module != "QQConnectLogin.LoginServer" || body.Req.Method != "QQLogin" || body.Req.Param.Code != oauthCode {
				t.Errorf("the code was not the one granted: %+v", body)
			}
			io.WriteString(w, `{"code":0,"req":{"code":0,"data":{"musicid":10001,"musickey":"key-1","loginType":2,"nick":"听歌的人","logo":"https://example.test/a.png","keyExpiresIn":"7776000"}}}`)
		default:
			t.Errorf("unexpected request %s%s", r.Host, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	key, err := QRKey()
	if err != nil {
		t.Fatal(err)
	}
	got, err := QRImage(key)
	if err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("the code shown is not the one QQ drew: %v", err)
	}
	for _, want := range []int{QRWaiting, QRScanned, QRConfirmed} {
		code, _, _, _, err := QRCheck(key)
		if err != nil || code != want {
			t.Fatalf("QRCheck = %d, %v; want %d", code, err, want)
		}
	}
	if s := currentSession(); s.MusicKey != "key-1" || s.MusicID != "10001" || s.ExpiresIn != 7776000 {
		t.Errorf("session %+v", s)
	}
	if exchanges != 1 {
		t.Errorf("the grant was traded %d times", exchanges)
	}
	if _, err := QRImage(key); err == nil {
		t.Error("a used code still has an image")
	}
}

// A code whose dialog was closed may not sign in, however far its exchange
// had got.
func TestClosedCodeDoesNotSignIn(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	fakeLogin(t, func(w http.ResponseWriter, r *http.Request) {
		// WeChat says the code was scanned and gives its grant; the call
		// that trades it is held here, so the code can be closed while its
		// answer is on the way.
		if strings.HasPrefix(r.URL.Path, "/connect/l/qrconnect") {
			io.WriteString(w, `window.wx_errcode=405;window.wx_code='wx-code';`)
			return
		}
		close(started)
		<-release
		io.WriteString(w, `{"code":0,"req":{"code":0,"data":{"musicid":10001,"musickey":"late","loginType":1}}}`)
	})
	loginState.Lock()
	loginState.qr = map[string]qrLogin{"closed": {expires: time.Now().Add(time.Minute), wechat: &wechatQR{
		client: authHTTP, uuid: "uuid-fixture", ctx: context.Background(), cancel: func() {}, device: &wechatDevice{},
	}}}
	loginState.Unlock()
	done := make(chan int, 1)
	go func() {
		code, _, _, _, _ := QRCheck("closed")
		done <- code
	}()
	<-started
	CancelQR("closed")
	close(release)
	if got := <-done; got != QRExpired || currentSession().MusicKey != "" {
		t.Fatalf("a closed code signed in: %d", got)
	}
}

// A failed fetch leaves no half-made code behind, and the next one is a code
// of its own.
func TestFailedCodeLeavesNothing(t *testing.T) {
	pngBytes := testPNG(t)
	calls := 0
	fakeLogin(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "qrsig", Value: "sig-" + strconv.Itoa(calls), Domain: "ptlogin2.qq.com"})
		w.Write(pngBytes)
	})
	if _, err := QRKey(); err == nil {
		t.Fatal("a failed request was taken as a code")
	}
	first, err := QRKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := QRKey()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("two codes share a signature")
	}
}

// A WeChat login is signed in as a phone: the fingerprint comes from QQ, the
// page's cookie stays with the page, the poll carries the cursor of a code
// already scanned, and the session keeps the device it was made from.
func TestWechatLoginWalksThePages(t *testing.T) {
	pngBytes := testPNG(t)
	polls := 0
	var device *wechatDevice
	fakeLogin(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Host + r.URL.Path {
		case "api.tencentmusic.com/tme/trpc/proxy":
			if r.Header.Get("Method") != "GetQimei" || r.Header.Get("User-Agent") != "QQMusic" {
				t.Error("the fingerprint was asked for without its headers")
			}
			var body struct {
				Params map[string]string `json:"qimeiParams"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			p := body.Params
			if p["sign"] != protocolMD5(p["key"], p["params"], p["time"]+"000", p["nonce"], "ZdJqM15EeO2zWc08", p["extra"]) {
				t.Error("the fingerprint's envelope is not signed")
			}
			io.WriteString(w, `{"code":0,"data":"{\"code\":0,\"data\":{\"q16\":\"q16-fixture\",\"q36\":\"q36-fixture\"}}"}`)
		case "u.y.qq.com/cgi-bin/musicu.fcg":
			var body struct {
				Comm map[string]any
				Req  struct {
					Method string
					Param  map[string]any
				} `json:"req"`
				Req0 struct{ Method string } `json:"req_0"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Req0.Method == "GetSession" {
				if body.Comm["QIMEI"] != "q16-fixture" {
					t.Error("the device session was asked for without the fingerprint")
				}
				io.WriteString(w, `{"code":0,"req_0":{"code":0,"data":{"session":{"uid":7,"sid":"sid-7"}}}}`)
				return
			}
			if body.Req.Param["code"] != "wx-code" || body.Req.Param["strAppid"] != wechatApp {
				t.Errorf("the WeChat grant was not the one scanned: %+v", body.Req.Param)
			}
			io.WriteString(w, `{"code":0,"req":{"code":0,"data":{"musicid":10002,"musickey":"wx-key","loginType":1,"refresh_key":"rk","openid":"open","refresh_token":"rt","unionid":"uid"}}}`)
		case "open.weixin.qq.com/connect/qrconnect":
			if r.URL.Query().Get("appid") != wechatApp {
				t.Error("another app's WeChat code")
			}
			http.SetCookie(w, &http.Cookie{Name: "wx_session", Value: "web", Domain: "weixin.qq.com", Path: "/"})
			io.WriteString(w, `<img src="/connect/qrcode/uuid-fixture"><a href="https://open.weixin.qq.com/connect/confirm?uuid=uuid-fixture">`)
		case "open.weixin.qq.com/connect/qrcode/uuid-fixture":
			w.Write(pngBytes)
		case "lp.open.weixin.qq.com/connect/l/qrconnect":
			if r.Header.Get("Cookie") != "wx_session=web" {
				t.Error("the poll lost WeChat's cookie")
			}
			if polls >= 2 && r.URL.Query().Get("last") != "404" {
				t.Error("the poll lost the cursor of a scanned code")
			}
			polls++
			switch polls {
			case 1:
				io.WriteString(w, `window.wx_errcode=408;window.wx_code='';`)
			case 2:
				io.WriteString(w, `window.wx_errcode=404;window.wx_code='';`)
			case 3:
				io.WriteString(w, `window.wx_errcode=408;window.wx_code='';`)
			default:
				io.WriteString(w, `window.wx_errcode=405;window.wx_code='wx-code';`)
			}
		default:
			t.Errorf("unexpected request %s%s", r.Host, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	key, err := WechatQRKey()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := QRImage(key); err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("the code shown is not the one WeChat drew: %v", err)
	}
	for _, want := range []int{QRWaiting, QRScanned, QRScanned, QRConfirmed} {
		code, _, _, _, err := QRCheck(key)
		if err != nil || code != want {
			t.Fatalf("QRCheck = %d, %v; want %d", code, err, want)
		}
	}
	s := currentSession()
	if s.MusicKey != "wx-key" || s.LoginType != 1 || s.RefreshKey != "rk" {
		t.Fatalf("session %+v", s)
	}
	device = s.Device
	if device == nil || device.Qimei != "q16-fixture" || device.UID != "7" {
		t.Fatalf("the session did not keep its device: %+v", device)
	}
	// A WeChat session is one that may be signed in again without a scan;
	// a QQ one is not.
	if !s.canRefresh() {
		t.Error("a WeChat session is not refreshable")
	}
	if (Session{MusicID: "1", MusicKey: "k", LoginType: 2, RefreshKey: "rk"}).canRefresh() {
		t.Error("a QQ session was taken as refreshable")
	}
}

// A WeChat key QQ no longer knows is renewed once from the token the login
// left, and asked again with the new one.
func TestWechatSessionRenewsWithoutAScan(t *testing.T) {
	read, renewed := 0, 0
	base := fakeLogin(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Comm map[string]any
			Req  struct {
				Method string
				Param  map[string]any
			} `json:"req"`
			Req0 struct{ Method string } `json:"req_0"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if body.Req0.Method == "GetLoginUserInfo" {
			read++
			if read == 1 {
				io.WriteString(w, `{"code":0,"req_0":{"code":1000,"data":{}}}`)
				return
			}
			if body.Comm["authst"] != "new-key" {
				t.Error("the page was asked again with the key QQ had just refused")
			}
			io.WriteString(w, `{"code":0,"req_0":{"code":0,"data":{"info":{"nick":"改过的名字"}}}}`)
			return
		}
		renewed++
		p := body.Req.Param
		if p["openid"] != "open" || p["refresh_token"] != "rt" || p["refresh_key"] != "rk" || p["loginMode"] != float64(2) {
			t.Errorf("the renewal is missing part of its credential: %+v", p)
		}
		io.WriteString(w, `{"code":0,"req":{"code":0,"data":{"musicid":10002,"musickey":"new-key","loginType":1}}}`)
	})
	// The app's own calls are answered from the same server: the renewal is
	// signed in as the phone, not through the pages above.
	oldMusicu := musicuURL
	musicuURL = base.String() + "/musicu"
	t.Cleanup(func() { musicuURL = oldMusicu })
	dir := t.TempDir()
	oldDir := sessionDir
	sessionDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { sessionDir = oldDir })
	setSession(Session{
		MusicID: "10002", MusicKey: "old-key", LoginType: 1,
		CreatedAt: time.Now().Unix(), ExpiresIn: 3600,
		OpenID: "open", RefreshToken: "rt", RefreshKey: "rk", UnionID: "uid",
		Device: &wechatDevice{Qimei: "q16", Qimei36: "q36", UID: "7", SID: "sid"},
	})
	p, err := Account()
	if err != nil || p == nil || p.Nickname != "改过的名字" {
		t.Fatalf("Account = %+v, %v", p, err)
	}
	if read != 2 || renewed != 1 {
		t.Errorf("the key was read %d times and renewed %d", read, renewed)
	}
	if s := currentSession(); s.MusicKey != "new-key" {
		t.Errorf("the renewed key was not kept: %+v", s)
	}
	// The token QQ did not send back is the one still held: a renewal that
	// dropped it would need a scan the next time.
	if s := currentSession(); s.RefreshKey != "rk" || s.OpenID != "open" {
		t.Errorf("the renewal dropped what it did not resend: %+v", s)
	}
	if !RestoreSession() {
		t.Error("the renewed session was not saved")
	}
}

// An expired QQ session has no token to renew with, and is not held to be
// signed in.
func TestExpiredQQSessionIsGone(t *testing.T) {
	fakeLogin(t, func(w http.ResponseWriter, r *http.Request) { t.Error("an expired session made a call") })
	setSession(Session{MusicID: "10001", MusicKey: "old", LoginType: 2, CreatedAt: 1, ExpiresIn: 1})
	if p, err := Account(); err != nil || p != nil {
		t.Errorf("Account = %+v, %v", p, err)
	}
	if SignedIn() {
		t.Error("an expired session was taken as signed in")
	}
}

// QQ's status answer is quoted strings, which are read as data: quotes,
// backslashes and the escapes of the page.
func TestPtuiIsReadAsData(t *testing.T) {
	for _, c := range []struct {
		body string
		want []string
	}{
		{`ptuiCB('0','0','url','0','ok','nick');`, []string{"0", "0", "url", "0", "ok", "nick"}},
		{`ptuiCB('66','0','','0','二维码未失效','')`, []string{"66", "0", "", "0", "二维码未失效", ""}},
		{`ptuiCB('0','0','a\'b','0','x','')`, []string{"0", "0", "a'b", "0", "x", ""}},
	} {
		got, err := parsePtui(c.body)
		if err != nil {
			t.Fatalf("%q: %v", c.body, err)
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%q = %q, want %q", c.body, got, c.want)
		}
	}
	for _, body := range []string{``, `ptuiCB()`, `ptuiCB('0','0')`, `alert('0')`, `ptuiCB('0','0','u','0','m','n')x`} {
		if _, err := parsePtui(body); err == nil {
			t.Errorf("%q was read as a status", body)
		}
	}
}

// The login says which part of it failed, and never the URL or the code it
// failed with.
func TestLoginErrorsAreForTheListener(t *testing.T) {
	secret := "https://ssl.ptlogin2.graph.qq.com/check_sig?uin=1&sig=SECRET-GRANT"
	cases := []struct {
		err  error
		want string
	}{
		{loginFailure("check-sig", errors.New(secret)), "QQ 授权校验失败，请稍后再试"},
		{loginFailure("qr-show", loginHTTPError(503)), "二维码获取服务返回 HTTP 503，请刷新二维码重试"},
		{&loginResponseError{Kind: "rejected", Gateway: 0, Business: 104400}, "QQ 音乐拒绝登录（服务码 0/104400）"},
		{&loginResponseError{Kind: "qr-image"}, "登录服务返回的二维码图片无效，请刷新重试"},
	}
	for _, c := range cases {
		if got := LoginErrorMessage(c.err); got != c.want {
			t.Errorf("LoginErrorMessage = %q, want %q", got, c.want)
		}
		if strings.Contains(LoginErrorMessage(c.err), "SECRET") || strings.Contains(LoginErrorMessage(c.err), "http") {
			t.Errorf("%v leaked its cause", c.err)
		}
	}
	if LoginErrorMessage(nil) != "" {
		t.Error("no error was given a message")
	}
}

// The answer that trades a grant for a key is read for what it needs, and
// an answer missing any of it is refused rather than half signed in.
func TestLoginAnswerNeedsAKeyAndAnId(t *testing.T) {
	key := func(data string) []byte {
		return []byte(`{"code":0,"req":{"code":0,"data":` + data + `}}`)
	}
	s, err := decodeLoginResponse(key(`{"musicid":"10001","musickey":"k","loginType":1,"keyExpiresIn":60}`), 1)
	if err != nil || s.MusicID != "10001" || s.ExpiresIn != 60 {
		t.Fatalf("session %+v, %v", s, err)
	}
	// A number id is the shape the phone's answer uses, and the same id.
	if s, err := decodeLoginResponse(key(`{"musicid":10001,"musickey":"k"}`), 2); err != nil || s.MusicID != "10001" || s.LoginType != 2 {
		t.Fatalf("session %+v, %v", s, err)
	}
	for _, bad := range []string{
		`{"musickey":"k"}`,
		`{"musicid":"10001"}`,
		`{"musicid":"10001","musickey":"k","loginType":7}`,
	} {
		if _, err := decodeLoginResponse(key(bad), 2); err == nil {
			t.Errorf("%s was signed in with", bad)
		}
	}
	if _, err := decodeLoginResponse([]byte(`{"code":0,"req":{"code":104400}}`), 2); err == nil {
		t.Error("a refusal was signed in with")
	}
	if _, err := decodeLoginResponse([]byte(`nonsense`), 2); err == nil {
		t.Error("nonsense was signed in with")
	}
}

// The authorization the phone gives is only taken from QQ's own hosts: a
// redirect anywhere else is refused before its grant is read.
func TestLoginOnlyGoesWhereQQSendsIt(t *testing.T) {
	fakeLogin(t, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "qrsig", Value: "sig", Domain: "ptlogin2.qq.com"})
		w.Write(testPNG(t))
	})
	loginState.Lock()
	loginState.qr = map[string]qrLogin{"k": {expires: time.Now().Add(time.Minute)}}
	loginState.qr["k"] = qrLogin{expires: time.Now().Add(time.Minute)}
	loginState.Unlock()
	code, _, _, _, err := QRCheck("k")
	if err == nil {
		t.Errorf("an answer without a status was read as %d", code)
	}
	for _, raw := range []string{
		"http://ssl.ptlogin2.graph.qq.com/check_sig",
		"https://evil.test/check_sig",
		"https://ssl.ptlogin2.graph.qq.com/other",
	} {
		if _, err := trustedURL(raw, "ssl.ptlogin2.graph.qq.com", "/check_sig"); err == nil {
			t.Errorf("%s was trusted", raw)
		}
	}
}
