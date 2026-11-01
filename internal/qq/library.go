package qq

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// What QQ Music keeps of the listener, by their account: the songs it
// picks for them today, the playlists they made and saved, what they like,
// and the station of songs it plays on and on. Each says so when asked
// without a session, rather than answer with an empty page that reads as
// "you have nothing".

// errNoAccount is what a personal page says without a session.
var errNoAccount = fmt.Errorf("qq: this needs the account: %w", source.ErrNoAccount)

// signedSession is the session a personal call is made with, or the error
// saying there is none.
func signedSession() (Session, error) {
	s := currentSession()
	if s.MusicID == "" || s.MusicKey == "" {
		return Session{}, errNoAccount
	}
	return s, nil
}

// Daily returns the songs QQ picks for the listener today.
func (Provider) Daily() ([]music.Track, error) {
	if _, err := signedSession(); err != nil {
		return nil, err
	}
	var data object
	err := rpc("music.srfDissInfo.aiDissInfo", "uniform_get_Dissinfo", map[string]any{
		"dirid": 202, "optype": 3, "tag": 1, "userinfo": 0, "pic_dpi": 300,
	}, &data)
	if err != nil {
		return nil, err
	}
	return convertTracks(objects(data, "songlist", "song_info")), nil
}

// Recommended returns the day's playlists QQ picks for the listener, from
// what they listen to.
func (Provider) Recommended() ([]music.Playlist, error) {
	if _, err := signedSession(); err != nil {
		return nil, err
	}
	var data object
	err := rpcWith("music.recommend.RecommendFeed", "get_recommend_feed", map[string]any{},
		&data, map[string]any{"ct": 20, "cv": 13020008}, nil)
	if err != nil {
		return nil, err
	}
	var out []music.Playlist
	for _, entry := range objects(data, "vec_recommend", "recommend") {
		pl := obj(entry["playlist"])
		if pl == nil {
			pl = entry
		}
		id := number(pl, "tid", "id", "content_id")
		if id <= 0 {
			continue
		}
		out = append(out, music.Playlist{
			Source:      music.SourceQQ,
			ID:          id,
			Key:         strconv.FormatInt(id, 10),
			Name:        text(pl, "title", "name"),
			CoverURL:    text(obj(pl["cover"]), "default_url", "big_url"),
			Description: text(pl, "desc"),
			TrackCount:  int(number(pl, "song_cnt")),
		})
	}
	return out, nil
}

// RadioModes are QQ's ways of picking a station's next songs: it offers
// one, from the listener's taste.
func (Provider) RadioModes() []music.RadioMode {
	return []music.RadioMode{{ID: "DEFAULT", Name: "推荐"}}
}

// Radio returns a station's next songs.
func (Provider) Radio(mode string) ([]music.Track, error) {
	if _, err := signedSession(); err != nil {
		return nil, err
	}
	if mode == "" {
		mode = "DEFAULT"
	}
	if mode != "DEFAULT" {
		return nil, errUnsupported
	}
	var data object
	err := rpc("music.radioProxy.MbTrackRadioSvr", "get_radio_track", map[string]any{
		"id": 99, "num": 15,
	}, &data)
	if err != nil {
		return nil, err
	}
	return convertTracks(objects(data, "tracks", "track_list")), nil
}

// Library returns the playlists the listener made, and those they saved.
func (Provider) Library(uid int64) (made, saved []music.Playlist, err error) {
	if _, err := signedSession(); err != nil {
		return nil, nil, err
	}
	var made4 object
	err = rpc("music.playlist.PlaylistBaseRead", "GetPlaylistByUin", map[string]any{"uin": strconv.FormatInt(uid, 10)}, &made4)
	if err != nil {
		return nil, nil, err
	}
	made = libraryPlaylists(objects(made4, "v_playlist", "playlist"))

	var fav object
	// The saved ones are keyed by the account's encrypted id, which only a
	// session has.
	s, _ := signedSession()
	if s.EncryptUin != "" {
		if err = rpc("music.playlist.PlaylistFavRead", "CgiGetPlaylistFavInfo",
			map[string]any{"uin": s.EncryptUin}, &fav); err == nil {
			saved = libraryPlaylists(objects(fav, "v_playlist", "playlist"))
		}
	}
	return made, saved, nil
}

