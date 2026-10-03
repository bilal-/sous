// Package thread holds the declared half of the board: notes a person or agent
// wrote. Everything goes through store.Modify so concurrent agents can't clobber.
package thread

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/text"
)

type Kind string

const (
	Me   Kind = "me"
	Them Kind = "them"
	Idea Kind = "idea"
)

func (k Kind) Valid() bool { return k == Me || k == Them || k == Idea }

type Thread struct {
	ID int `json:"id"` // short, for typing: sous done 3
	// UID is random and never changes. Filing markers use it, so notes from
	// two installs can never collide in a shared tracker.
	UID string `json:"uid"`
	// Legacy: the note was made before notes had a uid, so it may have been
	// filed with an older, number based marker. Set by the upgrade.
	Legacy       bool       `json:"legacy,omitempty"`
	Project      string     `json:"project"`
	Remote       *string    `json:"remote"`
	Text         string     `json:"text"`
	Kind         Kind       `json:"kind"`
	Since        time.Time  `json:"since"`
	Source       string     `json:"source"`
	Ref          *string    `json:"ref"`
	SnoozedUntil *time.Time `json:"snoozed_until"`
	Closed       *time.Time `json:"closed"`
	// ClosedBy: "" (the user, via done) or "upstream" (the tracker said
	// closed; the system of record wins and the provenance is kept).
	ClosedBy string `json:"closed_by,omitempty"`
	// Run: the note was handed to a runner (sous go --run).
	Run *Run `json:"run,omitempty"`
}

// Who wrote a note, and who closed it, besides the person.
const (
	SourceHuman      = "human"    // the person, at the command line or through their agent's hand
	ClosedByUpstream = "upstream" // its tracker closed it
)

// RunState is how a run is going.
type RunState string

const (
	RunStarting RunState = "starting"  // sous asked the runner; no answer yet
	RunRunning  RunState = "running"   // its agent is at it
	RunNeedsYou RunState = "needs_you" // it asked the person something
	RunDone     RunState = "done"      // finished; its work waits for review
	RunFailed   RunState = "failed"
)

// Working: the run is still at it, so it waits on others, not the person.
func (s RunState) Working() bool { return s == RunStarting || s == RunRunning }

// Run is a note handed to a runner. State is the runner's word for it
// (running, needs_you, done, failed; starting before the runner answered).
type Run struct {
	Runner   string     `json:"runner"`
	Ref      string     `json:"ref,omitempty"`
	Key      string     `json:"key,omitempty"` // go --key: a retry finds this run
	State    RunState   `json:"state"`
	Text     string     `json:"text,omitempty"` // the runner's summary or question
	Branch   string     `json:"branch,omitempty"`
	Worktree string     `json:"worktree,omitempty"`
	Log      string     `json:"log,omitempty"`
	Checked  *time.Time `json:"checked,omitempty"` // when the state was last asked
}

// Belongs: is this thread about the project at path (or with this remote)?
// Remote first: repos move, remotes don't.
func (t Thread) Belongs(path, remote string) bool {
	return t.Project == path || (remote != "" && t.Remote != nil && *t.Remote == remote)
}

type Doc struct {
	Version int      `json:"version"`
	NextID  int      `json:"next_id"`
	Threads []Thread `json:"threads"`
}

// View is a Thread as the board sees it.
type View struct {
	Thread
	Snoozed bool `json:"snoozed"`
	// Upstream: "" (not filed) | open | closed | unknown | error — set by
	// reconciliation. UpstreamErr says why when it is "error".
	Upstream    string `json:"upstream,omitempty"`
	UpstreamErr string `json:"upstream_err,omitempty"`
	// RunErr: why the run's state could not be read this time; the state
	// shown is the last one known.
	RunErr string `json:"run_err,omitempty"`
	// LogTail: for a run that failed, the last lines of its log, when show
	// read them.
	LogTail []string `json:"-"`
}

var ErrNotFound = errors.New("no open thread")

// ValidationError marks caller mistakes (bad kind, empty text) so the CLI can
// report them as usage errors rather than failures.
type ValidationError string

func (v ValidationError) Error() string { return string(v) }

const name = "threads"

// Migrator: v0 was the pre-release shape without version/next_id and without
// remote/source/ref/snoozed_until/closed. v2 gave every note a uid. v3
// records which notes predate uids (Legacy).
type Migrator struct{}

func (Migrator) Empty() []byte { return []byte(`{"version":3,"next_id":1,"threads":[]}`) }
func (Migrator) Current() int  { return 3 }
func (Migrator) Migrate(from int, raw []byte) ([]byte, error) {
	switch from {
	case 0:
		return migrateV0(raw)
	case 1: // no note had a uid yet: every one is legacy
		return edit(raw, 2, func(t *Thread) { t.UID, t.Legacy = newUID(), true })
	case 2: // upgraded by 0.1.1 to 0.1.6, which gave uids without saying which were new
		return edit(raw, 3, func(t *Thread) { t.Legacy = t.Legacy || t.Since.Before(UIDSince) })
	}
	return nil, fmt.Errorf("unknown version %d", from)
}

