package hook

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/testutil"
)

func TestStartRoot(t *testing.T) {
	dir := t.TempDir()
	if _, ok := StartRoot(harness.Input{CWD: dir, Fresh: true}, "", ""); ok {
		t.Fatal("non-repo cwd must not resolve")
	}
	repo := testutil.Repo(t, filepath.Join(dir, "chime"), false, "")
	if _, ok := StartRoot(harness.Input{CWD: repo}, "", ""); ok {
		t.Fatal("a session that is not fresh gets nothing")
	}
	if _, ok := StartRoot(harness.Input{Fresh: true}, dir, ""); ok {
		t.Fatal("empty cwd falls back to the caller's directory, which is not a repo here")
	}
	if root, ok := StartRoot(harness.Input{Fresh: true}, repo, ""); !ok || filepath.Base(root) != "chime" {
		t.Fatalf("fallback to the caller's directory: %q %v", root, ok)
	}
}

// Recording a session end, with its guard, lives here, not in the CLI.
// The last message is read the way the agent's harness writes it.
func TestRecordEndStoresTheLastMessage(t *testing.T) {
	for _, tc := range []struct{ agent, line string }{
		{"claude", `{"type":"assistant","message":{"content":[{"type":"text","text":"all green"}]}}`},
		{"codex", `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"all green"}]}}`},
	} {
		repo := testutil.Repo(t, filepath.Join(t.TempDir(), "api"), true, "")
		tr := filepath.Join(t.TempDir(), "t.jsonl")
		os.WriteFile(tr, []byte(tc.line+"\n"), 0o644)
		s := &store.Store{Home: t.TempDir()}
		h, _ := harness.Find(tc.agent)
		RecordEnd(s, harness.Input{CWD: repo, TranscriptPath: tr}, h, "", "", time.Now())
		all, _ := session.All(s)
		if len(all) != 1 {
			t.Fatalf("%s: %v", tc.agent, all)
		}
		for _, sess := range all {
			if sess.LastMessage == nil || *sess.LastMessage != "all green" || sess.SessionID != "unknown" || sess.Agent != tc.agent {
				t.Fatalf("%s: %+v", tc.agent, sess)
			}
		}
	}
}
