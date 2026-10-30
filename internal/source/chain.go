package source

import (
	"errors"
	"strings"
	"sync"
	"unicode"

	"netease-music-mygo/internal/music"
)

// A Chain is the listener's order of sources to play a song from: the
// services the app ships and the scripts it imported, each a link of the
// chain, in the order the settings keep them. It is lx-music-desktop's
// 音源顺序.
//
// A song is tried on its own source first, by the links that answer for
// that source, in order. Failing that, the chain looks for the song on the
// other sources the app can search, and tries what it finds, so that a
// song its own source will not stream, as a VIP one, plays from a source
// that will. Every link that fails gives way to the next, until none has
// the song.

// Script is a source script the listener imported, as the chain asks it.
type Script interface {
	// Name is what the listener calls it.
	Name() string
	// Supports reports whether it answers for a source with a stream. It
	// may start the script to find out, which takes a moment: it is asked
	// only when the script's turn comes.
	Supports(source string) bool
	// Stream returns where t streams from on source, at level.
	Stream(source string, t music.Track, level string) (string, error)
	// Lyrics returns t's words on source and their translation, and
	// whether it has them.
	Lyrics(source string, t music.Track) (lrc, tlyric string, ok bool)
	// Pic returns t's cover on source, and whether it names one.
	Pic(source string, t music.Track) (url string, ok bool)
}

// Link is one entry of the chain: a source the app ships, or a script.
type Link struct {
	// ID names it among the links: the source's id for one the app ships,
	// or the script's id.
	ID   string
	Name string
	// Provider is the source the app ships, or nil for a script.
	Provider Provider
	// Catalog is a catalogue the app searches but does not stream from,
	// which may still have a song's words: Kugou, QQ Music and Migu are
	// searched, and play through whoever can, but they know the words of
	// their own songs.
	Catalog Catalog
	// Script is the imported script, or nil for a source the app ships.
	Script Script
}

// Lyricist is a catalogue that knows its songs' words: what a source the
// app does not stream from may still give, as Kugou, QQ Music and Migu do.
type Lyricist interface {
	Lyrics(t music.Track) (music.Lyrics, error)
}

// serves reports whether the link answers for a source with a stream.
func (l Link) serves(src string) bool {
	switch {
	case l.Provider != nil:
		return l.Provider.ID() == src
	case l.Script != nil:
		return l.Script.Supports(src)
	}
	return false
}

// searches reports whether the link can look a song up by name: the
// sources the app ships can, each with its own catalogue, and a script
// cannot, knowing only the songs of its own source's numbering.
func (l Link) searches() bool { return l.Provider != nil }

// Chain is the links in the listener's order.
//
// It is read as songs load, on the loading goroutine, and set on the main
// thread as the settings change: what changes is behind the lock.
type Chain struct {
	mu    sync.RWMutex
	links []Link
	// catalogs are the catalogues the app searches but does not stream
	// from, in the listener's order: no link of their own, they are asked
	// for what a song of theirs knows and nobody else does, its words.
	catalogs []Link
	// found remembers where a song was found on another source, by its
	// identity: a song played again is not looked up again, as lx-music-
	// desktop keeps what it found.
	found map[string]music.Track
}

// NewChain returns a chain of links, in the order given.
func NewChain(links ...Link) *Chain {
	return &Chain{links: links, found: map[string]music.Track{}}
}

// Set puts the links in the chain, in the listener's order: what the
// settings ask for, the sources turned off left out.
func (c *Chain) Set(links []Link) {
	c.mu.Lock()
	c.links = append([]Link(nil), links...)
	c.mu.Unlock()
}

// SetCatalogs puts the catalogues the app searches but does not stream
// from in the chain, in the listener's order: they are asked only for a
// song's words, which a catalogue alone may know.
func (c *Chain) SetCatalogs(catalogs []Link) {
	c.mu.Lock()
	c.catalogs = append([]Link(nil), catalogs...)
	c.mu.Unlock()
}

// Links returns the links as the chain has them.
func (c *Chain) Links() []Link {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Link(nil), c.links...)
}

// Order returns the ids of the links, in the order they are tried.
func (c *Chain) Order() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, len(c.links))
	for i, l := range c.links {
		out[i] = l.ID
	}
	return out
}

// ErrNotPlayable is returned when no source has a song to stream: what
// the player shows the listener, whose sources were all tried.
var ErrNotPlayable = errors.New("没有来源能播放这首歌")