// libraryPlaylists reads the lists QQ keeps of a listener's playlists.
//
// The 我喜欢 list is one of them, and the app knows it by SpecialType: its
// hearts write to it, and its page offers the albums collected beside its
// songs. QQ names it by a directory (201) rather than by a list id, which
// is what tells it from the lists the listener made.
func libraryPlaylists(entries []object) []music.Playlist {
	var out []music.Playlist
	for _, o := range entries {
		id := number(o, "tid", "dissid", "dirId", "dirid")
		if id <= 0 {
			continue
		}
		pl := music.Playlist{
			Source:     music.SourceQQ,
			ID:         id,
			Key:        strconv.FormatInt(id, 10),
			Name:       text(o, "title", "dissname", "name"),
			CoverURL:   text(o, "picurl", "logo", "cover_url"),
			TrackCount: int(number(o, "song_cnt", "songnum", "total_song_num")),
			// QQ's private lists are the ones only their owner sees.
			Privacy: int(number(o, "dir_show", "show")),
		}
		pl.Creator.Nickname = text(o, "nick", "nickname")
		if number(o, "dirId", "dirid") == likedDir {
			pl.SpecialType = 5
			out = append([]music.Playlist{pl}, out...)
			continue
		}
		out = append(out, pl)
	}
	return out
}

// LikedIDs returns the ids of the songs the listener likes, as QQ lists
// them in the 我喜欢 playlist.
//
// The list is paged by how many songs it is asked for, and QQ stops at a
// page that is short of that; a page that changes nothing means the list
// is being served from a stale answer, and the read is reported as failed
// rather than returned half-done.
func (Provider) LikedIDs(uid int64) ([]int64, error) {
	s, err := signedSession()
	if err != nil {
		return nil, err
	}
	var ids []int64
	seen := map[int64]bool{}
	for begin := 0; begin < 100000; begin += 100 {
		var data object
		err := rpc("music.srfDissInfo.DissInfo", "CgiGetDiss", map[string]any{
			"disstid": 0, "dirid": 201, "tag": 1, "song_begin": begin, "song_num": 100,
			"userinfo": 1, "orderlist": 1, "enc_host_uin": s.EncryptUin,
		}, &data)
		if err != nil {
			return nil, err
		}
		songs := objects(data, "songlist")
		fresh := 0
		for _, raw := range songs {
			if id := number(raw, "id", "songid"); id > 0 && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
				fresh++
			}
		}
		total := int(number(data, "total_song_num"))
		if len(songs) == 0 || total > 0 && len(ids) >= total {
			return ids, nil
		}
		if fresh == 0 {
			return nil, errors.New("qq: the liked songs were not read completely")
		}
	}
	return ids, nil
}

// Like likes or unlikes a song: QQ keeps the liked songs in one list, and
// writing one is adding it to or taking it out of that list.
//
// The write is read back rather than trusted: QQ answers a write with a
// result of its own that is not always 0 for an edit that did happen, and
// the listener is owed the truth about whether it did.
func (Provider) Like(id int64, on bool) error {
	if _, err := signedSession(); err != nil {
		return err
	}
	mid := songMID(id)
	if mid == "" {
		// A like names a song by its mid back to QQ, so one seen only by
		// number — from a liked list, say — is asked its mid first.
		t, err := songByID(id)
		if err != nil {
			return err
		}
		mid = t.Key
	}
	read := func() (bool, error) {
		var data object
		if err := rpc("music.musicasset.SongFavRead", "IsSongFanByMid",
			map[string]any{"v_songMid": []string{mid}}, &data); err != nil {
			return false, err
		}
		var value bool
		raw, ok := obj(data["m_fan"])[mid]
		if !ok || json.Unmarshal(raw, &value) != nil {
			return false, errors.New("qq: no liked status in the answer")
		}
		return value, nil
	}
	current, err := read()
	if err != nil || current == on {
		return err
	}
	method := "DelSonglist"
	if on {
		method = "AddSonglist"
	}
	if _, err := writeResult("music.musicasset.PlaylistDetailWrite", method, map[string]any{
		"dirId": 201, "v_songInfo": []map[string]any{{"songId": id, "songType": 0}},
	}); err != nil {
		return err
	}
	current, err = read()
	if err != nil {
		return err
	}
	if current != on {
		return errors.New("qq: the like was not confirmed")
	}
	return nil
}

