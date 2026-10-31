package signal

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/bilal-/sous/internal/store"
)

// ErrSelector marks an ambiguous or too-short signal selector.
var ErrSelector = errors.New("invalid signal selector")

type selectorError string

func (e selectorError) Error() string        { return string(e) }
func (e selectorError) Is(target error) bool { return target == ErrSelector }

var ErrOpReused = errors.New("operation reused with different input")

type snoozeReceipt struct {
	Target string `json:"target"`
	ID     string `json:"id"`
	Hash   string `json:"hash"`
}
type observationMigrator struct{}

func (observationMigrator) Current() int  { return 2 }
func (observationMigrator) Empty() []byte { return []byte(`{"version":2,"signals":{}}`) }
func (observationMigrator) Migrate(from int, raw []byte) ([]byte, error) {
	if from != 1 {
		return nil, fmt.Errorf("unknown version %d", from)
	}
	var d ObsDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	d.Version = 2
	return json.Marshal(d)
}

func (d *ObsDoc) resolve(id string) (string, error) {
	if _, ok := d.Signals[id]; ok {
		return id, nil
	}
	var hits []string
	for full := range d.Signals {
		if strings.HasPrefix(full, id) {
			hits = append(hits, full)
		}
	}
	sort.Strings(hits)
	switch {
	case len(id) < len(IDPrefix)+3:
		return "", selectorError(fmt.Sprintf("%s is too short; give at least 3 characters of the id after s:", id))
	case len(hits) == 0:
		return "", fmt.Errorf("%w: %s", ErrUnknownSignal, id)
	case len(hits) > 1:
		return "", selectorError(fmt.Sprintf("%s matches %d rows: %s", id, len(hits), strings.Join(hits, ", ")))
	}
	return hits[0], nil
}
func (d *ObsDoc) snooze(id string) {
	e := d.Signals[id]
	h := e.Hash
	e.SnoozedHash = &h
	d.Signals[id] = e
}

// Snooze hides a signal until its content changes. A unique prefix suffices.
func Snooze(s *store.Store, id string) error {
	_, err := store.Modify[ObsDoc](s, "observed", ObsFile, func(d *ObsDoc) error {
		full, err := d.resolve(id)
		if err != nil {
			return err
		}
		d.snooze(full)
		return nil
	})
	return err
}

// SnoozeOnce binds the request to its original selector, full ID and hash in
// the same replacement as the snooze. Replay precedes resolution: a changed,
// removed or pruned observation must not hide new work on a retry.
func SnoozeOnce(s *store.Store, target, op string) (string, bool, error) {
	var id string
	var already bool
	_, err := store.Modify[ObsDoc](s, "observed", ObsFile, func(d *ObsDoc) error {
		if op == "" {
			return fmt.Errorf("operation is empty")
		}
		if r, ok := d.SnoozeOps[op]; ok {
			if r.Target != target {
				return ErrOpReused
			}
			id, already = r.ID, true
			return nil
		}
		var err error
		id, err = d.resolve(target)
		if err != nil {
			return err
		}
		d.snooze(id)
		if d.SnoozeOps == nil {
			d.SnoozeOps = map[string]snoozeReceipt{}
		}
		d.SnoozeOps[op] = snoozeReceipt{Target: target, ID: id, Hash: d.Signals[id].Hash}
		return nil
	})
	return id, already, err
}
