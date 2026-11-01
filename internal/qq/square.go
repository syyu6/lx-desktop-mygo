package qq

import (
	"net/url"
	"strconv"
	"sync"

	"netease-music-mygo/internal/music"
)

// QQ's playlist square: the categories its own 歌单 page sorts playlists
// by, and the playlists under one.
//
// The categories come in groups — 热门, 语种, 风格, 场景, 心情 — and a
// playlist is asked for by the number of its category, so a Tag's ID is
// that number and its Name the whole path to it ("语种 · 国语"): the page
// shows one row of chips, and two categories of different groups are not
// told apart by their names alone.
//
// The page names a category by the name it shows, as it does a Kuwo one's,
// so the numbers are kept by name, as Kuwo keeps its tags': a name the last
// Tags did not list is answered with nothing rather than with everyone's
// playlists.

// squareTags is what the last Tags returned, by name: the category number
// a name stands for.
var squareTags = struct {
	sync.Mutex
	byName map[string]int64
}{}

// Tags returns QQ's categories of playlists.
func (Provider) Tags() ([]music.Tag, error) {
	var body object
	if err := cgi("/splcloud/fcgi-bin/fcg_get_diss_tag_conf.fcg", nil, &body); err != nil {
		return nil, err
	}
	data := obj(body["data"])
	byName := map[string]int64{}
	var out []music.Tag
	for _, group := range objects(data, "categories") {
		groupName := text(group, "categoryGroupName")
		for _, item := range objects(group, "items") {
			// A category QQ calls "usable 0" is a heading of its own, not
			// a list of playlists.
			if number(item, "usable") == 0 && number(item, "categoryId") != 10000000 {
				continue
			}
			// The names come escaped as the playlists' do: 流派 · R&#38;B
			// is what QQ's own list says of R&B.
			name := unescape(text(item, "categoryName"))
			if name == "" {
				continue
			}
			id := number(item, "categoryId")
			if groupName != "" && groupName != "热门" {
				name = unescape(groupName) + " · " + name
			}
			byName[name] = id
			out = append(out, music.Tag{ID: id, Name: name})
		}
	}
	squareTags.Lock()
	squareTags.byName = byName
	squareTags.Unlock()
	return out, nil
}

// SquarePlaylists returns a page of the playlists under a category, as the
// listener sorted them: by what is played most.
//
// The category is the name Tags gave it, which the page knows a category
// by; a number is taken for the category's own number as well, for a caller
// that has one. A name no Tags listed is no playlists, rather than all of
// them: the page asks under a chip the last tags did not offer only if they
// changed under it.
func (Provider) SquarePlaylists(tag string, limit, offset int) ([]music.Playlist, error) {
	id := int64(10000000) // 全部
	if tag != "" {
		n, err := strconv.ParseInt(tag, 10, 64)
		if err != nil {
			squareTags.Lock()
			n, ok := squareTags.byName[tag]
			squareTags.Unlock()
			if !ok {
				return nil, nil
			}
			id = n
		} else {
			id = n
		}
	}
	limit = min(max(limit, 1), 60)
	offset = max(offset, 0)
	var body object
	// ein is the index of the last playlist of the page, counted in, not
	// one past it: asking for offset+limit puts that page's last playlist
	// at the head of the next one, and the page would list it twice.
	err := cgi("/splcloud/fcgi-bin/fcg_get_diss_by_tag.fcg", url.Values{
		"categoryId": {strconv.FormatInt(id, 10)},
		"sortId":     {"5"}, // 最热
		"sin":        {strconv.Itoa(offset)},
		"ein":        {strconv.Itoa(offset + limit - 1)},
	}, &body)
	if err != nil {
		return nil, err
	}
	var out []music.Playlist
	for _, entry := range objects(obj(body["data"]), "list") {
		if pl, ok := playlistOf(entry); ok {
			out = append(out, pl)
		}
	}
	return out, nil
}

// playlistOf reads a playlist from an entry naming its fields as QQ's
// playlist endpoints do, or reports that it is not one.
func playlistOf(entry object) (music.Playlist, bool) {
	key := text(entry, "dissid", "disstid", "tid")
	n, _ := strconv.ParseInt(key, 10, 64)
	if n == 0 {
		return music.Playlist{}, false
	}
	pl := music.Playlist{
		Source:      music.SourceQQ,
		ID:          n,
		Key:         key,
		Name:        unescape(text(entry, "dissname", "title", "name")),
		CoverURL:    text(entry, "imgurl", "logo", "picurl"),
		Description: unescape(text(entry, "introduction", "desc")),
		TrackCount:  int(number(entry, "song_count", "songnum", "total_song_num")),
		PlayCount:   float64(number(entry, "listennum", "visitnum", "play_cnt")),
	}
	if c := obj(entry["creator"]); c != nil {
		pl.Creator.Nickname = text(c, "name", "nick", "nickname")
		pl.Creator.UserID = number(c, "creator_uin", "uin", "qq")
	}
	if pl.Creator.Nickname == "" {
		pl.Creator.Nickname = text(entry, "nick", "nickname")
	}
	return pl, pl.Name != ""
}

// unescape undoes the entities QQ writes into the names it gives: its
// playlist names come with `&#58;` and `&#32;` in them.
func unescape(s string) string {
	if len(s) == 0 {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == '&' && i+2 < len(s) && s[i+1] == '#' {
			j, n := i+2, 0
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				n = n*10 + int(s[j]-'0')
				j++
			}
			if j < len(s) && s[j] == ';' && n > 0 && n < 0x110000 {
				out = append(out, []byte(string(rune(n)))...)
				i = j + 1
				continue
			}
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}