// edit applies fn to every note and sets the document's version.
func edit(raw []byte, version int, fn func(*Thread)) ([]byte, error) {
	var d Doc
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	d.Version = version
	for i := range d.Threads {
		fn(&d.Threads[i])
	}
	return json.Marshal(d)
}

// UIDSince is when notes began carrying a uid (sous 0.1.1). It is used only
// to upgrade files that 0.1.1 to 0.1.6 upgraded without recording Legacy.
var UIDSince = time.Date(2026, 9, 29, 3, 25, 0, 0, time.UTC)

// newUID: 12 random hex characters.
func newUID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported systems
	}
	return hex.EncodeToString(b[:])
}

func migrateV0(raw []byte) ([]byte, error) {
	var d Doc
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	d.Version = 1
	max := 0
	for i := range d.Threads {
		if d.Threads[i].Source == "" {
			d.Threads[i].Source = SourceHuman
		}
		if d.Threads[i].ID > max {
			max = d.Threads[i].ID
		}
	}
	d.NextID = max + 1
	return json.Marshal(d)
}

// Note saves a note and returns its number. It is safe to retry: an open
// note in the same project with the same kind and text is that note
// (existed), whoever wrote it.
func Note(s *store.Store, p project.Project, kind Kind, note, source string, now time.Time) (id int, existed bool, err error) {
	if !kind.Valid() {
		return 0, false, ValidationError("kind must be me, them, or idea")
	}
	note = text.OneLine(note)
	if note == "" {
		return 0, false, ValidationError("note text is empty")
	}
	_, err = store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error {
		for _, t := range d.Threads {
			if t.Closed == nil && t.Run == nil && t.Kind == kind && t.Text == note && t.Belongs(p.Path, remoteOf(p)) {
				id, existed = t.ID, true
				return nil
			}
		}
		id = d.add(Thread{Project: p.Path, Remote: p.Remote, Text: note, Kind: kind, Since: now.UTC(), Source: source})
		return nil
	})
	return id, existed, err
}

// remoteOf is p's remote, or "" when it has none.
func remoteOf(p project.Project) string {
	if p.Remote == nil {
		return ""
	}
	return *p.Remote
}

// add appends t as a new note, with the next number, a new uid, and human
// as its source unless it says otherwise. It returns the number.
func (d *Doc) add(t Thread) int {
	t.ID, t.UID = d.NextID, newUID()
	d.NextID++
	if t.Source == "" {
		t.Source = SourceHuman
	}
	d.Threads = append(d.Threads, t)
	return t.ID
}

// NoteRun makes the note for a new run: kind them (waiting on the runner),
// its text the brief's first line. With a key, an open run in the same
// project with that key is returned instead (existed), so a retry never
// starts a second run.
func NoteRun(s *store.Store, p project.Project, brief, key, runnerName, source string, now time.Time) (int, bool, error) {
	first, _, _ := strings.Cut(strings.TrimSpace(brief), "\n")
	title := text.OneLine(first)
	if title == "" {
		return 0, false, ValidationError("the brief is empty")
	}
	remote := remoteOf(p)
	if key == "" {
		key = briefKey(brief)
	}
	var id int
	var existed bool
	_, err := store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error {
		for _, t := range d.Threads {
			started := t.Run != nil && !(t.Run.Ref == "" && t.Run.State == RunFailed) // a failed start keeps no key
			if key != "" && t.Closed == nil && started && t.Run.Key == key && t.Belongs(p.Path, remote) {
				id, existed = t.ID, true
				return nil
			}
		}
		id = d.add(Thread{Project: p.Path, Remote: p.Remote, Text: title, Kind: Them,
			Since: now.UTC(), Source: source, Run: &Run{Runner: runnerName, Key: key, State: RunStarting}})
		return nil
	})
	return id, existed, err
}

// briefKey is a run's key when the caller gave none: its brief, so the
// same brief retried finds the same run.
func briefKey(brief string) string {
	sum := sha256.Sum256([]byte(text.OneLine(brief)))
	return "brief:" + hex.EncodeToString(sum[:6])
}

// SetRun changes an open run under the lock.
func SetRun(s *store.Store, id int, fn func(*Run)) error {
	missing := false
	err := touch(s, id, func(t *Thread) {
		if t.Run == nil {
			missing = true
			return
		}
		fn(t.Run)
	})
	if err == nil && missing {
		return fmt.Errorf("%w %d with a run", ErrNotFound, id)
	}
	return err
}

// Runs: every open run, snoozed or not.
func Runs(s *store.Store, now time.Time) ([]View, error) {
	return filter(s, now, func(t Thread) bool { return t.Closed == nil && t.Run != nil })
}

