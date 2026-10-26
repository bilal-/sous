package runs

import (
	"fmt"
	"regexp"
	"time"

	"github.com/bilal-/sous/internal/store"
)

// An op is one action a host asked for, named by an id the host made. The
// same op with the same input is answered the same way forever, so a host
// that lost an answer can ask again without the action happening twice.

// The outcomes an op can have.
const (
	Done        = "done"
	AlreadyDone = "already_done" // done before; this is that answer again
	InProgress  = "in_progress"  // the same op is being done right now
	Refused     = "refused"
	Conflict    = "conflict" // what it names moved on
	Failed      = "failed"   // tried and failed; the same op may be tried again
	NotReady    = "not_ready"
)

// Outcome is what an op came to: the note it made or acted on, and for a
// refusal, conflict or failure, why in one word.
type Outcome struct {
	Outcome string `json:"outcome"`
	ID      string `json:"id,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// opRecord is one op as kept: what was asked (Hash, over the action and
// its input) and, once final, how it came out. A reservation without an
// outcome means it is being done, or sous stopped half way.
type opRecord struct {
	Action string    `json:"action"`
	Hash   string    `json:"hash"`
	At     time.Time `json:"at"`
	Final  *Outcome  `json:"final,omitempty"`
}

type opsDoc struct {
	Version int                 `json:"version"`
	Ops     map[string]opRecord `json:"ops"`
}

const opsFile = "ops"

var opsMigrator = store.V1(`{"version":1,"ops":{}}`)

// Ops keeps the ops hosts asked for. Records are a few dozen bytes and kept
// for good: an op forgotten could be done twice.
type Ops struct {
	Store *store.Store
	Now   func() time.Time
}

// opFormat is what a host may use as an op id.
var opFormat = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

// inProgressFor is how long a reservation counts as being done. A start
// has twice the runner's time limit (startGrace); after that the sous
// doing it has stopped, and the op is done again: every action under an op
// is safe to repeat on its own.
const inProgressFor = startGrace

// Do does an op once. A repeat with the same action and hash gets the
// recorded outcome (done becomes already_done) without calling do; with
// another action or hash it is refused (op_reused). Done, conflict and a
// refusal of the input itself are final: they come out the same every
// time. Anything else (a failure, a refusal that depends on settings, not
// set up) forgets the op, so the same op may be tried again.
func (o Ops) Do(op, action, hash string, do func() Outcome) Outcome {
	if !opFormat.MatchString(op) {
		return Outcome{Outcome: Refused, Reason: "bad_input"}
	}
	early, err := o.begin(op, action, hash)
	if err != nil {
		return Outcome{Outcome: Failed, Reason: "op_record: " + err.Error()}
	}
	if early != nil {
		return *early
	}
	out := do()
	if err := o.finish(op, out); err != nil && out.Outcome == Done {
		// Done, but not recorded: say so, as a repeat would do it again.
		out.Reason = fmt.Sprintf("op_not_recorded: %v", err)
	}
	return out
}

// begin reserves op, or says how an earlier one with it came out.
func (o Ops) begin(op, action, hash string) (*Outcome, error) {
	var early *Outcome
	_, err := store.Modify[opsDoc](o.Store, opsFile, opsMigrator, func(d *opsDoc) error {
		if d.Ops == nil {
			d.Ops = map[string]opRecord{}
		}
		now := o.Now()
		if r, ok := d.Ops[op]; ok {
			switch {
			case r.Action != action || r.Hash != hash:
				early = &Outcome{Outcome: Refused, Reason: "op_reused"}
				return nil
			case r.Final != nil:
				out := *r.Final
				if out.Outcome == Done {
					out.Outcome = AlreadyDone
				}
				early = &out
				return nil
			case now.Sub(r.At) < inProgressFor:
				early = &Outcome{Outcome: InProgress}
				return nil
			}
		}
		d.Ops[op] = opRecord{Action: action, Hash: hash, At: now.UTC()}
		return nil
	})
	return early, err
}

// reserve records op as being done, for tests of a sous that stopped.
func (o Ops) reserve(op, action, hash string) {
	o.begin(op, action, hash)
}

// finish records a final outcome, or forgets the op so it can be tried again.
func (o Ops) finish(op string, out Outcome) error {
	_, err := store.Modify[opsDoc](o.Store, opsFile, opsMigrator, func(d *opsDoc) error {
		r, ok := d.Ops[op]
		if !ok {
			return nil
		}
		switch {
		case out.Outcome == Done, out.Outcome == Conflict, out.Outcome == Refused && out.Reason == "bad_input":
			final := out
			r.Final = &final
			d.Ops[op] = r
		default:
			delete(d.Ops, op)
		}
		return nil
	})
	return err
}
