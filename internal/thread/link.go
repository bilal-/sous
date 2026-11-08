package thread

import (
	"errors"
	"fmt"
	"slices"

	"github.com/bilal-/sous/internal/store"
)

// Links are what a note can point at besides its text: Ref, an item that
// already exists in a tracker, and After, notes it waits on. For Unlink, a
// Ref of "*" forgets whichever ref the note has.
type Links struct {
	Ref   string
	After []int
}

// ErrLinked: the note already points at another tracker item.
var ErrLinked = errors.New("already linked")

// Link points note id at what links names. Linking what it already points
// at changes nothing (changed is false), so a retry is safe. A run has its
// work on its branch, not in a tracker, so it takes no ref. A wait must
// name another open note and must not make a loop.
func Link(s *store.Store, id int, l Links) (changed bool, err error) {
	_, err = store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error {
		t, err := d.find(id, false)
		if err != nil {
			return err
		}
		changed, err = d.link(t, l)
		return err
	})
	return changed, err
}

func (d *Doc) link(t *Thread, l Links) (bool, error) {
	changed := false
	if l.Ref != "" {
		switch {
		case t.Run != nil:
			return false, ValidationError(fmt.Sprintf("note %d is a run: its work is on its branch, not in a tracker", t.ID))
		case t.Ref != nil && *t.Ref == l.Ref:
		case t.Ref != nil:
			return false, fmt.Errorf("%w: note %d is linked to %s; sous unlink %d --ref first", ErrLinked, t.ID, *t.Ref, t.ID)
		default:
			ref := l.Ref
			t.Ref, changed = &ref, true
		}
	}
	for _, after := range l.After {
		if after == t.ID {
			return false, ValidationError(fmt.Sprintf("note %d cannot wait on itself", t.ID))
		}
		if _, err := d.find(after, false); err != nil {
			return false, err
		}
		if slices.Contains(t.After, after) {
			continue
		}
		if d.reaches(after, t.ID) {
			return false, ValidationError(fmt.Sprintf("note %d already waits on %d, so %d cannot wait on it", after, t.ID, t.ID))
		}
		t.After, changed = append(t.After, after), true
	}
	return changed, nil
}

// reaches: note from waits on note to, directly or through others.
func (d *Doc) reaches(from, to int) bool {
	seen := map[int]bool{}
	next := []int{from}
	for len(next) > 0 {
		id := next[0]
		next = next[1:]
		if id == to {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		for i := range d.Threads {
			if d.Threads[i].ID == id {
				next = append(next, d.Threads[i].After...)
			}
		}
	}
	return false
}

// Unlink forgets what links names; what the note did not point at is
// ignored, so a retry is safe. Forgetting a ref leaves the tracker item as
// it is: sous only stops following it.
func Unlink(s *store.Store, id int, l Links) (changed bool, err error) {
	err = touch(s, id, func(t *Thread) {
		if l.Ref != "" && t.Ref != nil && (l.Ref == "*" || l.Ref == *t.Ref) {
			t.Ref, changed = nil, true
		}
		for _, after := range l.After {
			if i := slices.Index(t.After, after); i >= 0 {
				t.After, changed = slices.Delete(t.After, i, i+1), true
			}
		}
		if len(t.After) == 0 {
			t.After = nil
		}
	})
	return changed, err
}
