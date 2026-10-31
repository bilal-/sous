package runs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/bilal-/sous/internal/store"
)

// An op names one request, with its input binding retained for good.
const (
	Done        = "done"
	AlreadyDone = "already_done"
	InProgress  = "in_progress"
	Refused     = "refused"
	Conflict    = "conflict"
	Failed      = "failed"
	NotReady    = "not_ready"
)

type Outcome struct {
	Outcome string `json:"outcome"`
	ID      string `json:"id,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type opRecord struct {
	Action     string    `json:"action"`
	Hash       string    `json:"hash"`
	At         time.Time `json:"at"`
	Generation string    `json:"generation,omitempty"`
	Final      *Outcome  `json:"final,omitempty"`
}
type opsDoc struct {
	Version int                 `json:"version"`
	Ops     map[string]opRecord `json:"ops"`
}

const opsFile = "ops"

type opMigrator struct{}

func (opMigrator) Current() int  { return 2 }
func (opMigrator) Empty() []byte { return []byte(`{"version":2,"ops":{}}`) }
func (opMigrator) Migrate(from int, raw []byte) ([]byte, error) {
	if from != 1 {
		return nil, fmt.Errorf("unknown version %d", from)
	}
	var d opsDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	d.Version = 2
	return json.Marshal(d)
}

var opsMigrator = opMigrator{}
var opFormat = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type Ops struct {
	Store *store.Store
	Now   func() time.Time
}

// Do holds ownership through the request, independently of wall-clock age.
// A retryable failure retains its input binding and permits the same request
// to retry once its owner releases the lock. Actions own recovery of their
// effects if a process dies between applying them and recording the outcome.
func (o Ops) Do(op, action, hash string, do func() Outcome) Outcome {
	if !opFormat.MatchString(op) {
		return Outcome{Outcome: Refused, Reason: "bad_input"}
	}
	var out Outcome
	acquired, err := o.Store.TryLocked("op-owner-"+op, func() error {
		generation, early, err := o.begin(op, action, hash)
		if err != nil {
			return err
		}
		if early != nil {
			out = *early
			return nil
		}
		out = do()
		if err := o.finish(op, generation, out); err != nil {
			if out.Outcome == Done || out.Outcome == AlreadyDone {
				out.Reason = "op_not_recorded: " + err.Error()
				return nil
			}
			return err
		}
		return nil
	})
	if err != nil {
		return Outcome{Outcome: Failed, Reason: "op_record: " + err.Error()}
	}
	if acquired {
		return out
	}
	d, err := store.Load[opsDoc](o.Store, opsFile, opsMigrator)
	if err != nil {
		return Outcome{Outcome: Failed, Reason: "op_record: " + err.Error()}
	}
	if r, ok := d.Ops[op]; ok {
		if early := recorded(r, action, hash); early != nil {
			return *early
		}
	}
	return Outcome{Outcome: InProgress}
}

func recorded(r opRecord, action, hash string) *Outcome {
	if r.Action != action || r.Hash != hash {
		return &Outcome{Outcome: Refused, Reason: "op_reused"}
	}
	if r.Final != nil {
		out := *r.Final
		if out.Outcome == Done {
			out.Outcome = AlreadyDone
		}
		return &out
	}
	return nil
}

// begin is called while holding this op's owner lock. A pending current
// reservation has lost its earlier owner; an unresolved legacy one cannot
// establish that and must be inspected before initiating another action.
func (o Ops) begin(op, action, hash string) (string, *Outcome, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", nil, err
	}
	generation := hex.EncodeToString(token[:])
	var early *Outcome
	_, err := store.Modify[opsDoc](o.Store, opsFile, opsMigrator, func(d *opsDoc) error {
		if d.Ops == nil {
			d.Ops = map[string]opRecord{}
		}
		if r, ok := d.Ops[op]; ok {
			early = recorded(r, action, hash)
			if early != nil {
				return nil
			}
			if r.Generation == "" {
				early = &Outcome{Outcome: NotReady, Reason: "legacy_operation"}
				return nil
			}
		}
		d.Ops[op] = opRecord{Action: action, Hash: hash, At: o.Now().UTC(), Generation: generation}
		return nil
	})
	return generation, early, err
}

func (o Ops) finish(op, generation string, out Outcome) error {
	_, err := store.Modify[opsDoc](o.Store, opsFile, opsMigrator, func(d *opsDoc) error {
		r, ok := d.Ops[op]
		if !ok || generation == "" || r.Generation != generation {
			return fmt.Errorf("operation ownership changed")
		}
		switch {
		case out.Outcome == Done, out.Outcome == AlreadyDone, out.Outcome == Conflict, out.Outcome == Refused && out.Reason == "bad_input":
			r.Final = &out
		}
		d.Ops[op] = r
		return nil
	})
	return err
}
