package subscription

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"netease-music-mygo/internal/music"
)

func playlist(src string, id int64, name string, tracks ...string) *music.Playlist {
	pl := &music.Playlist{Source: src, ID: id, Name: name, TrackCount: len(tracks)}
	for i, n := range tracks {
		pl.Tracks = append(pl.Tracks, music.Track{Source: src, ID: int64(i + 1), Name: n})
	}
	return pl
}

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "subs.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A playlist kept as a subscription comes back after the app is opened
// again, songs and all.
func TestAddKeepsASubscription(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subs.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Add(playlist("kw", 12, "通勤", "甲", "乙"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if sub.Key() != "kw/12" || len(sub.Tracks) != 2 || !sub.AutoUpdate {
		t.Fatalf("sub %+v", sub)
	}
	again, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Get("kw/12")
	if got == nil || got.Name != "通勤" || len(got.Tracks) != 2 {
		t.Fatalf("read back %+v", got)
	}
	if got.Tracks[0].Src() != music.SourceKuwo {
		t.Errorf("song source %q", got.Tracks[0].Src())
	}
}

// A playlist kept again is fetched anew, not added twice, and the
// listener's choice to fetch it as the app opens stays theirs.
func TestAddAgainUpdates(t *testing.T) {
	s := newStore(t)
	s.Add(playlist("kw", 12, "通勤", "甲"))
	s.SetAutoUpdate("kw/12", false)
	if _, err := s.Add(playlist("kw", 12, "通勤新", "甲", "乙", "丙")); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List()); n != 1 {
		t.Fatalf("%d subscriptions", n)
	}
	sub := s.Get("kw/12")
	if sub.Name != "通勤新" || len(sub.Tracks) != 3 {
		t.Errorf("sub %+v", sub)
	}
	if sub.AutoUpdate {
		t.Error("it fetched again as the app opens, which the listener turned off")
	}
}

// Two services' playlists of the same number are two subscriptions.
func TestKeysStayApart(t *testing.T) {
	s := newStore(t)
	s.Add(playlist("kw", 1, "酷我的"))
	s.Add(playlist("tx", 1, "QQ 的"))
	if n := len(s.List()); n != 2 {
		t.Fatalf("%d subscriptions", n)
	}
	if err := s.Remove("kw/1"); err != nil {
		t.Fatal(err)
	}
	if s.Get("kw/1") != nil {
		t.Error("removed stayed")
	}
	if s.Get("tx/1") == nil {
		t.Error("the other went with it")
	}
}

// Updating takes the songs the playlist has now, and leaves who follows it.
func TestUpdate(t *testing.T) {
	s := newStore(t)
	s.Add(playlist("kw", 3, "老名字"))
	s.SetAutoUpdate("kw/3", false)
	ok, err := s.Update("kw/3", playlist("kw", 3, "新名字", "甲"))
	if err != nil || !ok {
		t.Fatalf("Update: %v, %v", ok, err)
	}
	sub := s.Get("kw/3")
	if sub.Name != "新名字" || len(sub.Tracks) != 1 {
		t.Errorf("sub %+v", sub)
	}
	if sub.AutoUpdate || sub.UpdatedAt.IsZero() || time.Since(sub.UpdatedAt) > time.Minute {
		t.Errorf("sub %+v", sub)
	}
	if ok, _ := s.Update("kw/404", playlist("kw", 404, "没有")); ok {
		t.Error("a playlist not followed was updated")
	}
}

// A link to a playlist, or its id, names it as its service does.
func TestParseLink(t *testing.T) {
	cases := []struct {
		source, text, want string
		ok                 bool
	}{
		{"kw", "2886046289", "2886046289", true},
		{"kw", "http://www.kuwo.cn/playlist_detail/2886046289", "2886046289", true},
		{"kw", "https://m.kuwo.cn/h5app/playlist/2736267853?t=qqfriend", "2736267853", true},
		{"wy", "https://music.163.com/#/playlist?id=7217720898", "7217720898", true},
		{"wy", "https://music.163.com/playlist?id=12&userid=3", "12", true},
		{"tx", "https://y.qq.com/n/yqq/playlist/7217720898.html", "7217720898", true},
		{"tx", "https://i.y.qq.com/n2/m/share/details/taoge.html?id=99&ADTAG=x", "99", true},
		{"kg", "https://www.kugou.com/yy/special/single/1067062.html", "1067062", true},
		{"mg", "https://music.migu.cn/v3/music/playlist/161044573?page=1", "161044573", true},
		{"kw", "不是链接", "", false},
		{"unknown", "1", "", false},
	}
	for _, c := range cases {
		got, ok := ParseLink(c.source, c.text)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseLink(%q, %q) = %q, %v; want %q, %v", c.source, c.text, got, ok, c.want, c.ok)
		}
	}
}

// A pasted link names its service, for a listener who does not say.
func TestSourceOf(t *testing.T) {
	cases := map[string]string{
		"https://music.163.com/playlist?id=1":            "netease",
		"http://www.kuwo.cn/playlist_detail/1":           "kw",
		"https://y.qq.com/n/yqq/playlist/1":              "tx",
		"https://www.kugou.com/yy/special/single/1.html": "kg",
		"https://music.migu.cn/v3/music/playlist/1":      "mg",
	}
	for link, want := range cases {
		if got, ok := SourceOf(link); !ok || got != want {
			t.Errorf("SourceOf(%q) = %q, %v; want %q", link, got, ok, want)
		}
	}
	if _, ok := SourceOf("https://example.com"); ok {
		t.Error("a link of no service named one")
	}
}

// A store with no file yet has none, and one written in part is not read.
func TestMissingFile(t *testing.T) {
	s := newStore(t)
	if len(s.List()) != 0 {
		t.Errorf("%d subscriptions", len(s.List()))
	}
	if err := os.WriteFile(s.path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := NewStore(s.path)
	if err != nil || len(again.List()) != 0 {
		t.Errorf("empty file: %v, %d", err, len(again.List()))
	}
}
