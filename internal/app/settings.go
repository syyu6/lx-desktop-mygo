package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// settings are the listener's choices, kept in settings.json in the user
// data directory.
type settings struct {
	// Notch shows the quick player around the notch: the listener turns
	// it on.
	Notch bool `json:"notch"`
	// NotchActivity shows the cover and the music's levels beside the
	// notch while music plays, which hides the menu bar there.
	NotchActivity bool `json:"notchActivity"`
	// NotchVirtual keeps a notch of the app's own showing at the top of a
	// display without one; else it shows only while the pointer asks.
	NotchVirtual bool `json:"notchVirtual"`
	// NotchGreeted says the notch showed itself, once, as the first song
	// played (greet).
	NotchGreeted bool `json:"notchGreeted"`
	// Quality is the level of the streams: standard, higher, exhigh,
	// lossless or hires.
	Quality string `json:"quality"`
	// Appearance is light, dark, system to follow the system, or cover to
	// follow the player: light or dark as the system is, with the accent
	// taken from the playing song's cover, so that the app is tinted by the
	// music.
	Appearance string `json:"appearance"`
	// Smoke makes the progress bars comets trailing smoke as music plays,
	// for a few percent of a core: the listener turns it on.
	Smoke bool `json:"smoke"`
	// LyricsStage shows lyrics on the stage, Folia's style, in Now
	// Playing, rather than scrolling.
	LyricsStage bool `json:"lyricsStage"`
	// StageStyle is how the stage sets its lyrics (stageStyles): empty for
	// Folia's classic, or the id of another of its visualizers.
	StageStyle string `json:"stageStyle,omitempty"`
	// StageSparks throws sparks off each word on the stage as it is sung,
	// whatever its style.
	StageSparks bool `json:"stageSparks,omitempty"`
	// Volume is where the volume was left, from 0 to 1.
	Volume float64 `json:"volume"`
	// Normalize evens out the loudness of tracks, by what NetEase
	// measured of each.
	Normalize bool `json:"normalize"`
	// Prefetch fetches the next song while the one playing plays, so that
	// it starts as the last one ends rather than after asking the sources
	// for it: the listener turns it off to save data.
	Prefetch bool `json:"prefetch"`
	// HotComments shows a song's hot comments in its quiet parts, in
	// place of the lyrics (quietComment): the listener turns it on.
	HotComments bool `json:"hotComments"`
	// Fog fills Now Playing's backdrop with fog of the cover's color,
	// rising and swelling with the bass: an experiment the listener turns
	// on.
	Fog bool `json:"fog"`
	// AppIcon is where the app's icon shows on macOS (appIcons); elsewhere
	// it is always in the notification area.
	AppIcon string `json:"appIcon"`
	// Captions writes down episodes as they play, for their captions,
	// once the listener has downloaded the model.
	Captions bool `json:"captions"`
	// CacheMB is how many megabytes the songs kept on disk may take; 0
	// keeps none.
	CacheMB int `json:"cacheMB"`
	// SearchHistory keeps submitted keywords, most recent first.
	SearchHistory []string `json:"searchHistory,omitempty"`
	// ScriptOrder names the imported source scripts, the first to be asked
	// first (lx.go).
	ScriptOrder []string `json:"scriptOrder,omitempty"`
	// SourceOrder names every source the app has, the built-in services and
	// the scripts, in the order the app tries them: the first that has
	// what is asked of it answers (chain.go). The scripts are asked before
	// the built-in ones, whatever this says (chain.go).
	SourceOrder []string `json:"sourceOrder,omitempty"`
	// LibrariesOff names the sources whose catalogue the listener turned
	// off: a source so turned off is not searched, and its own pages are
	// not shown. Its songs still play from it (SourcesOff), as its words
	// are still asked of it (LyricsOff).
	LibrariesOff []string `json:"librariesOff,omitempty"`
	// SourcesOff names the sources whose songs the listener turned off: a
	// source so turned off is not tried to play a song, though its
	// catalogue is still searched (sources.go). A song still plays from one
	// while it is queued.
	SourcesOff []string `json:"sourcesOff,omitempty"`
	// LyricsOff names the sources not asked for a song's words, and
	// CommentsOff the sources not asked for its remarks: each capability
	// has its own switch, so that a source kept for its songs alone is not
	// asked for words.
	LyricsOff   []string `json:"lyricsOff,omitempty"`
	CommentsOff []string `json:"commentsOff,omitempty"`
	// SearchOff named the catalogues the listener did not search, before
	// a source's songs were one switch: it is read once, for those it kept
	// off (migrateSettings).
	SearchOff []string `json:"searchOff,omitempty"`
	// CoverTint is the color the last song played gave the window while the
	// appearance followed it, and CoverTintCover the cover it was taken
	// from. They are kept so that the window opens in that song's colors,
	// rather than in the system's until its cover has loaded.
	CoverTint      string `json:"coverTint,omitempty"`
	CoverTintCover string `json:"coverTintCover,omitempty"`
	// Sidebar is how wide the sidebar shows, which the listener drags its
	// edge to change, SidebarHidden whether it shows at all, and SidebarRail
	// whether it shows folded to a rail of icons.
	Sidebar       float32 `json:"sidebar,omitempty"`
	SidebarHidden bool    `json:"sidebarHidden,omitempty"`
	SidebarRail   bool    `json:"sidebarRail,omitempty"`

	path string // where they were loaded from, and are saved to
}

