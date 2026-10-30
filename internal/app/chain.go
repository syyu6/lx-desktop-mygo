package app

import (
	"strings"

	"netease-music-mygo/internal/lxsource"
	"netease-music-mygo/internal/music"
	"netease-music-mygo/internal/source"
)

// The chain is the order the app tries the sources to play a song in: the
// services it ships, NetEase among them, and the scripts the listener
// imported, mixed in the order the settings keep them (package source's
// Chain). A song its own source will not stream, as a VIP one, is looked up
// on the others and plays from the first that has it.

// scriptLink is an imported script as the chain asks it: a script knows a
// song by its id on its own service, so a track of another source is
// described to it as one of its own.
type scriptLink struct {
	id   string
	name string
	src  *lxsource.Resolver
}

func (s scriptLink) Name() string { return s.name }

// Supports reports whether the script answers for a source with a stream,
// starting it to find out: it is asked only when its turn comes.
func (s scriptLink) Supports(source string) bool {
	ls, ok := lxSourceOf(source)
	if !ok {
		return false
	}
	return s.src.Supports(s.id, ls, lxsource.ActionMusicURL)
}

func (s scriptLink) Stream(source string, t music.Track, level string) (string, error) {
	return s.src.StreamWith(s.id, sourceTrackOf(t), lxsource.QualityOfLevel(level))
}

func (s scriptLink) Lyrics(source string, t music.Track) (lrc, tlyric string, ok bool) {
	l, err := s.src.LyricsWith(s.id, sourceTrackOf(t))
	if err != nil || strings.TrimSpace(l.Lyric) == "" {
		return "", "", false
	}
	return l.Lyric, l.Tlyric, true
}

func (s scriptLink) Pic(source string, t music.Track) (string, bool) {
	url, err := s.src.PicWith(s.id, sourceTrackOf(t))
	return url, err == nil && url != ""
}

// lxSourceOf is the source a script names a service by, from the app's id
// of it.
func lxSourceOf(id string) (lxsource.Source, bool) {
	switch id {
	case music.SourceNetease:
		return lxsource.SourceNetease, true
	case music.SourceKuwo:
		return lxsource.SourceKuwo, true
	case music.SourceKugou:
		return lxsource.SourceKugou, true
	case music.SourceQQ:
		return lxsource.SourceTencent, true
	case music.SourceMigu:
		return lxsource.SourceMigu, true
	}
	return "", false
}

// sourceOrder is the built-in services the app has, in the order the
// listener put them: the order they are searched in, asked for words in,
// and played from. A name the app no longer has is left out, and a service
// the order does not name goes after those it does, in the app's own
// order.
func (a *app) sourceOrder() []string {
	if a.reg == nil {
		return nil
	}
	var all []string
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			if _, ok := a.reg.Catalog(id); ok {
				seen[id] = true
				all = append(all, id)
			}
		}
	}
	for _, id := range a.settings.SourceOrder {
		add(id)
	}
	for _, id := range a.reg.Sources() {
		add(id)
	}
	return all
}

// hasSource reports whether the app has a source or a script by that id.
func (a *app) hasSource(id string) bool {
	if a.reg != nil {
		if _, ok := a.reg.Get(id); ok {
			return true
		}
	}
	_, ok := a.sourceByID(id)
	return ok
}

// rebuildChain gives the player the links to try a song on. The scripts the
// listener imported come first, in their own order: they are the sources
// that serve what the built-in ones will not (a VIP song, a service's
// region), so asking them first is what makes them worth importing. The
// built-in services follow, in the listener's order (sourceOrder).
//
// The catalogues are the services asked for a song's words: the source of
// a song the app does not stream from knows the words of its own songs,
// and every source the listener asks for lyrics is asked for them.
func (a *app) rebuildChain() {
	var links, catalogs []source.Link
	// The scripts, in their own order, and only those turned on.
	if a.src != nil {
		for _, id := range a.scriptOrder() {
			info, ok := a.sourceByID(id)
			if !ok || !info.Enabled {
				continue
			}
			links = append(links, source.Link{ID: id, Name: info.Name, Script: scriptLink{id: id, name: info.Name, src: a.src}})
		}
	}
	if a.reg != nil {
		for _, id := range a.sourceOrder() {
			p, isProvider := a.reg.Get(id)
			c, isCatalog := a.reg.Catalog(id)
			if !isCatalog {
				continue
			}
			// The sources the app streams from itself: its 单曲 switch,
			// which is apart from its 曲库 — a source kept for searching
			// plays nothing, and one kept for playing is still not asked
			// for a search.
			if isProvider && a.reg.Asked(id, source.CapStream) {
				links = append(links, source.Link{ID: id, Name: p.Name(), Provider: p})
			}
			// The catalogues asked for words, in the listener's order: a
			// source kept for its lyrics alone is exactly that, and one
			// whose lyrics switch is off is not asked at all. Its 曲库 or
			// 单曲 says nothing here.
			if a.reg.Has(id, source.CapLyrics) && a.reg.Asked(id, source.CapLyrics) {
				catalogs = append(catalogs, source.Link{ID: id, Name: c.Name(), Catalog: c})
			}
		}
	}
	if a.chain != nil {
		a.chain.Set(links)
		a.chain.SetCatalogs(catalogs)
	}
	// The resolver asks its scripts in the order the chain has them, for
	// what is asked of a script without naming it.
	var ask []string
	for _, l := range links {
		if l.Script != nil {
			ask = append(ask, l.ID)
		}
	}
	a.setScriptOrder(ask)
}

// scriptOrder returns the scripts the listener has, in the order they are
// asked: the settings' order, with any script they do not name after the
// rest, as it was imported.
func (a *app) scriptOrder() []string {
	known := map[string]bool{}
	for _, info := range a.sources {
		known[info.ID] = true
	}
	var out []string
	for _, id := range a.settings.ScriptOrder {
		if known[id] {
			out = append(out, id)
			delete(known, id)
		}
	}
	for _, info := range a.sources {
		if known[info.ID] {
			out = append(out, info.ID)
		}
	}
	return out
}

// moveSource puts a built-in service a place up or down the order, and
// keeps the order: what the settings' arrows do, for the order the app
// searches in, asks for words in, and plays from.
func (a *app) moveSource(id string, up bool) {
	order := a.sourceOrder()
	at := -1
	for i, s := range order {
		if s == id {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	to := at - 1
	if !up {
		to = at + 1
	}
	if to < 0 || to >= len(order) {
		return
	}
	order[at], order[to] = order[to], order[at]
	a.settings.SourceOrder = order
	a.rebuildChain()
	a.settings.save()
	a.redraw()
}

// moveScript puts an imported script a place up or down the order, and
// keeps it: the scripts are asked in their own order, before the services,
// and their arrows move them among themselves.
func (a *app) moveScript(id string, up bool) {
	order := a.scriptOrder()
	at := -1
	for i, s := range order {
		if s == id {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	to := at - 1
	if !up {
		to = at + 1
	}
	if to < 0 || to >= len(order) {
		return
	}
	order[at], order[to] = order[to], order[at]
	a.settings.ScriptOrder = order
	a.rebuildChain()
	a.settings.save()
	a.redraw()
}
