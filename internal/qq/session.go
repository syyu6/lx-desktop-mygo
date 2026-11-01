package qq

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// Session is QQ Music's own credential: the account's id, the key QQ signs
// its calls with, and what kind of login made it. It is what the app keeps
// between launches, and what every call is made with while there is one.
//
// QQ's own cookies are transient and not kept: a musicid and a musickey
// are what its app holds, and what this holds.
type Session struct {
	MusicID string `json:"musicid"`
	// MusicKey is the key the calls are signed with, and expires.
	MusicKey string `json:"musickey"`
	// LoginType is how it was made: 1 for WeChat, 2 for QQ.
	LoginType int `json:"loginType,omitempty"`
	// CreatedAt is when QQ issued the key, and ExpiresIn how long it lasts:
	// together they say when a WeChat session is due to be refreshed.
	CreatedAt  int64  `json:"musickeyCreateTime,omitempty"`
	ExpiresIn  int64  `json:"keyExpiresIn,omitempty"`
	Nickname   string `json:"nick,omitempty"`
	AvatarURL  string `json:"logo,omitempty"`
	EncryptUin string `json:"encryptUin,omitempty"`

	// What a WeChat login keeps to sign in again without a scan. QQ's own
	// login has none of these: its key is the one it was given, and the
	// listener scans again when it expires, as its own clients do.
	OpenID       string `json:"openid,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	UnionID      string `json:"unionid,omitempty"`
	RefreshKey   string `json:"refresh_key,omitempty"`
	// Device is the WeChat session's own device identity, which the call
	// that refreshes it is made with.
	Device *wechatDevice `json:"device,omitempty"`
}

// loginKind is how the session was made. A session written before the kind
// was recorded is QQ's: that was the only way in then, and WeChat's is the
// one that needs the kind to be refreshed.
func (s Session) loginKind() int {
	if s.LoginType == 0 {
		return 2
	}
	return s.LoginType
}

// valid reports whether the session's key is one QQ may still accept.
func (s Session) valid() bool {
	if s.MusicKey == "" || s.MusicID == "" || (s.loginKind() != 1 && s.loginKind() != 2) {
		return false
	}
	id, err := strconv.ParseInt(s.MusicID, 10, 64)
	if err != nil || id <= 0 {
		return false
	}
	// A key without a date QQ gave is one whose age nobody knows: it is
	// tried, and the call that fails says whether it still works.
	return s.CreatedAt <= 0 || s.ExpiresIn <= 0 || time.Now().Unix() < s.CreatedAt+s.ExpiresIn
}

// canRefresh reports whether the session is a WeChat one that may be
// signed in again without the listener scanning.
func (s Session) canRefresh() bool {
	if s.loginKind() != 1 || s.MusicKey == "" || s.MusicID == "" {
		return false
	}
	if id, err := strconv.ParseInt(s.MusicID, 10, 64); err != nil || id <= 0 {
		return false
	}
	return s.RefreshKey != "" || s.RefreshToken != ""
}

// Profile is who a session belongs to, as the app shows them.
type Profile struct {
	UserID    int64  `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	VIP       string `json:"vip,omitempty"`
}

var sessionStore struct {
	sync.RWMutex
	session Session
}

// currentSession is the session every call is made with, or the zero
// Session when nobody is signed in.
func currentSession() Session {
	sessionStore.RLock()
	defer sessionStore.RUnlock()
	return sessionStore.session
}

// setSession puts a session where the calls find it, or clears it.
func setSession(s Session) {
	sessionStore.Lock()
	sessionStore.session = s
	sessionStore.Unlock()
}

// SignedIn reports whether the listener is signed in to QQ Music, with a
// key QQ may still take: a session whose key has run out is one the pages
// ask for a scan for, not one they call signed in.
func SignedIn() bool { return currentSession().valid() }

