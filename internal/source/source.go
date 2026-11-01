// Package source is where the app asks its music services: NetEase, which
// it ships, and the services lx-music-desktop asks, whose scripts the
// listener may import for the songs no built-in source will stream.
//
// A provider answers for one source id: searching it, reading a playlist,
// and streaming a song of it. What a service cannot do, its provider does
// not implement: the pages ask a capability by asserting the interface,
// and hide what the source does not have rather than show it empty.
package source

import (
	"errors"
	"sync"

	"netease-music-mygo/internal/music"
)

// Catalog is a source the app can search: its name, and what its search
// finds. It is what a search source is, and half of a music source.
//
// The two are separate, as they are in lx-music-desktop. A search asks the
// catalogues (搜索源), whichever of them the listener turned on, and puts
// their answers together; a song plays from a music source (音乐源), which
// is a service the app streams from or a script the listener imported.
// NetEase is both: its catalogue is searched, and its service streams.
// Kugou, QQ Music and Migu are searched alone, their songs playing through
// whoever can stream them.
type Catalog interface {
	// ID is the source id: music.SourceNetease, or one of the services'
	// short ids (music.SourceKuwo and so on).
	ID() string
	// Name is what the listener calls it.
	Name() string
	// SearchSongs returns a page of what matches query, and how many
	// match in all.
	SearchSongs(query string, limit, offset int) ([]music.Track, int, error)
}

// Provider is a music source: a catalogue the app streams from itself.
type Provider interface {
	Catalog
	// Stream returns where a song streams from, at level (one of
	// music.Levels), or the best below it the source serves.
	Stream(t music.Track, level string) (*music.Stream, error)
	// Lyrics returns a song's words: as an error if it has none.
	Lyrics(t music.Track) (music.Lyrics, error)
}

// Lister is a provider whose playlists the app can open: a service that
// names its playlists by an id of its own, as NetEase and Kuwo do. Migu's
// are not, so it streams its songs and opens no playlist of them.
type Lister interface {
	// Playlist returns a playlist with its songs.
	Playlist(id string) (*music.Playlist, error)
}

// Searcher is a provider that searches playlists too.
type Searcher interface {
	SearchPlaylists(query string, limit, offset int) ([]music.Playlist, int, error)
}

// PeopleSearcher is a source whose catalogue finds artists and albums: not
// every one has a page for them to open, and the ones that do are hoped for
// rather than required.
type PeopleSearcher interface {
	SearchArtists(query string, limit, offset int) ([]music.ArtistProfile, int, error)
	SearchAlbums(query string, limit, offset int) ([]music.AlbumProfile, int, error)
}

// Coverer is a provider that finds a song's cover apart from its album's.
type Coverer interface {
	Pic(t music.Track) (string, error)
}

// Square is a provider with a playlist square: categories, and the
// playlists under one.
type Square interface {
	Tags() ([]music.Tag, error)
	SquarePlaylists(tag string, limit, offset int) ([]music.Playlist, error)
}

// The interfaces below are the pages a source has when it has more than a
// catalogue: what its home offers, what its charts are, and the pages a
// song, an artist, an album and a listener open. A source answers only
// for what it has, and the page asks by asserting the interface, as it
// does for a square: a service with no daily songs shows no daily card
// rather than an empty one.
//
// What a source will not give without an account is an error like any
// other, and its page says so: a source may have daily songs that only a
// listener signed in hears.

// FeaturedSource is a source with playlists for its home page.
type FeaturedSource interface {
	Featured(limit int) ([]music.Playlist, error)
}

// Charted is a source with charts: playlists of what is played most,
// which its home shows a few of and its charts page all.
type Charted interface {
	Charts() ([]music.Playlist, error)
}

// Daily is a source that picks songs for the listener each day.
type Daily interface {
	Daily() ([]music.Track, error)
}

// Recommended is a source that picks playlists for the listener, from
// what they listen to: it has to know who is listening.
type Recommended interface {
	Recommended() ([]music.Playlist, error)
}

// Radio is a source with a station of songs it plays on and on, from the
// listener's taste or from one song's. Modes are its own, and it says
// which: a service may offer a station and nothing else.
type Radio interface {
	// RadioModes are the modes its station has, the first the one it
	// starts on; empty for a source with none, as a station of songs it
	// picks alone.
	RadioModes() []music.RadioMode
	// Radio returns a station's next songs.
	Radio(mode string) ([]music.Track, error)
}