func touch(s *store.Store, id int, fn func(*Thread)) error {
	_, err := store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error {
		for i := range d.Threads {
			if d.Threads[i].ID == id && d.Threads[i].Closed == nil {
				fn(&d.Threads[i])
				return nil
			}
		}
		return fmt.Errorf("%w %d", ErrNotFound, id)
	})
	return err
}

func Edit(s *store.Store, id int, note string) error {
	note = text.OneLine(note)
	if note == "" {
		return ValidationError("text is empty")
	}
	return touch(s, id, func(t *Thread) { t.Text = note })
}

func SetKind(s *store.Store, id int, kind Kind) error {
	if !kind.Valid() {
		return ValidationError("kind must be me, them, or idea")
	}
	isRun := false
	err := touch(s, id, func(t *Thread) {
		if isRun = t.Run != nil; !isRun {
			t.Kind = kind
		}
	})
	if err == nil && isRun {
		return ValidationError(fmt.Sprintf("note %d is a run: it is on others while it works and on you once it stops", id))
	}
	return err
}

func Snooze(s *store.Store, id, days int, now time.Time) error {
	until := now.UTC().Add(time.Duration(days) * 24 * time.Hour)
	return touch(s, id, func(t *Thread) { t.SnoozedUntil = &until })
}

func Done(s *store.Store, id int, now time.Time) error {
	c := now.UTC()
	return touch(s, id, func(t *Thread) { t.Closed = &c })
}

func view(t Thread, now time.Time) View {
	return View{Thread: t, Snoozed: t.SnoozedUntil != nil && t.SnoozedUntil.After(now)}
}

// filter is every note keep says yes to, as seen at now, in the order they
// were made. Never nil.
func filter(s *store.Store, now time.Time, keep func(Thread) bool) ([]View, error) {
	d, err := store.Load[Doc](s, name, Migrator{})
	if err != nil {
		return nil, err
	}
	out := []View{}
	for _, t := range d.Threads {
		if keep(t) {
			out = append(out, view(t, now))
		}
	}
	return out, nil
}

// Open: every open note, for the board. A snoozed one is marked Snoozed:
// the board hides it, and --json lists it as snoozed.
func Open(s *store.Store, now time.Time) ([]View, error) {
	return filter(s, now, func(t Thread) bool { return t.Closed == nil })
}

// ForProject: all open threads for a project, by path or remote, incl. snoozed.
func ForProject(s *store.Store, path, remote string, now time.Time) ([]View, error) {
	return filter(s, now, func(t Thread) bool { return t.Closed == nil && t.Belongs(path, remote) })
}

// Get returns a thread whether open or closed.
func Get(s *store.Store, id int) (Thread, error) {
	d, err := store.Load[Doc](s, name, Migrator{})
	if err != nil {
		return Thread{}, err
	}
	for _, t := range d.Threads {
		if t.ID == id {
			return t, nil
		}
	}
	return Thread{}, fmt.Errorf("%w %d", ErrNotFound, id)
}

// FileAtomically files an open thread under the threads lock: if it already
// has a ref that ref is returned and do is not called; otherwise do runs
// (the upstream write) and its ref is recorded in the same critical section.
// Two callers cannot both see "no ref"; a crash after do() but before the
// write is repaired by the backend's own idempotency on thread id.
// With refile, an existing ref is ignored (its marker was found missing).
func FileAtomically(s *store.Store, id int, refile bool, do func(Thread) (string, error)) (string, error) {
	var ref string
	_, err := store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error {
		for i := range d.Threads {
			t := &d.Threads[i]
			if t.ID != id {
				continue
			}
			if t.Closed != nil {
				return fmt.Errorf("thread %d is closed", id)
			}
			if t.Ref != nil && !refile {
				ref = *t.Ref
				return nil
			}
			r, err := do(*t)
			if err != nil {
				return err
			}
			t.Ref = &r
			ref = r
			return nil
		}
		return fmt.Errorf("%w %d", ErrNotFound, id)
	})
	return ref, err
}

func CloseUpstreamClosed(s *store.Store, id int, now time.Time) error {
	c := now.UTC()
	return touch(s, id, func(t *Thread) { t.Closed = &c; t.ClosedBy = ClosedByUpstream })
}

// RecentlyClosedUpstream: what the tracker closed for this project lately,
// so the user sees it happened.
func RecentlyClosedUpstream(s *store.Store, path, remote string, now time.Time, within time.Duration) ([]View, error) {
	return filter(s, now, func(t Thread) bool {
		return t.ClosedBy == ClosedByUpstream && t.Closed != nil && now.Sub(*t.Closed) <= within && t.Belongs(path, remote)
	})
}

// ClosedSince: every thread closed at or after since, newest first — by
// the user (done) or by the tracker (closed_by upstream).
func ClosedSince(s *store.Store, since, now time.Time) ([]View, error) {
	out, err := filter(s, now, func(t Thread) bool { return t.Closed != nil && !t.Closed.Before(since) })
	sort.Slice(out, func(i, j int) bool { return out[i].Closed.After(*out[j].Closed) })
	return out, err
}
