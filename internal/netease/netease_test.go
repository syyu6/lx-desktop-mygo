package netease

import "testing"

// A VIP song plays: the app's own source may not stream it, but the app
// looks for it on the others (package source's Chain). Only a song taken
// down, or one its source says nothing can stream, does not.
func TestPlayableFollowsPrivilege(t *testing.T) {
	tracks := []Track{{ID: 1, Fee: 1}, {ID: 2, Fee: 8}, {ID: 3, Fee: 8}, {ID: 4, Fee: 1}}
	if !tracks[0].Playable() || !tracks[1].Playable() {
		t.Fatal("without privileges, a song plays until its source says it cannot")
	}
	withPrivileges(tracks, []Privilege{
		{ID: 1, Pl: 320000},           // VIP song, the listener subscribes
		{ID: 2, Pl: 0},                // VIP only: looked for on the others
		{ID: 3, St: -200, Pl: 128000}, // taken down
	})
	want := []bool{true, true, false, true}
	for i, tr := range tracks {
		if tr.Playable() != want[i] {
			t.Errorf("track %d: playable %v", tr.ID, tr.Playable())
		}
	}
	// The two it may not stream itself are VIP ones, which another source
	// may have, but the one taken down is not.
	if !tracks[1].VIP() || tracks[2].VIP() {
		t.Errorf("VIP: track 2 %v, track 3 %v", tracks[1].VIP(), tracks[2].VIP())
	}
}

// A VIP song is marked as one, for the pages to say so, though it plays.
func TestVIP(t *testing.T) {
	for i, tc := range []struct {
		track Track
		vip   bool
	}{
		{Track{Fee: 1}, true},
		{Track{Fee: 4}, true},
		{Track{Fee: 0}, false},
		{Track{Fee: 8}, false},
		{Track{Fee: 1, ProgramID: 3}, false}, // an episode is not a VIP song
		{Track{Privilege: &Privilege{Pl: 0}}, true},
		{Track{Privilege: &Privilege{Pl: 320000}}, false},
	} {
		if got := tc.track.VIP(); got != tc.vip {
			t.Errorf("case %d: VIP %v", i, got)
		}
	}
	// A song taken down is not VIP: nothing plays it.
	takenDown := Track{Privilege: &Privilege{St: -200, Pl: 0}}
	if takenDown.VIP() {
		t.Error("a taken-down song was marked VIP")
	}
}
