package netease

import (
	"errors"

	"netease-music-mygo/internal/music"
)

// The pages NetEase has beyond its catalogue, as package source asks
// them: what its home offers, its charts, its daily songs, a station, and
// the pages of a song, an artist, an album and a listener.
//
// Each is a method on Provider rather than a separate type, so that a
// page asks `reg.Get(id).(source.Charted)` and gets NetEase's charts or
// nothing. A method that needs an account says so with an error the page
// shows, as it does for a song it will not stream: NetEase's daily songs
// and the listener's own lists are theirs alone.

// Featured returns the playlists NetEase's home shows.
func (Provider) Featured(limit int) ([]music.Playlist, error) {
	return FeaturedPlaylists(limit)
}

// Charts returns NetEase's charts.
func (Provider) Charts() ([]music.Playlist, error) {
	return Charts()
}

// Daily returns the songs NetEase picks for the listener today, which it
// has only once it knows who is listening.
func (Provider) Daily() ([]music.Track, error) {
	return DailySongs()
}

// Recommended returns the day's playlists NetEase picks for the listener,
// from what they listen to.
func (Provider) Recommended() ([]music.Playlist, error) {
	return RecommendedPlaylists()
}

// RadioModes are NetEase's three ways of picking a station's next songs.
func (Provider) RadioModes() []music.RadioMode {
	return []music.RadioMode{
		{ID: FMDefault, Name: "推荐"},
		{ID: FMFamiliar, Name: "熟悉"},
		{ID: FMExplore, Name: "探索"},
	}
}

// Radio returns a station's next songs.
func (Provider) Radio(mode string) ([]music.Track, error) {
	return PersonalFM(mode)
}

// Artist returns an artist with the songs they are best known for.
func (Provider) Artist(id int64) (*music.ArtistProfile, error) {
	return ArtistDetail(id)
}

// ArtistSongs returns an artist's best known songs, or their newest.
func (Provider) ArtistSongs(id int64, newest bool, limit int) ([]music.Track, error) {
	if !newest {
		ar, err := ArtistDetail(id)
		if err != nil {
			return nil, err
		}
		if len(ar.Songs) <= limit {
			return ar.Songs, nil
		}
		return ar.Songs[:limit], nil
	}
	return ArtistLatestSongs(id, limit)
}

// ArtistAlbums returns an artist's albums, newest first.
func (Provider) ArtistAlbums(id int64, limit, offset int) ([]music.AlbumProfile, bool, error) {
	return ArtistAlbums(id, limit, offset)
}

// Album returns an album with its songs.
func (Provider) Album(id int64) (*music.AlbumProfile, error) {
	return AlbumDetail(id)
}

// User returns who a listener is.
func (Provider) User(id int64) (*music.User, error) {
	return UserDetail(id)
}

// UserPlaylists returns the playlists a listener made and those they
// saved.
func (Provider) UserPlaylists(id int64) (made, saved []music.Playlist, err error) {
	return UserPlaylists(id)
}

// Library returns the listening listener's own playlists and those they
// saved. NetEase keeps them per account, so uid says whose: asked for
// another's, it returns nothing rather than a page the listener cannot
// change.
func (p Provider) Library(uid int64) (made, saved []music.Playlist, err error) {
	prof, err := Account()
	if err != nil {
		return nil, nil, err
	}
	if prof == nil {
		return nil, nil, errors.New("netease: no account")
	}
	if prof.UserID != uid {
		return nil, nil, errors.New("netease: not your account")
	}
	return UserPlaylists(uid)
}

// LikedIDs returns the ids of the songs the listener likes.
func (Provider) LikedIDs(uid int64) ([]int64, error) {
	return LikedIDs(uid)
}

// Like likes or unlikes a song.
func (Provider) Like(id int64, on bool) error {
	return Like(id, on)
}

// Thread names the comments of a song or an episode.
func (Provider) Thread(t music.Track) string {
	return Thread(&t)
}

// ThreadComments returns a page of a thread's comments, and on the first
// page its hot ones.
func (Provider) ThreadComments(thread string, offset, limit int) (*music.Comments, error) {
	return ThreadComments(thread, offset, limit)
}

// LikeComment likes or unlikes a remark.
func (Provider) LikeComment(thread string, id int64, on bool) error {
	return LikeComment(thread, id, on)
}

// PurchasedSongs and PurchasedAlbums are what the listener bought, which
// NetEase sells nothing of: it has no shop, so the page is not offered.
func (Provider) PurchasedSongs(limit, offset int) ([]music.Track, bool, error) {
	return nil, false, errNoShop
}

func (Provider) PurchasedAlbums(limit, offset int) ([]music.AlbumProfile, bool, error) {
	return nil, false, errNoShop
}

// Membership is what the listener's NetEase account is, which NetEase
// does not say to an app outside its own: unknown, as it is to it.
func (Provider) Membership() (music.Membership, error) {
	return music.MembershipUnknown, errNoShop
}

var errNoShop = errors.New("netease: nothing is sold here")
