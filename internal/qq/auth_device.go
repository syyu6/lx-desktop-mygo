package qq

// The device identity a WeChat login is made with.
//
// QQ Music's WeChat login is the phone app's, not the web page's: its call
// says which phone it is from, and QQ's servers check that the identifiers
// a phone has are there and plausible. None of the phone's own is read from
// this machine — it has none — so they are made up, as the reference client
// makes them up: a random Android id, a random device id, and an IMEI whose
// check digit makes it a well-formed number.
//
// The qimei is QQ's own device fingerprint, which its server issues in
// answer to this identity. It is asked for once per login, and kept with the
// session so that a refresh is signed as the same phone.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"crypto/md5"
)

// wechatDevice is the phone a WeChat session says it is.
type wechatDevice struct {
	AndroidID string `json:"androidId"`
	OpenUDID  string `json:"openUdid"`
	IMEI      string `json:"imei"`
	Qimei     string `json:"qimei"`
	Qimei36   string `json:"qimei36"`
	UID       string `json:"uid,omitempty"`
	SID       string `json:"sid,omitempty"`
}

// comm is the identity every call of the session carries.
func (d *wechatDevice) comm() map[string]any {
	comm := map[string]any{
		"ct": 11, "cv": 14090008, "v": 14090008, "chid": "10003505", "tmeAppID": "qqmusic",
		"QIMEI": d.Qimei, "QIMEI36": d.Qimei36,
		"OpenUDID": d.OpenUDID, "udid": d.OpenUDID, "OpenUDID2": d.OpenUDID,
		"aid": d.AndroidID, "os_ver": "10", "phonetype": "MI 6", "devicelevel": "29",
		"newdevicelevel": "29", "rom": "xiaomi/iarim/sagit:10/eomam.200122.001/2001221:user/release-keys",
	}
	if d.UID != "" {
		comm["uid"], comm["sid"] = d.UID, d.SID
	}
	return comm
}

// deviceDigits is n random decimal digits.
func deviceDigits(n int) (string, error) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte('0' + byte(v.Int64()))
	}
	return b.String(), nil
}

