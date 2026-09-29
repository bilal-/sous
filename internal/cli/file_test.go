package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/backend/backendtest"
	"github.com/bilal-/sous/internal/signal/signaltest"
)

func TestMarkdownBackendViaRunner(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	exe, _ := os.Executable()
	bs := backend.Backends(exe, []string{"markdown"}, nil)
	b, err := backend.Detect(context.Background(), bs, p, "", nil)
	if err != nil || b.Name != "markdown" {
		t.Fatalf("detect via subprocess: %+v %v", b, err)
	}
	ref, err := backend.File(context.Background(), b, backend.Request{ID: 3, UID: "000000000003", Project: p, Text: "via runner", Kind: "idea"})
	if err != nil || ref != "md:FOLLOWUPS.md:000000000003" {
		t.Fatal(ref, err)
	}
	if st, _ := backend.Status(context.Background(), b, p, ref); st != "open" {
		t.Fatal(st)
	}
	if err := backend.Close(context.Background(), b, p, ref); err != nil {
		t.Fatal(err)
	}
	if st, _ := backend.Status(context.Background(), b, p, ref); st != "closed" {
		t.Fatal(st)
	}
	if _, ok, _ := backend.URL(context.Background(), b, p, ref); ok {
		t.Fatal("markdown has no url op → unsupported")
	}
}

func threadJSON(t *testing.T, f *fx, id int) map[string]any {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(f.SousHome, "threads.json"))
	var doc struct{ Threads []map[string]any }
	json.Unmarshal(b, &doc)
	for _, th := range doc.Threads {
		if int(th["id"].(float64)) == id {
			return th
		}
	}
	t.Fatalf("thread %d not found", id)
	return nil
}

func TestFileAndCloseFlow(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	q := f.mkrepo("a/plain", true)
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)

	out, errs, code := f.runIn(p, "note", "--file", "-k", "me", "wire the export")
	if code != 0 {
		t.Fatalf("%d %s", code, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || lines[0] != "1" || !strings.HasPrefix(lines[1], "md:FOLLOWUPS.md:") || len(lines[1]) != len("md:FOLLOWUPS.md:")+12 {
		t.Fatalf("expected id then ref:\n%s", out)
	}
	md, _ := os.ReadFile(filepath.Join(p, "FOLLOWUPS.md"))
	if !strings.Contains(string(md), "- [ ] wire the export <!-- sous:") {
		t.Fatalf("%s", md)
	}
	if out2, _, _ := f.run("file", "1"); strings.TrimSpace(out2) != lines[1] {
		t.Fatalf("idempotent file: %q vs %q", out2, lines[1])
	}
	if md, _ = os.ReadFile(filepath.Join(p, "FOLLOWUPS.md")); strings.Count(string(md), "<!-- sous:") != 1 {
		t.Fatalf("duplicate marker:\n%s", md)
	}
	f.runIn(p, "note", "-k", "me", "second")
	f.run("file", "2")
	f.run("done", "2")
	if md, _ = os.ReadFile(filepath.Join(p, "FOLLOWUPS.md")); strings.Count(string(md), "- [ ]") != 2 {
		t.Fatalf("plain done must not touch upstream:\n%s", md)
	}
	if _, errs, code := f.run("done", "1", "--close"); code != 0 {
		t.Fatal(code, errs)
	}
	if md, _ = os.ReadFile(filepath.Join(p, "FOLLOWUPS.md")); !strings.Contains(string(md), "- [x] wire the export") {
		t.Fatalf("%s", md)
	}
	f.runIn(p, "note", "-k", "me", "third")
	f.run("file", "3")
	os.Remove(filepath.Join(p, "FOLLOWUPS.md"))
	if _, errs, code = f.run("done", "3", "--close"); code != 1 || errs == "" {
		t.Fatalf("close failure must be loud: %d %q", code, errs)
	}
	if th := threadJSON(t, f, 3); th["closed"] != nil {
		t.Fatalf("thread 3 must remain open: %+v", th)
	}
	f.runIn(q, "note", "plain idea")
	if _, errs, code = f.run("file", "4"); code != 1 || !strings.Contains(errs, "no tracker") || strings.Contains(errs, "add FOLLOWUPS.md") {
		t.Fatalf("%d %q", code, errs)
	}
	f.writeConfig("roots = [\"" + f.WS + "\"]\n[projects.\"a/plain\"]\nbackend = \"jira\"\n")
	if _, errs, code = f.run("file", "4"); code != 1 || !strings.Contains(errs, "jira") {
		t.Fatalf("%d %q", code, errs)
	}
	f.writeConfig("roots = [\"" + f.WS + "\"]\n[projects.\"a/plain\"]\nbackend = \"markdown\"\n")
	if out, _, code = f.run("file", "4"); code != 0 || !strings.HasPrefix(strings.TrimSpace(out), "md:") {
		t.Fatalf("override: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(q, "FOLLOWUPS.md")); err != nil {
		t.Fatal("declared backend creates the file")
	}
	f.runIn(q, "note", "unfiled")
	if _, _, code := f.run("done", "5", "--close"); code != 0 {
		t.Fatal("unfiled --close is plain done")
	}
	f.writeConfig("roots = [\"" + f.WS + "\"]\n[projects.\"a/plain\"]\nbackend = \"jira\"\n")
	out, errs, code = f.runIn(q, "note", "--file", "kept locally")
	if code != 1 || strings.TrimSpace(out) != "6" || !strings.Contains(errs, "saved as 6") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
}

func TestConcurrentFileIsIdempotent(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	f.runIn(p, "note", "-k", "me", "race me")
	exe, _ := os.Executable()
	var wg sync.WaitGroup
	refs := make([]string, 8)
	for i := range refs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, _ := exec.Command(exe, "file", "1").Output()
			refs[i] = strings.TrimSpace(string(out))
		}(i)
	}
	wg.Wait()
	md, _ := os.ReadFile(filepath.Join(p, "FOLLOWUPS.md"))
	if strings.Count(string(md), "<!-- sous:") != 1 {
		t.Fatalf("concurrent file must produce one marker:\n%s", md)
	}
	for _, r := range refs {
		if r != refs[0] || r == "" {
			t.Fatalf("all callers must see the same ref: %v", refs)
		}
	}
}

