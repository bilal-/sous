package cli

import (
	"fmt"
	"strings"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/thread"
)

// changedJSON is what a command that changed something answers with
// --json: what it did, to which note or row, and the commands that make
// sense next. Retrying a change gives the same answer.
type changedJSON struct {
	ID   string   `json:"id"`
	Did  string   `json:"did"` // one of dids
	Ref  *string  `json:"ref"` // where it is filed, when it is
	Next []string `json:"next"`
	// Run: for go --run, the run it started or found.
	Run *board.RunItem `json:"run,omitempty"`
	// Error: part of it could not be done (noted, but not filed: why).
	Error string `json:"error,omitempty"`
}

// What a change did, as its answer's did says. docs/commands.md lists them
// all (a test holds it to this list).
const (
	didNoted          = "noted"
	didAlreadyNoted   = "already_noted"
	didEdited         = "edited"
	didKind           = "kind"
	didSnoozed        = "snoozed"
	didClosed         = "closed"
	didAlreadyClosed  = "already_closed"
	didFiled          = "filed"
	didReplied        = "replied"
	didStarted        = "started"
	didAlreadyStarted = "already_started"
)

var dids = []string{didNoted, didAlreadyNoted, didEdited, didKind, didSnoozed, didClosed, didAlreadyClosed, didFiled, didReplied, didStarted, didAlreadyStarted}

// changed prints c: as JSON, or as what it did and what to do next on
// one line.
func (e *Env) changed(c changedJSON, said string) int {
	if e.JSON {
		return e.writeJSON(c)
	}
	if len(c.Next) > 0 {
		said += " · " + strings.Join(c.Next, " · ")
	}
	fmt.Fprintln(e.Stdout, said)
	return 0
}

// noteNext is what makes sense next for note id, as board.Next says.
func noteNext(e *Env, id int) []string {
	th, err := thread.Get(e.store(), id)
	if err != nil {
		return []string{}
	}
	return board.Next(thread.View{Thread: th})
}
