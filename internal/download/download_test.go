package download

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"netease-music-mygo/internal/music"
)

// A file's name is what the listener finds the song by: the words of the
// scheme, without what a file system refuses, and short enough to keep.
func TestFileName(t *testing.T) {
	tr := music.Track{
		Name:    "晴天",
		Artists: []music.Artist{{Name: "周杰伦"}},
	}
	for _, c := range []struct{ format, want string }{
		{"歌名 - 歌手", "晴天 - 周杰伦"},
		{"歌手 - 歌名", "周杰伦 - 晴天"},
		{"歌名", "晴天"},
	} {
		if got := FileName(c.format, tr); got != c.want {
			t.Errorf("FileName(%q) = %q, want %q", c.format, got, c.want)
		}
	}

	// What a file system will not take goes, and a name left with nothing
	// but its separators does not end in them.
	odd := music.Track{Name: `a/b:c*?"<>|d`, Artists: []music.Artist{{Name: "e"}}}
	if got, want := FileName("歌名", odd), "abcd"; got != want {
		t.Errorf("FileName dropped %q, want %q", got, want)
	}
	if got, want := FileName("歌名 - 歌手", music.Track{Name: "a", Artists: []music.Artist{{Name: "/"}}}), "a"; got != want {
		t.Errorf("FileName kept the dangling separator: %q, want %q", got, want)
	}

	// A song with no name at all still makes a file: one named as unknown.
	if got := FileName("歌名", music.Track{}); got == "" {
		t.Error("FileName of a nameless song is empty")
	}

	// A name longer than a file system takes is cut, not refused — by
	// characters and by bytes, since a Chinese title is three bytes a
	// character and ext4 takes 255 bytes a name.
	long := music.Track{Name: repeat("长", 400)}
	if got := FileName("歌名", long); len([]rune(got)) > maxFileName {
		t.Errorf("FileName is %d runes long, want at most %d", len([]rune(got)), maxFileName)
	}
	if got := FileName("歌名 - 歌手", music.Track{Name: repeat("长", 400), Artists: []music.Artist{{Name: repeat("歌", 100)}}}); len(got) > maxFileNameBytes {
		t.Errorf("FileName is %d bytes, want at most %d", len(got), maxFileNameBytes)
	}
	// Cut by bytes does not split a character: the name stays valid text.
	cut := FileName("歌名", long)
	if !utf8.ValidString(cut) {
		t.Errorf("FileName cut mid-character: %q", cut)
	}
}

