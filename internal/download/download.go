// Package download saves the songs the listener wants kept, as files of
// their own: lx-music-desktop's 下载, which plays what it has to and keeps
// what it saves.
//
// The manager here holds the tasks and runs the number of them the
// listener allows at once, a first in, first out queue. What a task does
// to get its song — resolve it on a source, fetch it, write its tags — is
// the app's (run), since only the app knows the sources; the manager
// knows what the listener sees: which task waits, runs, paused, finished
// or failed, and how far each has come.
package download

import (
	"context"
	"sort"
	"sync"
	"time"

	"netease-music-mygo/internal/music"
)

// A Status is where a task is.
type Status string

const (
	// Waiting tasks wait for a slot: the manager starts them as the ones
	// running end.
	Waiting Status = "waiting"
	Running Status = "running"
	// Paused tasks were stopped by the listener, and start again when
	// they are asked to. Tasks waiting when the app closes are paused as
	// it opens: nothing resumes on its own.
	Paused Status = "paused"
	Done   Status = "done"
	// Failed tasks errored: the reason says why, in words the listener
	// reads, and the task may be tried again.
	Failed Status = "failed"
)

// A Task is a song on its way to a file.
type Task struct {
	// ID names the task among all of them: the song, and the level asked
	// for. Asking for a song twice at one level is asking once.
	ID string `json:"id"`
	// Track is the song, and Level the level asked for: the one served
	// may be lower, and Served says which.
	Track music.Track `json:"track"`
	Level string      `json:"level"`
	// Served is the level the source actually gave, and Ext the format of
	// the file, as its own bytes say: mp3, flac, m4a or aac.
	Served string `json:"served,omitempty"`
	Ext    string `json:"ext,omitempty"`
	// Name is the file's name with its extension, and Dir the folder it
	// goes in. Path is where it is once written.
	Name string `json:"name"`
	Dir  string `json:"dir"`
	Path string `json:"path,omitempty"`
	// Status is where the task is, and Reason why it failed, in words the
	// listener reads. Note is what the app says of a task that did not
	// fail, as that its file was already there.
	Status Status `json:"status"`
	Reason string `json:"reason,omitempty"`
	Note   string `json:"note,omitempty"`
	// Done and Size are the bytes written and expected, 0 when the source
	// did not say.
	Done int64 `json:"done"`
	Size int64 `json:"size"`
	// Speed is how fast the bytes are coming, in bytes a second: kept as
	// it is measured, 0 when the task is not running.
	Speed float64 `json:"speed,omitempty"`
	// FromCache says the bytes were the ones the playing cache already
	// kept, so that the download cost no traffic.
	FromCache bool      `json:"fromCache,omitempty"`
	At        time.Time `json:"at"`
}

// File returns the file the task makes, or "" before it knows.
func (t *Task) File() string {
	if t.Path != "" {
		return t.Path
	}
	return ""
}

// Key is the song's identity among the sources': a task of a song is found
// by it, whatever level it asks for.
func (t *Task) Key() string { return t.Track.Ident() }

// A Run gets a task's song and writes it: it resolves where the song
// streams from, fetches it, tags it, and tells p what it learns and how
// far it has come. It returns nil once the file is written, or an error
// whose words the listener reads.
type Run func(ctx context.Context, t *Task, p *Progress) error

// A Manager holds the tasks and runs them.
//
// Its methods may be called from the main thread only, as the app's are;
// the tasks it runs report back through p and the manager's own goroutines,
// which take its lock.
type Manager struct {
	mu      sync.Mutex
	tasks   []*Task
	max     int
	running map[string]context.CancelFunc
	run     Run
	// changed is asked for a redraw as tasks move, no oftener than
	// changeEvery, and saved to keep the tasks, as one ends or is added.
	changed func()
	saved   func([]Task)
	// lastChange is when changed was last asked, and lastSave when the
	// tasks were.
	lastChange, lastSave time.Time
	// dirty says the tasks changed since they were last saved.
	dirty bool
}

// changeEvery is how often progress asks for a redraw: the rows show a
// percentage and a speed, which read the same at a few times a second as
// at sixty.
const changeEvery = 250 * time.Millisecond

