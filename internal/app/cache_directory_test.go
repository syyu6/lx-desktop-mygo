package app

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// cacheDirApp is an app whose settings name a folder of the test's, and
// whose cache keeps its songs there: the state the settings' row reads.
func cacheDirApp(t *testing.T, root string) *app {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.CacheDir = root
	a.pl.Cache = player.NewCache(filepath.Join(root, "songs"), 1<<30)
	openSettingsTab(a, settingsGeneral)
	return a
}

// The settings show where the songs are kept, and choosing the folder they
// are kept in stages the move for the next start rather than make it: the
// cache is open, and a song playing would lose its file under it.
func TestCacheDirectoryIsChosenForTheNextStart(t *testing.T) {
	root := t.TempDir()
	a := cacheDirApp(t, root)
	tt := ui.NewTester(a.settingsView, settingsWidth, 1600)
	tt.Frame()
	if !tt.HasText(filepath.Join(root, "songs")) {
		t.Fatalf("the settings do not say where the songs are kept: %q", tt.Texts())
	}
	target := t.TempDir()
	if err := a.setCacheDirectory(target); err != nil {
		t.Fatal(err)
	}
	if a.settings.CacheDir != root {
		t.Errorf("the folder is %q, though the move is staged", a.settings.CacheDir)
	}
	pending := a.settings.PendingCacheDir
	if pending == nil || *pending != target {
		t.Fatalf("the next folder is %v", pending)
	}
	tt.Frame()
	if !tt.HasText("重启后迁移并生效") {
		t.Errorf("the settings do not say the move waits for a restart: %q", tt.Texts())
	}
	if !tt.HasText(filepath.Join(target, "songs")) {
		t.Errorf("the settings do not say the folder that comes: %q", tt.Texts())
	}
	// Changing one's mind puts the folder back as it is, and stages nothing.
	if err := a.setCacheDirectory(root); err != nil {
		t.Fatal(err)
	}
	if a.settings.PendingCacheDir != nil {
		t.Errorf("the move is still staged: %v", a.settings.PendingCacheDir)
	}
}

// A folder that cannot be written to is refused there and then: the cache
// would not find out until the restart, with the old songs already gone.
func TestCacheDirectoryMustTakeAFile(t *testing.T) {
	root := t.TempDir()
	a := cacheDirApp(t, root)
	if err := a.setCacheDirectory(filepath.Join(root, "missing", "deeper")); err != nil {
		t.Fatalf("a folder under a folder that is made: %v", err)
	}
	if a.settings.PendingCacheDir == nil {
		t.Error("the new folder is not staged")
	}
	// A file where the folder should be is no folder.
	file := filepath.Join(root, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.setCacheDirectory(file); err == nil {
		t.Error("a file was taken for a folder")
	}
}

// The songs move across at the next start, and only then does the new
// folder become the cache's: the old files go after the setting is saved.
func TestCacheMovesAtTheNextStart(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	cache := player.NewCache(filepath.Join(from, "songs"), 1<<30)
	if err := os.MkdirAll(cache.Directory(), 0o700); err != nil {
		t.Fatal(err)
	}
	writeKeptSong(t, cache.Directory(), "1-exhigh", 2048)
	writeKeptSong(t, cache.Directory(), "2-exhigh", 4096)
	// A file that is not a whole song stays where it is: the cache drops
	// it in its own time, and a move is not the place to guess.
	os.WriteFile(filepath.Join(cache.Directory(), "3-exhigh.mp3"), []byte("half"), 0o600)

	target, err := prepareSongCache(settings{CacheDir: to, CacheMB: 1024})
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := cache.CopyTo(target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"1-exhigh.mp3", "1-exhigh.json", "2-exhigh.mp3", "2-exhigh.json"} {
		if _, err := os.Stat(filepath.Join(target.Directory(), name)); err != nil {
			t.Errorf("%s did not move: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target.Directory(), "3-exhigh.mp3")); err == nil {
		t.Error("a song that never all came in was copied")
	}
	// The originals are still there until the move is committed.
	if _, err := os.Stat(filepath.Join(cache.Directory(), "1-exhigh.mp3")); err != nil {
		t.Fatalf("a song was removed before the folder was saved: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache.Directory(), "1-exhigh.mp3")); err == nil {
		t.Error("a song stayed in the old folder")
	}
}

// With NM_FRAMES set, saves the settings' cache group, with and without a
// move waiting for the next start, to look at them.
func TestCacheDirectoryFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, pending := range []bool{false, true} {
		a := cacheDirApp(t, "/Volumes/音乐盘/Cache")
		a.cacheUsed = 512 << 20
		name := "settings-cache"
		if pending {
			next := "/Volumes/新盘"
			a.settings.PendingCacheDir = &next
			name += "-pending"
		}
		tt := ui.NewTester(a.settingsView, settingsWidth, 1400)
		tt.Advance(time.Second)
		tt.Frame()
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		png.Encode(f, tt.Image())
		f.Close()
	}
}

// writeKeptSong puts a song in dir as the cache keeps one: its bytes, and
// the stream beside it that says what it is.
func writeKeptSong(t *testing.T, dir, key string, size int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, key+".mp3"), make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	st := netease.Stream{Level: "exhigh", Type: "mp3", Size: int64(size)}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}