// Sizes read the way a listener reads them: megabytes for songs, kilobytes
// for the few that are not yet, and a dash for what is not known.
func TestSize(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{
		{0, "—"},
		{-5, "—"},
		{512, "1 KB"},
		{1024 * 1024 * 3 / 2, "1.5 MB"},
		{5 << 30, "5.0 GB"},
	} {
		if got := Size(c.n); got != c.want {
			t.Errorf("Size(%d) = %q, want %q", c.n, got, c.want)
		}
	}
	if got := Speed(0); got != "" {
		t.Errorf("Speed(0) = %q, want nothing", got)
	}
	if got := Speed(1 << 20); got != "1.0 MB/s" {
		t.Errorf("Speed(1MB) = %q", got)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

// newTestManager returns a manager that runs tasks by run, counting the
// ones started and ended.
func newTestManager(run Run) *Manager {
	return New(run, func() {}, func([]Task) {})
}

func waiting(id string) Task {
	return Task{ID: id, Track: music.Track{ID: 1, Name: id}, Level: "exhigh", Status: Waiting}
}

// Waiting for a slot: a task starts as a slot frees, and no more run at
// once than the manager allows.
func TestManagerRunsAtMax(t *testing.T) {
	var mu sync.Mutex
	started, running, most := 0, 0, 0
	release := make(chan struct{})
	m := newTestManager(func(ctx context.Context, t *Task, p *Progress) error {
		mu.Lock()
		started++
		running++
		if running > most {
			most = running
		}
		mu.Unlock()
		<-release
		mu.Lock()
		running--
		mu.Unlock()
		return nil
	})
	m.SetMax(2)
	m.Add([]Task{waiting("a"), waiting("b"), waiting("c")})
	if !waitUntil(func() bool { mu.Lock(); defer mu.Unlock(); return started == 2 }) {
		t.Fatalf("started %d tasks, want 2", started)
	}
	close(release)
	if !waitUntil(func() bool { mu.Lock(); defer mu.Unlock(); return started == 3 }) {
		t.Fatal("the third task never started")
	}
	if !waitUntil(func() bool {
		for _, tk := range m.Tasks() {
			if tk.Status != Done {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("tasks did not finish: %+v", m.Tasks())
	}
	if most > 2 {
		t.Errorf("%d tasks ran at once, want at most 2", most)
	}
}

// Asking for a song twice at one level is asking once; once it is done,
// asking again replaces it, where the listener left it.
func TestManagerAddDedupes(t *testing.T) {
	block := make(chan struct{})
	m := newTestManager(func(ctx context.Context, t *Task, p *Progress) error {
		<-block
		return nil
	})
	if n := m.Add([]Task{waiting("a")}); n != 1 {
		t.Fatalf("Add = %d, want 1", n)
	}
	if n := m.Add([]Task{waiting("a")}); n != 0 {
		t.Errorf("Add of a queued song = %d, want 0", n)
	}
	close(block)
	waitUntil(func() bool { t, _ := m.Task("a"); return t.Status == Done })
	m.SetMax(1)
	m.Add([]Task{waiting("b")})
	// "a" is done: asking again replaces it where it is, before "b".
	if n := m.Add([]Task{waiting("a")}); n != 1 {
		t.Errorf("Add of a done song = %d, want 1", n)
	}
	tasks := m.Tasks()
	if len(tasks) != 2 || tasks[0].ID != "a" {
		t.Errorf("the done task was not replaced in place: %+v", tasks)
	}
}

// Pause stops a running task where it is, and resume starts it again from
// the beginning; neither loses the task.
func TestManagerPauseResume(t *testing.T) {
	var mu sync.Mutex
	started := 0
	m := newTestManager(func(ctx context.Context, t *Task, p *Progress) error {
		mu.Lock()
		started++
		mu.Unlock()
		<-ctx.Done()
		return ctx.Err()
	})
	m.Add([]Task{waiting("a")})
	waitUntil(func() bool { mu.Lock(); defer mu.Unlock(); return started == 1 })
	m.Pause("a")
	if tsk, _ := m.Task("a"); tsk.Status != Paused {
		t.Fatalf("a paused task is %q", tsk.Status)
	}
	// The run's own return does not overwrite what the listener asked for.
	time.Sleep(20 * time.Millisecond)
	if tsk, _ := m.Task("a"); tsk.Status != Paused {
		t.Errorf("the task stopped being paused: %q", tsk.Status)
	}
	m.Resume("a")
	waitUntil(func() bool { mu.Lock(); defer mu.Unlock(); return started == 2 })
	m.Pause("")
	if tsk, _ := m.Task("a"); tsk.Status != Paused {
		t.Errorf("pausing all left the task %q", tsk.Status)
	}
}

// A run that fails says why, in the words it returned, and retry asks for
// it again.
func TestManagerFailure(t *testing.T) {
	fails := true
	m := newTestManager(func(ctx context.Context, t *Task, p *Progress) error {
		if fails {
			return errors.New("地址打不开")
		}
		return nil
	})
	m.Add([]Task{waiting("a")})
	waitUntil(func() bool { tk, _ := m.Task("a"); return tk.Status != Running && tk.Status != Waiting })
	tk, _ := m.Task("a")
	if tk.Status != Failed || tk.Reason != "地址打不开" {
		t.Fatalf("a failed task is %q, %q", tk.Status, tk.Reason)
	}
	fails = false
	m.Retry("a")
	waitUntil(func() bool { tk, _ := m.Task("a"); return tk.Status == Done })
	if tk, _ := m.Task("a"); tk.Status != Done || tk.Reason != "" {
		t.Errorf("a retried task is %q, %q", tk.Status, tk.Reason)
	}
}

// What the last session kept comes back paused: nothing starts on its own
// after a restart.
func TestManagerRestore(t *testing.T) {
	started := 0
	m := newTestManager(func(ctx context.Context, t *Task, p *Progress) error {
		started++
		return nil
	})
	m.Restore([]Task{{ID: "a", Status: Running}, {ID: "b", Status: Waiting}, {ID: "c", Status: Done, Path: "/x/a.flac"}})
	tasks := m.Tasks()
	if len(tasks) != 3 {
		t.Fatalf("Restore kept %d tasks, want 3", len(tasks))
	}
	for _, tk := range tasks[:2] {
		if tk.Status != Paused {
			t.Errorf("restored %q as %q, want paused", tk.ID, tk.Status)
		}
	}
	if tasks[2].Status != Done {
		t.Errorf("a finished task came back %q", tasks[2].Status)
	}
	time.Sleep(20 * time.Millisecond)
	if started != 0 {
		t.Errorf("%d restored tasks started on their own", started)
	}
}

// A task whose file is gone is waiting again: the listener took it out of
// the folder, and the page offers to save it anew.
func TestManagerRequeue(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	m := newTestManager(func(ctx context.Context, t *Task, p *Progress) error {
		<-block
		return nil
	})
	m.Restore([]Task{
		{ID: "a", Status: Done, Path: "/gone.flac", Done: 10, Size: 10},
		{ID: "b", Status: Done, Path: "/here.flac"},
	})
	m.Requeue(func(t *Task) bool { return t.Path == "/gone.flac" })
	tasks := m.Tasks()
	// It was started as it was requeued, which is what the page offers;
	// what the requeue cleared is what matters here.
	if tasks[0].Path != "" || (tasks[0].Status != Waiting && tasks[0].Status != Running) {
		t.Errorf("the stale task is %+v", tasks[0])
	}
	if tasks[1].Status != Done || tasks[1].Path != "/here.flac" {
		t.Errorf("the kept task is %+v", tasks[1])
	}
}

// Deleting takes the tasks asked for and no others, and a task that
// finished stays out.
func TestManagerDelete(t *testing.T) {
	m := newTestManager(func(ctx context.Context, t *Task, p *Progress) error { return nil })
	m.Add([]Task{waiting("a"), waiting("b"), waiting("c")})
	gone := m.Delete([]string{"a", "c"})
	if len(gone) != 2 {
		t.Fatalf("Delete took %d tasks, want 2", len(gone))
	}
	tasks := m.Tasks()
	if len(tasks) != 1 || tasks[0].ID != "b" {
		t.Errorf("after the delete: %+v", tasks)
	}
	// b was started as a and c went; it finishes on its own.
	waitUntil(func() bool { tk, _ := m.Task("b"); return tk.Status == Done })
	if done := m.ClearDone(); len(done) != 1 || done[0].ID != "b" {
		t.Errorf("ClearDone took %+v, want the finished b", done)
	}
}

// The page's order puts what moves on top.
func TestSortStatus(t *testing.T) {
	in := []Task{{ID: "d", Status: Done}, {ID: "f", Status: Failed}, {ID: "r", Status: Running}, {ID: "p", Status: Paused}, {ID: "w", Status: Waiting}}
	var got []string
	for _, t := range SortStatus(in) {
		got = append(got, t.ID)
	}
	want := []string{"r", "w", "p", "f", "d"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortStatus = %v, want %v", got, want)
		}
	}
	// The tasks the caller kept are not the page's to shuffle.
	if in[0].ID != "d" {
		t.Errorf("SortStatus sorted its argument in place")
	}
}

// waitUntil waits for cond, and reports whether it came true within a
// moment.
func waitUntil(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}