// ArtistPages is a source with an artist's page: who they are, what they
// are known for, their newest songs, and their albums.
type ArtistPages interface {
	Artist(id int64) (*music.ArtistProfile, error)
	// ArtistSongs returns an artist's songs: their best known, or their
	// newest, up to limit.
	ArtistSongs(id int64, newest bool, limit int) ([]music.Track, error)
	ArtistAlbums(id int64, limit, offset int) ([]music.AlbumProfile, bool, error)
}

// AlbumPages is a source with an album's page.
type AlbumPages interface {
	Album(id int64) (*music.AlbumProfile, error)
}

// UserPages is a source with a listener's page: who they are, and the
// playlists they made that anyone may see.
type UserPages interface {
	User(id int64) (*music.User, error)
	UserPlaylists(id int64) (made, saved []music.Playlist, err error)
}

// Library is a source whose account keeps what the listener collects:
// their own playlists, the songs they like, and changing either. The uid
// is who is listening, as the account's own profile said: a service that
// numbers its listeners, as both the app's do, is asked by it.
type Library interface {
	// Library returns the playlists the listener made, and those they
	// saved from others.
	Library(uid int64) (made, saved []music.Playlist, err error)
	// LikedIDs returns the ids of the songs the listener likes. A source
	// that likes songs without a list says so another way; this is the
	// one NetEase and QQ Music both have.
	LikedIDs(uid int64) ([]int64, error)
	// Like likes or unlikes a song.
	Like(id int64, on bool) error
}

// Comments are a source's remarks on a song: the thread naming them, the
// pages of them, and liking them. A source that has a thread but no likes
// returns an error for one, which its page does not offer.
type Comments interface {
	Thread(t music.Track) string
	ThreadComments(thread string, offset, limit int) (*music.Comments, error)
	LikeComment(thread string, id int64, on bool) error
}

// Extras is what a source sells or grants beyond listening: what the
// listener bought, and what their account is.
type Extras interface {
	PurchasedSongs(limit, offset int) ([]music.Track, bool, error)
	PurchasedAlbums(limit, offset int) ([]music.AlbumProfile, bool, error)
	Membership() (music.Membership, error)
}

// Collection is a source whose account keeps albums as well as songs: the
// albums the listener collected, shown beside the songs they like.
//
// It is apart from Library, which is the songs alone: NetEase has no such
// collection that an app outside its own may read, and its 我喜欢 page has
// no albums tab rather than an empty one.
type Collection interface {
	CollectedAlbums(limit, offset int) ([]music.AlbumProfile, bool, error)
	// AlbumCollected reports whether the listener collected an album.
	AlbumCollected(id int64) (bool, error)
	// CollectAlbum collects an album, or takes it out of the collection.
	CollectAlbum(id int64, on bool) error
}

var (
	// ErrNoSource is returned when no enabled source is asked.
	ErrNoSource = errors.New("source: no source is enabled")
	// ErrUnknownSource is returned for an id no provider answers to.
	ErrUnknownSource = errors.New("source: no such source")
	// ErrNoAccount is what a source returns for something only a signed-in
	// listener is given: its daily songs, the lists they made, what they
	// like. The pages tell it apart from a source that is not answering,
	// and say which one the listener should sign in to.
	ErrNoAccount = errors.New("source: this needs the account")
)

// Cap is something a source does, which the listener may want it for and
// not for another: a service whose songs are worth searching is not always
// one whose words are worth asking, and one kept for its lyrics alone is
// not searched.
type Cap int

const (
	// CapLibrary is a source's catalogue: searching it, its square of
	// playlists, and the pages NetEase alone has. It is apart from
	// CapStream since a source may be a catalogue without a way to play
	// what it finds — Kugou needs a signed address — and a catalogue the
	// listener does not search says nothing about where its songs play.
	CapLibrary Cap = iota
	// CapStream is a source's songs: a song of it is streamed from it.
	CapStream
	// CapLyrics is a source's words: asked for a song the sources before
	// it have no words for.
	CapLyrics
	// CapComments is a source's remarks on a song.
	CapComments
)

// Capabilities is every Cap, in the order a source's switches show.
var Capabilities = []Cap{CapLibrary, CapStream, CapLyrics, CapComments}

// CapName is what a capability is called on its switch.
func CapName(c Cap) string {
	switch c {
	case CapLibrary:
		return "曲库"
	case CapLyrics:
		return "歌词"
	case CapComments:
		return "评论"
	}
	return "单曲"
}