// New returns a manager that runs tasks by run, asking changed for a
// redraw as they move and saved to keep them.
func New(run Run, changed func(), saved func([]Task)) *Manager {
	return &Manager{max: 3, running: map[string]context.CancelFunc{}, run: run, changed: changed, saved: saved}
}

// SetMax sets how many tasks run at once, and starts what can start.
func (m *Manager) SetMax(n int) {
	m.mu.Lock()
	m.max = max(1, n)
	m.mu.Unlock()
	m.kick()
}

// Max returns how many tasks run at once.
func (m *Manager) Max() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.max
}

// Add queues tasks, skipping the songs already queued at their level, and
// returns how many were added. A song that failed or finished may be asked
// for again: that task is replaced.
func (m *Manager) Add(tasks []Task) int {
	m.mu.Lock()
	added := 0
	for _, t := range tasks {
		if t.At.IsZero() {
			t.At = time.Now()
		}
		t.Status, t.Reason, t.Note = Waiting, "", ""
		t.Done, t.Size, t.Speed, t.FromCache = 0, 0, 0, false
		switch i := m.index(t.ID); {
		case i < 0:
			m.tasks = append(m.tasks, &t)
			added++
		case m.tasks[i].Status == Done, m.tasks[i].Status == Failed:
			// Asked for again: the old file goes with the old task, and
			// the new one takes its place, where the listener left it.
			*m.tasks[i] = t
			added++
		}
	}
	m.markDirtyLocked()
	m.mu.Unlock()
	if added > 0 {
		m.notify()
		m.save()
	}
	m.kick()
	return added
}

// index returns where the task id is, or -1.
func (m *Manager) index(id string) int {
	for i, t := range m.tasks {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// Tasks returns the tasks as they are now, in the order they were queued,
// copies that a view may read as it builds.
func (m *Manager) Tasks() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Task, len(m.tasks))
	for i, t := range m.tasks {
		out[i] = *t
	}
	return out
}

// Task returns the task id as it is now.
func (m *Manager) Task(id string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.index(id); i >= 0 {
		return *m.tasks[i], true
	}
	return Task{}, false
}

// For returns the task of the song key, whether it finished, runs or waits.
func (m *Manager) For(key string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.Key() == key {
			return *t, true
		}
	}
	return Task{}, false
}

// Running returns how many tasks run now.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.running)
}

// Resume starts a task that waited, was paused or failed, or starts every
// one of them when id is empty.
func (m *Manager) Resume(id string) {
	m.mu.Lock()
	for _, t := range m.tasks {
		if (id == "" || t.ID == id) && (t.Status == Paused || t.Status == Failed) {
			// The note said why it waited — a file that went, say — which
			// is done with now that it is asked for again.
			t.Status, t.Reason, t.Note = Waiting, "", ""
		}
	}
	m.markDirtyLocked()
	m.mu.Unlock()
	m.notify()
	m.save()
	m.kick()
}

// Pause stops a task that runs, or pauses every one not finished when id
// is empty: one waiting stops waiting, and one running gives up its
// connection.
func (m *Manager) Pause(id string) {
	m.mu.Lock()
	var stop []context.CancelFunc
	for _, t := range m.tasks {
		if id != "" && t.ID != id {
			continue
		}
		switch t.Status {
		case Waiting, Running:
			if c, ok := m.running[t.ID]; ok {
				stop = append(stop, c)
			}
			t.Status, t.Speed = Paused, 0
		}
	}
	m.markDirtyLocked()
	m.mu.Unlock()
	for _, c := range stop {
		c()
	}
	m.notify()
	m.save()
}

// Retry asks for a failed task again, or for every one when id is empty.
func (m *Manager) Retry(id string) {
	m.mu.Lock()
	for _, t := range m.tasks {
		if (id == "" || t.ID == id) && t.Status == Failed {
			t.Status, t.Reason, t.Done = Waiting, "", 0
		}
	}
	m.markDirtyLocked()
	m.mu.Unlock()
	m.notify()
	m.save()
	m.kick()
}

