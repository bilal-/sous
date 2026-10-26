package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/thread"
)

// checkLimit bounds a project's check command, run once on finished work.
const checkLimit = 30 * time.Minute

// runCheck runs the project's check command in the run's worktree, when
// its settings name one and the agent committed work to check.
func runCheck(m runMeta) []thread.RunCheck {
	if m.Check == "" || m.Base == "" || uncommitted(m.Worktree) || head(m.Worktree) == m.Base {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", m.Check)
	cmd.Dir, cmd.Env = m.Worktree, agentEnv(m)
	out, err := cmd.CombinedOutput()
	c := thread.RunCheck{Name: m.Check, Status: "passed", Required: true}
	if err != nil {
		c.Status = "failed"
		fmt.Fprintf(os.Stderr, "check %q failed: %v\n%s", m.Check, err, lastBytes(out, 2048)) // into watcher.log
	}
	return []thread.RunCheck{c}
}

func lastBytes(b []byte, n int) []byte { return b[max(len(b)-n, 0):] }

// head is a worktree's current commit, "" when it cannot be read.
func head(worktree string) string {
	out, err := project.Git(worktree, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// describeRun adds the phase and the result to a finished run's status:
// ready, with the committed work as its result, unless a required check
// failed; approved or discarded once the person said so.
func describeRun(st *Status, dir string, m runMeta, r result) {
	switch st.State {
	case NeedsYou:
		st.Phase = thread.PhaseNeedsYou
		return
	case Failed:
		st.Phase = thread.PhaseFailed
		if r.Stopped {
			st.Phase = thread.PhaseStopped
		}
		return
	case Running:
		st.Phase = thread.PhaseWorking
		return
	}
	st.Phase = thread.PhaseReady
	if m.Base != "" && !uncommitted(m.Worktree) {
		if h := head(m.Worktree); h != "" && h != m.Base {
			st.Result = &thread.RunResult{Base: m.Base, Commit: h, Files: numstat(m.Worktree, m.Base, h), Checks: r.Checks}
		}
	}
	for _, c := range r.Checks {
		if c.Required && c.Status == "failed" {
			st.State, st.Phase, st.Text = Failed, thread.PhaseFailed, "checks failed: "+c.Name
			return
		}
	}
	var mark struct {
		Commit string `json:"commit"`
	}
	switch {
	case readJSON(dir, "discarded.json", &mark) == nil:
		st.State, st.Phase, st.Text = Failed, thread.PhaseDiscarded, "discarded"
	case readJSON(dir, "approved.json", &mark) == nil && st.Result != nil && mark.Commit == st.Result.Commit:
		st.Phase = thread.PhaseApproved
	}
}

// numstat counts what changed between two commits.
func numstat(worktree, base, commit string) *thread.RunFiles {
	out, err := project.Git(worktree, "diff", "--numstat", "--summary", base, commit)
	if err != nil {
		return nil
	}
	var f thread.RunFiles
	for _, l := range strings.Split(out, "\n") {
		switch fields := strings.Fields(l); {
		case len(fields) >= 3 && fields[0] != "create" && fields[0] != "delete" && fields[0] != "mode":
			f.Changed++
			a, _ := strconv.Atoi(fields[0])
			d, _ := strconv.Atoi(fields[1])
			f.LinesAdded, f.LinesRemoved = f.LinesAdded+a, f.LinesRemoved+d
		case len(fields) >= 2 && fields[0] == "create":
			f.Added++
		case len(fields) >= 2 && fields[0] == "delete":
			f.Removed++
		}
	}
	return &f
}

// Approval is approve's answer: the branch that holds the approved commit,
// for sous to push, and the commit it started from.
type Approval struct {
	V      int    `json:"v"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Base   string `json:"base"`
}

// Approve records that the person approved commit, when it is still the
// run's result; another commit is a conflict. It never pushes. Approving
// the same commit twice gives the same answer.
func (a *Agent) Approve(_, ref, commit string) (Approval, error) {
	dir, m, err := a.meta(ref)
	if err != nil {
		return Approval{}, err
	}
	st, err := a.Status("", ref)
	switch {
	case err != nil:
		return Approval{}, err
	case st.Result == nil || (st.Phase != thread.PhaseReady && st.Phase != thread.PhaseApproved):
		return Approval{}, fmt.Errorf("%w: run is %s, with no result to approve", ErrConflict, st.Phase)
	case st.Result.Commit != commit:
		return Approval{}, fmt.Errorf("%w: the result is now %s, not %s", ErrConflict, st.Result.Commit, commit)
	}
	if err := writeJSON(dir, "approved.json", map[string]string{"commit": commit}); err != nil {
		return Approval{}, err
	}
	return Approval{Branch: m.Branch, Commit: commit, Base: m.Base}, nil
}

// Discard turns the run's result down; its work stays until clean.
func (a *Agent) Discard(_, ref string) error {
	dir, _, err := a.meta(ref)
	if err != nil {
		return err
	}
	return writeJSON(dir, "discarded.json", map[string]string{})
}
