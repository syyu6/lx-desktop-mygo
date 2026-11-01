package qq

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"netease-music-mygo/internal/music"
)

// These ask QQ Music over the network, so they run only when the listener
// says so: `LIVE=1 go test ./internal/qq -run Live`. They exist to notice
// the day QQ changes an answer — a build that passes offline says nothing
// about that.

func live(t *testing.T) {
	t.Helper()
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE to contact QQ Music")
	}
	previous := currentSession()
	t.Cleanup(func() { setSession(previous) })
	setSession(Session{})
}

func TestSearchSongsLive(t *testing.T) {
	live(t)
	tracks, total, err := Provider{}.SearchSongs("周杰伦", 3, 0)
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}
	if total == 0 || len(tracks) == 0 {
		t.Fatalf("found %d songs of %d", len(tracks), total)
	}
	for _, tr := range tracks {
		if tr.Source != music.SourceQQ || tr.Key == "" || tr.Name == "" {
			t.Errorf("song %+v", tr)
		}
		if len(tr.Artists) == 0 {
			t.Errorf("song %q has no artist", tr.Name)
		}
		// A cover, which the search names through the album's mid.
		if !strings.HasPrefix(tr.Album.PicURL, "https://y.gtimg.cn/") {
			t.Errorf("song %q has no cover: %q", tr.Name, tr.Album.PicURL)
		}
	}
}

func TestSearchSongsPagesLive(t *testing.T) {
	live(t)
	first, _, err := Provider{}.SearchSongs("周杰伦", 2, 0)
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}
	second, _, err := Provider{}.SearchSongs("周杰伦", 2, 2)
	if err != nil {
		t.Fatalf("SearchSongs: %v", err)
	}
	if len(first) > 0 && len(second) > 0 && first[0].Key == second[0].Key {
		t.Error("the second page repeats the first")
	}
}

func TestSearchAlbumsLive(t *testing.T) {
	live(t)
	albums, total, err := Provider{}.SearchAlbums("周杰伦", 3, 0)
	if err != nil {
		t.Fatalf("SearchAlbums: %v", err)
	}
	if total == 0 || len(albums) == 0 {
		t.Fatalf("found %d albums of %d", len(albums), total)
	}
	for _, al := range albums {
		if al.Name == "" || al.ID == 0 {
			t.Errorf("album %+v", al)
		}
	}
}

func TestSearchArtistsLive(t *testing.T) {
	live(t)
	artists, total, err := Provider{}.SearchArtists("周杰伦", 3, 0)
	if err != nil {
		t.Fatalf("SearchArtists: %v", err)
	}
	if total == 0 || len(artists) == 0 {
		t.Fatalf("found %d artists of %d", len(artists), total)
	}
	for _, ar := range artists {
		if ar.Name == "" || ar.ID == 0 {
			t.Errorf("artist %+v", ar)
		}
	}
}

// A free song streams without an account, and its words come with it.
func TestStreamAndLyricsLive(t *testing.T) {
	live(t)
	tracks, _, err := Provider{}.SearchSongs("纯音乐", 10, 0)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("SearchSongs: %v, %d songs", err, len(tracks))
	}
	var played *music.Track
	for i := range tracks {
		st, err := Provider{}.Stream(tracks[i], "standard")
		if err == nil && st.URL != "" {
			played = &tracks[i]
			if !strings.HasPrefix(st.URL, "http") || st.Type == "" {
				t.Errorf("stream of %q = %+v", tracks[i].Name, st)
			}
			break
		}
	}
	if played == nil {
		t.Fatal("no song of the page streams")
	}
	if ly, err := (Provider{}).Lyrics(*played); err == nil {
		if !strings.Contains(ly.Lyric, "[") {
			t.Errorf("the words of %q are not timed: %q", played.Name, ly.Lyric[:min(len(ly.Lyric), 80)])
		}
	}
}

// The square lists its categories, and a page of the playlists under one
// opens with its songs.
func TestSquareLive(t *testing.T) {
	live(t)
	tags, err := Provider{}.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) == 0 {
		t.Fatal("no tags")
	}
	lists, err := Provider{}.SquarePlaylists(strconv.FormatInt(tags[0].ID, 10), 5, 0)
	if err != nil {
		t.Fatalf("SquarePlaylists: %v", err)
	}
	if len(lists) == 0 {
		t.Fatalf("no playlists under %q", tags[0].Name)
	}
	pl, err := Provider{}.Playlist(lists[0].Key)
	if err != nil {
		t.Fatalf("Playlist: %v", err)
	}
	if pl.Name == "" || len(pl.Tracks) == 0 {
		t.Fatalf("playlist %+v", pl)
	}
	for _, tr := range pl.Tracks[:min(len(pl.Tracks), 5)] {
		if tr.Source != music.SourceQQ || tr.Key == "" {
			t.Errorf("song %+v", tr)
		}
	}
}

// A chart opens as a playlist, and its songs stream.
func TestChartLive(t *testing.T) {
	live(t)
	charts, err := Provider{}.Charts()
	if err != nil {
		t.Fatalf("Charts: %v", err)
	}
	if len(charts) == 0 {
		t.Fatal("no charts")
	}
	pl, err := Provider{}.Playlist(charts[0].Key)
	if err != nil {
		t.Fatalf("Playlist(%q): %v", charts[0].Key, err)
	}
	if len(pl.Tracks) == 0 {
		t.Fatalf("chart %q has no songs", charts[0].Name)
	}
}

// An artist's page and an album's open from a song of them.
func TestPagesLive(t *testing.T) {
	live(t)
	tracks, _, err := Provider{}.SearchSongs("周杰伦", 5, 0)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("SearchSongs: %v", err)
	}
	song := tracks[0]
	if ap, err := (Provider{}).Artist(song.Artists[0].ID); err != nil {
		t.Errorf("Artist(%d): %v", song.Artists[0].ID, err)
	} else if ap.Name == "" {
		t.Errorf("artist page is empty: %+v", ap)
	}
	if alb, err := (Provider{}).Album(song.Album.ID); err != nil {
		t.Errorf("Album(%d): %v", song.Album.ID, err)
	} else if alb.Name == "" {
		t.Errorf("album page is empty: %+v", alb)
	}
}

// Comments load from the current web service and advance past the first
// page.
func TestCommentsLive(t *testing.T) {
	live(t)
	first, err := (Provider{}).ThreadComments("105816212", 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total <= 0 || len(first.Latest) == 0 || !first.More {
		t.Fatalf("incomplete first page: %+v", first)
	}
	next, err := (Provider{}).ThreadComments("105816212", len(first.Latest), 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Latest) == 0 {
		t.Fatal("empty second page")
	}
}
