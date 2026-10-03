package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"

	"github.com/bilal-/sous/internal/testutil"
)

func hookJSON(m map[string]any) string { b, _ := json.Marshal(m); return string(b) }

func TestHooksCLI(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("oss/app-next", true)
	f.runIn(p, "note", "-k", "me", "finish json api")

	out, errs, code := f.runStdin(hookJSON(map[string]any{"session_id": "s1", "cwd": p, "source": "startup"}), "hook", "session-start", "claude")
	if code != 0 || errs != "" || !strings.Contains(out, "app-next ·") || !strings.Contains(out, "finish json api") {
		t.Fatalf("startup: %d %q %q", code, out, errs)
	}
	for _, src := range []string{"resume", "clear", "compact"} {
		if out, _, code := f.runStdin(hookJSON(map[string]any{"cwd": p, "source": src}), "hook", "session-start", "claude"); out != "" || code != 0 {
			t.Errorf("%s must print nothing: %q", src, out)
		}
	}
	if out, _, _ := f.runStdin(hookJSON(map[string]any{"cwd": p}), "hook", "session-start", "claude"); !strings.Contains(out, "app-next ·") {
		t.Error("missing source is startup")
	}
	for _, in := range []string{hookJSON(map[string]any{"cwd": f.Home, "source": "startup"}), "garbage", ""} {
		if out, errs, code := f.runStdin(in, "hook", "session-start", "claude"); out != "" || errs != "" || code != 0 {
			t.Errorf("must be silent and 0 for %q: %q %q %d", in, out, errs, code)
		}
	}

	os.MkdirAll(filepath.Join(p, "src"), 0o755) // session-end is keyed by repo root, not cwd
	tr := filepath.Join(f.Home, "t.jsonl")
	os.WriteFile(tr, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"next is the widget template"}]}}`+"\n"), 0o644)
	_, errs, code = f.runStdin(hookJSON(map[string]any{"session_id": "s9", "cwd": filepath.Join(p, "src"), "transcript_path": tr}), "hook", "session-end", "claude")
	if code != 0 || errs != "" {
		t.Fatal(code, errs)
	}
	b, _ := os.ReadFile(filepath.Join(f.SousHome, "sessions.json"))
	testutil.Contains(t, string(b), `"agent": "claude"`, `"session_id": "s9"`, "next is the widget template", `"`+p+`"`)
	f.runStdin(hookJSON(map[string]any{"session_id": "s10", "cwd": p, "transcript_path": "/nope"}), "hook", "session-end", "codex")
	b, _ = os.ReadFile(filepath.Join(f.SousHome, "sessions.json"))
	if !strings.Contains(string(b), `"session_id": "s10"`) || !strings.Contains(string(b), `"last_message": null`) {
		t.Errorf("end without transcript:\n%s", b)
	}
	for _, in := range []string{hookJSON(map[string]any{"cwd": f.Home}), "garbage"} {
		if _, errs, code := f.runStdin(in, "hook", "session-end", "claude"); code != 0 || errs != "" {
			t.Errorf("end must be silent and 0 for %q", in)
		}
	}

	// setup
	os.MkdirAll(filepath.Join(f.Home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(f.Home, ".claude", "settings.json"), []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"/other/thing"}]}]}}`), 0o644)
	out, _, code = f.run("setup")
	if code != 0 || !strings.Contains(out, "sous is set up") || !strings.Contains(out, "Claude Code") {
		t.Fatalf("setup: %d %q", code, out)
	}
	cs, _ := os.ReadFile(filepath.Join(f.Home, ".claude", "settings.json"))
	cx, _ := os.ReadFile(filepath.Join(f.Home, ".codex", "hooks.json"))
	if strings.Count(string(cs), "hook session-start claude") != 1 || strings.Count(string(cs), "hook session-end claude") != 1 || !strings.Contains(string(cs), "/other/thing") {
		t.Errorf("claude settings:\n%s", cs)
	}
	if strings.Count(string(cx), "hook session-start codex") != 1 || strings.Contains(string(cx), "session-end codex") {
		t.Errorf("codex hooks:\n%s", cx)
	}
	f.run("setup")
	cs, _ = os.ReadFile(filepath.Join(f.Home, ".claude", "settings.json"))
	if strings.Count(string(cs), "hook session-start claude") != 1 {
		t.Error("setup must be idempotent")
	}
}

