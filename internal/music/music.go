// Package music is what the app plays and browses, apart from where it
// comes from: a song, a playlist, an artist, an album, and a page of what
// a search found.
//
// A source is a catalogue the app asks, by its id: NetEase, built in, or
// a music service reached by package source. Everything here carries the
// source it is from, so that a song NetEase does not have can sit in the
// same queue as one it does. The JSON fields are NetEase's, which these
// types were taken from: a source that builds them in Go fills the fields
// itself, and its tags matter only to NetEase.
package music

import (
	"strconv"
	"strings"
	"time"
)

// The ids of the sources the app knows: NetEase, which it ships, and the
// services lx-music-desktop asks, whose scripts a listener may import.
const (
	SourceNetease = "netease"
	SourceKuwo    = "kw"
	SourceKugou   = "kg"
	SourceQQ      = "tx"
	SourceMigu    = "mg"
)

// Artist is a singer or a band.
type Artist struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Album is what a song is in.
type Album struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}

// Track is a song, a podcast's episode, or anything else that plays, as
// its source has it.
type Track struct {
	// Source is the id of the source it is from, and ID its id there. The
	// JSON names of Source and Key begin with `_`, which no service's API
	// uses: they are kept where the app keeps its tracks, and ignored
	// where a service answers.
	Source string `json:"_src,omitempty"`
	ID     int64  `json:"id"`
	// Key is the id in the source where that is not the number ID, as a
	// Kugou file's hash or a QQ song's mid. NetEase, Kuwo and Migu name
	// their songs by ID alone.
	Key      string   `json:"_key,omitempty"`
	Name     string   `json:"name"`
	Artists  []Artist `json:"ar"`
	Album    Album    `json:"al"`
	Duration int64    `json:"dt"` // milliseconds
	// PicURL is the cover where the source names one apart from the
	// album's.
	PicURL string `json:"_pic,omitempty"`

	// Origin is the source a song of another source was found on, when the
	// source it is from would not stream it: the player plays it from
	// there, and says so. It is set as such a match is made, and not kept.
	Origin string `json:"_origin,omitempty"`

	// What NetEase says of the track, which other sources leave unset.
	Fee       int        `json:"fee"`
	Alias     []string   `json:"alia"`
	Reason    string     `json:"reason"` // why a daily recommendation picked it
	Privilege *Privilege `json:"privilege"`
	// A podcast's episode plays as a track of its audio, ID, with the
	// episode's ProgramID, the PodcastID it is in, and what it is about.
	ProgramID   int64  `json:"programId,omitempty"`
	PodcastID   int64  `json:"podcastId,omitempty"`
	Description string `json:"description,omitempty"`
}

// Privilege is what NetEase lets the listener signed in, or nobody, do
// with a track.
type Privilege struct {
	ID int64 `json:"id"`
	St int   `json:"st"` // below 0: taken down
	Pl int   `json:"pl"` // the best bit rate the listener may play; 0 for none
}

// Src is the id of the source the track is from: NetEase when its source
// is not set, as the app's own NetEase types leave it.
func (t *Track) Src() string {
	if t.Source == "" {
		return SourceNetease
	}
	return t.Source
}

// SongKey names the track in its source, as the songs kept on disk are
// named: its key where it has one, else its id as a number.
func (t *Track) SongKey() string {
	if t.Key != "" {
		return t.Key
	}
	return strconv.FormatInt(t.ID, 10)
}

// Ident names the track among every source's: two sources may number
// their songs alike, and their songs are not the same.
func (t *Track) Ident() string { return t.Src() + "/" + t.SongKey() }

