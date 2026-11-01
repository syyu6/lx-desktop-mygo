package account

import (
	"testing"

	"netease-music-mygo/internal/music"
)

// A source is signed in on its own: a session restored for one says nothing
// of another, and a source with no sign-in is not offered one.
func TestServicesAreOnePerSource(t *testing.T) {
	// The app's own packages register the real ones, and this package must
	// not import them back: a test registers a pair of its own, and puts
	// the registry back as it was.
	before := map[string]Service{}
	for k, v := range services {
		before[k] = v
	}
	beforeLogins := map[string]Login{}
	for k, v := range logins {
		beforeLogins[k] = v
	}
	t.Cleanup(func() {
		services, logins = before, beforeLogins
	})
	Register(blankService{music.SourceQQ}, blankLogin{music.SourceQQ})
	Register(blankService{music.SourceNetease}, blankLogin{music.SourceNetease})

	ids := Services()
	if len(ids) != 2 || ids[0] != music.SourceNetease || ids[1] != music.SourceQQ {
		t.Fatalf("services %q: NetEase is not first, or QQ is not after it", ids)
	}
	for _, id := range ids {
		if _, ok := Get(id); !ok {
			t.Errorf("%s has no account", id)
		}
		if _, ok := LoginOf(id); !ok {
			t.Errorf("%s has no sign-in", id)
		}
	}
	// A source nobody registered is not named, and has neither.
	for _, id := range []string{music.SourceKuwo, music.SourceKugou, music.SourceMigu} {
		if _, ok := Get(id); ok {
			t.Errorf("%s has an account it should not", id)
		}
		if _, ok := LoginOf(id); ok {
			t.Errorf("%s has a sign-in it should not", id)
		}
	}
}

// A source's name in a sentence is the short one, in the app's language.
func TestShortNames(t *testing.T) {
	for src, want := range map[string]string{
		music.SourceNetease: "网易云",
		music.SourceQQ:      "QQ 音乐",
	} {
		if got := ShortName(src); got != want {
			t.Errorf("ShortName(%q) = %q, want %q", src, got, want)
		}
	}
}

type blankService struct{ id string }

func (s blankService) Source() string             { return s.id }
func (s blankService) Restore() bool              { return false }
func (s blankService) Profile() (*Profile, error) { return nil, nil }
func (s blankService) Logout() error              { return nil }

type blankLogin struct{ id string }

func (l blankLogin) Source() string                                    { return l.id }
func (l blankLogin) Code(string) (Code, error)                         { return Code{}, nil }
func (l blankLogin) Check(string) (int, string, string, string, error) { return 0, "", "", "", nil }
func (l blankLogin) Failed(error) (string, bool)                       { return "", false }
func (l blankLogin) Save() error                                       { return nil }
func (l blankLogin) Cancel(string)                                     {}