// Homebrew's sous is a link into a versioned folder that brew
// upgrade deletes. Hooks must name the stable path on PATH when it is this
// same program.
func TestStableExePrefersThePathOnPATH(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "Cellar", "sous", "0.1.6", "bin", "sous")
	os.MkdirAll(filepath.Dir(real), 0o755)
	os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755)
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	os.Symlink(real, filepath.Join(bin, "sous"))
	t.Setenv("PATH", bin)
	if got := stableExe(real); got != filepath.Join(bin, "sous") {
		t.Fatalf("got %s", got)
	}
	other := filepath.Join(dir, "elsewhere", "sous")
	os.MkdirAll(filepath.Dir(other), 0o755)
	os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755)
	if got := stableExe(other); got != other {
		t.Fatalf("a different sous on PATH must not be used: %s", got)
	}
}

func TestHookSessionStartBoundedEvenIfGitHangs(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/one", true)
	f.bin("git", "sleep 30") // shadows git
	start := time.Now()
	_, errs, code := f.runStdin(hookJSON(map[string]any{"cwd": p, "source": "startup"}), "hook", "session-start", "claude")
	if el := time.Since(start); el > 8*time.Second || code != 0 || errs != "" {
		t.Fatalf("hook took %v, code=%d err=%q", el, code, errs)
	}
}

func TestSetupWritesEmbeddedShellSnippet(t *testing.T) {
	f := fixture(t)
	t.Setenv("SHELL", "/bin/zsh")
	out, _, code := f.run("setup")
	if code != 0 {
		t.Fatal(code)
	}
	snippet := filepath.Join(f.SousHome, "sous.zsh")
	b, err := os.ReadFile(snippet)
	if err != nil || !strings.Contains(string(b), "sous --ambient") {
		t.Fatalf("snippet not written: %v", err)
	}
	if rc, _ := os.ReadFile(filepath.Join(f.Home, ".zshrc")); !strings.Contains(string(rc), `source "$HOME/.sous/sous.zsh"`) {
		t.Fatalf("setup must add the SOUS_HOME snippet to .zshrc:\n%s", rc)
	}
	if strings.Contains(out, "shell/sous.zsh") {
		t.Fatal("must not reference the checkout")
	}
}

func TestVersionShowsBuildInfo(t *testing.T) {
	f := fixture(t)
	old := Version
	Version = "0.1.0-dev+abc1234"
	defer func() { Version = old }()
	out, _, _ := f.run("version")
	if !strings.Contains(out, "sous 0.1.0-dev+abc1234") {
		t.Fatalf("%q", out)
	}
}

func TestSetupInstallsSkill(t *testing.T) {
	f := fixture(t)
	if _, _, code := f.run("setup"); code != 0 {
		t.Fatal(code)
	}
	for _, p := range []string{filepath.Join(f.Home, ".claude", "skills", "sous", "SKILL.md"), filepath.Join(f.Home, ".codex", "skills", "sous", "SKILL.md")} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		s := strings.ToLower(string(b))
		testutil.Contains(t, s, "name: sous", "description:", "sous help")
	}
}

// `here` (and so the session hook and `sous go`) must be local
// and bounded — a hung tracker CLI must not delay or empty the resume view.
func TestHereIsLocalEvenWhenTrackersHang(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("a/r", true)
	f.git(p, "remote", "add", "origin", "git@github.com:o/r.git")
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	f.runIn(p, "note", "-k", "me", "local note")
	// A filed-to-github thread, so reconcile has a reason to call gh.
	f.fileAs(1, "github:o/r#1")
	f.bin("gh", "sleep 30")

	start := time.Now()
	out, errs, code := f.runStdin(hookJSON(map[string]any{"cwd": p, "source": "startup"}), "hook", "session-start", "claude")
	if el := time.Since(start); code != 0 || errs != "" || el > 5*time.Second || !strings.Contains(out, "local note") {
		t.Fatalf("hook must print the resume view within its guard despite a hung gh: %v code=%d err=%q\n%s", el, code, errs, out)
	}
	if strings.Contains(out, "status unavailable") {
		t.Fatalf("here must not ask the tracker at all:\n%s", out)
	}
	start = time.Now()
	out, _, _ = f.run("here", p)
	// From a shell there is no guard, but one status probe is capped at 3 s
	// and nothing waits on the 15 s plugin timeout.
	if el := time.Since(start); el > 8*time.Second || !strings.Contains(out, "local note") {
		t.Fatalf("here from the shell is bounded too: %v\n%s", el, out)
	}
}

