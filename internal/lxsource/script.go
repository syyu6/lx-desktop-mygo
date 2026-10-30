// Package lxsource runs the music sources people import, in the format of
// lx-music-desktop's user API: a script that is handed a `lx` object and
// answers requests for a song's stream, its lyrics and its cover.
//
// The scripts come from the internet and are run in QuickJS, the C library
// compiled to pure Go, so no cgo: it builds wherever the app does, and a
// script reaches nothing of the computer but the requests this package
// makes for it.
package lxsource

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// A Source is a music service a script can answer for.
type Source string

const (
	SourceKuwo     Source = "kw"
	SourceKugou    Source = "kg"
	SourceTencent  Source = "tx"
	SourceNetease  Source = "wy"
	SourceMigu     Source = "mg"
	SourceXimalaya Source = "xm"
)

// Sources are the services a script may answer for, as lx-music-desktop
// names them, which is also where a track's ID comes from: a script knows
// a song by its own ID on its own service.
var Sources = []Source{SourceKuwo, SourceKugou, SourceTencent, SourceNetease, SourceMigu, SourceXimalaya}

// Name is the service's name, as the settings show it.
func (s Source) Name() string {
	switch s {
	case SourceKuwo:
		return "酷我音乐"
	case SourceKugou:
		return "酷狗音乐"
	case SourceTencent:
		return "QQ 音乐"
	case SourceNetease:
		return "网易云音乐"
	case SourceMigu:
		return "咪咕音乐"
	case SourceXimalaya:
		return "喜马拉雅"
	}
	return string(s)
}

// Quality is a stream quality a script may serve, as lx-music-desktop
// names it.
type Quality string

const (
	Quality128k      Quality = "128k"
	Quality320k      Quality = "320k"
	QualityFlac      Quality = "flac"
	QualityFlac24bit Quality = "flac24bit"
	QualityHires     Quality = "hires"
	QualityAtmos     Quality = "atmos"
	QualityMaster    Quality = "master"
)

// Qualities are the qualities lx-music-desktop accepts, from the lowest.
var Qualities = []Quality{Quality128k, Quality320k, QualityFlac, QualityFlac24bit}

// QualityOfLevel maps the app's stream levels to the qualities scripts
// serve: the levels above 320k ask for the best a script has.
func QualityOfLevel(level string) Quality {
	switch level {
	case "standard":
		return Quality128k
	case "higher":
		return Quality320k
	case "exhigh":
		return Quality320k
	case "lossless":
		return QualityFlac
	case "hires":
		return QualityFlac24bit
	}
	return Quality320k
}

// An Action is something a script does for a source.
type Action string

const (
	ActionMusicURL Action = "musicUrl"
	ActionLyric    Action = "lyric"
	ActionPic      Action = "pic"
)

// Actions are the actions a script may offer, and this package runs.
// A script may declare others, such as a search of its own; nothing calls
// them, so they are dropped as the script is read.
var Actions = []Action{ActionMusicURL, ActionLyric, ActionPic}

// Capability is what a script does for one source.
type Capability struct {
	Actions   []Action
	Qualities []Quality
}

// Has reports whether the script does action for this source.
func (c Capability) Has(a Action) bool {
	for _, x := range c.Actions {
		if x == a {
			return true
		}
	}
	return false
}

// Info is a script as it was read from its header: what the settings
// list, without the code.
type Info struct {
	// ID names the script; it is made as the script is imported.
	ID string `json:"id"`
	// Name, Description, Author, Homepage and Version are the script's
	// own words about itself, from its header comment.
	Name        string `json:"name"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Homepage    string `json:"homepage"`
	Version     string `json:"version"`
	// URL is where the script was fetched from, when it was imported
	// from the internet rather than a file.
	URL string `json:"url,omitempty"`
	// Imported is when it was imported.
	Imported time.Time `json:"imported"`
	// Enabled reports whether the app asks this script for streams.
	Enabled bool `json:"enabled"`
	// Platforms are the services the script plays, as its runtime said the
	// last time it was asked: what the settings tag it with, kept so that
	// showing the list starts no runtime. It is empty until the script has
	// been asked, and an update clears it, since the new code may play
	// others.
	Platforms []Source `json:"platforms,omitempty"`
}

var (
	// ErrNoHeader is returned for a file that does not begin with the
	// comment a source script must.
	ErrNoHeader = errors.New("不是落雪音源脚本：文件开头没有 /*! ... */ 说明")
	// ErrTooLarge is returned for a script larger than the app runs.
	ErrTooLarge = errors.New("脚本太大")
)

// maxScript is the largest script the app imports. The biggest source
// published is a few hundred kilobytes.
const maxScript = 2 << 20

// The limits the app applies to what a script answers, as
// lx-music-desktop does: a URL no longer than this is a stream, not a
// page of markup or a data: URI.
const (
	maxURL     = 2048
	maxLyric   = 51200
	maxTrans   = 5120
	maxLXLyric = 8192
	maxPicURL  = 2048
)

// header is the comment a script opens with, which names it.
var header = regexp.MustCompile(`(?s)^/\*[*!][\s\S]+?\*/`)

// field is one @name value of the header.
var field = regexp.MustCompile(`(?m)^\s?\*\s?@(\w+)\s+(.+?)\s*$`)

// The header's fields the app reads, and how long each may be.
var headerFields = map[string]int{
	"name":        24,
	"description": 36,
	"author":      56,
	"homepage":    1024,
	"version":     36,
}

// ParseScript reads a script's header for what it says about itself. It
// does not run the script: what it answers for is known once it has run,
// which takes a runtime (see Engine.Load).
func ParseScript(script string) (Info, error) {
	if len(script) > maxScript {
		return Info{}, ErrTooLarge
	}
	h := header.FindString(script)
	if h == "" {
		return Info{}, ErrNoHeader
	}
	info := Info{}
	for _, m := range field.FindAllStringSubmatch(h, -1) {
		key, value := strings.ToLower(m[1]), m[2]
		limit, ok := headerFields[key]
		if !ok {
			continue
		}
		if len(value) > limit {
			value = value[:limit] + "…"
		}
		switch key {
		case "name":
			info.Name = value
		case "description":
			info.Description = value
		case "author":
			info.Author = value
		case "homepage":
			info.Homepage = value
		case "version":
			info.Version = value
		}
	}
	if info.Name == "" {
		// A script without a name is named by the number it was
		// imported at, as lx-music-desktop does.
		info.Name = "未命名音源"
	}
	if strings.TrimSpace(script[len(h):]) == "" {
		return Info{}, fmt.Errorf("脚本只有说明，没有代码")
	}
	return info, nil
}