// ArtistNames joins the track's artists as the credits read.
func (t *Track) ArtistNames() string {
	names := make([]string, len(t.Artists))
	for i, a := range t.Artists {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// Playable reports whether the track can play at all: a song taken down
// cannot, wherever it is looked for.
//
// A VIP song is playable: the app may not stream it from its own source,
// but it looks for the song on the others, as lx-music-desktop does, and
// one of them may have it. A source that says the listener may play none
// of it (a privilege of 0) is the same case.
func (t *Track) Playable() bool {
	if t.ProgramID > 0 {
		// An episode's audio has no privilege: whether it streams, as a
		// paid one bought, is up to its stream.
		return true
	}
	return t.Privilege == nil || t.Privilege.St >= 0
}

// VIP reports whether the track needs a paid account: its source may not
// stream it, and the app looks for it on the others. A song taken down is
// not a VIP one: no source plays it.
func (t *Track) VIP() bool {
	if t.ProgramID > 0 || !t.Playable() {
		return false
	}
	return t.Fee == 1 || t.Fee == 4 || (t.Privilege != nil && t.Privilege.Pl == 0)
}

func (t *Track) Length() time.Duration { return time.Duration(t.Duration) * time.Millisecond }

// Cover is the track's cover: its own where the source names one, else
// its album's.
func (t *Track) Cover() string {
	if t.PicURL != "" {
		return t.PicURL
	}
	return t.Album.PicURL
}

// Playlist is a list of songs, a chart, or the songs the listener likes.
type Playlist struct {
	Source      string  `json:"_src,omitempty"`
	ID          int64   `json:"id"`
	Key         string  `json:"_key,omitempty"` // its id in the source, where not the number ID
	Name        string  `json:"name"`
	CoverURL    string  `json:"coverImgUrl"`
	PicURL      string  `json:"picUrl"`
	Description string  `json:"description"`
	PlayCount   float64 `json:"playCount"`
	TrackCount  int     `json:"trackCount"`
	UpdateFreq  string  `json:"updateFrequency"`
	SpecialType int     `json:"specialType"` // 5: the listener's liked songs
	Privacy     int     `json:"privacy"`     // 10: only its creator sees it
	Creator     struct {
		UserID   int64  `json:"userId"`
		Nickname string `json:"nickname"`
	} `json:"creator"`
	Tracks   []Track `json:"tracks"`
	TrackIDs []struct {
		ID int64 `json:"id"`
	} `json:"trackIds"`
	// Preview holds a chart's top songs, as its card shows them.
	Preview []ChartLine `json:"-"`
}

// ChartLine is a song at the top of a chart: its name and its artist.
type ChartLine struct {
	First  string `json:"first"`
	Second string `json:"second"`
}

// Src is the id of the source the playlist is from: NetEase when unset.
func (p *Playlist) Src() string {
	if p.Source == "" {
		return SourceNetease
	}
	return p.Source
}

// KeyOf names the playlist in its source.
func (p *Playlist) KeyOf() string {
	if p.Key != "" {
		return p.Key
	}
	return strconv.FormatInt(p.ID, 10)
}

func (p *Playlist) Cover() string {
	if p.CoverURL != "" {
		return p.CoverURL
	}
	return p.PicURL
}

// Stream is where a track streams from, and what it is.
type Stream struct {
	URL  string `json:"-"`
	Size int64  `json:"size"` // in bytes
	// Type is the format: mp3, or flac for the lossless levels.
	Type string `json:"type"`
	// Level is the level served, which may be below the one asked for.
	Level string `json:"level"`
	// Gain is how many dB to change the track by to sound as loud as
	// others, and Peak its loudest sample, 1 at full scale, or 0 if
	// unknown.
	Gain float64 `json:"gain"`
	Peak float64 `json:"peak"`
}

// Levels are the stream levels, from the lowest.
var Levels = []string{"standard", "higher", "exhigh", "lossless", "hires"}

// Lossless reports whether level streams FLAC.
func Lossless(level string) bool { return level == "lossless" || level == "hires" }

// Lyrics are a track's words: timed, their translation, their
// transliteration, and the word-timed ones a lx-music source may give.
type Lyrics struct {
	Lyric   string
	Tlyric  string
	Rlyric  string
	LXLyric string
}

// Tag is a category of playlists a source's square sorts them by: a
// language, a style, a scene or a mood.
type Tag struct {
	ID   int64
	Name string
	// Hot marks the few tags a square lists at its top.
	Hot bool
}

// SearchKind is what a search looks for. The numbers are NetEase's; a
// source whose API numbers them otherwise maps them.
type SearchKind int

const (
	SearchSongs     SearchKind = 1
	SearchAlbums    SearchKind = 10
	SearchArtists   SearchKind = 100
	SearchPlaylists SearchKind = 1000
	SearchUsers     SearchKind = 1002
	SearchPodcasts  SearchKind = 1009
)

// SearchResult is a page of what a search found across the sources asked,
// of the one kind it looked for, and how many they found in all.
type SearchResult struct {
	Songs     []Track
	Playlists []Playlist
	Total     int
}

// Len is how many the page has.
func (r *SearchResult) Len() int { return len(r.Songs) + len(r.Playlists) }
