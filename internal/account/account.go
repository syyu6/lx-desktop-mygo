// Package account is the app's accounts: one per source, each signed in on
// its own, and all of them at once.
//
// A source's session is its own — NetEase's is a cookie jar, QQ Music's a
// key and an id — and signing out of one leaves the others as they were.
// The app asks this package who is signed in, and asks a source to sign in
// with the code the listener scans, whichever source it is.
package account

import (
	"errors"

	"netease-music-mygo/internal/music"
)

// Profile is who a source says is listening.
type Profile struct {
	Source    string `json:"source"`
	UserID    int64  `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	// VIP is what the source says of the membership, in its own words, or
	// empty where the source does not say.
	VIP string `json:"vip,omitempty"`
}

// Service is a source the app can sign in to.
type Service interface {
	// Source is the source id the account belongs to.
	Source() string
	// Restore reads back the session kept from last time, and reports
	// whether there was one worth keeping.
	Restore() bool
	// Profile asks the source who is signed in through that session, or
	// answers nil for nobody.
	Profile() (*Profile, error)
	// Logout signs out here and on the source's side.
	Logout() error
}

// Code is a QR code to show the listener: the address of the code, or the
// code itself as the service drew it. A code the service drew is shown as
// it came, since a code re-drawn from an address is not the same code.
type Code struct {
	Key string `json:"key"`
	URL string `json:"url,omitempty"`
	PNG []byte `json:"-"`
	Via string `json:"via,omitempty"` // "wechat" for WeChat's code
}

// The states a code is in, as the services report them.
const (
	Expired   = 800
	Waiting   = 801
	Scanned   = 802
	Confirmed = 803
)

// Login is a source's QR sign-in.
type Login interface {
	// Source is the source id the login signs in to.
	Source() string
	// Code asks for a code. via names the way in where the source has more
	// than one (QQ's app, or WeChat); it is ignored elsewhere.
	Code(via string) (Code, error)
	// Check asks whether the code was scanned, and signs in once it was.
	Check(key string) (state int, msg, nickname, avatar string, err error)
	// Failed says what an error from Check means for the code: whether it
	// ends the login (a code that was scanned and whose exchange failed is
	// not one to ask about again), and what to tell the listener, in words.
	// An empty message and a false end is a call that may be made again.
	Failed(err error) (msg string, ended bool)
	// Save keeps the session the confirmed login left, for the next launch.
	Save() error
	// Cancel drops a code the listener closed, so that its result cannot
	// sign in afterwards.
	Cancel(key string)
}

// ErrNoLogin is what a source without a sign-in says: the app shows none for
// it rather than a code that goes nowhere.
var ErrNoLogin = errors.New("account: this source cannot be signed in to")

var (
	services = map[string]Service{}
	logins   = map[string]Login{}
)

// Register adds a source's account and, if it has one, its QR login. It is
// called by the source packages, which are where the sessions live.
func Register(s Service, l Login) {
	services[s.Source()] = s
	if l != nil {
		logins[s.Source()] = l
	}
}

// Services names the sources the app may sign in to, in the order they were
// registered: NetEase first, then QQ Music.
func Services() []string {
	order := []string{music.SourceNetease, music.SourceQQ}
	var ids []string
	for _, id := range order {
		if _, ok := services[id]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// Get is a source's account.
func Get(src string) (Service, bool) {
	s, ok := services[src]
	return s, ok
}

// LoginOf is a source's QR sign-in.
func LoginOf(src string) (Login, bool) {
	l, ok := logins[src]
	return l, ok
}

// ShortName is what the app calls a source in a sentence: 网易云, QQ.
func ShortName(src string) string {
	switch src {
	case music.SourceNetease:
		return "网易云"
	case music.SourceQQ:
		return "QQ 音乐"
	}
	return src
}