func defaultSettings() settings {
	return settings{NotchActivity: true, Quality: "exhigh", Appearance: "system", Volume: 0.8, Normalize: true, Prefetch: true, AppIcon: "dock-and-menubar", CacheMB: 2048, Captions: true}
}

// migrate folds the settings an older version kept into the shape this one
// reads: a source's search switch and its music switch were two, and are
// one now, a source turned off for either being off for its songs.
func (s *settings) migrate() {
	if len(s.SearchOff) == 0 {
		return
	}
	off := map[string]bool{}
	for _, id := range s.SourcesOff {
		off[id] = true
	}
	for _, id := range s.SearchOff {
		if !off[id] {
			off[id] = true
			s.SourcesOff = append(s.SourcesOff, id)
		}
	}
	s.SearchOff = nil
}

func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// oldName is what the app was called before it took its bundle's name, and
// kept its data under.
const oldName = "网易云音乐"

// moveOldData moves the user data and the song cache kept under oldName to
// the directories of the app's name, unless those hold something already:
// the login, settings and where playback was left go on. It must run before
// anything reads them.
func moveOldData() {
	for _, name := range []mygo.PathName{mygo.PathUserData, mygo.PathCache} {
		dir, err := mygo.App.Path(name)
		if err != nil {
			continue
		}
		moveDir(filepath.Join(filepath.Dir(dir), oldName), dir)
	}
}

// moveDir moves the directory old to dir when dir is empty, as Path made
// it, and old exists.
func moveDir(old, dir string) {
	if old == dir {
		return
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	if os.Remove(dir) == nil {
		if err := os.Rename(old, dir); err != nil {
			os.MkdirAll(dir, 0o700)
		}
	}
}

// loadSettings reads the settings, or returns the defaults.
func loadSettings() settings {
	s := defaultSettings()
	if path, err := settingsPath(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			json.Unmarshal(b, &s)
			s.migrate()
		}
		s.path = path
	}
	return s
}

// save keeps the settings where they were loaded from; defaults that were
// never loaded, as in tests, are not saved.
func (s settings) save() error {
	path := s.path
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// applyAppearance makes the windows light, dark, or follow the system, and,
// following the player, tints them by what plays.
func (s settings) applyAppearance() {
	switch s.Appearance {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	case "cover":
		// The accents come from the cover, which the frames draw; the window
		// itself follows the system, as its bars and menus are not the app's.
		mygo.Theme.SetSource(mygo.ThemeSystem)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// qualityLevel is the stream level NetEase names a quality by.
var qualityLevels = []struct{ level, label string }{
	{"standard", "标准（128k）"},
	{"higher", "较高（192k）"},
	{"exhigh", "极高（320k）"},
	{"lossless", "无损（FLAC）"},
	{"hires", "Hi-Res"},
}

// cacheLimits are the sizes the song cache may take, in megabytes.
var cacheLimits = []struct {
	mb    int
	label string
}{
	{0, "关闭"},
	{1024, "1 GB"},
	{2048, "2 GB"},
	{5120, "5 GB"},
	{10240, "10 GB"},
}