// newWechatDevice makes up the phone a login is from.
func newWechatDevice() (*wechatDevice, error) {
	a, err := randomHex(8)
	if err != nil {
		return nil, err
	}
	u, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	imei, err := deviceDigits(14)
	if err != nil {
		return nil, err
	}
	// The IMEI's last digit is its Luhn check.
	sum := 0
	for i, c := range imei {
		n := int(c - '0')
		if i%2 == 1 {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
	}
	return &wechatDevice{AndroidID: a, OpenUDID: u, IMEI: imei + string(rune('0'+(10-sum%10)%10))}, nil
}

// protocolMD5 is QQ's signature over the parts of a call.
func protocolMD5(parts ...string) string {
	h := md5.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(h[:])
}

// qimeiEnvelope is the call that asks for a device's qimei, and the
// headers it is signed with. The key that signs the payload is itself
// RSA-wrapped for the server, and the payload AES-encrypted with it: that
// is the protocol, not a choice of this app's.
func qimeiEnvelope(d *wechatDevice, now time.Time) ([]byte, http.Header, error) {
	const appKey = "0AND0HD6FE4HY80F"
	const modulus = "c4231830a2eb5fc2827170641e79d80fec51bda9a22e4b4ab37d1f205a4ae44d928cda25879f66a3429051663312a127faf8a246bdaaf63918417e90d7c95b5908aa6a2d0f852e4a6770294a548ac1c2fe8f1f252fb826f4ac86ab9a00e7ce47d002a56e7c4b51eb889acc60ca6adbc9f72e81f4d31b1dd7464805264530ab1d"
	cryptKey, err := randomHex(8)
	if err != nil {
		return nil, nil, err
	}
	nonce, err := randomHex(8)
	if err != nil {
		return nil, nil, err
	}
	first, err := deviceDigits(6)
	if err != nil {
		return nil, nil, err
	}
	second, err := deviceDigits(9)
	if err != nil {
		return nil, nil, err
	}
	// The beacon is the phone's install dates and ids over 40 slots; QQ
	// asks for all of them, and this app has real ones for none.
	dated := map[int]bool{1: true, 2: true, 13: true, 14: true, 17: true, 18: true, 21: true, 22: true, 25: true, 26: true, 29: true, 30: true, 33: true, 34: true, 37: true, 38: true}
	var beacon strings.Builder
	for i := 1; i <= 40; i++ {
		v := now.UTC().Format("2006-01") + "-01" + first + "." + second
		switch {
		case i == 3:
			v = "0000000000000000"
		case i == 4:
			v = strings.ReplaceAll(cryptKey, "0", "1")
		case !dated[i]:
			v, err = deviceDigits(4)
			if err != nil {
				return nil, nil, err
			}
		}
		fmt.Fprintf(&beacon, "k%d:%s;", i, v)
	}
	reserved, _ := json.Marshal(map[string]any{
		"harmony": "0", "clone": "0", "containe": "",
		"oz": "UhYmelwouA+V2nPWbOvLTgN2/m8jwGB+yUB5v9tysQg=", "oo": "Xecjt+9S1+f8Pz2VLSxgpw==",
		"kelong": "0", "uptimes": now.UTC().Add(-time.Hour).Format("2006-01-02 15:04:05"),
		"multiUser": "0", "bod": "Xiaomi", "dv": "sagit", "firstLevel": "",
		"manufact": "Xiaomi", "name": "MI 6", "host": "se.infra",
		"kernel": "Linux 5.4.0-54-generic (android-build@google.com)",
	})
	payload, _ := json.Marshal(map[string]any{
		"androidId": d.AndroidID, "platformId": 1, "appKey": appKey,
		"appVersion": "14.9.0.8", "beaconIdSrc": beacon.String(),
		"brand": "Xiaomi", "channelId": "10003505", "cid": "", "imei": d.IMEI,
		"imsi": "", "mac": "", "model": "MI 6", "networkType": "unknown", "oaid": "",
		"osVersion": "Android 10,level 29", "qimei": "", "qimei36": "",
		"sdkVersion": "1.2.13.6", "targetSdkVersion": "33", "audit": "",
		"userId": "{}", "packageId": "com.tencent.qqmusic", "deviceType": "Phone",
		"sdkName": "", "reserved": string(reserved),
	})
	n, _ := new(big.Int).SetString(modulus, 16)
	wrapped, err := rsa.EncryptPKCS1v15(rand.Reader, &rsa.PublicKey{N: n, E: 65537}, []byte(cryptKey))
	if err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher([]byte(cryptKey))
	if err != nil {
		return nil, nil, err
	}
	pad := aes.BlockSize - len(payload)%aes.BlockSize
	payload = append(payload, bytes.Repeat([]byte{byte(pad)}, pad)...)
	ciphertext := make([]byte, len(payload))
	cipher.NewCBCEncrypter(block, []byte(cryptKey)).CryptBlocks(ciphertext, payload)
	key, params := base64.StdEncoding.EncodeToString(wrapped), base64.StdEncoding.EncodeToString(ciphertext)
	ts := fmt.Sprint(now.Unix())
	extra := `{"appKey":"` + appKey + `"}`
	body, err := json.Marshal(map[string]any{"app": 0, "os": 1, "qimeiParams": map[string]any{
		"key": key, "params": params, "time": ts, "nonce": nonce,
		"sign":  protocolMD5(key, params, fmt.Sprint(now.Unix()*1000), nonce, "ZdJqM15EeO2zWc08", extra),
		"extra": extra,
	}})
	headers := http.Header{
		"Method": {"GetQimei"}, "Service": {"trpc.tme_datasvr.qimeiproxy.QimeiProxy"},
		"Appid": {"qimei_qq_android"}, "Sign": {protocolMD5("qimei_qq_androidpzAuCmaFAaFaHrdakPjLIEqKrGnSOOvH", ts)},
		"Timestamp": {ts}, "Content-Type": {"application/json"}, "User-Agent": {"QQMusic"},
	}
	return body, headers, err
}

// bootstrapWechatDevice asks QQ for the qimei of a made-up phone, and for
// the session every call of that phone is made with.
//
// A wrong clock is the one thing the qimei call refuses over, and its answer
// says the time: the call is made again with that.
func bootstrapWechatDevice() (*wechatDevice, error) {
	d, err := newWechatDevice()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for attempt := 0; attempt < 2; attempt++ {
		body, headers, err := qimeiEnvelope(d, now)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodPost, deviceURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header = headers
		res, err := loginTransport().Do(req)
		if err != nil {
			return nil, loginFailure("device-bootstrap", err)
		}
		if res.StatusCode < 200 || res.StatusCode >= 400 {
			res.Body.Close()
			return nil, loginFailure("device-bootstrap", loginHTTPError(res.StatusCode))
		}
		serverTime, _ := http.ParseTime(res.Header.Get("Date"))
		read, err := readLimited(res, 64<<10)
		if err != nil {
			return nil, loginFailure("device-bootstrap", err)
		}
		if err := readFingerprint(read, d); err != nil {
			return nil, err
		}
		if d.Qimei != "" && d.Qimei36 != "" {
			break
		}
		if attempt == 0 && !serverTime.IsZero() && now.Sub(serverTime).Abs() > time.Minute {
			now = serverTime
			continue
		}
		return nil, errors.New("qq: no device fingerprint")
	}
	// The session is asked for with the phone's own identity and nobody
	// else's: a call signed as the listener would hand back theirs. It goes
	// through the login's own client, so that no cookie of the app's travels
	// with a call that is deciding who this phone is.
	var data struct {
		Session struct {
			UID json.RawMessage `json:"uid"`
			SID json.RawMessage `json:"sid"`
		} `json:"session"`
	}
	err = loginRPC("music.getSession.session", "GetSession",
		map[string]any{"uid": "", "vkey": 0, "caller": 0}, d.comm(), &data)
	if err != nil {
		return nil, loginFailure("device-bootstrap", err)
	}
	d.UID, d.SID = loginText(data.Session.UID), loginText(data.Session.SID)
	if d.UID == "" || d.SID == "" {
		return nil, errors.New("qq: missing device session")
	}
	return d, nil
}

// readFingerprint reads the qimei out of the answer, which QQ sends as a
// JSON string holding the object, twice over.
func readFingerprint(read []byte, d *wechatDevice) error {
	var outer struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(read, &outer) != nil {
		return errors.New("qq: malformed device answer")
	}
	var inner struct {
		Data struct {
			Q16 string `json:"q16"`
			Q36 string `json:"q36"`
		} `json:"data"`
	}
	if json.Unmarshal(embedded(outer.Data), &inner) != nil {
		return errors.New("qq: malformed device answer")
	}
	d.Qimei, d.Qimei36 = inner.Data.Q16, inner.Data.Q36
	return nil
}

// embedded reads a field that QQ sends as a JSON string holding JSON.
func embedded(raw json.RawMessage) []byte {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []byte(text)
	}
	return raw
}

// loginText reads a field QQ sends as a string, a number, or nothing.
func loginText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	if len(raw) > 0 && string(raw) != "null" {
		return string(raw)
	}
	return ""
}
