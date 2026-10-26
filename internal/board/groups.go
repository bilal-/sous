package board

import (
	"fmt"
	"strings"

	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

// runGroups are the board's runs, out of on you and on others and into the
// order the person acts on them: what needs an answer, what is moving, and
// what waits for review. A failed run stays on you, with the rest of what
// needs looking at.
type runGroups struct {
	needsYou, inProgress, ready []Row
}

// groupRuns moves s's runs into their groups, each row reading as its group
// shows it: the note, then what the run says beyond its group's name.
func groupRuns(s *Sections) runGroups {
	var g runGroups
	keep := func(rows []Row) []Row {
		var out []Row
		for _, r := range rows {
			switch {
			case r.Run == nil || r.Snoozed:
				out = append(out, r)
			case r.Run.State == thread.RunNeedsYou:
				r.Shown = r.Text + groupDetail(r.Run)
				g.needsYou = append(g.needsYou, r)
			case r.Run.State.Working():
				r.Shown = r.Text + groupDetail(r.Run)
				g.inProgress = append(g.inProgress, r)
			case r.Run.State == thread.RunDone:
				r.Shown = r.Text + groupDetail(r.Run)
				g.ready = append(g.ready, r)
			default:
				out = append(out, r)
			}
		}
		return out
	}
	s.Me, s.Them = keep(s.Me), keep(s.Them)
	return g
}

// headline is the board's counts when nothing is missing: every group and
// section that holds something, the empty ones left out. With a gap the
// board says so instead, with every count, so nothing missing reads as zero.
func headline(g runGroups, s Sections) string {
	parts := strings.TrimSuffix(g.counts(), " · ")
	for _, c := range []struct {
		n    int
		what string
	}{{len(s.Me), "on you"}, {len(s.Them), "on others"}, {len(s.Unfinished), "unfinished"}} {
		if c.n > 0 {
			if parts != "" {
				parts += " · "
			}
			parts += fmt.Sprintf("%d %s", c.n, c.what)
		}
	}
	return parts
}

func (g runGroups) total() int { return len(g.needsYou) + len(g.inProgress) + len(g.ready) }

// counts is the headline's start for the runs: "1 needs you · 2 running · ",
// leaving out the groups that are empty.
func (g runGroups) counts() string {
	var b strings.Builder
	for _, c := range []struct {
		n    int
		what string
	}{{len(g.needsYou), "needs you"}, {len(g.inProgress), "running"}, {len(g.ready), "ready"}} {
		if c.n > 0 {
			fmt.Fprintf(&b, "%d %s · ", c.n, c.what)
		}
	}
	return b.String()
}

// phaseWords is how a phase reads beside a run in progress.
var phaseWords = map[thread.RunPhase]string{
	thread.PhaseQueued: "queued", thread.PhasePlanning: "planning", thread.PhaseWorking: "working",
	thread.PhaseReviewing: "reviewing", thread.PhaseBlocked: "blocked",
}

// groupDetail is what follows a run's note in its group: its question, how
// far it has got, or what its result holds. Plain words, never the
// runner's own terms.
func groupDetail(r *thread.Run) string {
	var parts []string
	switch {
	case r.State == thread.RunNeedsYou:
		if q := r.Asked(); q != nil && q.Text != "" {
			parts = append(parts, fmt.Sprintf("%q", q.Text))
		}
	case r.State.Working():
		step := phaseWords[r.Phase]
		switch {
		case r.Phase == thread.PhaseBlocked && r.Reason != "":
			step = "blocked: " + r.Reason
		case r.Progress != nil && r.Progress.Step != "":
			step = r.Progress.Step
		case step == "" && r.State == thread.RunStarting:
			step = "starting"
		case step == "":
			step = "running"
		}
		parts = append(parts, step)
		if p := r.Progress; p != nil && p.Total > 0 {
			parts = append(parts, fmt.Sprintf("%d of %d", p.Done, p.Total))
		}
	case r.State == thread.RunDone:
		if pub := r.Publication; pub != nil && pub.Stage == thread.PubPublished {
			parts = append(parts, "draft PR open")
			break
		}
		if res := r.Result; res != nil {
			if res.Files != nil {
				parts = append(parts, plural(res.Files.Changed, "file"))
			}
			if c := checksWord(res.Checks); c != "" {
				parts = append(parts, c)
			}
			break // sous show has the branch and the rest
		}
		if r.Text != "" {
			parts = append(parts, r.Text)
		}
		if r.Branch != "" {
			parts = append(parts, r.Branch)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return text.Suffix(strings.Join(parts, " · "))
}

// checksWord sums up a result's checks: "tests passed", "1 check failed",
// "checks not run". "" when there are none.
func checksWord(checks []thread.RunCheck) string {
	if len(checks) == 0 {
		return ""
	}
	failed, notRun := 0, 0
	for _, c := range checks {
		switch c.Status {
		case "passed":
		case "failed":
			failed++
		default:
			notRun++
		}
	}
	switch {
	case failed > 0:
		return plural(failed, "check") + " failed"
	case notRun == len(checks):
		return "checks not run"
	}
	return "checks passed" // sous show lists any not run
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// groupLabel is a run's state in the board's words, for sous show.
func groupLabel(r *thread.Run) string {
	switch {
	case r.State == thread.RunNeedsYou:
		return "needs you"
	case r.State.Working():
		return "in progress"
	case r.State == thread.RunDone && r.Publication != nil && r.Publication.Stage == thread.PubPublished:
		return "published"
	case r.State == thread.RunDone:
		return "ready to review"
	}
	return look(r).label
}
