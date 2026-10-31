// Package effort groups existing projects, commitments and context around a
// shared outcome. Links and dependencies never change the linked work's state.
package effort

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

type NoteRef struct {
	ID    int          `json:"id"`
	UID   string       `json:"uid"`
	Scope memory.Scope `json:"scope"`
}
type MemoryRef struct {
	ID    string       `json:"id"`
	Scope memory.Scope `json:"scope"`
}
type Dependency struct {
	NoteUID  string `json:"note_uid"`
	AfterUID string `json:"after_uid"`
}
type Effort struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	CreatedAt    time.Time      `json:"created_at"`
	ArchivedAt   *time.Time     `json:"archived_at"`
	Projects     []memory.Scope `json:"projects"`
	Notes        []NoteRef      `json:"notes"`
	Memories     []MemoryRef    `json:"memories"`
	Refs         []string       `json:"refs"`
	Dependencies []Dependency   `json:"dependencies"`
}

func (e Effort) Includes(s memory.Scope) bool { return slices.ContainsFunc(e.Projects, s.Same) }

type Doc struct {
	Version int      `json:"version"`
	NextID  int      `json:"next_id"`
	Efforts []Effort `json:"efforts"`
}

var file = store.V1(`{"version":1,"next_id":1,"efforts":[]}`)
var ErrNotFound = errors.New("no effort")

type ValidationError string

func (e ValidationError) Error() string { return string(e) }

func All(s *store.Store) ([]Effort, error) {
	d, err := store.Load[Doc](s, "efforts", file)
	if err != nil {
		return nil, err
	}
	if d.Efforts == nil {
		return []Effort{}, nil
	}
	return d.Efforts, nil
}
func find(d *Doc, name string) (*Effort, error) {
	for i := range d.Efforts {
		e := &d.Efforts[i]
		if e.ID == name || strings.EqualFold(e.Name, name) {
			return e, nil
		}
	}
	return nil, fmt.Errorf("%w %q", ErrNotFound, name)
}
func Get(s *store.Store, name string) (Effort, error) {
	es, err := All(s)
	if err != nil {
		return Effort{}, err
	}
	e, err := find(&Doc{Efforts: es}, name)
	if err != nil {
		return Effort{}, err
	}
	return *e, nil
}
func Create(s *store.Store, name string, now time.Time) (Effort, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || strings.ContainsAny(name, "\r\n") || strings.HasPrefix(name, "e:") {
		return Effort{}, false, ValidationError("effort name must be one line, 1 to 200 bytes, and cannot start with e:")
	}
	var result Effort
	existed := false
	_, err := store.Modify[Doc](s, "efforts", file, func(d *Doc) error {
		if e, err := find(d, name); err == nil {
			result, existed = *e, true
			return nil
		}
		result = Effort{ID: fmt.Sprintf("e:%d", d.NextID), Name: name, CreatedAt: now.UTC(), Projects: []memory.Scope{}, Notes: []NoteRef{}, Memories: []MemoryRef{}, Refs: []string{}, Dependencies: []Dependency{}}
		d.NextID++
		d.Efforts = append(d.Efforts, result)
		return nil
	})
	return result, existed, err
}
func modify(s *store.Store, name string, fn func(*Effort) error) error {
	_, err := store.Modify[Doc](s, "efforts", file, func(d *Doc) error {
		e, err := find(d, name)
		if err != nil {
			return err
		}
		return fn(e)
	})
	return err
}
func Archive(s *store.Store, name string, archived bool, now time.Time) error {
	return modify(s, name, func(e *Effort) error {
		if archived && e.ArchivedAt == nil {
			at := now.UTC()
			e.ArchivedAt = &at
		}
		if !archived {
			e.ArchivedAt = nil
		}
		return nil
	})
}

type Changes struct {
	Projects       []project.Project
	Notes          []int
	Memories, Refs []string
}

