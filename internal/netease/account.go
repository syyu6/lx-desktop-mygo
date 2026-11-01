package netease

import (
	"netease-music-mygo/internal/account"
	"netease-music-mygo/internal/music"
)

// NetEase's account, as the app asks every source for one.
type service struct{}

func (service) Source() string { return music.SourceNetease }

func (service) Restore() bool { return RestoreSession() }

func (service) Profile() (*account.Profile, error) {
	p, err := Account()
	if err != nil || p == nil {
		return nil, err
	}
	return &account.Profile{
		Source: music.SourceNetease, UserID: p.UserID,
		Nickname: p.Nickname, AvatarURL: p.AvatarURL,
	}, nil
}

func (service) Logout() error { Logout(); return nil }

// NetEase's QR sign-in: the code is the address the app opens, and its own
// states are the ones every source's are.
type login struct{}

func (login) Source() string { return music.SourceNetease }

func (login) Code(string) (account.Code, error) {
	key, err := QRKey()
	if err != nil {
		return account.Code{}, err
	}
	return account.Code{Key: key, URL: LoginURL(key)}, nil
}

func (login) Check(key string) (int, string, string, string, error) {
	code, msg, name, avatar, err := QRCheck(key)
	// NetEase refuses a login with a code of its own rather than a state:
	// it is passed on as it came, and the dialog shows the message with it.
	return code, msg, name, avatar, err
}

// Failed reads a refused poll. NetEase's poll is a plain call: one that
// fails is made again, and the code's own expiry ends the login.
func (login) Failed(error) (string, bool) { return "", false }

func (login) Save() error { return SaveSession() }

func (login) Cancel(string) {}

// The account's own words for what it is: a listener's membership, which
// NetEase does not state in the session, and a nickname to show.
func init() { account.Register(service{}, login{}) }
