package qq

import (
	"strconv"
	"strings"
	"time"

	"netease-music-mygo/internal/music"
)

// The shapes QQ's answers come in, and how each becomes what the app
// knows. A song becomes a music.Track with its mid in Key — QQ names a
// song by a mid, not a number — and an artist and an album keep their mid
// here, in the table that lets their pages open (mids.go).

// artist is a singer, as QQ's answers give them.
type artist struct {
	ID   int64
	Mid  string
	Name string
}

// artistOf is artist as the app knows an artist, remembering the mid.
func artistOf(a artist) music.Artist {
	artistMids.put(a.ID, a.Mid)
	return music.Artist{ID: a.ID, Name: a.Name, Source: music.SourceQQ}
}

// artistProfileOf is artist as their page shows them.
func artistProfileOf(a artist) music.ArtistProfile {
	p := music.ArtistProfile{Artist: artistOf(a)}
	p.Avatar = ArtistCover(a.Mid, 500)
	p.PicURL = p.Avatar
	return p
}

// album is what a song is in, as QQ's answers give it.
type album struct {
	ID   int64
	Mid  string
	Name string
}

// albumOf is album as the app knows an album, remembering the mid.
func albumOf(a album) music.Album {
	albumMids.put(a.ID, a.Mid)
	cover := ""
	if a.Mid != "" {
		cover = AlbumCover(a.Mid, 300)
	}
	return music.Album{ID: a.ID, Name: a.Name, PicURL: cover, Source: music.SourceQQ}
}

// convertArtist reads an artist from an answer that names its fields as
// QQ's older endpoints do.
func convertArtist(o object) artist {
	return artist{
		ID:   number(o, "id", "singerid", "singer_id", "singerID", "uin"),
		Mid:  text(o, "mid", "singermid", "singer_mid", "singerMid", "singerMID"),
		Name: text(o, "name", "singername", "singer_name", "singerName", "title"),
	}
}

// convertAlbum reads an album from such an answer.
func convertAlbum(o object) album {
	return album{
		ID:   number(o, "id", "albumid", "albumID", "album_id"),
		Mid:  text(o, "mid", "albummid", "albumMID", "albumMid", "album_mid"),
		Name: text(o, "name", "title", "albumname", "albumName", "album_name"),
	}
}

// convertTrack reads a song from an answer. It unwraps the shapes QQ
// nests one in: its search puts it under musicData, its details under
// track_info.
func convertTrack(o object) music.Track {
	if nested := obj(o["musicData"]); nested != nil {
		o = nested
	}
	if nested := obj(o["songInfo"]); nested != nil {
		o = nested
	}
	if nested := obj(o["track_info"]); nested != nil {
		o = nested
	}
	t := music.Track{
		Source:   music.SourceQQ,
		ID:       number(o, "id", "songid", "songId"),
		Key:      text(o, "mid", "songmid", "songMid"),
		Name:     text(o, "title", "name", "songname", "songName"),
		Duration: number(o, "interval") * 1000,
	}
	// Liking a song is asked by its number, and answered with its mid:
	// remembering the pair here is what lets a like name the mid back.
	songMids.put(t.ID, t.Key)
	for _, a := range objects(o, "singer", "singers", "artist") {
		if ar := convertArtist(a); ar.Name != "" {
			t.Artists = append(t.Artists, artistOf(ar))
		}
	}
	al := obj(o["album"])
	if al != nil {
		t.Album = albumOf(convertAlbum(al))
	}
	if t.Album.Name == "" || t.Album.ID == 0 {
		flat := album{
			ID:   number(o, "albumid", "album_id"),
			Mid:  text(o, "albummid", "album_mid"),
			Name: text(o, "albumname", "album_name"),
		}
		if flat.ID != 0 || flat.Mid != "" || flat.Name != "" {
			t.Album = albumOf(flat)
		}
	}
	if t.Album.PicURL == "" {
		// A search that names the album's mid but not the song's.
		if mid := text(o, "albummid", "album_mid"); mid != "" {
			t.Album.PicURL = AlbumCover(mid, 300)
		}
	}
	// A podcast episode is a song of its audio with the episode's own name
	// and cover; QQ's are not among the sources the app browses, so a
	// program's fields are left where they are found.
	if pic := text(o, "picurl", "picUrl"); pic != "" && t.Album.PicURL == "" {
		t.Album.PicURL = pic
	}
	return t
}

// convertTracks reads a page of songs, leaving out what has no mid: a song
// QQ names only by number could not be streamed.
func convertTracks(entries []object) []music.Track {
	out := make([]music.Track, 0, len(entries))
	for _, o := range entries {
		t := convertTrack(o)
		if t.ID > 0 && t.Key != "" && t.Name != "" {
			out = append(out, t)
		}
	}
	return out
}

// dateMillis reads a date QQ writes as 2006-01-02, or as a number of
// seconds, as Unix milliseconds.
func dateMillis(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n < 1e12 {
			n *= 1000
		}
		return n
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UnixMilli()
	}
	return 0
}
