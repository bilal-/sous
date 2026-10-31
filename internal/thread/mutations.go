package thread

import (
	"errors"
	"time"

	"github.com/bilal-/sous/internal/store"
)

// ErrOpReused means an operation already names another private mutation.
var ErrOpReused = errors.New("operation reused with different input")

type mutationReceipt struct {
	Action string    `json:"action"`
	ID     int       `json:"id"`
	UID    string    `json:"uid"`
	Days   int       `json:"days,omitempty"`
	At     time.Time `json:"at"`
}

func (d *Doc) mutationRecorded(op, action string, id, days int) (bool, error) {
	if op == "" {
		return false, ValidationError("operation is empty")
	}
	r, ok := d.MutationOps[op]
	if ok && (r.Action != action || r.ID != id || r.Days != days) {
		return false, ErrOpReused
	}
	return ok, nil
}
func (d *Doc) recordMutation(op, action string, t *Thread, days int, at time.Time) {
	if d.MutationOps == nil {
		d.MutationOps = map[string]mutationReceipt{}
	}
	d.MutationOps[op] = mutationReceipt{Action: action, ID: t.ID, UID: t.UID, Days: days, At: at}
}

// SnoozeOnce records the first deadline with the mutation, so recovering an
// interrupted request never extends it or undoes a later user action.
func SnoozeOnce(s *store.Store, id, days int, now time.Time, op string) (bool, error) {
	if days <= 0 {
		return false, ValidationError("snooze days must be positive")
	}
	var already bool
	_, err := store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error {
		var err error
		already, err = d.mutationRecorded(op, "snooze", id, days)
		if err != nil || already {
			return err
		}
		t, err := d.find(id, false)
		if err != nil {
			return err
		}
		until := now.UTC().Add(time.Duration(days) * 24 * time.Hour)
		t.SnoozedUntil = &until
		d.recordMutation(op, "snooze", t, days, until)
		return nil
	})
	return already, err
}

// DoneRecorded lets an interrupted closure recover before requiring the
// runner or its configuration again. DoneOnce repeats this under the lock.
func DoneRecorded(s *store.Store, id int, op string) (bool, error) {
	d, err := store.Load[Doc](s, name, Migrator{})
	if err != nil {
		return false, err
	}
	return d.mutationRecorded(op, "done", id, 0)
}

// DoneOnce closes only the private note. An existing closure keeps its time
// and provenance, and a replay never writes an upstream task or filed note.
func DoneOnce(s *store.Store, id int, now time.Time, op string) (bool, error) {
	var already bool
	_, err := store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error {
		var err error
		already, err = d.mutationRecorded(op, "done", id, 0)
		if err != nil || already {
			return err
		}
		t, err := d.find(id, true)
		if err != nil {
			return err
		}
		if t.Closed == nil {
			at := now.UTC()
			t.Closed = &at
		} else {
			already = true
		}
		d.recordMutation(op, "done", t, 0, *t.Closed)
		return nil
	})
	return already, err
}