// Here (and so the session hook) must not run any tracker CLI —
// not even `glab auth status` from constructing the gitlab plugin.
func TestHereRunsNoTrackerCLI(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	calls := filepath.Join(f.Home, "tool-calls")
	for _, tool := range []string{"gh", "glab"} {
		f.bin(tool, "echo \""+tool+" $*\" >> "+calls+"\nexit 1")
	}
	// A note filed on GitHub: here must not ask GitHub about it (review:
	// reconciliation is the board's job; here is local).
	f.runIn(p, "note", "-k", "me", "filed elsewhere")
	f.fileAs(1, "github:acme/api#4")
	f.runStdin(hookJSON(map[string]any{"cwd": p, "source": "startup"}), "hook", "session-start", "claude")
	if out, errs, code := f.run("here", p); code != 0 || !strings.Contains(out, "filed elsewhere") {
		t.Fatalf("here must still show the filed note: %d %q %q", code, out, errs)
	}
	f.runStdin(p+"\n", "signal", "git", "scan")
	if b, err := os.ReadFile(calls); err == nil {
		t.Fatalf("here/hook/git scan must not call tracker CLIs:\n%s", b)
	}
}

// setup knows where sous really is; the menu bar script uses that path.
func TestSetupBakesTheRealPathIntoTheMenuBarScript(t *testing.T) {
	f := fixture(t)
	f.run("setup")
	b, _ := os.ReadFile(filepath.Join(f.SousHome, "sous.5m.sh"))
	exe, _ := os.Executable()
	if !strings.Contains(string(b), exe) || strings.Contains(string(b), ".local/bin/sous") {
		t.Fatalf("%s", b)
	}
}

// sous setup does the whole setup: finds projects, writes config, and adds
// itself to the shell once.
func TestSetupFindsProjectsAndAddsItselfToTheShell(t *testing.T) {
	f := fixture(t)
	os.Remove(filepath.Join(f.SousHome, "config.toml"))
	f.git(f.Home, "init", "-q", filepath.Join(f.Home, "code", "acme", "api"))
	t.Setenv("SHELL", "/bin/zsh")
	out, errs, code := f.run("setup")
	if code != 0 || !strings.Contains(out, "~/code") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	cfg, _ := os.ReadFile(filepath.Join(f.SousHome, "config.toml"))
	if !strings.Contains(string(cfg), `roots = ["~/code"]`) {
		t.Fatalf("%s", cfg)
	}
	f.run("setup")
	rc, _ := os.ReadFile(filepath.Join(f.Home, ".zshrc"))
	if strings.Count(string(rc), ".sous/sous.zsh") != 1 {
		t.Fatalf("one line, added once:\n%s", rc)
	}
	// Folders given on the line replace the roots.
	os.MkdirAll(filepath.Join(f.Home, "work"), 0o755)
	f.run("setup", filepath.Join(f.Home, "work"))
	cfg, _ = os.ReadFile(filepath.Join(f.SousHome, "config.toml"))
	if !strings.Contains(string(cfg), `roots = ["~/work"]`) {
		t.Fatalf("%s", cfg)
	}
	if _, _, code := f.run("setup", filepath.Join(f.Home, "missing")); code != 2 {
		t.Fatal("a folder that does not exist is a usage error")
	}
}

func TestSetupShells(t *testing.T) {
	for shell, rc := range map[string]string{"/bin/bash": ".bashrc", "/usr/local/bin/fish": ".config/fish/conf.d/sous.fish"} {
		f := fixture(t)
		t.Setenv("SHELL", shell)
		f.run("setup")
		b, err := os.ReadFile(filepath.Join(f.Home, rc))
		if err != nil || !strings.Contains(string(b), "sous --ambient") {
			t.Fatalf("%s: %v %q", shell, err, b)
		}
	}
	f := fixture(t)
	t.Setenv("SHELL", "/bin/zsh")
	f.run("setup", "--no-shell")
	if _, err := os.Stat(filepath.Join(f.Home, ".zshrc")); err == nil {
		t.Fatal("--no-shell must not touch the shell")
	}
}

// Nothing found and nothing given: say the one command to run.
func TestSetupWithNoProjectsFoundSaysWhatToDo(t *testing.T) {
	f := fixture(t)
	os.Remove(filepath.Join(f.SousHome, "config.toml"))
	out, _, code := f.run("setup", "--no-shell")
	if code != 0 || !strings.Contains(out, "sous setup ~/") {
		t.Fatalf("%d %q", code, out)
	}
}

