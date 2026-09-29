package cli

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/bilal-/sous/internal/hook"
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
		if guarded(func() {
			if root, ok := hook.StartRoot(in, e.Cwd, e.UserHome); ok {
				cmdHere(sub, argv{pos: []string{root}})
			}
		}) && buf.Len() > 0 {
			fmt.Fprintln(e.Stdout, sessionIntro)
			io.Copy(e.Stdout, &buf)
		}
	case hook.RoleEnd:
		guarded(func() { hook.RecordEnd(e.store(), in, args[1], e.Cwd, e.UserHome, time.Now()) })
	}
	return 0
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
