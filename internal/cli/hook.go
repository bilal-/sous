package cli

import (
	"bytes"
	"io"
	"time"

	"github.com/bilal-/sous/internal/hook"
	"github.com/bilal-/sous/internal/session"
)

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
		go func() {
			defer close(done)
			root, ok := hook.StartRoot(in, e.Cwd)
			if !ok {
				return
			}
			// Plugins and tracker probes together must finish inside the guard.
			sub := e.child(&buf, io.Discard, true)
			defer sub.close()
			sub.PluginTimeout = 2 * time.Second
			sub.Deadline = time.Now().Add(4 * time.Second)
			cmdHere(sub, argv{pos: []string{root}})
		}()
		select {
		case <-done:
			io.Copy(e.Stdout, &buf)
		case <-time.After(5 * time.Second):
		}
	case "session-end":
		root, ok := hook.Root(in, e.Cwd)
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
