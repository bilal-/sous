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
	Did  string   `json:"did"` // see docs/commands.md: noted, already_noted, edited, kind, snoozed, closed, already_closed, filed, replied, started, already_started
	Ref  *string  `json:"ref"` // where it is filed, when it is
	Next []string `json:"next"`
	// Run: for go --run, the run it started or found.
	Run *board.RunItem `json:"run,omitempty"`
	// Error: part of it could not be done (noted, but not filed: why).
	Error string `json:"error,omitempty"`
}

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

// noteNext is what makes sense next for note id: seeing it in full, then
// what its state allows. Only what can be read is suggested.
func noteNext(e *Env, id int) []string {
	th, err := thread.Get(e.store(), id)
	if err != nil {
		return []string{}
	}
	return append([]string{fmt.Sprintf("sous show %d", id)}, board.Next(thread.View{Thread: th})...)
}