func TestReconcileUpstreamState(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	f.runIn(p, "note", "--file", "-k", "me", "tick me by hand")
	f.runIn(p, "note", "--file", "-k", "me", "lose my marker")
	out, _, _ := f.run("here", p)
	if !strings.Contains(out, "tick me by hand") || !strings.Contains(out, "→ md:FOLLOWUPS.md:") {
		t.Fatalf("here shows refs:\n%s", out)
	}
	md, _ := os.ReadFile(filepath.Join(p, "FOLLOWUPS.md"))
	edited := strings.Replace(string(md), "- [ ] tick me by hand", "- [x] tick me by hand", 1)
	var kept []string
	for _, l := range strings.Split(edited, "\n") {
		if !strings.Contains(l, "lose my marker") {
			kept = append(kept, l)
		}
	}
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte(strings.Join(kept, "\n")), 0o644)
	out, _, _ = f.run("here", p)
	if !strings.Contains(out, "✓ 1  tick me by hand  (closed upstream") || !strings.Contains(out, "lose my marker") || !strings.Contains(out, "(ref missing)") {
		t.Fatalf("reconcile:\n%s", out)
	}
	if th := threadJSON(t, f, 1); th["closed"] == nil || th["closed_by"] != "upstream" {
		t.Fatalf("auto-closed with provenance: %+v", th)
	}
	if th := threadJSON(t, f, 2); th["closed"] != nil {
		t.Fatal("ref missing must not close")
	}
	out, _, _ = f.run()
	if strings.Contains(out, "tick me by hand") || !strings.Contains(out, "lose my marker") {
		t.Fatalf("board after reconcile:\n%s", out)
	}
}

