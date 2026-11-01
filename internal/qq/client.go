package qq

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// The transport the rest of the package is built on: QQ sends its answers
// in two shapes, one JSON object from the musicu endpoint and one JSON or
// JSONP body from c.y.qq.com, and both name their status differently.
//
// A call is answered with the account's cookies when there is a session,
// and without them when there is not: what needs one fails, and what does
// not still works, which is what lets the listener browse QQ before
// signing in.

// errUnsupported is what a call says for a thing QQ does not offer: there
// is no such endpoint, and no pretending there is.
var errUnsupported = fmt.Errorf("qq: not supported")

// The two hosts the package talks to. They are variables so a test can
// point them at a server of its own; nothing else sets them.
var (
	musicuURL = "https://u.y.qq.com/cgi-bin/musicu.fcg"
	cgiURL    = "https://c.y.qq.com"
)

// rpc asks one module and method of musicu, and reads its answer as out.
func rpc(module, method string, param any, out any) error {
	return rpcWith(module, method, param, out, nil, nil)
}

// ProtocolError is QQ's refusal, with both of its codes: the gateway's,
// which says whether QQ music as a service took the call at all, and the
// call's own.
type ProtocolError struct {
	GlobalCode int
	Code       int
	Module     string
	Method     string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("qq: %s/%s: gateway %d code %d", e.Module, e.Method, e.GlobalCode, e.Code)
}

// rpcWith is rpc with the comm and the headers a few calls need of their
// own: QQ's search answers only to the identity of its lite client, and
// the chart list only to the mobile one.
func rpcWith(module, method string, param any, out any, extra map[string]any, headers map[string]string) error {
	call := map[string]any{
		"comm":  commWith(extra),
		"req_0": map[string]any{"module": module, "method": method, "param": param},
	}
	body, err := json.Marshal(call)
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
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	read, err := readHTTP(req)
	if err != nil {
		return err
	}
	// The status is outside the answer, at two levels, and a call that
	// failed may still carry a `data` that says nothing.
	var res struct {
		Code *int `json:"code"`
		Req  *struct {
			Code    *int            `json:"code"`
			Subcode int             `json:"subcode"`
			Data    json.RawMessage `json:"data"`
		} `json:"req_0"`
	}
	if err := json.Unmarshal(read, &res); err != nil {
		return fmt.Errorf("qq: malformed answer: %w", err)
	}
	if res.Code == nil || res.Req == nil || res.Req.Code == nil {
		return fmt.Errorf("qq: no status in the answer")
	}
	if *res.Code != 0 || *res.Req.Code != 0 || res.Req.Subcode != 0 {
		code := *res.Req.Code
		if code == 0 {
			code = res.Req.Subcode
		}
		return &ProtocolError{GlobalCode: *res.Code, Code: code, Module: module, Method: method}
	}
	if out == nil {
		return nil
	}
	if len(res.Req.Data) == 0 {
		return fmt.Errorf("qq: %s/%s answered nothing", module, method)
	}
	return json.Unmarshal(res.Req.Data, out)
}

// cgi asks one of the older endpoints under c.y.qq.com, which answer with
// a JSON body, or with JSONP, and sometimes in GB18030.
func cgi(path string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("format", "json")
	q.Set("inCharset", "utf8")
	q.Set("outCharset", "utf-8")
	req, err := http.NewRequest(http.MethodGet, cgiURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://y.qq.com/")
	return cgiDo(req, path, out)
}

// cgiDo is cgi with the request built by its caller: a few endpoints want
// a query of their own shape, or a cookie.
func cgiDo(req *http.Request, path string, out any) error {
	read, err := readHTTP(req)
	if err != nil {
		return err
	}
	if !utf8.Valid(read) {
		if read, err = simplifiedchinese.GB18030.NewDecoder().Bytes(read); err != nil {
			return err
		}
	}
	read = bytes.TrimSpace(read)
	if m := jsonpPattern.FindSubmatch(read); m != nil {
		read = m[1]
	}
	var status struct {
		Code    *int `json:"code"`
		Subcode int  `json:"subcode"`
	}
	if err := json.Unmarshal(read, &status); err != nil {
		return fmt.Errorf("qq: %s: %w", path, err)
	}
	if status.Code == nil {
		return fmt.Errorf("qq: %s: no status in the answer", path)
	}
	if *status.Code != 0 || status.Subcode != 0 {
		code := *status.Code
		if code == 0 {
			code = status.Subcode
		}
		return fmt.Errorf("qq: %s: code %d", path, code)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(read, out)
}

var jsonpPattern = regexp.MustCompile(`(?s)^\w+\((.*)\);?\s*$`)

// readHTTP sends req, with the session's cookies when there is one, and
// returns the body.
func readHTTP(req *http.Request) ([]byte, error) {
	if s := currentSession(); s.MusicKey != "" {
		req.Header.Set("Cookie", "uin="+s.MusicID+"; qqmusic_uin="+s.MusicID+
			"; qqmusic_key="+s.MusicKey+"; qm_keyst="+s.MusicKey)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("qq: HTTP %s", res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 32<<20))
}

// commWith is what every musicu call carries: the client the app says it
// is, and, signed in, who is asking. The extra keys a few calls need are
// laid over it.
func commWith(extra map[string]any) map[string]any {
	c := comm()
	s := currentSession()
	if s.MusicKey != "" {
		c["uin"] = s.MusicID
		c["authst"] = s.MusicKey
		c["tmeLoginType"] = strconv.Itoa(s.LoginType)
	}
	for k, v := range extra {
		c[k] = v
	}
	return c
}

// object is an answer whose fields the app reads by trying several names:
// QQ names the same field differently in its search, its playlists and its
// charts.
type object map[string]json.RawMessage

func obj(raw json.RawMessage) object { var o object; _ = json.Unmarshal(raw, &o); return o }

func text(o object, keys ...string) string {
	for _, k := range keys {
		var s string
		if json.Unmarshal(o[k], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

func number(o object, keys ...string) int64 {
	for _, k := range keys {
		var n json.Number
		if json.Unmarshal(o[k], &n) == nil {
			if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
				return i
			}
		}
		var s string
		if json.Unmarshal(o[k], &s) == nil {
			if i, err := strconv.ParseInt(s, 10, 64); err == nil {
				return i
			}
		}
	}
	return 0
}

func objects(o object, keys ...string) []object {
	for _, k := range keys {
		var out []object
		if json.Unmarshal(o[k], &out) == nil && out != nil {
			return out
		}
	}
	return nil
}

// textOr is the first of the values that is not empty.
func textOr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// picture resizes one of QQ's covers: its addresses name the size in the
// file name, as T002R300x300M000<mid>.jpg.
var pictureSize = regexp.MustCompile(`(T00[12])R\d+x\d+M000`)

// Sized asks QQ's image server for a cover of n×n pixels.
func Sized(pic string, n int) string {
	if pic == "" {
		return ""
	}
	if pictureSize.MatchString(pic) {
		return pictureSize.ReplaceAllString(pic, "${1}R"+strconv.Itoa(n)+"x"+strconv.Itoa(n)+"M000")
	}
	return pic
}

// stringsJoin joins what is not empty, for an artist's credits.
func stringsJoin(values []string, sep string) string {
	var kept []string
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			kept = append(kept, v)
		}
	}
	return strings.Join(kept, sep)
}
