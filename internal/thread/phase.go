package thread

import "time"

// RunPhase is where a run is, in more detail than its state: what a runner
// that describes itself as speaking phases reports beside the state. The
// set is open; a phase sous does not know is shown as reported.
type RunPhase string

const (
	PhaseQueued    RunPhase = "queued"    // waiting for its turn
	PhasePlanning  RunPhase = "planning"  // working out the tasks
	PhaseWorking   RunPhase = "working"   // doing them
	PhaseReviewing RunPhase = "reviewing" // checking the work
	PhaseBlocked   RunPhase = "blocked"   // stuck on the computer: rate limited, signed out; Reason says which
	PhaseNeedsYou  RunPhase = "needs_you" // asked the person a question
	PhaseReady     RunPhase = "ready"     // its result passed its checks and waits for review
	PhaseApproved  RunPhase = "approved"  // the person approved its result
	PhaseFailed    RunPhase = "failed"
	PhaseStopped   RunPhase = "stopped"   // stopped on request
	PhaseDiscarded RunPhase = "discarded" // its result was turned down
)

// State is the state a runner reports beside a known phase, so a host that
// knows only states still reads it right. Unknown phases have none.
func (p RunPhase) State() (RunState, bool) {
	switch p {
	case PhaseQueued, PhasePlanning, PhaseWorking, PhaseReviewing, PhaseBlocked:
		return RunRunning, true
	case PhaseNeedsYou:
		return RunNeedsYou, true
	case PhaseReady, PhaseApproved:
		return RunDone, true
	case PhaseFailed, PhaseStopped, PhaseDiscarded:
		return RunFailed, true
	}
	return "", false
}

// Working: the run is still at it, so it can still change its work.
func (p RunPhase) Working() bool {
	return p == PhaseQueued || p == PhasePlanning || p == PhaseWorking || p == PhaseReviewing
}

// RunProgress is how far a run has got: tasks done of total, and the
// current step in plain words.
type RunProgress struct {
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Step  string `json:"step,omitempty"`
}

// RunQuestion is what a run asked. A reply names ID and Token, so an answer
// typed before the question changed never answers the new one.
type RunQuestion struct {
	ID        string     `json:"id"`
	Token     string     `json:"token"`
	Text      string     `json:"text"`
	Choices   []string   `json:"choices,omitempty"`
	Other     bool       `json:"other,omitempty"` // free text is accepted beside the choices
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// RunResult is the work a run offers for review. Commit never changes once
// reported: new work is a new commit and a new result.
type RunResult struct {
	Summary string     `json:"summary,omitempty"` // the runner's words
	Base    string     `json:"base"`              // the commit the work started from
	Commit  string     `json:"commit"`            // exactly what is reviewed and approved
	Files   *RunFiles  `json:"files,omitempty"`
	Checks  []RunCheck `json:"checks,omitempty"` // as the runner reports them
	Review  *RunReview `json:"review,omitempty"` // as the runner reports it
}

// RunFiles counts what a result changed.
type RunFiles struct {
	Changed      int `json:"changed"`
	Added        int `json:"added"`
	Removed      int `json:"removed"`
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

// RunCheck is one check the runner ran on a result.
type RunCheck struct {
	Name     string `json:"name"`
	Status   string `json:"status"` // passed | failed | not_run
	Required bool   `json:"required,omitempty"`
}

// RunReview is who reviewed a result, as the runner reports it.
type RunReview struct {
	By          string `json:"by"`
	Independent bool   `json:"independent"` // a different vendor from the one that did the work
}
