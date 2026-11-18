package hook

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	long := "all\n\ngreen " + strings.Repeat("and more ", 100)
	for _, tc := range []struct{ agent, line, said string }{
		{agent: "claude", line: `{"type":"assistant","message":{"content":[{"type":"text","text":"all green"}]}}`},
		{agent: "codex", line: `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"all green"}]}}`},
		{agent: "agy", line: `{"source":"MODEL","type":"PLANNER_RESPONSE","content":"all green"}`},
		{agent: "opencode", said: "all green"}, // its plugin says it; no transcript
		{agent: "claude", line: `{"type":"assistant","message":{"content":[{"type":"text","text":` + strconv.Quote(long) + `}]}}`},
		{agent: "opencode", said: long},
	} {
		repo := testutil.Repo(t, filepath.Join(t.TempDir(), "api"), true, "")
		in := harness.Input{CWD: repo, LastMessage: tc.said}
		if tc.line != "" {
			in.TranscriptPath = filepath.Join(t.TempDir(), "t.jsonl")
			os.WriteFile(in.TranscriptPath, []byte(tc.line+"\n"), 0o644)
		}
		s := &store.Store{Home: t.TempDir()}
		h, _ := harness.Find(tc.agent)
		RecordEnd(s, in, h, "", "", time.Now())
		all, _ := session.All(s)
		if len(all) != 1 {
			t.Fatalf("%s: %v", tc.agent, all)
		}
		for _, sess := range all {
			// However it was said, it is kept on one line, cut short.
			if sess.LastMessage == nil || !strings.HasPrefix(*sess.LastMessage, "all green") || strings.Contains(*sess.LastMessage, "\n") ||
				utf8.RuneCountInString(*sess.LastMessage) > session.LastMessageRunes || sess.SessionID != "unknown" || sess.Agent != tc.agent {
				t.Fatalf("%s: %+v", tc.agent, sess)
			}
		}
	}
}

func TestRecordEndCapturesBranchPromptAndFiles(t *testing.T) {
	repo := testutil.Repo(t, filepath.Join(t.TempDir(), "api"), true, "")
	if real, err := filepath.EvalSymlinks(repo); err == nil {
		repo = real
	}
	testutil.Git(t, repo, "checkout", "-b", "feat/memory")
	os.WriteFile(filepath.Join(repo, "work.go"), []byte("package main\n"), 0o644)
	tr := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(tr, []byte(`{"type":"user","message":{"content":[{"type":"text","text":"wire branch capture"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"added branch field"}]}}
`), 0o644)
	s := &store.Store{Home: t.TempDir()}
	h, _ := harness.Find("claude")
	if err := RecordEnd(s, harness.Input{SessionID: "s1", CWD: repo, TranscriptPath: tr}, h, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	hist, err := session.ForProject(s, repo)
	if err != nil || len(hist) != 1 {
		t.Fatalf("%+v %v", hist, err)
	}
	got := hist[0]
	if got.Branch != "feat/memory" || got.Prompt == nil || *got.Prompt != "wire branch capture" || !slices.Contains(got.Files, "work.go") {
		t.Fatalf("missing rich capture: %+v", got)
	}
}
