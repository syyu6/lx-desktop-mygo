package qq

import (
	"errors"

	"netease-music-mygo/internal/account"
	"netease-music-mygo/internal/music"
)

// QQ Music's account, as the app asks every source for one: the session is
// the musicid and musickey QQ issued, kept in the user data directory.
type service struct{}

func (service) Source() string { return music.SourceQQ }

func (service) Restore() bool { return RestoreSession() }

func (service) Profile() (*account.Profile, error) {
	p, err := Account()
	if err != nil || p == nil {
		return nil, err
	}
	return &account.Profile{
		Source: music.SourceQQ, UserID: p.UserID,
		Nickname: p.Nickname, AvatarURL: p.AvatarURL, VIP: p.VIP,
	}, nil
}

func (service) Logout() error { Logout(); return nil }

// QQ Music's sign-in, which has two ways in: the code this app's QQ reads,
// and WeChat's. They leave the same session, and differ only in how the
// listener's phone confirms it.
type login struct{}

func (login) Source() string { return music.SourceQQ }

func (login) Code(via string) (account.Code, error) {
	if via == "wechat" {
		key, err := WechatQRKey()
		if err != nil {
			return account.Code{}, err
		}
		png, err := QRImage(key)
		if err != nil {
			CancelQR(key)
			return account.Code{}, err
		}
		return account.Code{Key: key, PNG: png, Via: "wechat"}, nil
	}
	key, err := QRKey()
	if err != nil {
		return account.Code{}, err
	}
	png, err := QRImage(key)
	if err != nil {
		CancelQR(key)
		return account.Code{}, err
	}
	return account.Code{Key: key, PNG: png}, nil
}

func (login) Check(key string) (int, string, string, string, error) {
	code, msg, name, avatar, err := QRCheck(key)
	// QQ's own failure text is shown for the state it refuses with: the
	// states it shares with every source are the ones the dialog knows.
	return code, msg, name, avatar, err
}

// Failed reads a refused poll. QQ's code is a walk over several of its
// pages, and a walk that broke says so in words the listener can act on: a
// code already scanned has been given up by the time its exchange fails,
// and asking about it again asks QQ for the grant a second time.
func (login) Failed(err error) (string, bool) {
	var failure *LoginError
	if !errors.As(err, &failure) {
		return "", false
	}
	switch failure.Step {
	case "qr-poll":
		// The poll itself did not arrive, or answered nothing readable: the
		// code is still good, and is asked about again.
		return "", false
	}
	return LoginErrorMessage(err), true
}

func (login) Save() error { return SaveSession() }

func (login) Cancel(key string) { CancelQR(key) }

func init() { account.Register(service{}, login{}) }