// sessionDir is where the session's file goes: the user data directory,
// which a test points elsewhere.
//
// It is a file and not the system's keychain, as NetEase's session is, and
// for the same reason: the app is signed ad hoc, so every release is a new
// binary, and a keychain entry records which binary may read it — an
// update then has the new one ask permission on its first launch.
var sessionDir = func() (string, error) { return mygo.App.Path(mygo.PathUserData) }

// SessionPath is where the session is kept, or "" if the app has no user
// data directory.
func SessionPath() string {
	dir, err := sessionDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "qq-session")
}

// RestoreSession reads the session kept from last time, and reports whether
// there was one that is still good, or one a WeChat login may sign in
// again from without a scan.
func RestoreSession() bool {
	path := SessionPath()
	if path == "" {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var s Session
	if json.Unmarshal(b, &s) != nil {
		return false
	}
	if !s.valid() && !s.canRefresh() {
		return false
	}
	setSession(s)
	return true
}

// SaveSession keeps the session for the next launch.
func SaveSession() error {
	s := currentSession()
	if s.MusicID == "" || s.MusicKey == "" {
		return os.ErrInvalid
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	path := SessionPath()
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// ClearSession forgets the session, here and on disk.
func ClearSession() {
	setSession(Session{})
	if path := SessionPath(); path != "" {
		os.Remove(path)
	}
}

// Logout is ClearSession under the name a sign-out button uses.
func Logout() { ClearSession() }

// Account asks QQ who is signed in through the session it has. A WeChat
// session past its key is signed in again from the token its login left, so
// that the listener does not scan again for it; a QQ one has no such token,
// and an expired one comes back nil for the page to ask for a scan.
//
// The name and the picture come from QQ rather than from the session: they
// are whatever the listener called themselves once the scan's copy was
// made, and only QQ knows that.
func Account() (*Profile, error) {
	s := currentSession()
	if !s.valid() {
		if !s.canRefresh() {
			return nil, nil
		}
		var err error
		if s, err = refreshWechatSession(s); err != nil {
			return nil, err
		}
		setSession(s)
		if err := SaveSession(); err != nil {
			return nil, err
		}
	}
	var data struct {
		Info map[string]json.RawMessage `json:"info"`
	}
	err := rpc("music.UserInfo.userInfoServer", "GetLoginUserInfo", map[string]any{}, &data)
	// A key the server no longer knows may have been rotated rather than
	// revoked: a WeChat session is signed in again once and asked again.
	if s.LoginType == 1 && isRejected(err) && s.canRefresh() {
		if refreshed, refreshErr := refreshWechatSession(s); refreshErr == nil {
			setSession(refreshed)
			if SaveSession() == nil {
				s = refreshed
				err = rpc("music.UserInfo.userInfoServer", "GetLoginUserInfo", map[string]any{}, &data)
			}
		}
	}
	if err != nil {
		if isRejected(err) {
			ClearSession()
			return nil, nil
		}
		return nil, err
	}
	p := &Profile{UserID: toInt(s.MusicID), Nickname: s.Nickname, AvatarURL: s.AvatarURL}
	for _, key := range []string{"nick", "nickname"} {
		var v string
		if json.Unmarshal(data.Info[key], &v) == nil && v != "" {
			p.Nickname = v
			break
		}
	}
	for _, key := range []string{"headurl", "logo", "avatarUrl", "avatar"} {
		var v string
		if json.Unmarshal(data.Info[key], &v) == nil && v != "" {
			p.AvatarURL = v
			break
		}
	}
	if p.Nickname == "" {
		p.Nickname = s.MusicID
	}
	return p, nil
}

// isRejected reports whether err is QQ saying the session is not one it
// knows: the codes a key that was revoked comes back with, not the one an
// expiring key gets.
func isRejected(err error) bool {
	var rejected *ProtocolError
	if !errors.As(err, &rejected) || rejected.GlobalCode != 0 {
		return false
	}
	return rejected.Code == 1000 || rejected.Code == 104400 || rejected.Code == 104401
}

// toInt reads an id QQ gives as a string of digits.
func toInt(s string) int64 {
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}