// Remove takes tasks out of the list, stopping those that run, or every
// one when id is empty. It returns those it took, whose files the caller
// may delete.
func (m *Manager) Remove(id string) []Task {
	m.mu.Lock()
	var gone []Task
	var stop []context.CancelFunc
	kept := m.tasks[:0]
	for _, t := range m.tasks {
		if id != "" && t.ID != id {
			kept = append(kept, t)
			continue
		}
		if c, ok := m.running[t.ID]; ok {
			stop = append(stop, c)
		}
		gone = append(gone, *t)
	}
	m.tasks = kept
	m.markDirtyLocked()
	m.mu.Unlock()
	for _, c := range stop {
		c()
	}
	if len(gone) > 0 {
		m.notify()
		m.save()
	}
	return gone
}

// Delete takes the tasks with ids out of the list, stopping those that run,
// and returns what it took: the page removes a batch of them at once, and
// the files follow their tasks.
func (m *Manager) Delete(ids []string) []Task {
	m.mu.Lock()
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
	}
	var gone []Task
	var stop []context.CancelFunc
	kept := m.tasks[:0]
	for _, t := range m.tasks {
		if !drop[t.ID] {
			kept = append(kept, t)
			continue
		}
		if c, ok := m.running[t.ID]; ok {
			stop = append(stop, c)
		}
		gone = append(gone, *t)
	}
	m.tasks = kept
	m.markDirtyLocked()
	m.mu.Unlock()
	for _, c := range stop {
		c()
	}
	if len(gone) > 0 {
		m.notify()
		m.save()
	}
	return gone
}

// ClearDone takes the finished tasks out, and returns them.
func (m *Manager) ClearDone() []Task {
	m.mu.Lock()
	var gone []Task
	kept := m.tasks[:0]
	for _, t := range m.tasks {
		if t.Status == Done {
			gone = append(gone, *t)
			continue
		}
		kept = append(kept, t)
	}
	m.tasks = kept
	m.markDirtyLocked()
	m.mu.Unlock()
	if len(gone) > 0 {
		m.notify()
		m.save()
	}
	return gone
}

// Restore puts back the tasks kept from the last session: those that ran
// or waited are paused, since nothing resumes on its own.
func (m *Manager) Restore(tasks []Task) {
	m.mu.Lock()
	for _, t := range tasks {
		switch t.Status {
		case Waiting, Running:
			t.Status, t.Speed, t.Done = Paused, 0, 0
		}
		m.tasks = append(m.tasks, &t)
	}
	m.mu.Unlock()
	m.notify()
}

// Requeue marks the tasks stale finds questionable as paused, keeping their
// song and level: for a task done with no file where it said it put it,
// which the listener took out of the folder, or moved. Paused rather than
// waiting, since nothing asked for it: the page offers the song again, and
// the listener says when — starting it here would fetch a song nobody
// asked for, and would fail outright with downloads switched off.
func (m *Manager) Requeue(stale func(*Task) bool) {
	m.mu.Lock()
	changed := false
	for _, t := range m.tasks {
		if stale(t) {
			t.Status, t.Path, t.Done, t.Size = Paused, "", 0, 0
			t.Reason, t.Speed = "", 0
			t.Note = "文件已经不在了，继续下载可以重新保存"
			changed = true
		}
	}
	m.markDirtyLocked()
	m.mu.Unlock()
	if changed {
		m.notify()
		m.save()
	}
}

