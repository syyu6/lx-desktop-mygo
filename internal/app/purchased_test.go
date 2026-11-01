package app

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/account"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/source"
)

// fakeShop is a source with a shop and a collection: what the purchased
// page and the collected albums show, answering what the test says and
// telling on each call.
type fakeShop struct {
	songs           []music.Track
	albums          []music.AlbumProfile
	collectedAlbums []music.AlbumProfile
	member          music.Membership
	// err makes every call fail, as a shop that is not answering does.
	err error
	// mu guards asked: the pages ask on their own threads, and the test
	// reads what was asked as it goes.
	mu    sync.Mutex
	asked []string
	// albumCollected is what AlbumCollected says, and collects what
	// CollectAlbum was told to do.
	albumCollected bool
	collects       []bool
}

func (f *fakeShop) ID() string   { return "shop" }
func (f *fakeShop) Name() string { return "店源音乐" }
func (f *fakeShop) SearchSongs(string, int, int) ([]music.Track, int, error) {
	return nil, 0, nil
}
func (f *fakeShop) Stream(music.Track, string) (*music.Stream, error) { return nil, nil }
func (f *fakeShop) Lyrics(music.Track) (music.Lyrics, error)          { return music.Lyrics{}, nil }

func (f *fakeShop) note(what string, offset int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, fmt.Sprintf("%s:%d", what, offset))
}

// askings is what the shop has been asked, as the test reads it.
func (f *fakeShop) askings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

func (f *fakeShop) PurchasedSongs(limit, offset int) ([]music.Track, bool, error) {
	f.note("songs", offset)
	if f.err != nil {
		return nil, false, f.err
	}
	return f.songs, false, nil
}

func (f *fakeShop) PurchasedAlbums(limit, offset int) ([]music.AlbumProfile, bool, error) {
	f.note("albums", offset)
	if f.err != nil {
		return nil, false, f.err
	}
	return f.albums, false, nil
}

func (f *fakeShop) Membership() (music.Membership, error) {
	if f.err != nil {
		return music.MembershipUnknown, f.err
	}
	return f.member, nil
}

func (f *fakeShop) CollectedAlbums(limit, offset int) ([]music.AlbumProfile, bool, error) {
	f.note("collected", offset)
	if f.err != nil {
		return nil, false, f.err
	}
	return f.collectedAlbums, false, nil
}

func (f *fakeShop) AlbumCollected(int64) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.albumCollected, nil
}

func (f *fakeShop) CollectAlbum(_ int64, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.collects = append(f.collects, on)
	if f.err != nil {
		return f.err
	}
	f.albumCollected = on
	return nil
}

// collecteds is what CollectAlbum was told, as the test reads it.
func (f *fakeShop) collecteds() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.collects...)
}

// shopTestApp is signed in to the fake source, whose shop the pages show,
// while NetEase is signed out: a page per source, and this is the fake's.
func shopTestApp(t *testing.T, shop *fakeShop) *app {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	// The account layer is the packages', which know NetEase and QQ Music
	// but not a fake; the fake's is signed in here, as signedIn does for one
	// the app is told of.
	a.accounts = map[string]*account.Profile{
		"shop": {Source: "shop", UserID: 7, Nickname: "听歌的人", VIP: "会员"},
	}
	a.reg = source.NewRegistry(netease.Provider{}, shop)
	a.purchasePick = "shop"
	a.loaded()
	return a
}

// loaded is what the app holds after Run has built it: the maps the pages
// write to as they load, which a test that opens a page needs there.
func (a *app) loaded() {
	a.playlists = map[string]*netease.Playlist{}
	a.loadingPl = map[string]bool{}
	a.plErr = map[string]string{}
	a.artists = map[string]*artistData{}
	a.albums = map[string]*albumData{}
	a.users = map[string]*userData{}
}

// The purchased page is offered while a source that sells is signed in, and
// is not while none is: a page nobody can fill has no way in.
func TestPurchasedPageFollowsTheShop(t *testing.T) {
	shop := &fakeShop{}
	a := shopTestApp(t, shop)
	if src, ok := a.purchaseSource(); !ok || src != "shop" {
		t.Fatalf("the page is %q, %v", src, ok)
	}
	// Signed out of it, the page has no shop to show.
	a.accounts = nil
	if src, ok := a.purchaseSource(); ok {
		t.Errorf("a shop with nobody signed in is %q", src)
	}
	// NetEase sells nothing, so it is never the page's.
	a.accounts = map[string]*account.Profile{music.SourceNetease: {Source: music.SourceNetease, UserID: 1}}
	if src, ok := a.purchaseSource(); ok {
		t.Errorf("NetEase answered a shop: %q", src)
	}
}

