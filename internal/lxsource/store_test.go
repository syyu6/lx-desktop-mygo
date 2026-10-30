package lxsource

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestStoreImportKeepsOneScript checks that importing the same script
// twice keeps one entry, and that importing it again keeps when it was
// imported and whether it was turned on.
func TestStoreImportKeepsOneScript(t *testing.T) {
	s := tempStore(t)
	first, err := s.Import(script(t, "simple.js"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !first.Enabled || first.Imported.IsZero() {
		t.Errorf("imported = %+v, want it enabled and dated", first)
	}
	if first.Name != "测试音源" {
		t.Errorf("name = %q, want the header's", first.Name)
	}

	if _, err := s.SetEnabled(first.ID, false); err != nil {
		t.Fatal(err)
	}
	again, err := s.Import(script(t, "simple.js"))
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Errorf("the same script got two IDs: %s and %s", first.ID, again.ID)
	}
	if again.Enabled {
		t.Error("importing again turned a script back on")
	}
	if !again.Imported.Equal(first.Imported) {
		t.Errorf("importing again changed when it was imported: %v then %v", first.Imported, again.Imported)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("List = %d scripts, want 1", len(list))
	}
}

// TestStoreKeepsWhatAScriptPlays checks that the platforms a script was
// found to play are kept beside it, survive being imported again, and go
// with its code when it is updated: the settings tag a script from them,
// without a runtime of its own.
func TestStoreKeepsWhatAScriptPlays(t *testing.T) {
	s := tempStore(t)
	code := script(t, "simple.js")
	info, err := s.Import(code)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Platforms) != 0 {
		t.Errorf("a script imported plays %v, before it was asked", info.Platforms)
	}
	if err := s.SetPlatforms(info.ID, []Source{SourceNetease, SourceKugou}); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || len(list[0].Platforms) != 2 || list[0].Platforms[0] != SourceNetease {
		t.Fatalf("List platforms = %v, want what SetPlatforms kept", list)
	}
	// The same code is the same script, and keeps what it was found to
	// play.
	if _, err := s.Import(code); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.get(info.ID); len(again.Platforms) != 2 {
		t.Errorf("importing it again kept %v", again.Platforms)
	}
}

// TestStoreRejectsWhatIsNotAScript checks that a file without the header
// lx-music-desktop's sources open with is not imported.
func TestStoreRejectsWhatIsNotAScript(t *testing.T) {
	s := tempStore(t)
	for _, bad := range []string{"", "console.log(1)", "/* just a comment */\nfoo()"} {
		if _, err := s.Import(bad); !errors.Is(err, ErrNoHeader) {
			t.Errorf("Import(%q) = %v, want ErrNoHeader", bad, err)
		}
	}
	// A header with no code is a note, not a source.
	if _, err := s.Import("/*!\n * @name 空的\n */\n"); err == nil {
		t.Error("a script with no code was imported")
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Errorf("a rejected script was kept: %v", list)
	}
}

// TestStoreKeepsCodeWithItsEntry checks that a script's code is stored as
// imported, and that removing it removes both files.
func TestStoreKeepsCodeWithItsEntry(t *testing.T) {
	s := tempStore(t)
	code := script(t, "simple.js")
	info, err := s.Import(code)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Source(info)
	if err != nil {
		t.Fatal(err)
	}
	if got != code {
		t.Error("the stored code is not what was imported")
	}
	if err := s.Remove(info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Source(info); !errors.Is(err, ErrNotFound) {
		t.Errorf("Source after Remove = %v, want ErrNotFound", err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("Remove left %v behind", names)
	}
	if err := s.Remove(info.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove twice = %v, want ErrNotFound", err)
	}
}

// TestStoreFetchesOnlyHTTP checks that a remote address is held to what a
// script may be fetched over.
func TestStoreFetchesOnlyHTTP(t *testing.T) {
	s := tempStore(t)
	for _, addr := range []string{"file:///etc/passwd", "qjs:std", "", "not a url", "ftp://example.com/x.js"} {
		if _, err := s.ImportURL(addr); !errors.Is(err, ErrBadURL) {
			t.Errorf("ImportURL(%q) = %v, want ErrBadURL", addr, err)
		}
	}
}

// TestStoreListsByImport checks that scripts come back in the order they
// were imported, so that a list does not move as they are turned on.
func TestStoreListsByImport(t *testing.T) {
	s := tempStore(t)
	var names []string
	for _, code := range []string{
		"/*!\n * @name 甲\n */\na()\n",
		"/*!\n * @name 乙\n */\nb()\n",
		"/*!\n * @name 丙\n */\nc()\n",
	} {
		info, err := s.Import(code)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, info.Name)
	}
	// Turn the first off: it stays where it was in the list.
	list, _ := s.List()
	if _, err := s.SetEnabled(list[0].ID, false); err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, info := range got {
		order = append(order, info.Name)
	}
	if strings.Join(order, ",") != strings.Join(names, ",") {
		t.Errorf("order = %v, want %v", order, names)
	}
}

// The store's directory is the listener's data: an entry that will not
// read is skipped rather than taking the list down with it.
func TestStoreSkipsBrokenEntries(t *testing.T) {
	s := tempStore(t)
	info, err := s.Import(script(t, "simple.js"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "junk.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != info.ID {
		t.Errorf("List = %v, want the one good script", list)
	}
}
