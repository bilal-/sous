// Package hook implements the agent-session integration. Hooks record what
// happened and inject context at startup; they never infer and never fail.
// What each agent sends, and how its transcript reads, is its harness's
// business (internal/harness).
package hook

import (
	"time"

	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/store"
)

// StartRoot: the repo to inject for, only on a fresh session.
func StartRoot(in harness.Input, fallback, home string) (string, bool) {
	if !in.Fresh {
		return "", false
	}
	return Root(in, fallback, home)
}

// Root is the repo the hook's cwd is in — the input's cwd, or fallback
// (the hook process's own directory) when the agent sent none.
func Root(in harness.Input, fallback, home string) (string, bool) {
	cwd := in.CWD
	if cwd == "" {
		cwd = fallback
	}
	if cwd == "" {
		return "", false
	}
	return project.ForPath(cwd, home)
}

// RecordEnd remembers the session that just ended in its project: which
// agent, when, and its last message. fallback is the hook's own folder when
// the agent sent none.
func RecordEnd(s *store.Store, in harness.Input, h harness.Harness, fallback, home string, now time.Time) error {
	root, ok := Root(in, fallback, home)
	if !ok {
		return nil
	}
	sess := session.Session{Agent: h.Name, SessionID: in.SessionID, Ended: now}
	if sess.SessionID == "" {
		sess.SessionID = "unknown"
	}
	if in.TranscriptPath != "" && h.Last != nil {
		if m := h.Last(in.TranscriptPath, session.LastMessageRunes); m != "" {
			sess.LastMessage = &m
		}
	}
	return session.Record(s, root, sess)
}