// Resolution is where a song streams from, and the source that served it.
type Resolution struct {
	Stream *music.Stream
	// Served is the source id of the link that served it: the song's own,
	// or the one the chain found it on.
	Served string
	// Link is the id of the link that served it: the source the app ships,
	// or the script. A stream that does not play is asked for again with
	// this link left out, so that the next one gets its turn.
	Link string
	// Track is the song as that source has it, which may not be the one
	// asked for: another service's recording of it.
	Track music.Track
}

// Resolve returns where t streams from at level, or the reason it does
// not. It is asked as a song loads, off the main thread.
//
// The song's own source is tried first. Failing that, the chain looks the
// song up on the other sources that can search, in the listener's order,
// and streams the first match that plays.
func (c *Chain) Resolve(t music.Track, level string) (Resolution, error) {
	return c.ResolveExcept(t, level, "")
}

// ResolveExcept is Resolve, leaving out a link: the one whose stream the
// player could not open. A source may name a song's address and then fail
// to serve it — an imported script whose upstream is down answers with a
// URL that is a 404 — and the next link that has the song must get its
// turn rather than the song failing outright.
//
// skip is the id of a link to leave out, or "" for every link.
func (c *Chain) ResolveExcept(t music.Track, level, skip string) (Resolution, error) {
	links := c.linksNow()
	if len(links) == 0 {
		return Resolution{}, ErrNoSource
	}
	if skip != "" {
		links = withoutLink(links, skip)
	}
	res, err := c.resolve(links, t, level)
	if err != nil {
		return Resolution{}, err
	}
	if res.Served != t.Src() {
		c.remember(t, res.Track)
	}
	return res, nil
}

// withoutLink returns links without the one skip names: the link whose
// stream did not open.
func withoutLink(links []Link, skip string) []Link {
	out := make([]Link, 0, len(links))
	for _, l := range links {
		if l.ID != skip {
			out = append(out, l)
		}
	}
	return out
}

// resolve walks the links for t: its own source first, then the others'.
func (c *Chain) resolve(links []Link, t music.Track, level string) (Resolution, error) {
	if res, ok := tryOwn(links, t, level); ok {
		return res, nil
	}
	own := t.Src()
	// What it was found on before is tried before the sources are searched
	// again: a song played twice is looked up once.
	if found, ok := c.recall(t); ok && found.Src() != own {
		if res, ok := tryOwn(links, found, level); ok {
			return res, nil
		}
	}
	for _, l := range links {
		if !l.searches() || l.Provider.ID() == own {
			continue
		}
		found, ok := findOn(l.Provider, t)
		if !ok {
			continue
		}
		// Found there: stream it from there, by whichever link serves that
		// source first, which may be a script of the listener's.
		if res, ok := tryOwn(links, found, level); ok {
			return res, nil
		}
	}
	return Resolution{}, ErrNotPlayable
}

// remember keeps where a song was found on another source.
func (c *Chain) remember(t, found music.Track) {
	c.mu.Lock()
	key := t.Ident()
	if len(c.found) > 500 {
		// A queue's worth is plenty: the map is emptied rather than grown
		// without bound.
		clear(c.found)
	}
	c.found[key] = found
	c.mu.Unlock()
}

// recall returns where a song was found on another source before.
func (c *Chain) recall(t music.Track) (music.Track, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	f, ok := c.found[t.Ident()]
	return f, ok
}

// tryOwn asks the links that serve t's own source, in order, until one
// has a URL.
func tryOwn(links []Link, t music.Track, level string) (Resolution, bool) {
	src := t.Src()
	for _, l := range links {
		if !l.serves(src) {
			continue
		}
		if st, ok := streamFrom(l, t, level); ok {
			return Resolution{Stream: st, Served: src, Link: l.ID, Track: t}, true
		}
	}
	return Resolution{}, false
}

// streamFrom asks one link where t streams from, on the source it serves.
func streamFrom(l Link, t music.Track, level string) (*music.Stream, bool) {
	if l.Provider != nil {
		st, err := l.Provider.Stream(t, level)
		if err != nil || st == nil || st.URL == "" {
			return nil, false
		}
		return st, true
	}
	url, err := l.Script.Stream(t.Src(), t, level)
	if err != nil || url == "" {
		return nil, false
	}
	// What a script gives is not known until its first bytes come: the
	// player reads the format and the length for itself.
	return &music.Stream{URL: url, Level: level, Gain: 1}, true
}

