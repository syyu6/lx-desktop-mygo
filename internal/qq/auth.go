package qq

// Signing in to QQ Music, by either of the two codes its own app offers:
// QQ's, a code read by the QQ app, and WeChat's, read by WeChat.
//
// Both are the phone clients' logins, not the web player's: the QQ one
// goes through QQ's ptlogin page and its OAuth grant, and the WeChat one
// through WeChat's QR connect, and both end in the same call that trades
// the grant for a music key. The codes are fetched as PNG by QQ itself and
// shown as they come, because a QR re-encoded from its URL is not the same
// code as the one QQ signed.
//
// A login is kept for two minutes, the life of the code, and a closed or
// replaced one is dropped: its result may not sign in later.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"netease-music-mygo/internal/network"
)

// The states a code is in, as the login pages report them.
const (
	QRWaiting   = 801 // not scanned
	QRScanned   = 802 // scanned, waiting for the phone's yes
	QRConfirmed = 803 // signed in
	QRExpired   = 800
)

// LoginError is a login that failed, with the step it failed at: the dialog
// says which part to try again, and never prints a URL or a credential.
type LoginError struct {
	Step string
	Err  error
}

func (e *LoginError) Error() string { return LoginErrorMessage(e) }
func (e *LoginError) Unwrap() error { return e.Err }

// loginFailure marks err with the step it happened at.
func loginFailure(step string, err error) error {
	if err == nil {
		return nil
	}
	var failure *LoginError
	if errors.As(err, &failure) {
		return err
	}
	return &LoginError{Step: step, Err: err}
}

// LoginErrorMessage is what the dialog shows for a failed login: which part
// of it failed, in words, without the URL or the code that failed.
func LoginErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	stage := "登录"
	var failure *LoginError
	if errors.As(err, &failure) {
		switch failure.Step {
		case "qr-show":
			stage = "二维码获取"
		case "device-bootstrap":
			stage = "登录设备初始化"
		case "qr-poll":
			stage = "扫码状态查询"
		case "check-sig":
			stage = "QQ 授权校验"
		case "login-jump":
			stage = "QQ 授权跳转"
		case "authorize":
			stage = "QQ 授权确认"
		case "qq-login":
			stage = "QQ 音乐登录"
		}
	}
	var status loginHTTPError
	if errors.As(err, &status) {
		return fmt.Sprintf("%s服务返回 HTTP %d，请刷新二维码重试", stage, int(status))
	}
	var response *loginResponseError
	if errors.As(err, &response) {
		return response.message()
	}
	return stage + "失败，请稍后再试"
}

// loginHTTPError is a status other than 200 where one was expected.
type loginHTTPError int

func (e loginHTTPError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }

// loginResponseError is an answer of a shape the login does not expect.
type loginResponseError struct {
	Kind                         string
	Gateway, Business, LoginType int
}

func (e *loginResponseError) Error() string { return e.message() }

func (e *loginResponseError) message() string {
	switch e.Kind {
	case "qr-signature":
		return "QQ 未返回二维码签名，请刷新重试"
	case "qr-image":
		return "登录服务返回的二维码图片无效，请刷新重试"
	case "qr-key":
		return "二维码已失效，请刷新重试"
	case "rejected":
		return "QQ 音乐拒绝登录（服务码 " + strconv.Itoa(e.Gateway) + "/" + strconv.Itoa(e.Business) + "）"
	case "json":
		return "QQ 音乐登录响应无法解析"
	case "status":
		return "QQ 音乐未返回登录状态"
	case "data":
		return "QQ 音乐未返回登录数据"
	case "id":
		return "QQ 音乐返回的账号标识无效"
	case "key":
		return "QQ 音乐未返回登录凭据"
	case "type":
		return "QQ 音乐返回未支持的登录类型（" + strconv.Itoa(e.LoginType) + "）"
	case "expired":
		return "QQ 音乐返回的登录凭据已过期，请重新扫码"
	}
	return "QQ 音乐登录响应无效"
}

