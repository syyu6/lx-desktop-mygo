package netease

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// The listener's session is NetEase's login cookie, MUSIC_U, which every
// request carries once signed in. It is kept in a private file in the app's
// user data directory (0600), not in the system's keychain.
//
// The keychain was the wrong place for it on macOS. The app is signed ad
// hoc, so every release is a different binary, and the Keychain records
// the binary it let read an item: an update then makes the new one ask
// "LxMuc wants to use your confidential information stored in …", on the
// first launch after every release, which reads as the app asking for the
// password of something. The entry was only as private as the Keychain's
// item name (the app's identifier), so a file under the user data
// directory, which is already 0700 and holds the listener's songs and
// settings, keeps it no less well. Linux and Windows were files already,
// for the same reason.

const sessionSecret = "netease-session"

// sessionDir is where the session's file is, the user data directory: a
// test points it elsewhere, as the app there has none of its own.
var sessionDir = func() (string, error) { return mygo.App.Path(mygo.PathUserData) }

func sessionFile() (string, error) {
	dir, err := sessionDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session"), nil
}

func readSession() ([]byte, error) {
	path, err := sessionFile()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func writeSession(b []byte) error {
	path, err := sessionFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func deleteSession() {
	if path, err := sessionFile(); err == nil {
		os.Remove(path)
	}
}

var siteURL, _ = url.Parse(apiBase)

func init() {
	httpClient.Jar, _ = cookiejar.New(nil)
}

// adoptOnce keeps the read of the keychain's entry to one a process, and
// nothing at all once the session is in its file.
var adoptOnce sync.Once

// adoptKeychainSession takes the session a release before 0.1.26 kept in
// the keychain, once, into the file it is kept in now: the listener stays
// signed in across the update instead of being asked to sign in again.
//
// The Keychain asks before letting this binary read the entry, as it asks
// of every new binary — so the first launch after this update asks, for the
// last time. It is left in place rather than deleted: deleting it needs the
// item's ACL opened as well, which asks again for a listener who chose
// "Allow" over "Always Allow", and two prompts are worse than an entry
// nothing reads any more.
func adoptKeychainSession() []byte {
	var adopted []byte
	adoptOnce.Do(func() {
		b, err := mygo.Secrets.Get(sessionSecret)
		if err != nil || len(b) == 0 {
			return
		}
		if err := writeSession(b); err != nil {
			return
		}
		adopted = b
	})
	return adopted
}

// RestoreSession puts the session from its file in the cookie jar, and
// reports whether there was one.
func RestoreSession() bool {
	b, err := readSession()
	if err == nil && len(b) == 0 {
		// Signed in by a release before 0.1.26, which kept it in the
		// keychain: taken from there once, and put where it is kept now.
		b = adoptKeychainSession()
	}
	if err != nil || len(b) == 0 {
		return false
	}
	var cookies []*http.Cookie
	for _, part := range strings.Split(string(b), ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			cookies = append(cookies, &http.Cookie{Name: name, Value: value, Path: "/"})
		}
	}
	httpClient.Jar.SetCookies(siteURL, cookies)
	return true
}

// SaveSession keeps the login cookies of the jar in the keychain.
func SaveSession() error {
	var parts []string
	for _, c := range httpClient.Jar.Cookies(siteURL) {
		if c.Name == "MUSIC_U" || c.Name == "__csrf" {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	if len(parts) == 0 {
		return errors.New("no session cookie")
	}
	return writeSession([]byte(strings.Join(parts, "; ")))
}

// ClearSession forgets the session, here and in the keychain.
func ClearSession() {
	httpClient.Jar.SetCookies(siteURL, []*http.Cookie{
		{Name: "MUSIC_U", Path: "/", MaxAge: -1},
		{Name: "__csrf", Path: "/", MaxAge: -1},
	})
	deleteSession()
}

type Profile struct {
	UserID    int64  `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}

// Account returns the profile of who is signed in, or nil.
func Account() (*Profile, error) {
	var res struct {
		Profile *Profile `json:"profile"`
	}
	err := get("/api/nuser/account/get", nil, &res)
	return res.Profile, err
}

// QRKey starts a QR code login: the code shows LoginURL(key). The login
// goes through the eapi, as the desktop client's does.
func QRKey() (string, error) {
	var res struct {
		Code   int    `json:"code"`
		Unikey string `json:"unikey"`
	}
	if err := eapiPost("/api/login/qrcode/unikey", map[string]any{"type": 3}, &res); err != nil {
		return "", err
	}
	if res.Unikey == "" {
		return "", fmt.Errorf("netease: no QR key (code %d)", res.Code)
	}
	return res.Unikey, nil
}

func LoginURL(key string) string { return "https://music.163.com/login?codekey=" + key }

// QR login states, as NetEase reports them.
const (
	QRExpired   = 800
	QRWaiting   = 801
	QRScanned   = 802
	QRConfirmed = 803
)

// QRCheck reports where the login of key is: a QR state, or another code
// with NetEase's message when it refuses. Once scanned, it names who
// scanned; once confirmed, the session cookie is in the jar.
func QRCheck(key string) (code int, msg, nickname, avatar string, err error) {
	var r struct {
		Code      int    `json:"code"`
		Message   string `json:"message"`
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatarUrl"`
	}
	err = eapiPost("/api/login/qrcode/client/login", map[string]any{"key": key, "type": 3}, &r)
	return r.Code, r.Message, r.Nickname, r.AvatarURL, err
}

// Logout ends the session with NetEase too.
func Logout() {
	post("/api/logout", url.Values{}, &struct{}{})
	ClearSession()
}

// UserPlaylists returns the playlists a listener made, the liked songs
// first, and those they saved.
func UserPlaylists(uid int64) (made, saved []Playlist, err error) {
	var res struct {
		Playlist []struct {
			Playlist
			Subscribed bool `json:"subscribed"`
		} `json:"playlist"`
	}
	q := url.Values{"uid": {strconv.FormatInt(uid, 10)}, "limit": {"200"}, "offset": {"0"}}
	if err := get("/api/user/playlist", q, &res); err != nil {
		return nil, nil, err
	}
	for _, p := range res.Playlist {
		if p.Subscribed {
			saved = append(saved, p.Playlist)
		} else {
			made = append(made, p.Playlist)
		}
	}
	return made, saved, nil
}

// RecommendedPlaylists returns the day's playlists picked for the listener.
func RecommendedPlaylists() ([]Playlist, error) {
	var res struct {
		Recommend []struct {
			Playlist
			Playcount float64 `json:"playcount"`
		} `json:"recommend"`
	}
	if err := get("/api/v1/discovery/recommend/resource", nil, &res); err != nil {
		return nil, err
	}
	out := make([]Playlist, len(res.Recommend))
	for i, r := range res.Recommend {
		out[i] = r.Playlist
		out[i].PlayCount = r.Playcount
	}
	return out, nil
}

// LikedIDs returns the songs a listener likes, in no order.
func LikedIDs(uid int64) ([]int64, error) {
	var res struct {
		Code int     `json:"code"`
		IDs  []int64 `json:"ids"`
	}
	if err := eapiPost("/api/song/like/get", map[string]any{"uid": strconv.FormatInt(uid, 10)}, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, fmt.Errorf("netease: liked songs: code %d", res.Code)
	}
	return res.IDs, nil
}

// Play is a song the listener played, on one of their devices.
type Play struct {
	Track Track
	At    time.Time
	// Device is the system NetEase names the device by: iphone, android,
	// osx, pc, web…
	Device string
}

// RecentPlays returns the songs the listener played last, on any device
// signed in as them, the latest first.
func RecentPlays(limit int) ([]Play, error) {
	var res struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Data     Track `json:"data"`
				PlayTime int64 `json:"playTime"` // milliseconds
				Terminal struct {
					OS string `json:"os"`
				} `json:"multiTerminalInfo"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := eapiPost("/api/play-record/song/list", map[string]any{"limit": limit}, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, fmt.Errorf("netease: recent plays: code %d", res.Code)
	}
	plays := make([]Play, len(res.Data.List))
	songs := make([]Track, len(plays))
	for i, l := range res.Data.List {
		songs[i] = l.Data
		plays[i] = Play{At: time.UnixMilli(l.PlayTime), Device: l.Terminal.OS}
	}
	// They come without privileges, as the daily songs do.
	detailPrivileges(songs)
	for i := range plays {
		plays[i].Track = songs[i]
	}
	return plays, nil
}

// Like likes a song, adding it to the listener's liked songs, or unlikes it.
func Like(id int64, like bool) error {
	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	data := map[string]any{"alg": "itembased", "trackId": strconv.FormatInt(id, 10), "like": strconv.FormatBool(like), "time": "3"}
	if err := eapiPost("/api/radio/like", data, &res); err != nil {
		return err
	}
	if res.Code != 200 {
		return fmt.Errorf("netease: like: code %d %s", res.Code, res.Msg)
	}
	return nil
}
