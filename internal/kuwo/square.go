package kuwo

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"netease-music-mygo/internal/music"
)

// Kuwo's playlist square: the playlists its clients show under a
// category, and the categories themselves. It is what the 歌单 page shows
// once NetEase is turned off.
//
// A category is one of Kuwo's tags, which come in groups (专区, 主题,
// 心情…). The playlists under one are its own kind, digest 10000 in the
// tag list; the 专区 groups hold curated sets rather than playlists, and
// are left out.

const (
	squareBase   = "http://wapi.kuwo.cn/api/pc/classify/playlist/get"
	squareClient = "&loginUid=0&loginSid=0&appUid=76039576"
	// tagPlaylists is the digest of the tags whose playlists Kuwo lists.
	tagPlaylists = "10000"
)

// tag is a category of Kuwo's square, as its tag list names it: an id and
// the digest that says what kind of category it is.
type tag struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
	Name   string `json:"name"`
}

// squareTags is what the tags last fetched were, by name, so that a
// category the page chose is the one asked for by its id: Kuwo names a
// category by a number, and the page only knows its name.
var squareTags = struct {
	sync.Mutex
	byName map[string]tag
}{}

// Tags returns the categories of Kuwo's square: those whose playlists it
// lists, in the groups its tag list them in.
func (Provider) Tags() ([]music.Tag, error) {
	addr := "http://wapi.kuwo.cn/api/pc/classify/playlist/getTagList?cmd=rcm_keyword_playlist&user=0" +
		"&prod=kwplayer_pc_9.0.5.0&vipver=9.0.5.0&source=kwplayer_pc_9.0.5.0" + squareClient
	var res struct {
		Code int `json:"code"`
		Data []struct {
			Name string `json:"name"`
			Data []tag  `json:"data"`
		} `json:"data"`
	}
	if err := get(addr, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, errors.New("kuwo: the playlist tags did not come")
	}
	by := map[string]tag{}
	var out []music.Tag
	for _, group := range res.Data {
		for _, t := range group.Data {
			if t.Digest != tagPlaylists || t.Name == "" {
				continue
			}
			if _, seen := by[t.Name]; seen {
				continue
			}
			by[t.Name] = t
			// A tag's ID is a number in Kuwo's, and the page does not use
			// it: its name is what the square is asked with.
			id, _ := strconv.ParseInt(t.ID, 10, 64)
			out = append(out, music.Tag{ID: id, Name: t.Name})
		}
	}
	squareTags.Lock()
	squareTags.byName = by
	squareTags.Unlock()
	return out, nil
}

// squarePlaylist is a playlist as Kuwo's square lists it: its own shape,
// apart from the search's.
type squarePlaylist struct {
	ID     num    `json:"id"`
	Name   string `json:"name"`
	Author string `json:"uname"`
	Pic    string `json:"img"`
	Desc   string `json:"desc"`
	Info   string `json:"info"`
	Songs  num    `json:"total"`
	Played num    `json:"listencnt"`
}

// playlist returns the playlist as the app shows it.
func (p *squarePlaylist) playlist() music.Playlist {
	pl := music.Playlist{
		Source:      music.SourceKuwo,
		ID:          p.ID.Int(),
		Name:        p.Name,
		CoverURL:    p.Pic,
		Description: p.Desc,
		PlayCount:   p.Played.Float(),
		TrackCount:  int(p.Songs.Int()),
	}
	if pl.Description == "" {
		pl.Description = p.Info
	}
	pl.Creator.Nickname = p.Author
	return pl
}

// SquarePlaylists returns a page of the playlists under a category of
// Kuwo's square, or under none: its most played, as lx-music-desktop asks
// for them.
func (Provider) SquarePlaylists(name string, limit, offset int) ([]music.Playlist, error) {
	page := offset/limit + 1
	var addr string
	squareTags.Lock()
	t, ok := squareTags.byName[name]
	squareTags.Unlock()
	if name != "" && ok {
		addr = squareBase + "TagPlayList?" + strings.TrimPrefix(squareClient, "&") +
			"&pn=" + strconv.Itoa(page) + "&rn=" + strconv.Itoa(limit) + "&id=" + url.QueryEscape(t.ID)
	} else {
		addr = squareBase + "RcmPlayList?" + strings.TrimPrefix(squareClient, "&") +
			"&pn=" + strconv.Itoa(page) + "&rn=" + strconv.Itoa(limit) + "&order=hot"
	}
	var res struct {
		Code int `json:"code"`
		Data struct {
			List []squarePlaylist `json:"data"`
		} `json:"data"`
	}
	if err := get(addr, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, errors.New("kuwo: the playlists did not come")
	}
	out := make([]music.Playlist, len(res.Data.List))
	for i := range res.Data.List {
		out[i] = res.Data.List[i].playlist()
	}
	return out, nil
}