// kick starts the tasks that wait, as many as run at once allows.
func (m *Manager) kick() {
	type start struct {
		id  string
		t   Task
		ctx context.Context
	}
	var starts []start
	m.mu.Lock()
	for _, t := range m.tasks {
		if len(m.running) >= m.max {
			break
		}
		if t.Status != Waiting {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.running[t.ID] = cancel
		t.Status, t.Reason, t.Speed = Running, "", 0
		starts = append(starts, start{id: t.ID, t: *t, ctx: ctx})
	}
	if len(starts) == 0 {
		m.mu.Unlock()
		return
	}
	m.markDirtyLocked()
	m.mu.Unlock()
	m.notify()
	m.save()
	for _, s := range starts {
		go m.work(s.ctx, s.id, s.t)
	}
}

// work runs one task to its end.
func (m *Manager) work(ctx context.Context, id string, t Task) {
	p := &Progress{m: m, id: id, ctx: ctx}
	err := m.run(ctx, &t, p)
	m.mu.Lock()
	i := m.index(id)
	delete(m.running, id)
	if i >= 0 {
		task := m.tasks[i]
		switch {
		case task.Status == Paused:
			// Paused while it ran: it stays paused, where it stopped.
		case ctx.Err() != nil:
			// Cancelled by Remove, which has taken the task away, or by
			// Pause, which already set the task paused.
		case err != nil:
			task.Status, task.Reason, task.Speed = Failed, err.Error(), 0
		default:
			task.Status, task.Reason, task.Speed = Done, "", 0
			if task.Size <= 0 && p.total > 0 {
				task.Done, task.Size = p.total, p.total
			}
		}
	}
	m.markDirtyLocked()
	m.mu.Unlock()
	m.notify()
	m.save()
	m.kick()
}

// markDirtyLocked says the tasks changed, to be kept. Its caller holds the
// lock.
func (m *Manager) markDirtyLocked() { m.dirty = true }

// notify asks for a redraw, no oftener than changeEvery.
func (m *Manager) notify() {
	m.mu.Lock()
	if time.Since(m.lastChange) < changeEvery {
		m.mu.Unlock()
		return
	}
	m.lastChange = time.Now()
	changed := m.changed
	m.mu.Unlock()
	if changed != nil {
		changed()
	}
}

// save keeps the tasks, if they changed since they were last kept. It is
// called as tasks are added, end or are taken away, not as they progress.
func (m *Manager) save() {
	m.mu.Lock()
	// Only structural changes reach here, a few a second at most while a
	// batch starts: no throttle, so that nothing is left unsaved when the
	// app closes right after.
	if !m.dirty || m.saved == nil {
		m.mu.Unlock()
		return
	}
	m.dirty, m.lastSave = false, time.Now()
	out := make([]Task, len(m.tasks))
	for i, t := range m.tasks {
		out[i] = *t
		out[i].Speed = 0
	}
	saved := m.saved
	m.mu.Unlock()
	saved(out)
}

// A Progress is a task's report to the manager as it runs: how far it has
// come, and what it has learned.
type Progress struct {
	m     *Manager
	id    string
	ctx   context.Context
	total int64
	last  time.Time
	lastN int64
}

// Total says how many bytes the song is, as its source said, or 0.
func (p *Progress) Total(n int64) {
	p.m.mu.Lock()
	if i := p.m.index(p.id); i >= 0 {
		p.m.tasks[i].Size = n
	}
	p.total = n
	p.m.mu.Unlock()
	p.m.notify()
}

// Bytes says how many bytes are written so far, and keeps the speed it is
// going at.
func (p *Progress) Bytes(n int64) {
	p.m.mu.Lock()
	i := p.m.index(p.id)
	if i >= 0 {
		t := p.m.tasks[i]
		t.Done = n
		if p.total > 0 {
			t.Size = p.total
		}
	}
	now := time.Now()
	if p.lastN > 0 && n > p.lastN {
		dt := now.Sub(p.last).Seconds()
		if dt > 0 {
			speed := float64(n-p.lastN) / dt
			p.last, p.lastN = now, n
			if i >= 0 {
				// Eased, so that a stalled connection's speed falls
				// rather than jumps.
				p.m.tasks[i].Speed = p.m.tasks[i].Speed*0.4 + speed*0.6
			}
		}
	} else {
		p.last, p.lastN = now, n
	}
	p.m.mu.Unlock()
	p.m.notify()
}

// Update runs fn with the task, under the manager's lock, for what the run
// learned of it: the level served, the format, the file's name and path.
func (p *Progress) Update(fn func(*Task)) {
	p.m.mu.Lock()
	if i := p.m.index(p.id); i >= 0 {
		fn(p.m.tasks[i])
	}
	p.m.mu.Unlock()
}

// Context is the run's context, which is cancelled as the task is paused or
// taken away.
func (p *Progress) Context() context.Context { return p.ctx }

// SortStatus returns the tasks in the order a page shows them: the ones
// running, then those waiting, then paused, then failed, and those finished
// last, each group in the order they were queued.
func SortStatus(tasks []Task) []Task {
	out := append([]Task(nil), tasks...)
	rank := func(s Status) int {
		switch s {
		case Running:
			return 0
		case Waiting:
			return 1
		case Paused:
			return 2
		case Failed:
			return 3
		}
		return 4
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Status) < rank(out[j].Status) })
	return out
}
