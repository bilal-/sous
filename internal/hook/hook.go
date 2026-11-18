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
	"github.com/bilal-/sous/internal/text"
)

// StartRoot: the repo to inject for, only on a fresh session.
func StartRoot(in harness.Input, fallback, home string) (string, bool) {
	if !in.Fresh {
		return "", false
	}
	return rootOf(in, fallback, home)
}

// ShouldStart resolves the repo root to inject context for, deduplicating
// per-prompt harnesses (such as Kimi's UserPromptSubmit) once per session.
func ShouldStart(s *store.Store, in harness.Input, h harness.Harness, fallback, home string, now time.Time) (string, bool) {
	root, ok := StartRoot(in, fallback, home)
	if !ok {
		return "", false
	}
	if h.Name == "kimi" && in.SessionID != "" && !session.MarkStarted(s, h.Name+":"+in.SessionID, now) {
		return "", false
	}
	return root, true
}

// rootOf is the repo the hook's cwd is in — the input's cwd, or fallback
// (the hook process's own directory) when the agent sent none.
func rootOf(in harness.Input, fallback, home string) (string, bool) {
	cwd := in.CWD
	if cwd == "" {
		cwd = fallback
	}
	if cwd == "" {
		return "", false
	}
	return project.ForPath(cwd, home)
}

// RecordEnd remembers the session state in its project: which agent, branch,
// prompt intent, modified files, latest commit, when it ended, and its last
// message. fallback is the hook's own folder when the agent sent none.
func RecordEnd(s *store.Store, in harness.Input, h harness.Harness, fallback, home string, now time.Time) error {
	root, ok := rootOf(in, fallback, home)
	if !ok {
		return nil
	}
	ws := project.WorkingSnapshot(root)
	sess := session.Session{
		Agent:     h.Name,
		SessionID: in.SessionID,
		Ended:     now,
		Branch:    ws.Branch,
		Files:     ws.DirtyFiles,
	}
	if len(ws.RecentCommits) > 0 {
		sess.Commit = ws.RecentCommits[0]
	}
	if sess.SessionID == "" {
		sess.SessionID = "unknown"
	}
	p := in.Prompt
	if p == "" && in.TranscriptPath != "" && h.LastUser != nil {
		p = h.LastUser(in.TranscriptPath)
	}
	if p = text.Cut(text.OneLine(p), session.LastMessageRunes); p != "" {
		sess.Prompt = &p
	}
	m := in.LastMessage
	if m == "" && in.TranscriptPath != "" && h.Last != nil {
		m = h.Last(in.TranscriptPath)
	}
	if m = text.Cut(text.OneLine(m), session.LastMessageRunes); m != "" {
		sess.LastMessage = &m
	}
	return session.Record(s, root, sess)
}
