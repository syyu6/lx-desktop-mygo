package app

import (
	"slices"
	"strings"

	"netease-music-mygo/internal/kugou"
	"netease-music-mygo/internal/kuwo"
	"netease-music-mygo/internal/lxsource"
	"netease-music-mygo/internal/migu"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/qq"
	"netease-music-mygo/internal/source"
)

// The sources the app browses and plays. Two kinds of them, as
// lx-music-desktop has them: the catalogues it searches (搜索源) and the
// music sources it streams from (音乐源).
//
// NetEase, Kuwo, QQ Music and Migu are built in as both: their catalogues
// are searched, and their services stream, each as far as the service lets
// an app without an account stream — a song it will not serve plays from
// whichever source will, as any song does. Kugou is searched alone: it
// streams through an endpoint that asks for a signature of the song and
// the listener's account, so its songs play through a script the listener
// imported for them, and its words are fetched here.
//
// The listener may turn each on or off, and the app goes on with the rest:
// turning the music sources off leaves the search working, and turning the
// search sources off leaves playback working. A song of a source turned off
// keeps playing from it while it is queued.
//
// The scripts the listener imported (lx.go) are music sources: they answer
// where no built-in service will stream a song. Which of them is asked, and
// in what order, is the chain (chain.go).

// loadRegistry builds the sources, and hands the player the chain to try
// them in.
func (a *app) loadRegistry() {
	a.reg = source.NewRegistry(netease.Provider{}, kuwo.Provider{}, kugou.Catalog{}, qq.Provider{}, migu.Provider{})
	a.applySources()
	a.chain = source.NewChain()
	a.rebuildChain()
	a.pl.Resolve = func(t netease.Track, level string) (*netease.Stream, string, error) {
		res, err := a.chain.Resolve(t, level)
		if err != nil {
			return nil, "", err
		}
		return res.Stream, res.Served, nil
	}
	// The player asks again with the link whose address did not open left
	// out: a script whose upstream is down answers with a URL that is a
	// 404, and the next source that has the song takes its turn.
	a.pl.ResolveExcept = func(t netease.Track, level, skip string) (*netease.Stream, string, string, error) {
		res, err := a.chain.ResolveExcept(t, level, skip)
		if err != nil {
			return nil, "", "", err
		}
		return res.Stream, res.Served, res.Link, nil
	}
	a.pl.ResolveLyrics = func(t netease.Track) (string, string, bool) {
		return a.chain.ResolveLyrics(t)
	}
	// Once the song's own source has answered with no words, the others
	// are asked: a NetEase instrumental often has words on QQ Music.
	a.pl.ResolveLyricsElsewhere = func(t netease.Track) (string, string, bool) {
		return a.chain.ResolveLyricsElsewhere(t)
	}
	a.pl.ResolveCover = func(t netease.Track) (string, bool) {
		return a.chain.ResolveCover(t)
	}
}

// applySources gives the registry the sources and what is asked of them,
// as the settings say.
func (a *app) applySources() {
	if a.reg == nil {
		return
	}
	for _, id := range a.reg.Sources() {
		a.reg.SetAsked(id, source.CapLibrary, !slices.Contains(a.settings.LibrariesOff, id))
		a.reg.SetAsked(id, source.CapStream, !slices.Contains(a.settings.SourcesOff, id))
		a.reg.SetAsked(id, source.CapLyrics, !slices.Contains(a.settings.LyricsOff, id))
		a.reg.SetAsked(id, source.CapComments, !slices.Contains(a.settings.CommentsOff, id))
	}
}

// sourceOn reports whether the listener streams from a music source:
// NetEase is streamed from unless turned off, and so is a service.
func (a *app) sourceOn(id string) bool {
	if id == "" {
		id = music.SourceNetease
	}
	if a.reg == nil {
		return id == music.SourceNetease
	}
	return a.reg.Enabled(id)
}

// searchesOn reports whether the listener searches a search source's
// catalogue: NetEase's is searched unless turned off, and so is a service's.
func (a *app) searchesOn(id string) bool {
	if id == "" {
		id = music.SourceNetease
	}
	if a.reg == nil {
		return id == music.SourceNetease
	}
	return a.reg.Searched(id)
}

// askedOn reports whether the listener asks a source for a capability, the
// suggestions and the lyrics among them: what a source's switches say.
func (a *app) askedOn(id string, cap source.Cap) bool {
	if a.reg == nil {
		return id == "" || id == music.SourceNetease
	}
	return a.reg.Asked(id, cap)
}

// sourceName is what the listener calls a source, for the pages to say
// where a song or a playlist is from.
func (a *app) sourceName(id string) string {
	if id == "" || id == music.SourceNetease {
		return ""
	}
	if a.reg != nil {
		if c, ok := a.reg.Catalog(id); ok {
			return c.Name()
		}
	}
	for _, info := range a.sources {
		if info.ID == id {
			return info.Name
		}
	}
	return id
}

// sourceTag is what a search result's badge says: the source's name, and
// NetEase's too, so that the listener knows what each row is from.
func (a *app) sourceTag(id string) string {
	if name := a.sourceName(id); name != "" && name != id {
		return shortSource(name)
	}
	if id == "" || id == music.SourceNetease {
		return "网易云"
	}
	if a.reg != nil {
		if p, ok := a.reg.Get(id); ok {
			return shortSource(p.Name())
		}
	}
	return id
}

// shortSource is a source's name as a badge says it: the service's own
// short name, without the 音乐 most of them end in.
func shortSource(name string) string {
	name = strings.TrimSuffix(name, "音乐")
	return strings.TrimSpace(name)
}

// sourceTrackOf is a music.Track as a source script is told about it: a
// script knows a song by its id on its own service, which is the one the
// track carries.
func sourceTrackOf(t music.Track) lxsource.Track {
	src := lxsource.SourceNetease
	if s, ok := lxSourceOf(t.Src()); ok {
		src = s
	}
	return lxsource.Track{
		Source:   src,
		ID:       t.SongKey(),
		Name:     t.Name,
		Singer:   t.ArtistNames(),
		Album:    t.Album.Name,
		Interval: formatInterval(t.Duration),
		PicURL:   netease.Sized(t.Cover(), 300),
	}
}