// writeResult asks a write of musicu and reads the status it gives its own
// result, which differs from the one rpc checks.
func writeResult(module, method string, param any) (object, error) {
	var data object
	if err := rpc(module, method, param, &data); err != nil {
		return nil, err
	}
	if raw, ok := data["retCode"]; !ok || string(raw) != "0" {
		return nil, fmt.Errorf("qq: %s rejected (retCode %s)", method, data["retCode"])
	}
	result := obj(data["result"])
	if result == nil {
		return nil, fmt.Errorf("qq: %s has no result", method)
	}
	return result, nil
}

// Thread names a song's comments: QQ's thread is its number.
func (Provider) Thread(t music.Track) string {
	if t.Src() != music.SourceQQ {
		return ""
	}
	return strconv.FormatInt(t.ID, 10)
}

// ThreadComments returns a page of a song's comments, and on the first
// page its hot ones.
//
// QQ pages its latest comments by a cursor of its own, not an offset: one
// page is read and its cursor kept, so a second page can follow it. A
// cursor that has gone — the app restarted, or another account signed in —
// says so, and the comments are opened again.
func (Provider) ThreadComments(thread string, offset, limit int) (*music.Comments, error) {
	id, err := strconv.ParseInt(thread, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("qq: bad thread %q", thread)
	}
	limit = min(max(limit, 1), 25)
	cursor := commentCursorFor(thread, offset)
	if offset > 0 && cursor == nil {
		return nil, errors.New("qq: the comments have gone stale; open them again")
	}
	latest, total, more, next, err := readComments(id, limit, cursor, false)
	if err != nil {
		return nil, err
	}
	out := &music.Comments{Latest: latest, Total: total, More: more}
	if offset == 0 {
		hot, _, _, _, err := readComments(id, limit, nil, true)
		if err == nil {
			out.Hot = hot
		}
	}
	if next != nil {
		rememberCursor(thread, offset+len(latest), next)
	}
	return out, nil
}

// LikeComment likes or unlikes a remark. QQ's comment likes are not among
// the endpoints this app calls, so it says so rather than pretend.
func (Provider) LikeComment(thread string, id int64, on bool) error {
	return errUnsupported
}

// User returns who a listener is: QQ tells the app who is signed in, and
// nobody else, so a page of another listener is not one it has.
// User is the listener's own page: QQ shows no one else's, and the name on
// it is the one the session was signed with, refreshed when the account was
// last asked.
func (Provider) User(id int64) (*music.User, error) {
	s := currentSession()
	if !s.valid() {
		return nil, errNoAccount
	}
	if me := toInt(s.MusicID); me != id {
		return nil, errors.New("qq: only your own page is here")
	}
	return &music.User{UserID: toInt(s.MusicID), Nickname: s.Nickname, AvatarURL: s.AvatarURL}, nil
}

// UserPlaylists returns the playlists a listener made and those they
// saved, which QQ shows of the signed-in one alone.
func (Provider) UserPlaylists(id int64) (made, saved []music.Playlist, err error) {
	return (Provider{}).Library(id)
}

// PurchasedSongs and PurchasedAlbums are what the listener bought.
func (Provider) PurchasedSongs(limit, offset int) ([]music.Track, bool, error) {
	list, more, err := purchased("2", "songlist", limit, offset)
	if err != nil {
		return nil, false, err
	}
	return convertTracks(list), more, nil
}

func (Provider) PurchasedAlbums(limit, offset int) ([]music.AlbumProfile, bool, error) {
	list, more, err := purchased("1", "albumlist", limit, offset)
	if err != nil {
		return nil, false, err
	}
	var out []music.AlbumProfile
	for _, o := range list {
		al := music.AlbumProfile{Album: albumOf(convertAlbum(o))}
		al.Name = textOr(al.Name, text(o, "album_name", "name"))
		if al.Name != "" {
			out = append(out, al)
		}
	}
	return out, more, nil
}