// The two tabs load apart, each from where it left, and switching between
// them asks only the one opened.
func TestPurchasedTabsLoadApart(t *testing.T) {
	shop := &fakeShop{
		songs:  []music.Track{{ID: 1, Name: "春日影", Duration: 200000}},
		albums: []music.AlbumProfile{{Album: music.Album{ID: 8, Name: "迷跡波"}}},
	}
	a := shopTestApp(t, shop)
	a.open(route{page: pagePurchased})
	waitFor(t, func() bool { got := shop.askings(); return len(got) == 1 && got[0] == "albums:0" })
	// As the answer lands, which post drops without a window.
	a.purchasedAnswered("shop", purchasedAlbums, a.purchased[purchasedAlbums], 7, nil, shop.albums, false, nil)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("迷跡波") {
		t.Fatalf("the albums did not show: %q", tt.Texts())
	}
	// The songs' tab asks them once it opens.
	if err := tt.Click("单曲"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { got := shop.askings(); return len(got) == 2 && got[1] == "songs:0" })
	a.purchasedAnswered("shop", purchasedSongs, a.purchased[purchasedSongs], 7, shop.songs, nil, false, nil)
	tt.Frame()
	if !tt.HasText("春日影") {
		t.Errorf("the songs did not show: %q", tt.Texts())
	}
	if err := tt.Click("专辑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := shop.askings(); len(got) != 2 {
		t.Errorf("asked %q again: the albums were loaded", got)
	}
	if !tt.HasText("迷跡波") {
		t.Errorf("the albums loaded were asked for again: %q", tt.Texts())
	}
}

// A shop that does not answer says so on the page, and asking again asks
// from where the failed page left.
func TestPurchasedRetriesFromWhereItStopped(t *testing.T) {
	shop := &fakeShop{err: errors.New("offline")}
	a := shopTestApp(t, shop)
	a.open(route{page: pagePurchased})
	waitFor(t, func() bool { return len(shop.askings()) == 1 })
	a.purchasedAnswered("shop", purchasedAlbums, a.purchased[purchasedAlbums], 7, nil, nil, false, shop.err)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("无法连接店源音乐，请检查网络") {
		t.Errorf("the page does not say the shop is not answering: %q", tt.Texts())
	}
	shop.err = nil
	if err := tt.Click("刷新已购音乐"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(shop.askings()) == 2 })
	if got := shop.askings(); got[len(got)-1] != "albums:0" {
		t.Errorf("asked %q, want the albums from the start again", got)
	}
}

// Signed out, the page says what it is for and signs in to the source that
// has a shop, rather than show an empty list.
func TestPurchasedSignedOut(t *testing.T) {
	shop := &fakeShop{}
	a := newTestApp()
	a.settings = defaultSettings()
	a.loadRegistry()
	a.reg = source.NewRegistry(netease.Provider{}, shop)
	a.open(route{page: pagePurchased})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("登录店源音乐后查看已购音乐") {
		t.Errorf("the page does not ask for a sign-in: %q", tt.Texts())
	}
	if _, ok := tt.Find("登录店源音乐"); !ok {
		t.Error("the page has no way to sign in")
	}
}

// A liked playlist of a source that keeps albums shows them as its second
// tab: its songs are the first, and the albums are the other.
func TestLikedPlaylistHasItsAlbums(t *testing.T) {
	shop := &fakeShop{collectedAlbums: []music.AlbumProfile{{Album: music.Album{ID: 8, Name: "迷跡波"}}}}
	a := shopTestApp(t, shop)
	liked := &netease.Playlist{
		Source: "shop", ID: 201, SpecialType: 5, Name: "我喜欢",
		Tracks: []netease.Track{{Source: "shop", ID: 1, Name: "春日影"}},
	}
	a.playlists[playlistKey("shop", 201)] = liked
	a.open(route{page: pagePlaylist, src: "shop", id: 201})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if _, ok := tt.Find("专辑"); !ok {
		t.Fatal("the liked playlist has no albums tab")
	}
	if got := shop.askings(); len(got) != 0 {
		t.Errorf("asked %q for the songs' tab", got)
	}
	if err := tt.Click("专辑"); err != nil {
		t.Fatal(err)
	}
	if a.route.tab != likedAlbums {
		t.Fatalf("the route's tab is %d", a.route.tab)
	}
	tt.Frame()
	waitFor(t, func() bool { got := shop.askings(); return len(got) == 1 && got[0] == "collected:0" })
	a.collectedAnswered("shop", a.collected["shop"], 7, shop.collectedAlbums, false, nil)
	tt.Frame()
	if !tt.HasText("迷跡波") {
		t.Errorf("the collected album is not shown: %q", tt.Texts())
	}
}

// NetEase's liked playlist keeps songs alone: its page has no albums tab,
// and asks for none.
func TestLikedPlaylistOfASourceWithoutAlbums(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.loadRegistry()
	a.playlists[playlistKey("", 5)] = &netease.Playlist{ID: 5, SpecialType: 5, Name: "我喜欢的音乐",
		Tracks: []netease.Track{{ID: 1, Name: "春日影"}}}
	a.open(route{page: pagePlaylist, id: 5})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if _, ok := tt.Find("专辑"); ok {
		t.Error("NetEase's liked playlist offers an albums tab")
	}
}