// authHTTP is the client the login's own pages are read with: no redirects
// followed by the client, since each one is the next step and is checked
// before it is taken. It is a variable so that a test can send the login to
// a server of its own.
var authHTTP = &http.Client{
	Timeout:   15 * time.Second,
	Transport: network.Transport(),
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// loginTransport is the client a step of the login is read with, copied so
// that one step's cookie jar is not another's.
func loginTransport() *http.Client {
	c := *authHTTP
	return &c
}

// The hosts the login talks to, as variables so that a test can point them
// at a server of its own.
var (
	ptloginURL = "https://xui.ptlogin2.qq.com"
	ptqrCheck  = "https://ssl.ptlogin2.graph.qq.com"
	graphURL   = "https://graph.qq.com"
	wechatURL  = "https://open.weixin.qq.com"
	wechatPoll = "https://lp.open.weixin.qq.com"
	deviceURL  = "https://api.tencentmusic.com/tme/trpc/proxy"
)

// loginJump is where QQ sends a phone that scanned its code.
const loginJump = "https://graph.qq.com/oauth2.0/login_jump"

// The two codes' scopes: QQ's app in QQ Music's account, and WeChat's.
const (
	qqApp      = "100497308"
	wechatApp  = "wx48db31d50e334801"
	redirectQQ = "https://y.qq.com/portal/wx_redirect.html?login_type=1&surl=https://y.qq.com/"
	redirectWX = "https://y.qq.com/portal/wx_redirect.html?login_type=2&surl=https://y.qq.com/"
)

// qrLogin is a code waiting to be scanned.
type qrLogin struct {
	image   []byte
	expires time.Time
	wechat  *wechatQR // set for a WeChat code
}

// loginState is the codes under way, by their key.
var loginState struct {
	sync.Mutex
	qr map[string]qrLogin
}

// QRImagePath is QQ's signing of a code, and the code itself, as it gave
// it: the PNG the app shows, and the key the poll is made with.
type QRCode struct {
	// Key names the login, and is what QRCheck is asked with.
	Key string
	// PNG is the code, as QQ drew it.
	PNG []byte
}

// QRKey asks for a code, by QQ's app.
func QRKey() (string, error) {
	return qqQRKey()
}

// WechatQRKey asks for a code, by WeChat.
func WechatQRKey() (string, error) {
	return wechatQRKey()
}

// QRImage is the code key drew, as PNG.
func QRImage(key string) ([]byte, error) {
	loginState.Lock()
	defer loginState.Unlock()
	q, ok := loginState.qr[key]
	if !ok {
		return nil, &loginResponseError{Kind: "qr-key"}
	}
	return bytes.Clone(q.image), nil
}

// CancelQR drops a login: its result may not sign in afterwards.
func CancelQR(key string) {
	loginState.Lock()
	defer loginState.Unlock()
	if q, ok := loginState.qr[key]; ok && q.wechat != nil {
		q.wechat.cancel()
	}
	delete(loginState.qr, key)
}

// keepQR puts a code under key, dropping the expired ones.
func keepQR(key string, q qrLogin) {
	loginState.Lock()
	defer loginState.Unlock()
	if loginState.qr == nil {
		loginState.qr = map[string]qrLogin{}
	}
	for k, old := range loginState.qr {
		if time.Now().After(old.expires) {
			if old.wechat != nil {
				old.wechat.cancel()
			}
			delete(loginState.qr, k)
		}
	}
	loginState.qr[key] = q
}

// finishQR signs in with s, if the login key is still under way: a code
// whose dialog was closed may not.
func finishQR(key string, s Session) bool {
	loginState.Lock()
	defer loginState.Unlock()
	if _, ok := loginState.qr[key]; !ok {
		return false
	}
	setSession(s)
	delete(loginState.qr, key)
	return true
}

// qqQRKey asks QQ for the code its app reads.
//
// The PNG is QQ's own: re-encoding the URL it stands for cannot reproduce
// the code QQ signed, and the poll is made with the signature QQ set in a
// cookie beside it.
func qqQRKey() (key string, err error) {
	defer func() { err = loginFailure("qr-show", err) }()
	nonce, err := randomHex(8)
	if err != nil {
		return "", err
	}
	q := url.Values{
		"appid": {"716027609"}, "e": {"2"}, "l": {"M"}, "s": {"3"}, "d": {"72"}, "v": {"4"},
		"t": {nonce}, "daid": {"383"}, "pt_3rd_aid": {qqApp}, "u1": {loginJump},
	}
	req, err := http.NewRequest(http.MethodGet, ptloginURL+"/ssl/ptqrshow?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := loginTransport().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", loginHTTPError(res.StatusCode)
	}
	key = authCookie(res, "qrsig", "ptlogin2.qq.com")
	png, err := readLimited(res, 64<<10)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", &loginResponseError{Kind: "qr-signature"}
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(png)); err != nil {
		return "", &loginResponseError{Kind: "qr-image"}
	}
	keepQR(key, qrLogin{image: png, expires: time.Now().Add(120 * time.Second)})
	return key, nil
}

