package qq

import (
	"strconv"
	"sync"
)

// QQ names a song, an artist and an album by a mid, and its pages name them
// by a number: a song's mid is kept on its track (music.Track.Key), but an
// artist's and an album's are not on music.Artist and music.Album, which
// every source shares. Those two are remembered here, from whatever answer
// last carried them, so that opening an artist's page from a song's credit
// finds the mid to ask by.
//
// It is a cache, not a record: nothing is fetched to fill it, and one that
// has forgotten a mid asks QQ again by the number, which its search and its
// album page accept.

const midsKept = 4096

type midTable struct {
	mu    sync.RWMutex
	mids  map[int64]string
	order []int64
}

var (
	artistMids = newMids()
	albumMids  = newMids()
	songMids   = newMids()
)

func newMids() *midTable { return &midTable{mids: map[int64]string{}} }

// put remembers id's mid, dropping the oldest once the table is full: a
// mid is a page's key, not a thing to keep for good.
func (m *midTable) put(id int64, mid string) {
	if id <= 0 || mid == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mids[id]; !ok {
		m.order = append(m.order, id)
		for len(m.order) > midsKept {
			old := m.order[0]
			m.order = m.order[1:]
			delete(m.mids, old)
		}
	}
	m.mids[id] = mid
}

func (m *midTable) get(id int64) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mids[id]
}

// artistMID is an artist's mid as last seen, or "" if it has not been.
func artistMID(id int64) string { return artistMids.get(id) }

// albumMID is an album's mid as last seen, or "" if it has not been.
func albumMID(id int64) string { return albumMids.get(id) }

// songMID is a song's mid as last seen, or "" if it has not been. Songs
// carry theirs on the track, but liking a song is asked by its number
// alone, and the two are put together only from what was read before.
func songMID(id int64) string { return songMids.get(id) }

// keyOf names an id as the string the app routes with.
func keyOf(id int64) string { return strconv.FormatInt(id, 10) }