// Membership is what the listener's QQ account is.
func (Provider) Membership() (music.Membership, error) {
	s, err := signedSession()
	if err != nil {
		return music.MembershipUnknown, err
	}
	var data struct {
		Info map[string]struct {
			VIP  *int `json:"iVipFlag"`
			Huge *int `json:"HugeVip"`
		} `json:"infoMap"`
	}
	err = rpc("userInfo.VipQueryServer", "SRFVipQuery_V2",
		map[string]any{"uin_list": []string{s.MusicID}}, &data)
	if err != nil {
		return music.MembershipUnknown, err
	}
	info, ok := data.Info[s.MusicID]
	if !ok {
		return music.MembershipUnknown, errors.New("qq: no membership in the answer")
	}
	switch {
	case info.Huge == nil:
		return music.MembershipUnknown, nil
	case *info.Huge == 1:
		return music.MembershipSuper, nil
	case *info.Huge != 0:
		return music.MembershipUnknown, nil
	case info.VIP == nil:
		return music.MembershipUnknown, nil
	case *info.VIP == 1:
		return music.MembershipVIP, nil
	}
	return music.MembershipFree, nil
}

// likedDir is the directory QQ keeps the liked songs in: the 我喜欢 list is
// a directory of the account's, not a playlist the listener made.
const likedDir = 201

// pathCollected is the legacy endpoint the album collection is read from.
// The musicu method QQ's own client uses does not return the list.
const pathCollected = "/fav/fcgi-bin/fcg_get_profile_order_asset.fcg"

// CollectedAlbums returns the albums the listener collected, a page at a
// time. The endpoint pages by an inclusive range, which is why it is asked
// with both ends rather than with a count.
func (Provider) CollectedAlbums(limit, offset int) ([]music.AlbumProfile, bool, error) {
	s, err := signedSession()
	if err != nil {
		return nil, false, err
	}
	limit, offset = min(max(limit, 1), 100), max(offset, 0)
	var body struct {
		Data *struct {
			Albums []object `json:"albumlist"`
			Total  *int64   `json:"totalalbum"`
		} `json:"data"`
	}
	if err := cgi(pathCollected, url.Values{
		"ct": {"20"}, "cid": {"205360956"}, "userid": {s.MusicID},
		"reqtype": {"2"}, "sin": {strconv.Itoa(offset)}, "ein": {strconv.Itoa(offset + limit - 1)},
	}, &body); err != nil {
		return nil, false, err
	}
	// A total of nothing with no list is a listener who collected nothing;
	// a list that stops short of the total is a half-read answer, which is
	// reported rather than shown as a collection of fewer albums than it is.
	if body.Data == nil || body.Data.Total == nil || *body.Data.Total < 0 {
		return nil, false, errors.New("qq: no collected albums in the answer")
	}
	d := body.Data
	if d.Albums == nil && *d.Total > 0 || len(d.Albums) == 0 && int64(offset) < *d.Total {
		return nil, false, errors.New("qq: the collected albums were not read completely")
	}
	out := make([]music.AlbumProfile, 0, len(d.Albums))
	for _, entry := range d.Albums {
		al := music.AlbumProfile{Album: albumOf(convertAlbum(entry))}
		if al.ID <= 0 {
			return nil, false, errors.New("qq: a collected album without an id")
		}
		if al.Name == "" {
			al.Name = text(entry, "album_name", "albumName")
		}
		al.Size = int(number(entry, "song_count", "songnum", "totalNum"))
		if al.PicURL == "" {
			al.PicURL = text(entry, "picurl", "picUrl", "albumPicUrl")
		}
		for _, singer := range objects(entry, "singers", "singer") {
			if ar := convertArtist(singer); ar.Name != "" {
				al.Artists = append(al.Artists, artistOf(ar))
			}
		}
		if len(al.Artists) == 0 {
			if ar := convertArtist(entry); ar.Name != "" {
				al.Artists = append(al.Artists, artistOf(ar))
			}
		}
		if date := text(entry, "release_time", "publishDate", "pubtime"); date != "" {
			if parsed, err := time.Parse("2006-01-02", date); err == nil {
				al.PublishTime = parsed.UnixMilli()
			}
		}
		if al.Name != "" {
			out = append(out, al)
		}
	}
	return out, int64(offset)+int64(len(out)) < *d.Total, nil
}

