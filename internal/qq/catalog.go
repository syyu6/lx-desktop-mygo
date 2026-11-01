package qq

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"netease-music-mygo/internal/music"
)

// What QQ Music offers a listener who is not signed in: its playlists, its
// charts, its artists, its albums, and searching for any of them. What it
// will not give without an account is what is personal — the songs picked
// for a listener, their own lists, what they like — and those live in
// library.go, which says so when asked without one.

// chartBase offsets QQ's chart ids, which number from 1 and would collide
// with its playlists': a chart is a playlist here, as it is to every page,
// and its id says which kind it is.
const chartBase = int64(1) << 60

// songChart is the kind of chart a playlist's page can open: QQ lists
// album, MV and artist charts besides, which are not playlists of songs.
const songChart = 10005

// ErrUnavailable is returned for a song QQ has no link to play: one it
// brings only with an account, or a membership.
var ErrUnavailable = errors.New("qq: no link to play this song")

// Featured returns the playlists QQ's home shows.
func (Provider) Featured(limit int) ([]music.Playlist, error) {
	var data object
	err := rpc("music.playlist.PlaylistSquare", "GetRecommendFeed",
		map[string]any{"From": 0, "Size": min(max(limit, 1), 60)}, &data)
	if err != nil {
		return nil, err
	}
	var out []music.Playlist
	for _, entry := range objects(data, "List") {
		basic := obj(obj(entry["Playlist"])["basic"])
		if basic == nil {
			continue
		}
		id := number(basic, "tid")
		if id <= 0 {
			continue
		}
		pl := music.Playlist{
			Source:      music.SourceQQ,
			ID:          id,
			Key:         strconv.FormatInt(id, 10),
			Name:        text(basic, "title"),
			CoverURL:    text(obj(basic["cover"]), "default_url", "big_url", "medium_url", "small_url"),
			Description: text(basic, "desc"),
			TrackCount:  int(number(basic, "song_cnt")),
			PlayCount:   float64(number(basic, "play_cnt")),
		}
		pl.Creator.Nickname = text(obj(basic["creator"]), "nick")
		out = append(out, pl)
	}
	return out, nil
}

// Charts returns QQ's charts of songs, each a playlist its page opens.
//
// The list is asked as the mobile client asks it: the web one answers with
// album and MV charts only, which no playlist page can open.
func (Provider) Charts() ([]music.Playlist, error) {
	var data object
	err := rpcWith("musicToplist.ToplistInfoServer", "GetAll", map[string]any{}, &data,
		map[string]any{"ct": 24, "cv": 0}, nil)
	if err != nil {
		return nil, err
	}
	var out []music.Playlist
	for _, group := range objects(data, "group") {
		for _, entry := range objects(group, "toplist") {
			id := number(entry, "topId")
			if id <= 0 || number(entry, "recType") != songChart {
				continue
			}
			pl := music.Playlist{
				Source:      music.SourceQQ,
				ID:          chartBase + id,
				Key:         strconv.FormatInt(chartBase+id, 10),
				Name:        text(entry, "title"),
				CoverURL:    text(entry, "headPicUrl", "frontPicUrl", "musichallPicUrl"),
				Description: text(entry, "intro"),
				TrackCount:  int(number(entry, "totalNum")),
				PlayCount:   float64(number(entry, "listenNum")),
				UpdateFreq:  text(entry, "updateTime"),
			}
			for _, song := range objects(entry, "song") {
				pl.Preview = append(pl.Preview, music.ChartLine{
					First:  text(song, "title"),
					Second: text(song, "singerName"),
				})
			}
			out = append(out, pl)
		}
	}
	return out, nil
}