func TestFileRefusesGitFileCheckouts(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	wt := filepath.Join(f.WS, "a", "r-wt")
	f.git(p, "worktree", "add", "-q", wt, "-b", "feature")
	os.WriteFile(filepath.Join(wt, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	f.runIn(wt, "note", "-k", "me", "from worktree")
	if _, errs, code := f.run("file", "1"); code != 1 || !strings.Contains(errs, "worktree") {
		t.Fatalf("%d %q", code, errs)
	}
	if _, _, code := f.runIn(wt, "note", "--file", "-p", "r-wt", "explicit"); code != 0 {
		t.Fatal("explicit -p allows it")
	}
}

func TestReconcileDistinguishesBackendFailureFromMissing(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	f.runIn(p, "note", "--file", "-k", "me", "filed fine")
	// Swap the ref to a third-party backend that fails.
	broken := filepath.Join(f.Home, "bin", "sous-backend-jira")
	os.WriteFile(broken, []byte("#!/bin/sh\necho 'auth expired' >&2; exit 3\n"), 0o755)
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + broken + "\"]\n")
	f.fileAs(1, "jira:WAS-1")
	out, _, _ := f.run()
	if !strings.Contains(out, "status unavailable") || !strings.Contains(out, "auth expired") || strings.Contains(out, "ref missing") {
		t.Fatalf("backend failure must be named, not shown as missing:\n%s", out)
	}
	if th := threadJSON(t, f, 1); th["closed"] != nil {
		t.Fatal("must not close")
	}
	// Backend not configured at all.
	f.writeConfig("roots = [\"" + f.WS + "\"]\n")
	out, _, _ = f.run("here", p)
	if !strings.Contains(out, "backend jira not configured") {
		t.Fatalf("%s", out)
	}
}

func TestFileForceAndRefile(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	wt := filepath.Join(f.WS, "a", "r-wt")
	f.git(p, "worktree", "add", "-q", wt, "-b", "feature")
	os.WriteFile(filepath.Join(wt, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	f.runIn(wt, "note", "-k", "me", "from worktree")
	if _, errs, code := f.run("file", "1"); code != 1 || !strings.Contains(errs, "--force") {
		t.Fatalf("refusal must name the escape hatch: %d %q", code, errs)
	}
	if _, errs, code := f.run("file", "1", "--force"); code != 0 {
		t.Fatalf("%d %q", code, errs)
	}
	// Lost marker → sous file writes it again (same ref: the note's uid).
	f.runIn(p, "note", "-k", "me", "lose me")
	out1, _, _ := f.run("file", "2")
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	out2, _, code := f.run("file", "2")
	if code != 0 || strings.TrimSpace(out2) != strings.TrimSpace(out1) {
		t.Fatalf("must re-file a lost marker: %q vs %q", out1, out2)
	}
	md, _ := os.ReadFile(filepath.Join(p, "FOLLOWUPS.md"))
	if !strings.Contains(string(md), "- [ ] lose me <!-- sous:") {
		t.Fatalf("%s", md)
	}
}

func TestFiledThreadFollowsMovedRepo(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/old", true)
	f.git(p, "remote", "add", "origin", "git@github.com:o/r.git")
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	f.runIn(p, "note", "--file", "-k", "me", "moving")
	np := filepath.Join(f.WS, "a", "new")
	os.Rename(p, np)
	if out, _, _ := f.run("here", np); !strings.Contains(out, "→ md:FOLLOWUPS.md:") {
		t.Fatalf("here by remote:\n%s", out)
	}
	if _, errs, code := f.run("done", "1", "--close"); code != 0 {
		t.Fatal(code, errs)
	}
	md, _ := os.ReadFile(filepath.Join(np, "FOLLOWUPS.md"))
	if !strings.Contains(string(md), "- [x] moving") {
		t.Fatalf("%s", md)
	}
}

func TestBackendOpUsageErrors(t *testing.T) {
	f := fixture(t)
	for _, c := range [][]string{{"backend"}, {"backend", "nope", "detect", "/x"}, {"backend", "markdown", "detect"}, {"backend", "markdown", "status", "/x"}, {"backend", "markdown", "close", "/x"}, {"backend", "markdown", "url", "/x", "md:x"}} {
		if _, _, code := f.run(c...); code != 2 {
			t.Errorf("%v should exit 2, got %d", c, code)
		}
	}
	if _, _, code := f.runStdin("not json", "backend", "markdown", "file"); code != 2 {
		t.Error("bad file request")
	}
	p := f.mkrepo("a/r", true)
	if _, errs, code := f.run("backend", "markdown", "close", p, "md:FOLLOWUPS.md:9:abcdef12"); code != 1 || errs == "" {
		t.Errorf("close missing: %d %q", code, errs)
	}
	if _, errs, code := f.runStdin(`{"id":1,"project":"`+filepath.Join(p, "nope", "deeper")+`","text":"x","kind":"idea"}`, "backend", "markdown", "file"); code != 1 || errs == "" {
		t.Errorf("file into a missing dir: %d %q", code, errs)
	}
}

// Spec detection order: FOLLOWUPS.md before remote hosts. A repo with both
// files into markdown, and never touches GitHub.
func TestMarkdownWinsOverGitHubRemote(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	f.git(p, "remote", "add", "origin", "git@github.com:o/r.git")
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	calls := filepath.Join(f.Home, "gh-calls")
	os.WriteFile(filepath.Join(f.Home, "bin", "gh"), []byte("#!/bin/sh\necho \"$*\" >> "+calls+"\nexit 0\n"), 0o755)
	out, errs, code := f.runIn(p, "note", "--file", "-k", "me", "both")
	if code != 0 || !strings.Contains(out, "md:FOLLOWUPS.md:") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if b, _ := os.ReadFile(calls); strings.Contains(string(b), "issue create") {
		t.Fatalf("github must not be written:\n%s", b)
	}
}

// GitHub backend end to end through the runner and the filing flow.
func TestGitHubFileViaCLI(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/chime", true)
	f.git(p, "remote", "add", "origin", "git@github.com:acme/chime.git")
	calls := filepath.Join(f.Home, "gh-calls")
	os.WriteFile(filepath.Join(f.Home, "bin", "gh"), []byte(`#!/bin/sh
echo "TOKEN=$GH_TOKEN $*" >> `+calls+`
case "$*" in
  "auth status"*) exit 0;;
  "auth token --user work-account") echo tok-m;;
  *"issue list"*"--json number,body"*) printf '[]';;
  *"issue create"*) echo "https://github.com/acme/chime/issues/9";;
  *"issue view 9"*"--json state,url"*) printf '{"state":"CLOSED"}';;
esac
`), 0o755)
	f.writeConfig("roots = [\"" + f.WS + "\"]\n[projects.\"acme/*\"]\ngithub_account = \"work-account\"\n")
	out, errs, code := f.runIn(p, "note", "--file", "-k", "me", "notifications need context")
	if code != 0 || !strings.Contains(out, "github:acme/chime#9") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	b, _ := os.ReadFile(calls)
	if !strings.Contains(string(b), "TOKEN=tok-m issue create") {
		t.Fatalf("must file with the org's account:\n%s", b)
	}
	// Someone closed it on GitHub: here stays offline and does not ask; the
	// next board read closes it with provenance, and here then shows that.
	f.run("here", p)
	if th := threadJSON(t, f, 1); th["closed"] != nil {
		t.Fatal("here must not reconcile a GitHub ref")
	}
	f.run()
	out, _, _ = f.run("here", p)
	if !strings.Contains(out, "✓ 1  notifications need context  (closed upstream") {
		t.Fatalf("%s", out)
	}
	if th := threadJSON(t, f, 1); th["closed_by"] != "upstream" {
		t.Fatalf("%+v", th)
	}
}

// Review F5: a failed filing names the backend exactly once.
func TestFileErrorPrefixedOnce(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/chime", true)
	f.git(p, "remote", "add", "origin", "git@github.com:acme/chime.git")
	os.WriteFile(filepath.Join(f.Home, "bin", "gh"), []byte(`#!/bin/sh
case "$*" in
  "auth status"*) exit 0;;
  *"issue list"*) printf '[]';;
  *"issue create"*) echo "HTTP 403: forbidden" >&2; exit 1;;
esac
`), 0o755)
	_, errs, code := f.runIn(p, "note", "--file", "-k", "me", "x")
	if code != 1 || strings.Count(errs, "github:") != 1 || !strings.Contains(errs, "403") {
		t.Fatalf("%d %q", code, errs)
	}
}

// Review F5: a notice on gh's stderr must not corrupt the JSON on stdout.
func TestGitHubStatusIgnoresStderrNotices(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/chime", true)
	f.git(p, "remote", "add", "origin", "git@github.com:acme/chime.git")
	os.WriteFile(filepath.Join(f.Home, "bin", "gh"), []byte(`#!/bin/sh
echo "A new release of gh is available" >&2
case "$*" in
  "auth status"*) exit 0;;
  *"--json state,url"*) printf '{"state":"OPEN","url":"u"}';;
esac
`), 0o755)
	out, errs, code := f.run("backend", "github", "status", p, "github:acme/chime#1")
	if code != 0 || strings.TrimSpace(out) != "open" {
		t.Fatalf("%d %q %q", code, out, errs)
	}
}

// Review F10: the markdown built-in, reached through the same executable
// door a third-party backend uses, meets the backend contract end to end.
func TestMarkdownConformsThroughTheDoor(t *testing.T) {
	fixture(t)
	exe, _ := os.Executable()
	p := t.TempDir()
	os.WriteFile(filepath.Join(p, backend.MarkdownFile), []byte("# Follow-ups\n"), 0o644)
	backendtest.RunDoor(t, backend.Backends(exe, []string{"markdown"}, nil)[0], p)
}

// The built-in git signal, reached through the same door as any plugin,
// meets the signal contract.
func TestGitSignalConformsThroughTheDoor(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	os.WriteFile(filepath.Join(p, "x.txt"), []byte("uncommitted"), 0o644)
	exe, _ := os.Executable()
	signaltest.Run(t, []string{exe, "signal", "git", "scan"}, []string{p})
}

// The example plugin in examples/ meets the signal contract: a working
// starting point for anyone writing one.
func TestExamplePluginConforms(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	os.WriteFile(filepath.Join(p, "x.go"), []byte("// TODO(me) finish this\n"), 0o644)
	f.git(p, "add", "x.go")
	quiet := f.mkrepo("acme/web", true)
	example, _ := filepath.Abs(filepath.Join("..", "..", "examples", "sous-signal-todo"))
	signaltest.Run(t, []string{example, "scan"}, []string{p, quiet})
}

// Review: a folder name with a tab or quote keeps its exact name in the
// finding, so the board can match it to its project.
func TestExamplePluginKeepsOddFolderNames(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/tab\there \"q\"", true)
	os.WriteFile(filepath.Join(p, "x.go"), []byte("// TODO(me) x\n"), 0o644)
	f.git(p, "add", "x.go")
	example, _ := filepath.Abs(filepath.Join("..", "..", "examples", "sous-signal-todo"))
	signaltest.Run(t, []string{example, "scan"}, []string{p})
}