// ResolveLyrics returns t's words and their translation, asking the
// sources for its own first and then the others, in the listener's order.
//
// Its own source is asked first, and its script. Failing that, the other
// sources are asked for the words of the same song as they have it: a
// song NetEase has no words for, an instrumental to it or a song it no
// longer keeps, has them on QQ Music or Kuwo often enough, and a song of
// another source that source has none of has NetEase's. Finding the song
// is a search, so it happens only where the first answers are empty.
func (c *Chain) ResolveLyrics(t music.Track) (lrc, tlyric string, ok bool) {
	// Its own source's words, from whoever of its own source answers: the
	// service itself, a catalogue's own lyricist, or a script.
	if lrc, tlyric, ok := c.ownLyrics(t); ok {
		return lrc, tlyric, true
	}
	return c.ResolveLyricsElsewhere(t)
}

// ResolveLyricsElsewhere returns t's words from the sources other than its
// own: what the player asks once its own source has answered with none,
// rather than asking it the same thing twice.
func (c *Chain) ResolveLyricsElsewhere(t music.Track) (lrc, tlyric string, ok bool) {
	src := t.Src()
	// The others', in the listener's order, each asked for the song as it
	// has it.
	for _, l := range c.lyricistsNow() {
		if l.id == src {
			continue
		}
		found, ok := c.lookUp(l, t)
		if !ok {
			continue
		}
		if lrc, tlyric, ok := c.lyricsFrom(l, found); ok {
			return lrc, tlyric, true
		}
	}
	return "", "", false
}

// ownLyrics returns t's words from its own source: the service itself, a
// catalogue that knows them, or a script that serves it.
func (c *Chain) ownLyrics(t music.Track) (lrc, tlyric string, ok bool) {
	src := t.Src()
	for _, l := range c.linksNow() {
		if l.Provider != nil && l.Provider.ID() == src {
			if ly, err := l.Provider.Lyrics(t); err == nil && strings.TrimSpace(ly.Lyric) != "" {
				return ly.Lyric, ly.Tlyric, true
			}
		}
		if l.Script != nil && l.serves(src) {
			if lrc, tlyric, ok := l.Script.Lyrics(src, t); ok && strings.TrimSpace(lrc) != "" {
				return lrc, tlyric, true
			}
		}
	}
	// A catalogue the app does not stream from still knows the words of
	// its own songs: Kugou's, whose songs play elsewhere.
	for _, l := range c.catalogsNow() {
		if l.Catalog.ID() != src {
			continue
		}
		if lrc, tlyric, ok := catalogueLyrics(l.Catalog, t); ok {
			return lrc, tlyric, true
		}
	}
	return "", "", false
}

// lyricist is a source that may have a song's words: one whose catalogue
// the listener asks for lyrics, and where to find the song on it.
type lyricist struct {
	id      string
	catalog Catalog
}

// lyricistsNow returns the sources a song's words may be found on, in the
// listener's order: the catalogues asked for them, which the settings list
// in that order.
func (c *Chain) lyricistsNow() []lyricist {
	var out []lyricist
	seen := map[string]bool{}
	for _, l := range c.catalogsNow() {
		if l.Catalog == nil || seen[l.Catalog.ID()] {
			continue
		}
		seen[l.Catalog.ID()] = true
		out = append(out, lyricist{id: l.Catalog.ID(), catalog: l.Catalog})
	}
	return out
}

// lyricsFrom returns the words lyricist l has for t.
func (c *Chain) lyricsFrom(l lyricist, t music.Track) (lrc, tlyric string, ok bool) {
	if lrc, tlyric, ok := catalogueLyrics(l.catalog, t); ok {
		return lrc, tlyric, true
	}
	// A script of the same source may have what the service does not.
	for _, link := range c.linksNow() {
		if link.Script != nil && link.serves(l.id) {
			if lrc, tlyric, ok := link.Script.Lyrics(l.id, t); ok && strings.TrimSpace(lrc) != "" {
				return lrc, tlyric, true
			}
		}
	}
	return "", "", false
}

// catalogueLyrics returns the words a catalogue has for t, where it knows
// them.
func catalogueLyrics(c Catalog, t music.Track) (lrc, tlyric string, ok bool) {
	if ly, has := c.(Lyricist); has {
		if words, err := ly.Lyrics(t); err == nil && strings.TrimSpace(words.Lyric) != "" {
			return words.Lyric, words.Tlyric, true
		}
	}
	if p, has := c.(Provider); has {
		if words, err := p.Lyrics(t); err == nil && strings.TrimSpace(words.Lyric) != "" {
			return words.Lyric, words.Tlyric, true
		}
	}
	return "", "", false
}