// searchDesktop asks QQ's desktop search, which answers with songs, artists,
// albums or playlists by kind, and takes a page of at most 30.
//
// It is asked as the desktop client (ct 19), not as the one the rest of the
// app says it is: the same method under the web identity answers with empty
// lists, and the lite identity the older code here used was closed.
func searchDesktop(query string, kind music.SearchKind, limit, offset int) (object, object, []object, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 60 {
		return nil, nil, nil, fmt.Errorf("qq: bad search")
	}
	limit = min(max(limit, 1), 30)
	offset = max(offset, 0)
	code, key := 0, "song"
	switch kind {
	case music.SearchSongs:
	case music.SearchArtists:
		code, key = 1, "singer"
	case music.SearchAlbums:
		code, key = 2, "album"
	case music.SearchPlaylists:
		code, key = 3, "songlist"
	default:
		return nil, nil, nil, errUnsupported
	}
	var data object
	err := rpcWith("music.search.SearchCgiService", "DoSearchForQQMusicDesktop", map[string]any{
		"grp": 1, "num_per_page": limit, "page_num": offset/limit + 1,
		"query": query, "search_type": code, "searchid": searchID(),
	}, &data, map[string]any{"ct": "19", "cv": "1859", "uin": "0"}, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	body, meta := obj(data["body"]), obj(data["meta"])
	if body == nil || meta == nil {
		return nil, nil, nil, fmt.Errorf("qq: malformed search answer")
	}
	// Each section is an object of its own — {"list": [...], "total": n} —
	// not the bare array the lite endpoint answered with.
	entries := objects(obj(body[key]), "list")
	if entries == nil {
		if err := json.Unmarshal(body[key], &entries); err != nil {
			return nil, nil, nil, fmt.Errorf("qq: malformed search answer")
		}
	}
	for _, entry := range entries {
		if entry == nil {
			return nil, nil, nil, fmt.Errorf("qq: malformed search entry")
		}
	}
	return body, meta, entries, nil
}

// searchID is a number QQ's search wants of its own, of 18 digits: it is
// counted in its logs, not checked.
func searchID() string {
	var b [18]byte
	_, _ = rand.Read(b[:])
	for i := range b {
		b[i] = '0' + b[i]%10
	}
	if b[0] == '0' {
		b[0] = '1'
	}
	return string(b[:])
}

// SearchSongs returns a page of the songs matching query, and how many
// match in all.
func (Provider) SearchSongs(query string, limit, offset int) ([]music.Track, int, error) {
	_, meta, entries, err := searchDesktop(query, music.SearchSongs, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	var out []music.Track
	seen := map[string]bool{}
	for _, entry := range entries {
		t := convertTrack(entry)
		if t.Key == "" || seen[t.Key] {
			continue
		}
		seen[t.Key] = true
		out = append(out, t)
	}
	return out, int(number(meta, "sum")), nil
}

// SearchAlbums returns a page of the albums matching query, and how many
// match in all.
func (Provider) SearchAlbums(query string, limit, offset int) ([]music.AlbumProfile, int, error) {
	_, meta, entries, err := searchDesktop(query, music.SearchAlbums, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	var out []music.AlbumProfile
	for _, entry := range entries {
		al := music.AlbumProfile{Album: albumOf(convertAlbum(entry))}
		for _, a := range objects(entry, "singer", "singer_list") {
			al.Artists = append(al.Artists, artistOf(convertArtist(a)))
		}
		if len(al.Artists) == 0 && al.Name != "" {
			if names := strings.TrimSpace(text(entry, "singerName")); names != "" {
				al.Artists = append(al.Artists, music.Artist{Name: names})
			}
		}
		al.Size = int(number(entry, "song_count", "songcount", "songNum"))
		if al.Size == 0 {
			al.Size = int(number(entry, "songnum"))
		}
		if al.Name != "" {
			out = append(out, al)
		}
	}
	return out, int(number(meta, "sum")), nil
}

// SearchArtists returns a page of the artists matching query, and how many
// match in all.
func (Provider) SearchArtists(query string, limit, offset int) ([]music.ArtistProfile, int, error) {
	_, meta, entries, err := searchDesktop(query, music.SearchArtists, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	var out []music.ArtistProfile
	for _, entry := range entries {
		raw := convertArtist(entry)
		if raw.Name == "" {
			continue
		}
		ar := artistProfileOf(raw)
		ar.MusicSize = int(number(entry, "song_num", "songnum", "songNum"))
		ar.AlbumSize = int(number(entry, "album_num", "albumnum", "albumNum"))
		out = append(out, ar)
	}
	return out, int(number(meta, "sum")), nil
}

// SearchPlaylists returns a page of the playlists matching query, and how
// many match in all.
func (Provider) SearchPlaylists(query string, limit, offset int) ([]music.Playlist, int, error) {
	_, meta, entries, err := searchDesktop(query, music.SearchPlaylists, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	var out []music.Playlist
	for _, entry := range entries {
		pl, ok := playlistOf(entry)
		if !ok {
			continue
		}
		out = append(out, pl)
	}
	return out, int(number(meta, "sum")), nil
}

// Artist returns an artist with the songs they are best known for.
func (Provider) Artist(id int64) (*music.ArtistProfile, error) {
	mid := artistMID(id)
	if mid == "" {
		return nil, fmt.Errorf("qq: artist %d has no mid", id)
	}
	data, err := artistSongs(mid, 5, 50)
	if err != nil {
		return nil, err
	}
	info := obj(data["singer_info"])
	raw := convertArtist(info)
	if raw.ID == 0 {
		raw.ID = id
	}
	if raw.Mid == "" {
		raw.Mid = mid
	}
	p := artistProfileOf(raw)
	p.BriefDesc = text(info, "desc", "description")
	p.MusicSize = int(number(data, "total_song"))
	p.AlbumSize = int(number(data, "total_album"))
	p.Songs = convertTracks(objects(data, "songlist"))
	return &p, nil
}

// ArtistSongs returns an artist's best known songs, or their newest.
func (Provider) ArtistSongs(id int64, newest bool, limit int) ([]music.Track, error) {
	mid := artistMID(id)
	if mid == "" {
		return nil, fmt.Errorf("qq: artist %d has no mid", id)
	}
	sort := 5 // by popularity
	if newest {
		sort = 2
	}
	data, err := artistSongs(mid, sort, limit)
	if err != nil {
		return nil, err
	}
	return convertTracks(objects(data, "songlist")), nil
}

// songByID asks QQ about one song by its number: a mid is enough to play
// it, but a like is written back with both.
func songByID(id int64) (music.Track, error) {
	var data object
	if err := rpc("music.pf_song_detail_svr", "get_song_detail_yqq", map[string]any{
		"song_type": 0, "song_mid": songMids.get(id), "song_id": strconv.FormatInt(id, 10),
	}, &data); err != nil {
		return music.Track{}, err
	}
	t := convertTrack(obj(data["track_info"]))
	if t.ID <= 0 || t.Key == "" {
		return music.Track{}, fmt.Errorf("qq: song %d not found", id)
	}
	return t, nil
}

// artistSongs asks for an artist's songs by their mid, which every endpoint
// of that kind is keyed on: QQ's numeric artist ids mean nothing to them.
func artistSongs(mid string, sort, limit int) (object, error) {
	var data object
	err := rpc("music.web_singer_info_svr", "get_singer_detail_info",
		map[string]any{"sort": sort, "singermid": mid, "sin": 0, "num": limit}, &data)
	return data, err
}

// ArtistAlbums returns an artist's albums, newest first.
func (Provider) ArtistAlbums(id int64, limit, offset int) ([]music.AlbumProfile, bool, error) {
	mid := artistMID(id)
	if mid == "" {
		return nil, false, fmt.Errorf("qq: artist %d has no mid", id)
	}
	var data object
	// This one rejects the signed-in client's identity, so it is asked as
	// the web client.
	err := rpcWith("music.musichallAlbum.AlbumListServer", "GetAlbumList", map[string]any{
		"sort": 5, "singermid": mid, "begin": offset, "num": limit,
	}, &data, map[string]any{"ct": 24, "cv": 0}, nil)
	if err != nil {
		return nil, false, err
	}
	var out []music.AlbumProfile
	for _, entry := range objects(data, "albumList") {
		al := music.AlbumProfile{Album: albumOf(convertAlbum(entry))}
		al.PublishTime = number(entry, "publish_date") * 1000
		if al.Name != "" {
			out = append(out, al)
		}
	}
	return out, offset+len(out) < int(number(data, "total")), nil
}

// Album returns an album with its songs.
func (Provider) Album(id int64) (*music.AlbumProfile, error) {
	q := url.Values{}
	if mid := albumMID(id); mid != "" {
		q.Set("albummid", mid)
	} else {
		q.Set("albumid", strconv.FormatInt(id, 10))
	}
	var body object
	if err := cgi("/v8/fcg-bin/fcg_v8_album_info_cp.fcg", q, &body); err != nil {
		return nil, err
	}
	data := obj(body["data"])
	if data == nil {
		return nil, errors.New("qq: no such album")
	}
	al := music.AlbumProfile{Album: albumOf(convertAlbum(data))}
	if al.ID == 0 {
		al.ID = id
	}
	al.Description = text(data, "desc")
	al.Size = int(number(data, "total", "songnum"))
	al.Songs = convertTracks(objects(data, "list", "songlist"))
	if al.Size == 0 {
		al.Size = len(al.Songs)
	}
	for _, a := range objects(data, "singer") {
		al.Artists = append(al.Artists, artistOf(convertArtist(a)))
	}
	if len(al.Artists) == 0 && len(al.Songs) > 0 {
		al.Artists = al.Songs[0].Artists
	}
	if s := text(data, "aDate", "time_public"); s != "" {
		if al.PublishTime = dateMillis(s); al.PublishTime == 0 {
			al.PublishTime = 0
		}
	}
	return &al, nil
}