// The skill goes where every agent looks: Claude Code, Codex, the shared
// ~/.agents/skills (Gemini CLI, Kimi, Cursor and others), and Antigravity
// when it is installed. Setup says which agents it found.
func TestSetupGivesEveryAgentTheSkill(t *testing.T) {
	f := fixture(t)
	os.MkdirAll(filepath.Join(f.Home, ".gemini", "antigravity"), 0o755)
	out, _, code := f.run("setup", "--no-shell")
	if code != 0 {
		t.Fatal(code)
	}
	for _, dir := range []string{".claude/skills/sous", ".codex/skills/sous", ".agents/skills/sous", ".gemini/antigravity/skills/sous"} {
		if b, err := os.ReadFile(filepath.Join(f.Home, dir, "SKILL.md")); err != nil || !strings.HasPrefix(string(b), "---\nname: sous") {
			t.Errorf("%s: %v", dir, err)
		}
	}
	if !strings.Contains(out, "Antigravity") || !strings.Contains(out, "Gemini CLI, Kimi, Cursor") {
		t.Fatalf("%s", out)
	}
	g := fixture(t)
	g.run("setup", "--no-shell")
	if _, err := os.Stat(filepath.Join(g.Home, ".gemini")); err == nil {
		t.Fatal("no Antigravity folder is created when Antigravity is not installed")
	}
}

// An agent starting a session is told what sous is, not only shown its
// output.
func TestSessionStartIntroducesSous(t *testing.T) {
	f := fixture(t)
	testutil.OnlyGit(t) // only the fixture's programs and git: no sous of this machine's
	t.Setenv("PATH", filepath.Join(f.Home, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := f.mkrepo("acme/api", true)
	out, _, _ := f.runStdin(hookJSON(map[string]any{"cwd": p, "source": "startup"}), "hook", "session-start", "claude")
	exe, _ := os.Executable()
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasPrefix(first, "[sous] ") || !strings.Contains(first, "Run "+exe+" help") || !strings.Contains(out, "api · main") {
		t.Fatalf("with no sous on PATH it names this one by its path: %q", out)
	}
	// With this sous on PATH, its name is enough.
	os.Symlink(exe, filepath.Join(f.Home, "bin", "sous"))
	out, _, _ = f.runStdin(hookJSON(map[string]any{"cwd": p, "source": "startup"}), "hook", "session-start", "claude")
	if first := strings.SplitN(out, "\n", 2)[0]; !strings.Contains(first, "Run sous help") {
		t.Fatalf("with sous on PATH: %q", first)
	}
}

// Outside any project, an agent hears the board in one line, from the
// saved board, and how to see it.
func TestSessionStartOutsideAProject(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	f.runIn(p, "note", "-k", "me", "check the index")
	f.run() // saves the board
	out, _, _ := f.runStdin(hookJSON(map[string]any{"cwd": f.Home, "source": "startup"}), "hook", "session-start", "claude")
	testutil.Contains(t, out, "[sous] ", "1 on you · 0 on others · 0 unfinished across 1 project", "as of just now", "sous for the board")
}

// Runs that need the user are what an agent hears first at session start,
// from any folder, with the command to answer.
func TestSessionStartRuns(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/app-next", true)
	st := &store.Store{Home: f.SousHome}
	now := time.Now()
	for _, c := range []struct {
		state thread.RunState
		text  string
	}{{thread.RunNeedsYou, "which fixture should win?"}, {thread.RunDone, "the spec passes"}, {thread.RunRunning, ""}, {thread.RunFailed, "snoozed, so not news"}} {
		id, _, _ := thread.NoteRun(st, project.Project{Path: filepath.Join(f.WS, "acme/billing")}, "fix the flaky test", string(c.state), "fake", "agent", now)
		thread.SetRun(st, id, func(r *thread.Run) { r.Ref, r.State, r.Text, r.Branch = "fake:x", c.state, c.text, "sous/run-2" })
		if c.state == thread.RunFailed {
			thread.Snooze(st, id, 3, now)
		}
	}
	for _, cwd := range []string{p, f.Home} {
		out, _, code := f.runStdin(hookJSON(map[string]any{"cwd": cwd, "source": "startup"}), "hook", "session-start", "claude")
		if code != 0 || !strings.Contains(out, "Runs waiting on the user") || !strings.Contains(out, `1 billing: run needs you · fix the flaky test · "which fixture should win?" · sous reply 1 "<answer>" · sous done 1`) ||
			!strings.Contains(out, "2 billing: run done, review it · fix the flaky test · sous/run-2 · sous done 2 --clean") || strings.Contains(out, "3 billing") || strings.Contains(out, "4 billing") {
			t.Errorf("from %s:\n%s", cwd, out)
		}
	}
	if out, _, _ := f.runStdin(hookJSON(map[string]any{"cwd": p, "source": "resume"}), "hook", "session-start", "claude"); out != "" {
		t.Errorf("a resumed session hears nothing:\n%s", out)
	}
}
