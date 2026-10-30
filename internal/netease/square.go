package netease

import (
	"net/url"
	"strconv"
)

// The playlist square: what NetEase's own 歌单 page shows, a listener
// browsing playlists rather than looking one up.

// PlaylistTag is a category of playlists, as the square shows them: a
// language, a style, a scene or a mood.
type PlaylistTag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Hot marks the few tags the square lists first, at its top.
	Hot bool `json:"hot"`
}

// PlaylistTags returns the square's categories, its hot ones first.
func PlaylistTags() ([]PlaylistTag, error) {
	var res struct {
		Tags []PlaylistTag `json:"tags"`
	}
	if err := get("/api/playlist/highquality/tags", nil, &res); err != nil {
		return nil, err
	}
	return res.Tags, nil
}

// SquarePlaylists returns limit of the playlists under tag, the hottest
// first, from offset.
func SquarePlaylists(tag string, limit, offset int) ([]Playlist, error) {
	var res struct {
		Playlists []Playlist `json:"playlists"`
	}
	q := url.Values{"cat": {tag}, "order": {"hot"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if err := get("/api/playlist/list", q, &res); err != nil {
		return nil, err
	}
	return res.Playlists, nil
}
