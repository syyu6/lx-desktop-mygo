package app

import (
	"encoding/json"
	"testing"
)

// A settings file from before the player's colors were the default is moved
// to them once, since "system" was what it held by default rather than by
// choice; after that the version is written, and a listener who picks the
// system's colors again keeps them.
func TestTheAppearanceDefaultReachesSavedSettings(t *testing.T) {
	// Written before the change: no version, the old default.
	old := settings{}
	if err := json.Unmarshal([]byte(`{"appearance":"system","volume":0.4}`), &old); err != nil {
		t.Fatal(err)
	}
	old.migrate()
	if old.Appearance != "cover" {
		t.Errorf("a saved system appearance stayed %q", old.Appearance)
	}
	if old.Version != 1 {
		t.Errorf("the settings are version %d, want 1", old.Version)
	}
	if old.Volume != 0.4 {
		t.Errorf("the migration changed the volume to %v", old.Volume)
	}

	// Written by this version: the listener's own system choice stands.
	saved := settings{Appearance: "system", Version: 1}
	saved.migrate()
	if saved.Appearance != "system" {
		t.Errorf("a choice made after the change became %q", saved.Appearance)
	}

	// And a file that named no appearance at all takes the new default.
	blank := settings{}
	blank.migrate()
	if blank.Appearance != "cover" {
		t.Errorf("no appearance became %q, want the player's", blank.Appearance)
	}
}
