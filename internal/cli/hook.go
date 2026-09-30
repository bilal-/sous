package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/hook"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/thread"
)

// sessionIntro opens what an agent sees at session start, so it knows what
// sous is and when to reach for it, not only what sous printed.
const sessionIntro = "[sous] The user's list of what is waiting on them across projects. Below: where they left off here. Run sous help for how to use it."

// cmdHook: `sous hook session-start|session-end <agent>`. Always exit 0, stderr silent.
func cmdHook(e *Env, a argv) int {
	args := a.pos
	if len(args) != 2 {
		return 0
	}
	in, ok := hook.Parse(e.Stdin)
	if !ok {
		return 0
	}
	switch args[0] {
	case hook.RoleStart:
		// Plugins and tracker probes must finish inside the guard; when it
		// fires first, cancelling kills their process groups.
		var buf bytes.Buffer
		sub := e.child(&buf, io.Discard, true)
		sub.PluginTimeout = 2 * time.Second
		sub.Deadline = time.Now().Add(4 * time.Second)
		sub.ctx()
		defer sub.close()
		var runs string
		if guarded(func() {
			if root, ok := hook.StartRoot(in, e.Cwd, e.UserHome); ok {
				cmdHere(sub, argv{pos: []string{root}})
			}
			if hook.Fresh(in) {
				runs = runsWaiting(sub)
			}
		}) && buf.Len()+len(runs) > 0 {
			fmt.Fprintln(e.Stdout, sessionIntro)
			fmt.Fprint(e.Stdout, runs)
			io.Copy(e.Stdout, &buf)
		}
	case hook.RoleEnd:
		guarded(func() { hook.RecordEnd(e.store(), in, args[1], e.Cwd, e.UserHome, time.Now()) })
	}
	return 0
}

// runsWaiting lists the runs, in every project, that are done, need the
// user, or failed: what an agent should hear about first. Only the built
// in runners are asked (the hook must stay quick); the rest show as last
// known.
func runsWaiting(e *Env) string {
	views, err := thread.Runs(e.store())
	if err != nil || len(views) == 0 {
		return ""
	}
	views = e.dispatcher().Refresh(e.ctx(), views, true)
	var b strings.Builder
	for _, v := range views {
		name := project.Describe(v.Project).Name
		switch v.Run.State {
		case "needs_you":
			fmt.Fprintf(&b, "  %d %s: needs you · %s · answer with: sous reply %d \"<answer>\"\n", v.ID, name, v.Run.Text, v.ID)
		case "done":
			fmt.Fprintf(&b, "  %d %s: done, review it%s · then: sous done %d\n", v.ID, name, board.Suffix(v.Run.Branch), v.ID)
		case "failed":
			fmt.Fprintf(&b, "  %d %s: failed%s · sous show %d\n", v.ID, name, board.Suffix(v.Run.Text), v.ID)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "[sous] Runs waiting on the user:\n" + b.String()
}

// guarded runs fn but gives up after hookGuard, so a slow git, a huge
// transcript or a busy lock never holds up the agent. It reports whether
// fn finished in time.
func guarded(fn func()) bool {
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
		return true
	case <-time.After(hookGuard):
		return false
	}
}

// hookGuard is how long a hook may take before the agent carries on.
const hookGuard = 5 * time.Second