// QRCheck asks whether the code key was scanned, and signs in once it was.
// It answers with a state, a message worth showing, and who scanned it.
func QRCheck(key string) (code int, msg, nickname, avatar string, err error) {
	loginState.Lock()
	q, ok := loginState.qr[key]
	loginState.Unlock()
	if !ok || time.Now().After(q.expires) {
		return QRExpired, "二维码已失效", "", "", nil
	}
	if q.wechat != nil {
		return checkWechatQR(key, q)
	}
	q2 := url.Values{
		"u1": {loginJump}, "from_ui": {"1"}, "aid": {"716027609"}, "daid": {"383"},
		"pt_3rd_aid": {qqApp}, "ptqrtoken": {strconv.FormatUint(uint64(hash33(key, 0)), 10)},
	}
	req, err := http.NewRequest(http.MethodGet, ptloginURL+"/ssl/ptqrlogin?"+q2.Encode(), nil)
	if err != nil {
		return 0, "", "", "", loginFailure("qr-poll", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Cookie", "qrsig="+key)
	res, err := loginTransport().Do(req)
	if err != nil {
		return 0, "", "", "", loginFailure("qr-poll", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, "", "", "", loginFailure("qr-poll", loginHTTPError(res.StatusCode))
	}
	b, err := readLimited(res, 16<<10)
	if err != nil {
		return 0, "", "", "", loginFailure("qr-poll", err)
	}
	args, err := parsePtui(string(b))
	if err != nil {
		return 0, "", "", "", loginFailure("qr-poll", err)
	}
	switch args[0] {
	case "66":
		return QRWaiting, "", "", "", nil
	case "67":
		return QRScanned, "请在手机 QQ 上确认登录", "", "", nil
	case "65":
		return QRExpired, "二维码已失效", "", "", nil
	case "0":
	default:
		return 0, args[4], "", "", nil
	}
	s, err := exchangePtlogin(args[2])
	if err != nil {
		return 0, "", "", "", err
	}
	if !finishQR(key, s) {
		return QRExpired, "二维码已失效", "", "", nil
	}
	return QRConfirmed, "登录成功", s.Nickname, s.AvatarURL, nil
}

// exchangePtlogin trades the grant the scanned code came back with for a
// music key: QQ's OAuth check, its jump, its consent, and then the call
// that signs in to QQ Music.
func exchangePtlogin(raw string) (session Session, err error) {
	defer func() { err = loginFailure("check-sig", err) }()
	empty := Session{}
	u, err := trustedURL(raw, "ssl.ptlogin2.graph.qq.com", "/check_sig")
	if err != nil {
		return empty, err
	}
	res, err := loginPage(u)
	if err != nil {
		return empty, err
	}
	if res.StatusCode != http.StatusFound {
		return empty, errors.New("qq: check_sig did not redirect")
	}
	jump, err := trustedURL(res.Header.Get("Location"), "graph.qq.com", "/oauth2.0/login_jump")
	if err != nil {
		return empty, err
	}
	var cookies []string
	var skey string
	for _, name := range []string{"p_uin", "p_skey", "pt_oauth_token"} {
		v := authCookie(res, name, "graph.qq.com")
		if v == "" {
			return empty, errors.New("qq: incomplete authorization cookies")
		}
		cookies = append(cookies, name+"="+v)
		if name == "p_skey" {
			skey = v
		}
	}
	// The jump is taken with the cookies, and the consent that follows with
	// them too: QQ ties the grant to the session it set.
	req, err := http.NewRequest(http.MethodGet, jump.String(), nil)
	if err != nil {
		return empty, loginFailure("login-jump", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Cookie", strings.Join(cookies, "; "))
	res, err = loginTransport().Do(req)
	if err != nil {
		return empty, loginFailure("login-jump", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return empty, loginFailure("login-jump", loginHTTPError(res.StatusCode))
	}
	state, err := randomHex(16)
	if err != nil {
		return empty, err
	}
	uiID, err := randomHex(16)
	if err != nil {
		return empty, err
	}
	uiID = uiID[:8] + "-" + uiID[8:12] + "-" + uiID[12:16] + "-" + uiID[16:20] + "-" + uiID[20:]
	form := url.Values{
		"response_type": {"code"}, "client_id": {qqApp}, "redirect_uri": {redirectQQ},
		"scope": {"get_user_info,get_app_friends"}, "state": {state}, "switch": {""},
		"from_ptlogin": {"1"}, "src": {"1"}, "update_auth": {"1"}, "openapi": {"1010_1030"},
		"g_tk":      {strconv.FormatUint(uint64(hash33(skey, 5381)), 10)},
		"auth_time": {strconv.FormatInt(time.Now().UnixMilli(), 10)}, "ui": {uiID},
	}
	req, err = http.NewRequest(http.MethodPost, graphURL+"/oauth2.0/authorize", strings.NewReader(form.Encode()))
	if err != nil {
		return empty, loginFailure("authorize", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", strings.Join(cookies, "; "))
	res, err = loginTransport().Do(req)
	if err != nil {
		return empty, loginFailure("authorize", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound {
		return empty, loginFailure("authorize", errors.New("qq: authorization did not redirect"))
	}
	back, err := trustedURL(res.Header.Get("Location"), "y.qq.com", "/portal/wx_redirect.html")
	if err != nil {
		return empty, loginFailure("authorize", err)
	}
	codes, states := back.Query()["code"], back.Query()["state"]
	if len(codes) != 1 || !grantCode.MatchString(codes[0]) || len(states) != 1 || states[0] != state {
		return empty, loginFailure("authorize", errors.New("qq: invalid authorization state or code"))
	}
	return loginWith("QQConnectLogin.LoginServer", "QQLogin", map[string]any{"code": codes[0]}, 2)
}

// loginWith is the call both codes end in: the grant in, a music key out.
func loginWith(module, method string, param map[string]any, loginType int) (Session, error) {
	body, _ := json.Marshal(map[string]any{
		"comm": map[string]any{"g_tk": 5381, "platform": "yqq", "ct": 24, "cv": 0},
		"req":  map[string]any{"module": module, "method": method, "param": param},
	})
	req, err := http.NewRequest(http.MethodPost, musicuURL, bytes.NewReader(body))
	if err != nil {
		return Session{}, loginFailure("qq-login", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://y.qq.com/")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	read, err := loginRead(req, 16<<10)
	if err != nil {
		return Session{}, loginFailure("qq-login", err)
	}
	s, err := decodeLoginResponse(read, loginType)
	if err != nil {
		return Session{}, loginFailure("qq-login", err)
	}
	return s, nil
}

// loginRPC is an rpc made as a login step: with the comm given whole, and
// through the login's own client, so that a cookie of the app's is not sent
// to a call that decides who someone is.
func loginRPC(module, method string, param any, comm map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{
		"comm":  comm,
		"req_0": map[string]any{"module": module, "method": method, "param": param},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, musicuURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://y.qq.com/")
	req.Header.Set("Content-Type", "application/json")
	read, err := loginRead(req, 64<<10)
	if err != nil {
		return err
	}
	var res struct {
		Code *int `json:"code"`
		Req  *struct {
			Code *int            `json:"code"`
			Data json.RawMessage `json:"data"`
		} `json:"req_0"`
	}
	if json.Unmarshal(read, &res) != nil {
		return errors.New("qq: malformed answer")
	}
	if res.Code == nil || res.Req == nil || res.Req.Code == nil {
		return errors.New("qq: no status in the answer")
	}
	if *res.Code != 0 || *res.Req.Code != 0 {
		return &ProtocolError{GlobalCode: *res.Code, Code: *res.Req.Code, Module: module, Method: method}
	}
	if out == nil {
		return nil
	}
	if len(res.Req.Data) == 0 {
		return errors.New("qq: the answer had nothing in it")
	}
	return json.Unmarshal(res.Req.Data, out)
}

// loginRead sends a step of the login, and reads a body of at most limit
// bytes. It is the login's client, not the app's: the session a login is
// replacing is not one its calls may be signed with, and a cookie the app
// holds must not travel to a page that decides who is signing in.
func loginRead(req *http.Request, limit int64) ([]byte, error) {
	res, err := loginTransport().Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		res.Body.Close()
		return nil, loginHTTPError(res.StatusCode)
	}
	return readLimited(res, limit)
}

// loginPage gets a URL of the login, and returns the answer for its
// redirect to be read: the body is not what the next step needs.
func loginPage(u *url.URL) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	res, err := loginTransport().Do(req)
	if err != nil {
		return nil, err
	}
	// The cookies are read from it, and the body is closed by the caller:
	// a redirect's body is empty, and an error's is not worth reading.
	return res, nil
}

// authCookie reads a cookie of the answer's, for a host, and treats one
// the server asked to delete as gone.
func authCookie(res *http.Response, name, domain string) string {
	var value string
	for _, c := range res.Cookies() {
		if c.Name == name && strings.TrimPrefix(strings.ToLower(c.Domain), ".") == domain {
			value = c.Value
			if c.MaxAge < 0 {
				value = ""
			}
		}
	}
	return value
}

// readLimited reads an answer's body, and refuses one larger than limit.
func readLimited(res *http.Response, limit int64) ([]byte, error) {
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("qq: oversized login answer")
	}
	return b, err
}

// hash33 is the hash QQ's own pages sign their tokens with.
func hash33(s string, initial uint32) uint32 {
	h := initial
	for _, c := range s {
		h = h*33 + uint32(c)
	}
	return h & 0x7fffffff
}

// randomHex is n random bytes, written in hex.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// trustedURL reads a URL the login was sent to, and refuses one of another
// host or path: the login may only be sent where QQ sends it.
func trustedURL(raw, host, path string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != host || u.Path != path {
		return nil, errors.New("qq: untrusted login URL")
	}
	return u, nil
}

// grantCode is the shape of the grant QQ authorizes with.
var grantCode = regexp.MustCompile(`^[A-F0-9]{32}$`)

// ptuiString is a quoted string of a ptuiCB answer, with its escapes.
var ptuiString = regexp.MustCompile(`^'((?:[^'\\\r\n]|\\(?:['\\nrt]|x[0-9a-fA-F]{2}|u[0-9a-fA-F]{4}))*)'\s*`)

// parsePtui reads the `ptuiCB('0','0','url','0','message','nick')` answer
// QQ's login page gives, as data: no JavaScript is run to read it.
func parsePtui(body string) ([]string, error) {
	body = strings.TrimSpace(body)
	body = strings.TrimSuffix(body, ";")
	if !strings.HasPrefix(body, "ptuiCB(") || !strings.HasSuffix(body, ")") {
		return nil, errors.New("qq: malformed QR status")
	}
	rest := strings.TrimSpace(body[7 : len(body)-1])
	var values []string
	for rest != "" {
		m := ptuiString.FindStringSubmatch(rest)
		if m == nil {
			return nil, errors.New("qq: malformed QR status")
		}
		values = append(values, unescapePtui(m[1]))
		rest = strings.TrimSpace(rest[len(m[0]):])
		if rest == "" {
			break
		}
		if rest[0] != ',' || len(values) >= 7 {
			return nil, errors.New("qq: malformed QR status")
		}
		rest = strings.TrimSpace(rest[1:])
		if rest == "" {
			return nil, errors.New("qq: malformed QR status")
		}
	}
	if len(values) < 5 {
		return nil, errors.New("qq: malformed QR status")
	}
	return values, nil
}

// unescapePtui reads a quoted string's escapes.
func unescapePtui(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'x', 'u':
			n := 2
			if s[i] == 'u' {
				n = 4
			}
			if i+1+n > len(s) {
				return b.String()
			}
			v, _ := strconv.ParseUint(s[i+1:i+1+n], 16, 32)
			b.WriteRune(rune(v))
			i += n
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// loginOptionalInt reads an optional number QQ sends as a number or as a
// string of digits.
func loginOptionalInt(raw json.RawMessage) int64 {
	var value json.Number
	if json.Unmarshal(raw, &value) == nil {
		if n, err := value.Int64(); err == nil {
			return n
		}
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		n, _ := strconv.ParseInt(text, 10, 64)
		return n
	}
	return 0
}

// decodeLoginResponse reads the music key out of the answer that trades a
// grant for it.
func decodeLoginResponse(body []byte, expectedType int) (Session, error) {
	var result struct {
		Code *int `json:"code"`
		Req  *struct {
			Code *int            `json:"code"`
			Data json.RawMessage `json:"data"`
		} `json:"req"`
	}
	if json.Unmarshal(body, &result) != nil {
		return Session{}, &loginResponseError{Kind: "json"}
	}
	if result.Code == nil || result.Req == nil || result.Req.Code == nil {
		return Session{}, &loginResponseError{Kind: "status"}
	}
	if *result.Code != 0 || *result.Req.Code != 0 {
		return Session{}, &loginResponseError{Kind: "rejected", Gateway: *result.Code, Business: *result.Req.Code}
	}
	var data struct {
		MusicID    json.RawMessage `json:"musicid"`
		MusicKey   string          `json:"musickey"`
		LoginType  int             `json:"loginType"`
		CreatedAt  json.RawMessage `json:"musickeyCreateTime"`
		ExpiresIn  json.RawMessage `json:"keyExpiresIn"`
		Nickname   string          `json:"nick"`
		AvatarURL  string          `json:"logo"`
		EncryptUin string          `json:"encryptUin"`
		OpenID     string          `json:"openid"`
		Refresh    string          `json:"refresh_token"`
		UnionID    string          `json:"unionid"`
		RefreshKey string          `json:"refresh_key"`
	}
	if len(result.Req.Data) == 0 || json.Unmarshal(result.Req.Data, &data) != nil {
		return Session{}, &loginResponseError{Kind: "data"}
	}
	s := Session{
		MusicKey: data.MusicKey, LoginType: data.LoginType,
		CreatedAt: loginOptionalInt(data.CreatedAt),
		ExpiresIn: loginOptionalInt(data.ExpiresIn),
		Nickname:  data.Nickname, AvatarURL: data.AvatarURL, EncryptUin: data.EncryptUin,
		OpenID: data.OpenID, RefreshToken: data.Refresh, UnionID: data.UnionID, RefreshKey: data.RefreshKey,
	}
	switch v := loginOptionalInt(data.MusicID); {
	case v > 0:
		s.MusicID = strconv.FormatInt(v, 10)
	case len(data.MusicID) > 0 && json.Unmarshal(data.MusicID, &s.MusicID) != nil:
		return Session{}, &loginResponseError{Kind: "id"}
	}
	if s.MusicID == "" {
		return Session{}, &loginResponseError{Kind: "id"}
	}
	if s.MusicKey == "" {
		return Session{}, &loginResponseError{Kind: "key"}
	}
	if s.LoginType == 0 {
		s.LoginType = expectedType
	}
	if s.LoginType != 1 && s.LoginType != 2 {
		return Session{}, &loginResponseError{Kind: "type", LoginType: s.LoginType}
	}
	return s, nil
}

// wechatQR is a WeChat code's own state: its page's cookies, its code id,
// and the phone it will sign in as.
type wechatQR struct {
	client  *http.Client
	ctx     context.Context
	uuid    string
	cancel  func()
	mu      sync.Mutex
	scanned bool
	device  *wechatDevice
}

// wechatUUID and wechatStatus read WeChat's QR page and its poll's answer.
var (
	wechatUUID   = regexp.MustCompile(`(?:uuid=|/connect/qrcode/)([A-Za-z0-9_-]+)`)
	wechatStatus = regexp.MustCompile(`window\.wx_errcode\s*=\s*(\d+)\s*;\s*window\.wx_code\s*=\s*'([^'\r\n]*)'`)
)

// wechatQRKey asks WeChat for the code its app reads.
//
// A WeChat login is signed in as a phone, so it starts by asking QQ for the
// fingerprint that phone has (auth_device.go); the code's own cookies are
// kept apart from the app's, since a login in progress may be none.
func wechatQRKey() (key string, err error) {
	defer func() { err = loginFailure("qr-show", err) }()
	device, err := bootstrapWechatDevice()
	if err != nil {
		return "", err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", err
	}
	client := loginTransport()
	client.Jar, client.Timeout = jar, 20*time.Second
	ctx, cancel := context.WithCancel(context.Background())
	qr := &wechatQR{client: client, ctx: ctx, cancel: cancel, device: device}
	q := url.Values{
		"appid": {wechatApp}, "redirect_uri": {redirectWX}, "response_type": {"code"},
		"scope": {"snsapi_login"}, "state": {"STATE"},
		"href": {"https://y.qq.com/mediastyle/music_v17/src/css/popup_wechat.css#wechat_redirect"},
	}
	page, err := wechatSend(qr, wechatURL+"/connect/qrconnect?"+q.Encode(), "", 256<<10)
	if err != nil {
		cancel()
		return "", err
	}
	match := wechatUUID.FindSubmatch(page)
	if match == nil {
		cancel()
		return "", errors.New("qq: missing WeChat QR identifier")
	}
	qr.uuid = string(match[1])
	png, err := wechatSend(qr, wechatURL+"/connect/qrcode/"+qr.uuid, wechatURL, 64<<10)
	if err != nil {
		cancel()
		return "", err
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(png)); err != nil {
		cancel()
		return "", &loginResponseError{Kind: "qr-image"}
	}
	key, err = randomHex(24)
	if err != nil {
		cancel()
		return "", err
	}
	keepQR(key, qrLogin{image: png, expires: time.Now().Add(120 * time.Second), wechat: qr})
	return key, nil
}

// wechatSend gets a page of WeChat's login, with the code's own cookies.
func wechatSend(qr *wechatQR, raw, referer string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(qr.ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	res, err := qr.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, loginHTTPError(res.StatusCode)
	}
	return readLimited(res, limit)
}

// checkWechatQR asks WeChat whether its code was scanned, and signs in once
// it was.
func checkWechatQR(key string, login qrLogin) (code int, msg, nickname, avatar string, err error) {
	defer func() { err = loginFailure("qr-poll", err) }()
	qr := login.wechat
	qr.mu.Lock()
	defer qr.mu.Unlock()
	q := url.Values{"uuid": {qr.uuid}, "_": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}
	if qr.scanned {
		q.Set("last", "404")
	}
	b, err := wechatSend(qr, wechatPoll+"/connect/l/qrconnect?"+q.Encode(), wechatURL+"/", 16<<10)
	if err != nil {
		// A code nobody scanned holds the poll open past the deadline: that
		// is the code waiting, not a failure.
		var network interface{ Timeout() bool }
		if errors.As(err, &network) && network.Timeout() {
			if qr.scanned {
				return QRScanned, "请在微信上确认登录", "", "", nil
			}
			return QRWaiting, "", "", "", nil
		}
		return 0, "", "", "", err
	}
	match := wechatStatus.FindSubmatch(b)
	if match == nil {
		return 0, "", "", "", errors.New("qq: malformed WeChat QR status")
	}
	switch string(match[1]) {
	case "408":
		if qr.scanned {
			return QRScanned, "请在微信上确认登录", "", "", nil
		}
		return QRWaiting, "", "", "", nil
	case "404":
		qr.scanned = true
		return QRScanned, "请在微信上确认登录", "", "", nil
	case "402", "403":
		CancelQR(key)
		return QRExpired, "二维码已失效", "", "", nil
	case "405":
		if len(match[2]) == 0 {
			return 0, "", "", "", errors.New("qq: missing WeChat authorization code")
		}
		s, err := exchangeWechatSession(map[string]any{"code": string(match[2]), "strAppid": wechatApp}, nil, qr.device)
		if err != nil {
			return 0, "", "", "", err
		}
		if !finishQR(key, s) {
			return QRExpired, "二维码已失效", "", "", nil
		}
		qr.cancel()
		return QRConfirmed, "登录成功", s.Nickname, s.AvatarURL, nil
	default:
		return 0, "", "", "", fmt.Errorf("qq: WeChat QR status %s", match[1])
	}
}

// exchangeWechatSession trades a WeChat grant for a music key, as the phone
// the session was made from.
func exchangeWechatSession(param map[string]any, prior *Session, device *wechatDevice) (Session, error) {
	comm := device.comm()
	comm["tmeLoginType"] = 1
	if prior != nil {
		comm["qq"], comm["authst"] = prior.MusicID, prior.MusicKey
	}
	body, err := json.Marshal(map[string]any{
		"comm": comm,
		"req":  map[string]any{"module": "music.login.LoginServer", "method": "Login", "param": param},
	})
	if err != nil {
		return Session{}, loginFailure("qq-login", err)
	}
	req, err := http.NewRequest(http.MethodPost, musicuURL, bytes.NewReader(body))
	if err != nil {
		return Session{}, loginFailure("qq-login", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://y.qq.com/")
	req.Header.Set("Content-Type", "application/json")
	if prior != nil {
		req.Header.Set("Cookie", "uin="+prior.MusicID+"; qqmusic_uin="+prior.MusicID+"; qm_keyst="+prior.MusicKey+"; qqmusic_key="+prior.MusicKey)
	}
	read, err := loginRead(req, 16<<10)
	if err != nil {
		return Session{}, loginFailure("qq-login", err)
	}
	s, err := decodeLoginResponse(read, 1)
	s.Device = device
	return s, loginFailure("qq-login", err)
}

// refreshWechatSession signs a WeChat session in again from the token its
// login left, so that the listener does not scan again while the token
// lasts.
func refreshWechatSession(s Session) (Session, error) {
	device := s.Device
	if device == nil {
		var err error
		device, err = bootstrapWechatDevice()
		if err != nil {
			return Session{}, err
		}
	}
	refreshed, err := exchangeWechatSession(map[string]any{
		"openid": s.OpenID, "refresh_token": s.RefreshToken, "str_musicid": s.MusicID,
		"musickey": s.MusicKey, "unionid": s.UnionID, "refresh_key": s.RefreshKey, "loginMode": 2,
	}, &s, device)
	if err != nil {
		return Session{}, err
	}
	if refreshed.OpenID == "" {
		refreshed.OpenID = s.OpenID
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = s.RefreshToken
	}
	if refreshed.UnionID == "" {
		refreshed.UnionID = s.UnionID
	}
	if refreshed.RefreshKey == "" {
		refreshed.RefreshKey = s.RefreshKey
	}
	return refreshed, nil
}
