package lxsource

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// readCapabilities reads what a script says it does, as it sends it in
// lx's inited event, and keeps what this app runs: a source of music, the
// actions this package knows, and the qualities lx-music-desktop accepts.
// It returns nil when the script named no source.
func readCapabilities(info string) map[Source]Capability {
	if info == "" {
		return nil
	}
	var raw struct {
		Sources map[string]struct {
			Type      string   `json:"type"`
			Actions   []string `json:"actions"`
			Qualitys  []string `json:"qualitys"`
			Qualities []string `json:"qualities"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(info), &raw); err != nil {
		return nil
	}
	out := map[Source]Capability{}
	for name, s := range raw.Sources {
		source := Source(name)
		if !knownSource(source) || s.Type != "music" {
			continue
		}
		c := Capability{}
		for _, a := range s.Actions {
			if knownAction(Action(a)) {
				c.Actions = append(c.Actions, Action(a))
			}
		}
		// Scripts spell the field both ways; lx takes either.
		qualities := s.Qualitys
		if len(qualities) == 0 {
			qualities = s.Qualities
		}
		for _, q := range qualities {
			if knownQuality(Quality(q)) {
				c.Qualities = append(c.Qualities, Quality(q))
			}
		}
		if len(c.Actions) == 0 {
			continue
		}
		out[source] = c
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func knownSource(s Source) bool {
	for _, x := range Sources {
		if x == s {
			return true
		}
	}
	return false
}

func knownAction(a Action) bool {
	for _, x := range Actions {
		if x == a {
			return true
		}
	}
	return false
}

func knownQuality(q Quality) bool {
	for _, x := range Qualities {
		if x == q {
			return true
		}
	}
	return false
}

// A Track is what a script is told about a song: the ID it knows the song
// by on its own service, and enough of the song to find it again.
type Track struct {
	// Source is the service the song is on, and ID the song's own ID
	// there.
	Source Source `json:"source"`
	ID     string `json:"id"`
	// Name, Singer, Album and Interval describe it, as the service does;
	// Interval is its length, mm:ss.
	Name     string `json:"name"`
	Singer   string `json:"singer"`
	Album    string `json:"albumName"`
	Interval string `json:"interval"`
	// PicURL is its cover, when known.
	PicURL string `json:"picUrl,omitempty"`
}

// request makes the info a script's handler is given for t.
func request(t Track, quality Quality) map[string]any {
	musicInfo := map[string]any{
		"id":        t.ID,
		"songmid":   t.ID,
		"hash":      t.ID,
		"name":      t.Name,
		"singer":    t.Singer,
		"albumName": t.Album,
		"interval":  t.Interval,
		"source":    string(t.Source),
		"type":      string(quality),
		"meta": map[string]any{
			"songId":    t.ID,
			"albumName": t.Album,
			"picUrl":    t.PicURL,
		},
	}
	return map[string]any{
		"type":      string(quality),
		"musicInfo": musicInfo,
	}
}

// StreamURL asks the script where t streams from, at quality. What comes
// back is checked as lx-music-desktop checks it: a URL that could be a
// page or a data: URI is no stream.
func (e *Engine) StreamURL(t Track, quality Quality) (string, error) {
	raw, err := e.call(ActionMusicURL, map[string]any{
		"source": string(t.Source),
		"action": string(ActionMusicURL),
		"info":   request(t, quality),
	})
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", errors.New("音源脚本没有给出播放地址")
	}
	if len(s) == 0 || len(s) > maxURL {
		return "", errors.New("音源脚本给出的播放地址无效")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("音源脚本给出的播放地址无效")
	}
	return s, nil
}

// Lyrics asks the script for t's lyrics.
type Lyrics struct {
	Lyric   string `json:"lyric"`
	Tlyric  string `json:"tlyric"`
	Rlyric  string `json:"rlyric"`
	LXLyric string `json:"lxlyric"`
}

// Lyrics returns t's lyrics, as the script has them.
func (e *Engine) Lyrics(t Track) (Lyrics, error) {
	raw, err := e.call(ActionLyric, map[string]any{
		"source": string(t.Source),
		"action": string(ActionLyric),
		"info":   request(t, Quality320k),
	})
	if err != nil {
		return Lyrics{}, err
	}
	var l Lyrics
	if err := json.Unmarshal(raw, &l); err != nil {
		return Lyrics{}, errors.New("音源脚本没有给出歌词")
	}
	if strings.TrimSpace(l.Lyric) == "" {
		return Lyrics{}, errors.New("音源脚本没有给出歌词")
	}
	// The limits lx-music-desktop applies to what a script may return.
	l.Lyric = truncate(l.Lyric, maxLyric)
	l.Tlyric = truncate(l.Tlyric, maxTrans)
	l.Rlyric = truncate(l.Rlyric, maxTrans)
	l.LXLyric = truncate(l.LXLyric, maxLXLyric)
	return l, nil
}

// Pic returns the cover of t, as a URL the script knows it at.
func (e *Engine) Pic(t Track) (string, error) {
	raw, err := e.call(ActionPic, map[string]any{
		"source": string(t.Source),
		"action": string(ActionPic),
		"info":   request(t, Quality320k),
	})
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", errors.New("音源脚本没有给出封面")
	}
	if len(s) == 0 || len(s) > maxPicURL || !strings.HasPrefix(s, "http") {
		return "", errors.New("音源脚本给出的封面地址无效")
	}
	return s, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// String says what the script is, for logs.
func (e *Engine) String() string { return fmt.Sprintf("lxsource.Engine(%s)", e.info.Name) }
