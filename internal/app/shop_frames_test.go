package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
)

// With NM_FRAMES set, saves the pages a shop and a collection add, in the
// light and the dark, to look at them.
func TestShopPageFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	band := music.Artist{ID: 7, Name: "MyGO!!!!!"}
	shop := &fakeShop{
		songs: []music.Track{
			{Source: "shop", ID: 1, Name: "春日影", Artists: []music.Artist{band}, Duration: 231000},
			{Source: "shop", ID: 2, Name: "迷星叫", Artists: []music.Artist{band}, Duration: 245000},
		},
		albums: []music.AlbumProfile{
			{Album: music.Album{Source: "shop", ID: 8, Name: "迷跡波"}, Artists: []music.Artist{band}},
			{Album: music.Album{Source: "shop", ID: 9, Name: "跡暖空"}, Artists: []music.Artist{band}},
		},
		collectedAlbums: []music.AlbumProfile{
			{Album: music.Album{Source: "shop", ID: 10, Name: "壱雫空"}, Artists: []music.Artist{band}},
		},
		member: music.MembershipSuper,
	}
	scenes := []struct {
		name string
		r    route
	}{
		{"purchased-albums", route{page: pagePurchased}},
		{"purchased-songs", route{page: pagePurchased, tab: purchasedSongs}},
		{"liked-albums", route{page: pagePlaylist, src: "shop", id: 201, tab: likedAlbums}},
		{"album-collect", route{page: pageAlbum, src: "shop", id: 8}},
	}
	for _, scene := range scenes {
		for _, dark := range []bool{false, true} {
			a := shopTestApp(t, shop)
			a.playlists[playlistKey("shop", 201)] = &netease.Playlist{
				Source: "shop", ID: 201, SpecialType: 5, Name: "我喜欢", Tracks: shop.songs,
			}
			a.albums[sourceKey("shop", 8)] = &albumData{album: &netease.AlbumProfile{
				Album:       music.Album{Source: "shop", ID: 8, Name: "迷跡波"},
				Artists:     []music.Artist{band},
				Description: "MyGO!!!!! 的第一张专辑，从第一首歌开始的漫长而明亮的旅途。\n\n每一首都在讲一个还没有答案的问题。",
				Songs:       shop.songs,
			}}
			a.open(scene.r)
			// As the answers land, which post drops without a window.
			if d := a.purchased[purchasedAlbums]; d != nil {
				a.purchasedAnswered("shop", purchasedAlbums, d, 7, nil, shop.albums, false, nil)
			}
			if d := a.purchased[purchasedSongs]; d != nil {
				a.purchasedAnswered("shop", purchasedSongs, d, 7, shop.songs, nil, false, nil)
			}
			if d := a.collected["shop"]; d != nil {
				a.collectedAnswered("shop", d, 7, shop.collectedAlbums, false, nil)
			}
			if d := a.albums[sourceKey("shop", 8)]; d != nil {
				a.albumCollectedAnswered("shop", d, 7, true, nil)
			}
			tt := ui.NewTester(a.view, 1200, 800)
			tt.SetDark(dark)
			tt.Advance(time.Second)
			tt.Frame()
			name := scene.name
			if dark {
				name += "-dark"
			}
			f, _ := os.Create(filepath.Join(dir, "shop-"+name+".png"))
			png.Encode(f, tt.Image())
			f.Close()
		}
	}
}