// lookUp finds t on the source l, by its name and artists: what the source
// has of the song, numbered its own way.
func (c *Chain) lookUp(l lyricist, t music.Track) (music.Track, bool) {
	if t.Src() == l.id {
		return t, true
	}
	if p, ok := l.catalog.(Provider); ok {
		return findOn(p, t)
	}
	return music.Track{}, false
}

// FindOnNetease looks t up on the sources the listener has for its songs,
// and returns NetEase's record of it: what a song of another source is
// shown with, its comments among them. It returns false for a song NetEase
// does not have.
func (c *Chain) FindOnNetease(t music.Track) (music.Track, bool) {
	if t.Src() == music.SourceNetease {
		return t, true
	}
	for _, l := range c.linksNow() {
		if l.Provider != nil && l.Provider.ID() == music.SourceNetease {
			return findOn(l.Provider, t)
		}
	}
	for _, l := range c.catalogsNow() {
		if l.Catalog.ID() != music.SourceNetease {
			continue
		}
		if p, ok := l.Catalog.(Provider); ok {
			return findOn(p, t)
		}
	}
	return music.Track{}, false
}

// ResolveCover returns t's cover, from the first link that names one.
func (c *Chain) ResolveCover(t music.Track) (string, bool) {
	src := t.Src()
	for _, l := range c.linksNow() {
		if l.Provider == nil || l.Provider.ID() != src {
			continue
		}
		if cv, ok := l.Provider.(Coverer); ok {
			if url, err := cv.Pic(t); err == nil && url != "" {
				return url, true
			}
		}
	}
	for _, l := range c.linksNow() {
		if l.Script != nil && l.serves(src) {
			if url, ok := l.Script.Pic(src, t); ok && url != "" {
				return url, true
			}
		}
	}
	return "", false
}

// linksNow returns the links, or none.
func (c *Chain) linksNow() []Link {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.links
}

// catalogsNow returns the catalogues the app searches but does not stream
// from, or none.
func (c *Chain) catalogsNow() []Link {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.catalogs
}

// findOn looks t up on p by its name and artists, and returns the best
// match the source has, as lx-music-desktop finds one: the same song, by
// another service's numbering.
func findOn(p Provider, t music.Track) (music.Track, bool) {
	want := strings.TrimSpace(t.Name + " " + t.ArtistNames())
	if want == "" {
		return music.Track{}, false
	}
	found, _, err := p.SearchSongs(want, 20, 0)
	if err != nil {
		return music.Track{}, false
	}
	return bestMatch(t, found)
}

// bestMatch picks the track among found that is t: the first whose name
// and artists agree, preferring one of about t's length, and else one of
// its name alone, as a cover or a remaster would be.
func bestMatch(t music.Track, found []music.Track) (music.Track, bool) {
	name := normName(t.Name)
	if name == "" {
		return music.Track{}, false
	}
	var loose *music.Track
	for i := range found {
		f := &found[i]
		if normName(f.Name) != name {
			continue
		}
		if near(f.Duration, t.Duration) && sharesArtist(&t, f) {
			return *f, true
		}
		if loose == nil {
			loose = f
		}
	}
	if loose != nil {
		return *loose, true
	}
	return music.Track{}, false
}

// near reports whether two lengths are of the same recording: within five
// seconds, as lx-music-desktop allows, or as near as one says nothing.
func near(a, b int64) bool {
	if a <= 0 || b <= 0 {
		return true
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= 5000
}

// sharesArtist reports whether the two tracks name an artist alike.
func sharesArtist(a, b *music.Track) bool {
	if len(a.Artists) == 0 || len(b.Artists) == 0 {
		// One of them names none: the name alone has to do.
		return true
	}
	for _, x := range a.Artists {
		n := normName(x.Name)
		if n == "" {
			continue
		}
		for _, y := range b.Artists {
			if normName(y.Name) == n {
				return true
			}
		}
	}
	return false
}

// normName is a name as two services' spellings of it compare: in lower
// case, without the punctuation and the spacing that differ between them,
// and without the brackets a version is named in.
func normName(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '(' || r == '（' || r == '[' || r == '【':
			depth++
		case r == ')' || r == '）' || r == ']' || r == '】':
			if depth > 0 {
				depth--
			}
		case depth > 0:
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
