package cli

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/hook"
	"github.com/bilal-/sous/internal/thread"
)

// sessionIntro opens what an agent sees at session start, so it knows what
// sous is and when to reach for it, not only what sous printed. It names
// the sous the agent can run: the one on PATH, else this one by its path.
func sessionIntro(e *Env) string {
	sous := "sous"
	if onPath, err := exec.LookPath("sous"); err != nil || stableExe(e.Exe) != onPath {
		sous = e.Exe
	}
	return "[sous] The user's list of what is waiting on them across projects. Run " + sous + " help for how to use it."
}

// cmdHook: `sous hook session-start|session-end <agent>`. Always exit 0, stderr silent.
func cmdHook(e *Env, a argv) int {
	args := a.pos
	if len(args) != 2 {
		return 0
	}
	h, ok := harness.Find(args[1])
	if !ok {
		return 0
	}
	stdin, _ := io.ReadAll(io.LimitReader(e.Stdin, 1<<20))
	in := h.Parse(stdin)
	say := ""
	switch args[0] {
	case harness.RoleStart:
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
			} else if in.Fresh {
				// Outside any project: what waits across them, from the
				// saved board (never built here: this must stay quick).
				if c, err := board.ReadCache(sub.store()); err == nil && c.Data != nil {
					fmt.Fprintln(&buf, board.Summary(c.Data, time.Now()))
				}
			}
			if in.Fresh {
				runs = runsWaiting(sub)
			}
		}) && buf.Len()+len(runs) > 0 {
			say = sessionIntro(e) + "\n" + runs + buf.String()
		}
	case harness.RoleEnd:
		guarded(func() { hook.RecordEnd(e.store(), in, h, e.Cwd, e.UserHome, time.Now()) })
	}
	// Each agent reads the reply its own way (plain text, or JSON); one
	// that wants an answer gets one even when there is nothing to say.
	if reply := h.ReplyTo(args[0], say); reply != "" {
		fmt.Fprint(e.Stdout, reply)
		if !strings.HasSuffix(reply, "\n") {
			fmt.Fprintln(e.Stdout)
		}
	}
	return 0
}

// runsWaiting: the runs waiting on the user, asked of the built in runners
// only (the hook must stay quick); the rest show as last known.
func runsWaiting(e *Env) string {
	views, err := thread.Runs(e.store(), time.Now())
	if err != nil || len(views) == 0 {
		return ""
	}
	return board.RunsWaiting(e.dispatcher().Refresh(e.ctx(), views, true))
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