// The album page of a source that keeps a collection offers to collect the
// album, and says which it is in: the button asks the account, then writes
// and takes the source's answer.
func TestAlbumIsCollectedFromItsPage(t *testing.T) {
	shop := &fakeShop{}
	a := shopTestApp(t, shop)
	a.albums[sourceKey("shop", 8)] = &albumData{album: &netease.AlbumProfile{
		Album: music.Album{ID: 8, Name: "迷跡波", Source: "shop"},
		Songs: []netease.Track{{Source: "shop", ID: 1, Name: "春日影"}},
	}}
	a.open(route{page: pageAlbum, src: "shop", id: 8})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if _, ok := tt.Find("收藏专辑"); !ok {
		t.Fatalf("the album page has no way to collect it: %q", tt.Texts())
	}
	// The answer lands as the page draws, which post drops without a window.
	a.albumCollectedAnswered("shop", a.albums[sourceKey("shop", 8)], 7, false, nil)
	tt.Frame()
	if err := tt.Click("收藏专辑"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(shop.collecteds()) == 1 })
	if got := shop.collecteds()[0]; !got {
		t.Error("the album was dropped rather than collected")
	}
	a.albumCollectedToggled("shop", a.albums[sourceKey("shop", 8)], 7, true, nil)
	tt.Frame()
	if !tt.HasText("已收藏") {
		t.Errorf("the button does not say the album is collected: %q", tt.Texts())
	}
	// Clicking again drops it, and the button says so.
	if err := tt.Click("已收藏"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(shop.collecteds()) == 2 })
	if got := shop.collecteds()[1]; got {
		t.Error("the album was collected rather than dropped")
	}
	a.albumCollectedToggled("shop", a.albums[sourceKey("shop", 8)], 7, false, nil)
	tt.Frame()
	if !tt.HasText("收藏专辑") {
		t.Errorf("the button does not say the album is not collected: %q", tt.Texts())
	}
}

// NetEase's album pages keep no collection: the button is not there for an
// album whose source has none.
func TestAlbumOfASourceWithoutACollection(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.loadRegistry()
	a.albums[sourceKey("", 8)] = &albumData{album: &netease.AlbumProfile{
		Album: music.Album{ID: 8, Name: "夜间漫游"},
		Songs: []netease.Track{{ID: 1, Name: "春日影"}},
	}}
	a.open(route{page: pageAlbum, id: 8})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if _, ok := tt.Find("收藏专辑"); ok {
		t.Error("NetEase's album page offers to collect it")
	}
}

// The listener's own page on a source that says what their account is
// shows it: the badge is the account's, and only on their own page.
func TestMembershipIsOnTheOwnPage(t *testing.T) {
	shop := &fakeShop{member: music.MembershipVIP}
	a := shopTestApp(t, shop)
	a.users[sourceKey("shop", 7)] = &userData{
		user: &netease.User{UserID: 7, Nickname: "听歌的人"},
	}
	a.open(route{page: pageUser, src: "shop", id: 7})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("会员") {
		t.Errorf("the own page has no membership badge: %q", tt.Texts())
	}
	// Another listener's page says nothing of the account: it is not theirs.
	a.users[sourceKey("shop", 8)] = &userData{user: &netease.User{UserID: 8, Nickname: "别人"}}
	a.open(route{page: pageUser, src: "shop", id: 8})
	tt.Frame()
	if tt.HasText("会员") {
		t.Error("another listener's page shows the account's membership")
	}
}

// A long description opens in a card over the page, once the pointer rests
// on its two lines: the songs under the header do not move for it.
func TestDescriptionOpensAfterARest(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	long := "每一首歌，都记录着一段旅程。\n\n"
	for range 20 {
		long += "从城市的清晨到深夜的灯火，在旋律中慢慢寻找属于自己的片刻。"
	}
	a.albums[sourceKey("", 8)] = &albumData{album: &netease.AlbumProfile{
		Album:       music.Album{ID: 8, Name: "夜间漫游"},
		Description: long,
		Songs:       []netease.Track{{ID: 1, Name: "春日影"}},
	}}
	a.open(route{page: pageAlbum, id: 8})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(descriptionDelayOnce())
	anchor, ok := tt.Find("简介")
	if !ok {
		t.Fatal("the album's page has no description")
	}
	song, _ := tt.Find("春日影")
	tt.Move(anchor.X+anchor.W/2, anchor.Y+anchor.H/2)
	tt.Advance(descriptionDelay / 3)
	if _, shown := tt.Find("完整简介"); shown {
		t.Error("the card opened while the pointer was only passing over the lines")
	}
	tt.Advance(descriptionDelay)
	if _, shown := tt.Find("完整简介"); !shown {
		t.Error("the card did not open after the pointer rested on the lines")
	}
	if got, _ := tt.Find("春日影"); got != song {
		t.Errorf("opening the card moved a song from %+v to %+v", song, got)
	}
}

// descriptionDelayOnce is a rest longer than the card's delay, as the
// tester needs it: the time the app asks for before the card opens.
func descriptionDelayOnce() time.Duration { return descriptionDelay + descriptionGrace }