func Link(s *store.Store, c *config.Config, name string, changes Changes, remove bool) error {
	if len(changes.Projects)+len(changes.Notes)+len(changes.Memories)+len(changes.Refs) == 0 {
		return ValidationError("link or unlink needs --project, --note, --memory or --ref")
	}
	for _, ref := range changes.Refs {
		if strings.TrimSpace(ref) == "" || len(ref) > 2048 || strings.ContainsAny(ref, "\r\n") {
			return ValidationError("reference must be a nonempty line, at most 2048 bytes")
		}
	}
	// Resolve other files before locking efforts; no nested cross-file locks.
	var notes []NoteRef
	var memories []MemoryRef
	if !remove {
		for _, id := range changes.Notes {
			n, err := thread.Get(s, id)
			if err != nil {
				return err
			}
			notes = append(notes, NoteRef{ID: n.ID, UID: n.UID, Scope: memory.ForProject(c, project.Project{Path: n.Project, Remote: n.Remote})})
		}
		for _, id := range changes.Memories {
			r, err := memory.Get(s, id)
			if err != nil {
				return err
			}
			if r.ForgottenAt != nil {
				return ValidationError("cannot link a forgotten memory")
			}
			memories = append(memories, MemoryRef{ID: r.ID, Scope: r.Scope})
		}
	}
	return modify(s, name, func(e *Effort) error {
		if remove {
			for _, p := range changes.Projects {
				sc := memory.ForProject(c, p)
				e.Projects = slices.DeleteFunc(e.Projects, sc.Same)
			}
			e.Notes = slices.DeleteFunc(e.Notes, func(n NoteRef) bool { return slices.Contains(changes.Notes, n.ID) })
			e.Memories = slices.DeleteFunc(e.Memories, func(m MemoryRef) bool { return slices.Contains(changes.Memories, m.ID) })
			e.Refs = slices.DeleteFunc(e.Refs, func(r string) bool { return slices.Contains(changes.Refs, r) })
			e.Dependencies = slices.DeleteFunc(e.Dependencies, func(d Dependency) bool { return !hasNote(*e, d.NoteUID) || !hasNote(*e, d.AfterUID) })
			// A retained note or memory still associates its project with the effort.
			for _, n := range e.Notes {
				addProject(e, n.Scope)
			}
			for _, m := range e.Memories {
				addProject(e, m.Scope)
			}
			return nil
		}
		for _, p := range changes.Projects {
			addProject(e, memory.ForProject(c, p))
		}
		for _, n := range notes {
			if !hasNote(*e, n.UID) {
				e.Notes = append(e.Notes, n)
			}
			addProject(e, n.Scope)
		}
		for _, m := range memories {
			if !slices.ContainsFunc(e.Memories, func(r MemoryRef) bool { return r.ID == m.ID }) {
				e.Memories = append(e.Memories, m)
			}
			addProject(e, m.Scope)
		}
		for _, r := range changes.Refs {
			if !slices.Contains(e.Refs, r) {
				e.Refs = append(e.Refs, r)
			}
		}
		return nil
	})
}
func addProject(e *Effort, s memory.Scope) {
	if !e.Includes(s) {
		e.Projects = append(e.Projects, s)
	}
}
func hasNote(e Effort, uid string) bool {
	return slices.ContainsFunc(e.Notes, func(n NoteRef) bool { return n.UID == uid })
}

func Depend(s *store.Store, name string, note, after int, remove bool) error {
	return modify(s, name, func(e *Effort) error {
		var a, b string
		for _, n := range e.Notes {
			if n.ID == note {
				a = n.UID
			}
			if n.ID == after {
				b = n.UID
			}
		}
		if a == "" || b == "" || a == b {
			return ValidationError("dependency needs two different notes already linked to the effort")
		}
		d := Dependency{NoteUID: a, AfterUID: b}
		if remove {
			e.Dependencies = slices.DeleteFunc(e.Dependencies, func(old Dependency) bool { return old == d })
			return nil
		}
		if slices.Contains(e.Dependencies, d) {
			return nil
		}
		// Following prerequisites from b must never reach a.
		seen := map[string]bool{}
		var reaches func(string) bool
		reaches = func(uid string) bool {
			if uid == a {
				return true
			}
			if seen[uid] {
				return false
			}
			seen[uid] = true
			for _, old := range e.Dependencies {
				if old.NoteUID == uid && reaches(old.AfterUID) {
					return true
				}
			}
			return false
		}
		if reaches(b) {
			return ValidationError("dependency would create a cycle")
		}
		e.Dependencies = append(e.Dependencies, d)
		return nil
	})
}
