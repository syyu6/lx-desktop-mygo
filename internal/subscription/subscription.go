// Package subscription keeps the playlists the listener follows from
// another service, as lx-music-desktop's 收藏歌单 does: the playlist is
// copied into the app, and fetched anew when the listener asks or as the
// app opens, so that its songs show without asking the service each time.
package subscription

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"netease-music-mygo/internal/music"
)

// Subscription is a playlist the listener follows: where it is from, its
// id there, and its songs as last fetched.
type Subscription struct {
	Source string `json:"source"` // the playlist's source id
	ID     string `json:"id"`     // its id in that source
	Name   string `json:"name"`
	Cover  string `json:"cover,omitempty"`
	Author string `json:"author,omitempty"`
	Desc   string `json:"desc,omitempty"`
	Count  int    `json:"count,omitempty"`
	// UpdatedAt is when its songs were last fetched, or zero for never.
	UpdatedAt time.Time `json:"updatedAt"`
	// AutoUpdate fetches the songs again as the app opens.
	AutoUpdate bool `json:"autoUpdate,omitempty"`
	// Tracks are the songs it had when it was fetched: they show while
	// the service is unreachable.
	Tracks []music.Track `json:"tracks,omitempty"`
}

// Key names the subscription: its source and its id there, which is how
// two playlists of different services stay apart.
func (s *Subscription) Key() string { return s.Source + "/" + s.ID }

// FromPlaylist is a playlist as a subscription keeps it.
func FromPlaylist(pl *music.Playlist) *Subscription {
	key := pl.KeyOf()
	return &Subscription{
		Source:     pl.Src(),
		ID:         key,
		Name:       pl.Name,
		Cover:      pl.Cover(),
		Author:     pl.Creator.Nickname,
		Desc:       pl.Description,
		Count:      pl.TrackCount,
		UpdatedAt:  time.Now(),
		AutoUpdate: true,
		Tracks:     pl.Tracks,
	}
}

// Update takes what a playlist is now: its name and songs, but whether
// the listener wants it fetched as the app opens, which is theirs.
func (s *Subscription) Update(pl *music.Playlist) {
	auto := s.AutoUpdate
	name := pl.Name
	if name == "" {
		name = s.Name
	}
	*s = *FromPlaylist(pl)
	s.Name, s.AutoUpdate = name, auto
}

// Store keeps the subscriptions in a file, read once and written as they
// change.
type Store struct {
	path string
	list []*Subscription
}

// NewStore reads the subscriptions kept at path, or returns an empty store
// where there is no file yet.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	return s, json.Unmarshal(b, &s.list)
}

// List returns the subscriptions, as they were added.
func (s *Store) List() []*Subscription { return slices.Clone(s.list) }

// Get returns the subscription of a key.
func (s *Store) Get(key string) *Subscription {
	for _, sub := range s.list {
		if sub.Key() == key {
			return sub
		}
	}
	return nil
}

// Add keeps a playlist as a subscription, and returns it: a playlist
// already kept is fetched anew rather than added twice.
func (s *Store) Add(pl *music.Playlist) (*Subscription, error) {
	key := pl.Src() + "/" + pl.KeyOf()
	if sub := s.Get(key); sub != nil {
		sub.Update(pl)
		return sub, s.save()
	}
	sub := FromPlaylist(pl)
	s.list = append(s.list, sub)
	return sub, s.save()
}

// Update takes a playlist's songs as they are now, for the subscription of
// its key: false if it is not a subscription.
func (s *Store) Update(key string, pl *music.Playlist) (bool, error) {
	sub := s.Get(key)
	if sub == nil {
		return false, nil
	}
	sub.Update(pl)
	return true, s.save()
}

// SetAutoUpdate says whether to fetch a subscription as the app opens.
func (s *Store) SetAutoUpdate(key string, on bool) error {
	sub := s.Get(key)
	if sub == nil {
		return nil
	}
	sub.AutoUpdate = on
	return s.save()
}

// Remove forgets a subscription.
func (s *Store) Remove(key string) error {
	s.list = slices.DeleteFunc(s.list, func(sub *Subscription) bool { return sub.Key() == key })
	return s.save()
}

// save writes the subscriptions: to a file beside, renamed into place, so
// that a crash as it is written leaves the old ones whole.
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.list, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	part := s.path + ".part"
	if err := os.WriteFile(part, b, 0o600); err != nil {
		return err
	}
	return os.Rename(part, s.path)
}

// ParseLink reads a playlist's id from what the listener pasted: an id,
// or a link to the playlist on its service, and which source it is of.
//
// A service names its playlists in its links as its API expects, and the
// patterns are lx-music-desktop's: Kuwo's `playlist_detail/<id>` or
// `playlist/<id>`, QQ's `/playlist/<id>`, Kugou's `/<id>.html`, NetEase's
// `id=<id>`, Migu's `/playlist/<id>`.
func ParseLink(source, text string) (string, bool) {
	text = trimSpace(text)
	source = canonical(source)
	if text == "" || source == "" {
		return "", false
	}
	if isDigits(text) {
		return text, true
	}
	var pats []string
	switch source {
	case music.SourceNetease:
		pats = []string{"[?&]id=(\\d+)", "/playlist/(\\d+)"}
	case music.SourceKuwo:
		pats = []string{"/playlist(?:_detail)?/(\\d+)"}
	case music.SourceQQ:
		pats = []string{"/playlist/(\\d+)", "[?&]id=(\\d+)"}
	case music.SourceKugou:
		pats = []string{"/special/single/(\\d+)", "/(\\d+)\\.html", "[?&]specialid=(\\d+)"}
	case music.SourceMigu:
		pats = []string{"/playlist/(\\d+)", "[?&]playlistId=(\\d+)"}
	}
	for _, p := range pats {
		if id, ok := matchID(p, text); ok {
			return id, true
		}
	}
	return "", false
}

// Sources from a link: which service a pasted link is of, for a listener
// who pastes one rather than chooses.
func SourceOf(text string) (string, bool) {
	switch {
	case contains(text, "music.163.com"):
		return music.SourceNetease, true
	case contains(text, "kuwo.cn"):
		return music.SourceKuwo, true
	case contains(text, "y.qq.com"), contains(text, "qq.com"):
		return music.SourceQQ, true
	case contains(text, "kugou.com"):
		return music.SourceKugou, true
	case contains(text, "migu.cn"), contains(text, "nf.migu.cn"):
		return music.SourceMigu, true
	}
	return "", false
}

// IDString is an id as the sources keep it: the number, or the string
// where the source names playlists otherwise.
func IDString(n int64) string { return strconv.FormatInt(n, 10) }

// canonical is a source id as the app names it, from either the app's own
// name or lx-music-desktop's: a listener may paste either.
func canonical(id string) string {
	switch id {
	case music.SourceNetease, "wy":
		return music.SourceNetease
	case music.SourceKuwo:
		return music.SourceKuwo
	case music.SourceKugou:
		return music.SourceKugou
	case music.SourceQQ:
		return music.SourceQQ
	case music.SourceMigu:
		return music.SourceMigu
	}
	return ""
}
