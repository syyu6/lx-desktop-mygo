package netease

import (
	"errors"
	"strconv"

	"netease-music-mygo/internal/music"
)

// Provider is NetEase as package source asks it: the source the app
// ships, which browses and streams on its own, and which the listener may
// turn off for the services' sources alone.
type Provider struct{}

func (Provider) ID() string   { return music.SourceNetease }
func (Provider) Name() string { return "网易云音乐" }

func (Provider) SearchSongs(query string, limit, offset int) ([]music.Track, int, error) {
	res, err := SearchPage(query, SearchSongs, limit, offset)
	return res.Songs, res.Total, err
}

func (Provider) SearchPlaylists(query string, limit, offset int) ([]music.Playlist, int, error) {
	res, err := SearchPage(query, SearchPlaylists, limit, offset)
	for i := range res.Playlists {
		res.Playlists[i].Source = music.SourceNetease
	}
	return res.Playlists, res.Total, err
}

func (Provider) Playlist(id string) (*music.Playlist, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, err
	}
	pl, err := PlaylistDetail(n)
	if err != nil {
		return nil, err
	}
	pl.Source = music.SourceNetease
	for i := range pl.Tracks {
		pl.Tracks[i].Source = music.SourceNetease
	}
	return pl, nil
}

func (Provider) Stream(t music.Track, level string) (*music.Stream, error) {
	return SongURL(t.ID, level)
}

func (Provider) Lyrics(t music.Track) (music.Lyrics, error) {
	lrc, trans, yrc, err := Lyrics(t.ID)
	if err != nil {
		return music.Lyrics{}, err
	}
	if lrc == "" {
		return music.Lyrics{}, errors.New("netease: no lyrics")
	}
	return music.Lyrics{Lyric: lrc, Tlyric: trans, LXLyric: yrc}, nil
}

// HasComments reports that NetEase has remarks on its songs: it is the one
// source with a comment thread, which songs of the others are shown with,
// found by name.
func (Provider) HasComments() bool { return true }

// Tags and SquarePlaylists are the playlist square. NetEase has one, so
// this provider is a source.Square; its tags are named, its list is asked
// by name rather than by id.
func (Provider) Tags() ([]music.Tag, error) {
	tags, err := PlaylistTags()
	if err != nil {
		return nil, err
	}
	out := make([]music.Tag, len(tags))
	for i, t := range tags {
		out[i] = music.Tag{ID: t.ID, Name: t.Name, Hot: t.Hot}
	}
	return out, nil
}

func (Provider) SquarePlaylists(tag string, limit, offset int) ([]music.Playlist, error) {
	lists, err := SquarePlaylists(tag, limit, offset)
	for i := range lists {
		lists[i].Source = music.SourceNetease
	}
	return lists, err
}
