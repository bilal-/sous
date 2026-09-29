// Package thread holds the declared half of the board: notes a person or agent
// wrote. Everything goes through store.Modify so concurrent agents can't clobber.
package thread

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
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
	UID          string     `json:"uid"`
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
}

var ErrNotFound = errors.New("no open thread")

// ValidationError marks caller mistakes (bad kind, empty text) so the CLI can
// report them as usage errors rather than failures.
type ValidationError string

func (v ValidationError) Error() string { return string(v) }

const name = "threads"

// Migrator: v0 was the pre-release shape without version/next_id and without
// remote/source/ref/snoozed_until/closed. v2 gave every note a uid.
type Migrator struct{}

func (Migrator) Empty() []byte { return []byte(`{"version":2,"next_id":1,"threads":[]}`) }
func (Migrator) Current() int  { return 2 }
func (Migrator) Migrate(from int, raw []byte) ([]byte, error) {
	switch from {
	case 0:
		return migrateV0(raw)
	case 1:
		var d Doc
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		d.Version = 2
		for i := range d.Threads {
			if d.Threads[i].UID == "" {
				d.Threads[i].UID = newUID()
			}
		}
		return json.Marshal(d)
	}
	return nil, fmt.Errorf("unknown version %d", from)
}

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
			d.Threads[i].Source = "human"
		}
		if d.Threads[i].ID > max {
			max = d.Threads[i].ID
		}
	}
	d.NextID = max + 1
	return json.Marshal(d)
}

func Note(s *store.Store, p project.Project, kind Kind, text, source string, now time.Time) (int, error) {
	if !kind.Valid() {
		return 0, ValidationError("kind must be me, them, or idea")
	}
	text = oneLine(text)
	if text == "" {
		return 0, ValidationError("note text is empty")
	}
	if source == "" {
		source = "human"
	}
	var id int
	_, err := store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error {
		id = d.NextID
		d.NextID++
		d.Threads = append(d.Threads, Thread{ID: id, UID: newUID(), Project: p.Path, Remote: p.Remote, Text: text, Kind: kind, Since: now.UTC(), Source: source})
		return nil
	})
	return id, err
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

// oneLine: a thread is one line. Newlines and runs of whitespace collapse.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

func Edit(s *store.Store, id int, text string) error {
	text = oneLine(text)
	if text == "" {
		return ValidationError("text is empty")
	}
	return touch(s, id, func(t *Thread) { t.Text = text })
}

func SetKind(s *store.Store, id int, kind Kind) error {
	if !kind.Valid() {
		return ValidationError("kind must be me, them, or idea")
	}
	return touch(s, id, func(t *Thread) { t.Kind = kind })
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

// Open: open and not snoozed, for the board.
func Open(s *store.Store, now time.Time) ([]View, error) {
	d, err := store.Load[Doc](s, name, Migrator{})
	if err != nil {
		return nil, err
	}
	out := []View{}
	for _, t := range d.Threads {
		if v := view(t, now); t.Closed == nil && !v.Snoozed {
			out = append(out, v)
		}
	}
	return out, nil
}

// ForProject: all open threads for a project, by path or remote, incl. snoozed.
func ForProject(s *store.Store, path, remote string, now time.Time) ([]View, error) {
	d, err := store.Load[Doc](s, name, Migrator{})
	if err != nil {
		return nil, err
	}
	out := []View{}
	for _, t := range d.Threads {
		if t.Closed != nil {
			continue
		}
		if t.Belongs(path, remote) {
			out = append(out, view(t, now))
		}
	}
	return out, nil
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
	return touch(s, id, func(t *Thread) { t.Closed = &c; t.ClosedBy = "upstream" })
}

// RecentlyClosedUpstream: what the tracker closed for this project lately,
// so the user sees it happened.
func RecentlyClosedUpstream(s *store.Store, path, remote string, now time.Time, within time.Duration) ([]View, error) {
	d, err := store.Load[Doc](s, name, Migrator{})
	if err != nil {
		return nil, err
	}
	out := []View{}
	for _, t := range d.Threads {
		if t.ClosedBy != "upstream" || t.Closed == nil || now.Sub(*t.Closed) > within {
			continue
		}
		if t.Belongs(path, remote) {
			out = append(out, view(t, now))
		}
	}
	return out, nil
}

// ClosedSince: every thread closed at or after since, newest first — by
// the user (done) or by the tracker (closed_by upstream).
func ClosedSince(s *store.Store, since, now time.Time) ([]View, error) {
	d, err := store.Load[Doc](s, name, Migrator{})
	if err != nil {
		return nil, err
	}
	out := []View{}
	for _, t := range d.Threads {
		if t.Closed != nil && !t.Closed.Before(since) {
			out = append(out, view(t, now))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Closed.After(*out[j].Closed) })
	return out, nil
}
