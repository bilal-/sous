package cli

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/bilal-/sous/internal/hook"
	"github.com/bilal-/sous/internal/session"
)

// sessionIntro opens what an agent sees at session start, so it knows what
// sous is and when to reach for it, not only what sous printed.
const sessionIntro = "[sous] The user's list of what is waiting on them across projects. Below: where they left off here. When they want to remember something for later, in this or another project, use the sous skill (sous note)."

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
	case "session-start":
		// Everything — including git-backed repo resolution — runs under the
		// guard: a hanging git must not hang the agent session.
		done := make(chan struct{})
		var buf bytes.Buffer
		// Plugins and tracker probes together must finish inside the guard;
		// if the guard fires first, cancelling kills their process groups
		// instead of leaving them behind.
		sub := e.child(&buf, io.Discard, true)
		sub.PluginTimeout = 2 * time.Second
		sub.Deadline = time.Now().Add(4 * time.Second)
		sub.ctx()
		defer sub.close()
		go func() {
			defer close(done)
			root, ok := hook.StartRoot(in, e.Cwd, e.UserHome)
			if !ok {
				return
			}
			cmdHere(sub, argv{pos: []string{root}})
		}()
		select {
		case <-done:
			if buf.Len() > 0 {
				fmt.Fprintln(e.Stdout, sessionIntro)
			}
			io.Copy(e.Stdout, &buf)
		case <-time.After(5 * time.Second):
			sub.close()
		}
	case "session-end":
		root, ok := hook.Root(in, e.Cwd, e.UserHome)
		if !ok {
			return 0
		}
		sess := session.Session{Agent: args[1], SessionID: in.SessionID, Ended: time.Now()}
		if in.TranscriptPath != "" {
			if m := hook.LastAssistantText(in.TranscriptPath, 300); m != "" {
				sess.LastMessage = &m
			}
		}
		if sess.SessionID == "" {
			sess.SessionID = "unknown"
		}
		session.Record(e.store(), root, sess)
	}
	return 0
}