// AlbumCollected reports whether the listener collected an album, which QQ
// asks by mid rather than by the number the app knows it by.
func (Provider) AlbumCollected(id int64) (bool, error) {
	mid, err := collectedAlbumMID(id)
	if err != nil {
		return false, err
	}
	var data object
	if err := rpc("music.musicasset.AlbumFavRead", "IsAlbumFan",
		map[string]any{"uin": collectedOwner(), "v_albumMid": []string{mid}}, &data); err != nil {
		return false, err
	}
	var liked bool
	raw, ok := obj(data["m_fan"])[mid]
	if !ok || json.Unmarshal(raw, &liked) != nil {
		return false, errors.New("qq: no collected status in the answer")
	}
	return liked, nil
}

// CollectAlbum collects an album, or takes it out of the collection.
//
// The write is read back rather than trusted: QQ answers a write with a
// result of its own that is not always 0 for an edit that did happen.
func (Provider) CollectAlbum(id int64, on bool) error {
	mid, err := collectedAlbumMID(id)
	if err != nil {
		return err
	}
	current, err := (Provider{}).AlbumCollected(id)
	if err != nil || current == on {
		return err
	}
	method := "CancelFavAlbum"
	if on {
		method = "FavAlbum"
	}
	var data struct {
		Result     *int              `json:"result"`
		FailedIDs  []json.RawMessage `json:"v_failedAlbumId"`
		FailedMIDs []json.RawMessage `json:"v_failedAlbumMid"`
	}
	if err := rpc("music.musicasset.AlbumFavWrite", method,
		map[string]any{"uin": collectedOwner(), "v_albumMid": []string{mid}}, &data); err != nil {
		return err
	}
	if data.Result == nil || *data.Result != 0 || len(data.FailedIDs)+len(data.FailedMIDs) != 0 {
		return errors.New("qq: the collection was not confirmed")
	}
	current, err = (Provider{}).AlbumCollected(id)
	if err != nil {
		return err
	}
	if current != on {
		return errors.New("qq: the collection was not confirmed")
	}
	return nil
}

// collectedAlbumMID is an album's mid, which the app knows from the answers
// that carried the album. One it has not seen asks QQ for its page, which
// is where the mid comes from.
func collectedAlbumMID(id int64) (string, error) {
	if _, err := signedSession(); err != nil {
		return "", err
	}
	if mid := albumMID(id); mid != "" {
		return mid, nil
	}
	if _, err := (Provider{}).Album(id); err != nil {
		return "", err
	}
	if mid := albumMID(id); mid != "" {
		return mid, nil
	}
	return "", errors.New("qq: this album has no mid")
}

// collectedOwner is who a collection is asked of. QQ takes the id as a
// number while it fits in a JSON number, and as the string it is when it
// does not: its own client sends both, and it takes either.
func collectedOwner() any {
	s := currentSession()
	if id := toInt(s.MusicID); id > 0 && id <= 1<<53-1 {
		return id
	}
	return s.MusicID
}

// purchased asks the order list QQ's own profile shows.
func purchased(kind, list string, limit, offset int) ([]object, bool, error) {
	s, err := signedSession()
	if err != nil {
		return nil, false, err
	}
	limit = min(max(limit, 1), 100)
	offset = max(offset, 0)
	var body object
	err = cgi("/shop/fcgi-bin/fcg_get_order", url.Values{
		"from": {"1"}, "cmd": {"sales_album"}, "g_tk": {gtk(s.MusicKey)},
		"order_type": {kind}, "page": {strconv.Itoa(offset/limit + 1)}, "num": {strconv.Itoa(limit)},
	}, &body)
	if err != nil {
		return nil, false, err
	}
	data := obj(body["data"])
	entries := objects(data, list)
	total := int(number(data, "total"))
	return entries, offset+len(entries) < total, nil
}

// gtk is the token QQ's older endpoints check, a hash of the session key.
func gtk(musicKey string) string {
	var h uint32 = 5381
	for i := 0; i < len(musicKey); i++ {
		h = h*33 + uint32(musicKey[i])
	}
	return strconv.FormatUint(uint64(h&0x7fffffff), 10)
}