// Registry is the sources the app has: the services, in the listener's
// order, and what of each is asked of them.
//
// They are one set of services, each doing what it can: NetEase searches,
// streams, and knows the words and the remarks of its songs; Kuwo searches
// and streams; Kugou is searched alone. What the listener wants of a
// service is turned on per capability (Cap), so that a service kept for
// its lyrics alone is not searched, and one searched is not asked for
// words it does not have.
//
// A registry is read as the app builds its pages and as songs load, from
// both of its threads: what it holds is set once, at startup, and what
// changes, what the listener asks for and the order, is behind the lock.
type Registry struct {
	mu        sync.RWMutex
	catalogs  map[string]Catalog
	providers map[string]Provider
	order     []string          // the source ids, in the order they were given
	on        map[string]capSet // by source id: what is asked of it
}

// capSet is what a source is asked for.
type capSet map[Cap]bool

// NewRegistry returns a registry of sources, all on, in the order given:
// which is the order they are asked in. A source that is a Provider is
// streamed from as well as searched.
func NewRegistry(catalogs ...Catalog) *Registry {
	r := &Registry{
		catalogs:  map[string]Catalog{},
		providers: map[string]Provider{},
		on:        map[string]capSet{},
	}
	for _, c := range catalogs {
		id := c.ID()
		if _, ok := r.catalogs[id]; ok {
			continue
		}
		r.catalogs[id] = c
		r.order = append(r.order, id)
		if p, ok := c.(Provider); ok {
			r.providers[id] = p
		}
		// A source is asked for what it has, and nothing else: Kugou is
		// not streamed from, so it is never asked for a stream.
		set := capSet{}
		for _, cap := range Capabilities {
			set[cap] = r.provides(c, cap)
		}
		r.on[id] = set
	}
	return r
}

// provides reports whether a source does a capability at all: whether its
// switch is one there is anything behind.
func (r *Registry) provides(c Catalog, cap Cap) bool {
	switch cap {
	case CapLibrary:
		// Every catalogue is one.
		return true
	case CapStream:
		// Only a Provider plays its own songs: the others' play through
		// whoever can, a script or a service with the same song.
		_, ok := c.(Provider)
		return ok
	case CapLyrics:
		if _, ok := c.(Lyricist); ok {
			return true
		}
		_, ok := c.(Provider)
		return ok
	case CapComments:
		if cc, ok := c.(Commenter); ok {
			return cc.HasComments()
		}
	}
	return false
}

// Commenter is a source with remarks on its songs. Only NetEase has them:
// the others' songs are shown with NetEase's, found by name, so that a
// song of any source has a thread.
type Commenter interface {
	HasComments() bool
}

// Get returns the provider of a source id, whether or not it is enabled:
// a song already in the queue plays from its source though the listener
// has since turned the source off.
func (r *Registry) Get(id string) (Provider, bool) {
	if id == "" {
		id = music.SourceNetease
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

// Catalog returns the catalogue of a source id, as its own source or
// through its provider.
func (r *Registry) Catalog(id string) (Catalog, bool) {
	if id == "" {
		id = music.SourceNetease
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.catalogs[id]
	return c, ok
}

// Enabled returns whether the listener streams from a source: a source
// turned off for its songs is not tried for one. Its catalogue is a switch
// of its own (Searched), so a source kept for searching still lists its
// songs.
func (r *Registry) Enabled(id string) bool {
	return r.Asked(id, CapStream)
}

// Searched returns whether the listener searches a source's catalogue: a
// source whose 曲库 is off is not asked for a search, nor shown on its own
// pages, whatever its 单曲 says.
func (r *Registry) Searched(id string) bool {
	return r.Asked(id, CapLibrary)
}

// Asked reports whether the listener asks a source for a capability. A
// capability a source does not have is never asked of it, whatever the
// switch says.
func (r *Registry) Asked(id string, cap Cap) bool {
	if id == "" {
		id = music.SourceNetease
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.on[id][cap]
}

// Has reports whether a source does a capability at all: whether there is
// anything behind its switch.
func (r *Registry) Has(id string, cap Cap) bool {
	if id == "" {
		id = music.SourceNetease
	}
	r.mu.RLock()
	c, ok := r.catalogs[id]
	r.mu.RUnlock()
	return ok && r.provides(c, cap)
}

// Enable turns streaming from a source on or off, and returns whether the
// id is one the app has. It is the 单曲 switch.
func (r *Registry) Enable(id string, on bool) bool {
	return r.SetAsked(id, CapStream, on)
}

// SetEnabled is Enable, as the pages name it.
func (r *Registry) SetEnabled(id string, on bool) bool { return r.Enable(id, on) }

// SetSearched turns searching a source on or off: the 曲库 switch.
func (r *Registry) SetSearched(id string, on bool) bool {
	return r.SetAsked(id, CapLibrary, on)
}

// SetAsked turns what the listener asks of a source for a capability on or
// off, and returns whether the source has that capability to turn.
func (r *Registry) SetAsked(id string, cap Cap, on bool) bool {
	if id == "" {
		id = music.SourceNetease
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.catalogs[id]
	if !ok || !r.provides(c, cap) {
		return false
	}
	r.on[id][cap] = on
	return true
}

// EnabledSources returns the sources the listener streams from, in their
// order.
func (r *Registry) EnabledSources() []string {
	return r.enabledSources(CapStream)
}

// SearchedSources returns the catalogues the listener searches, in their
// order.
func (r *Registry) SearchedSources() []string {
	return r.enabledSources(CapLibrary)
}

// AskedSources returns the sources the listener asks for a capability, in
// their order: the order they are tried in.
func (r *Registry) AskedSources(cap Cap) []string {
	return r.enabledSources(cap)
}

func (r *Registry) enabledSources(cap Cap) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, id := range r.order {
		if r.on[id][cap] {
			out = append(out, id)
		}
	}
	return out
}

// Sources returns every source the app has, in the listener's order.
func (r *Registry) Sources() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// Name returns what the listener calls a source.
func (r *Registry) Name(id string) string {
	if c, ok := r.Catalog(id); ok {
		return c.Name()
	}
	return id
}

// SearchSongs asks every catalogue the listener searches, in their order,
// and returns the page: each source's songs as it gave them.
//
// It asks the catalogues, not the music sources: whether a service's songs
// play is the chain's business (chain.go), and a listener with no music
// source turned on still searches every catalogue.
//
// A source that fails does not fail the search: the others' answers are
// what the listener gets, as lx-music-desktop gives them.
func (r *Registry) SearchSongs(query string, limit, offset int) (music.SearchResult, error) {
	type answer struct {
		tracks []music.Track
		total  int
		err    error
	}
	ids := r.SearchedSources()
	if len(ids) == 0 {
		return music.SearchResult{}, ErrNoSource
	}
	answers := make([]answer, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		c, _ := r.Catalog(id)
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracks, total, err := c.SearchSongs(query, limit, offset)
			answers[i] = answer{tracks, total, err}
		}()
	}
	wg.Wait()
	var got [][]music.Track
	failed := 0
	for _, a := range answers {
		if a.err != nil {
			failed++
			continue
		}
		if len(a.tracks) > 0 {
			got = append(got, a.tracks)
		}
	}
	if failed == len(ids) {
		return music.SearchResult{}, ErrNoSource
	}
	// The sources' songs take turns, rather than one source's page before
	// another's: a search shows them together, each row saying where it is
	// from, as lx-music-desktop's does.
	var out music.SearchResult
	for _, a := range answers {
		out.Total += a.total
	}
	for i := 0; ; i++ {
		more := false
		for _, tracks := range got {
			if i < len(tracks) {
				out.Songs = append(out.Songs, tracks[i])
				more = true
			}
		}
		if !more {
			break
		}
	}
	return out, nil
}

// SearchPlaylists asks every catalogue the listener searches that has
// playlists, and returns the page.
func (r *Registry) SearchPlaylists(query string, limit, offset int) (music.SearchResult, error) {
	type answer struct {
		lists []music.Playlist
		total int
		err   error
	}
	var ids []string
	for _, id := range r.SearchedSources() {
		if c, ok := r.Catalog(id); ok {
			if _, ok := c.(Searcher); ok {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return music.SearchResult{}, ErrNoSource
	}
	answers := make([]answer, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		c, _ := r.Catalog(id)
		s := c.(Searcher)
		wg.Add(1)
		go func() {
			defer wg.Done()
			lists, total, err := s.SearchPlaylists(query, limit, offset)
			answers[i] = answer{lists, total, err}
		}()
	}
	wg.Wait()
	var out music.SearchResult
	failed := 0
	for _, a := range answers {
		if a.err != nil {
			failed++
			continue
		}
		out.Playlists = append(out.Playlists, a.lists...)
		out.Total += a.total
	}
	if failed == len(ids) {
		return music.SearchResult{}, ErrNoSource
	}
	return out, nil
}
