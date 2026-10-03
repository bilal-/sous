package runner

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/testutil"
)

// gitRepo makes a repo with one commit and a remote it could push to.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(t.TempDir(), "origin.git")
	for _, args := range [][]string{
		{"init", "-q", "--bare", bare},
		{"-C", dir, "init", "-q"},
		{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "one"},
		{"-C", dir, "remote", "add", "origin", bare},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	return dir
}

// fakeSous records the watcher's arguments instead of watching.
func fakeSous(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	p := filepath.Join(dir, "sous")
	os.WriteFile(p, []byte("#!/bin/sh\necho \"$*\" >> "+log+"\n"), 0o755)
	return p, log
}

func TestStartMakesAWorktreeAndLaunchesTheWatcher(t *testing.T) {
	testutil.FakeBin(t, "claude", "")
	repo := gitRepo(t)
	exe, calls := fakeSous(t)
	a, _ := Builtin("claude", t.TempDir(), exe, time.Hour)
	ref, err := a.Start(Request{ID: 7, UID: "u7", Project: repo, Brief: "Fix the flaky test."})
	if err != nil || ref != "claude:u7" {
		t.Fatalf("%q %v", ref, err)
	}
	dir := filepath.Join(a.Home, "runs", "u7")
	var m runMeta
	b, _ := os.ReadFile(filepath.Join(dir, "run.json"))
	json.Unmarshal(b, &m)
	if m.Branch != "sous/run-7" || m.Worktree != filepath.Join(dir, "worktree") || !strings.Contains(m.Prompt, "Fix the flaky test.") || !strings.Contains(m.Prompt, "SOUS: needs you") {
		t.Fatalf("%+v", m)
	}
	if out, _ := exec.Command("git", "-C", m.Worktree, "branch", "--show-current").Output(); strings.TrimSpace(string(out)) != "sous/run-7" {
		t.Fatalf("worktree branch: %s", out)
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("run folder mode %v", st.Mode().Perm())
	}
	waitFor(t, func() bool {
		b, _ := os.ReadFile(calls)
		return strings.Contains(string(b), "runner claude watch "+dir)
	})
	// Safe to repeat: same ref, no second worktree or watcher.
	if again, err := a.Start(Request{ID: 7, UID: "u7", Project: repo, Brief: "x"}); err != nil || again != ref {
		t.Fatalf("again: %q %v", again, err)
	}
	time.Sleep(100 * time.Millisecond)
	if b, _ := os.ReadFile(calls); strings.Count(string(b), "watch") != 1 {
		t.Fatalf("watchers: %s", b)
	}
}

func TestStartRefusesWhatItCannotRun(t *testing.T) {
	exe, _ := fakeSous(t)
	a, _ := Builtin("claude", t.TempDir(), exe, time.Hour)
	testutil.FakeBin(t, "claude", "")
	if _, err := a.Start(Request{ID: 1, UID: "u1", Project: t.TempDir(), Brief: "b"}); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("not git: %v", err)
	}
	repo := gitRepo(t)
	testutil.OnlyGit(t)
	if _, err := a.Start(Request{ID: 2, UID: "u2", Project: repo, Brief: "b"}); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("no claude: %v", err)
	}
}

// A push from inside the run fails, and the repo's config
// is untouched.
func TestPushIsBlocked(t *testing.T) {
	repo := gitRepo(t)
	bare := filepath.Join(t.TempDir(), "fork.git")
	exec.Command("git", "init", "-q", "--bare", bare).Run()
	// A remote whose pushes go elsewhere (pushurl), and one with a user
	// other than git: neither may slip through.
	exec.Command("git", "-C", repo, "remote", "add", "fork", "https://example.invalid/sam/billing.git").Run()
	exec.Command("git", "-C", repo, "config", "remote.fork.pushurl", bare).Run()
	exec.Command("git", "-C", repo, "remote", "add", "work", "sam@example.invalid:acme/billing.git").Run()
	rel := filepath.Join(filepath.Dir(repo), "rel.git")
	exec.Command("git", "init", "-q", "--bare", rel).Run()
	exec.Command("git", "-C", repo, "remote", "add", "near", "../rel.git").Run()
	before, _ := os.ReadFile(filepath.Join(repo, ".git", "config"))
	for _, remote := range []string{"origin", "fork", "work", "near", "https://example.invalid/acme/billing.git", "ssh://git@example.invalid/acme/billing.git", "git@example.invalid:acme/billing.git"} {
		cmd := exec.Command("git", "-C", repo, "push", remote, "HEAD:refs/heads/x")
		cmd.Env = append(os.Environ(), pushBlock(repo)...)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "sous-no-push") {
			t.Errorf("push to %s: %v %s", remote, err, out)
		}
	}
	if after, _ := os.ReadFile(filepath.Join(repo, ".git", "config")); string(after) != string(before) {
		t.Fatal("repo config changed")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out waiting")
}

func TestReplyRefusals(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	release := holdLock(t, dir)
	if err := a.Reply("", ref, "yes"); err == nil || !strings.Contains(err.Error(), "still working") {
		t.Fatalf("running: %v", err)
	}
	release()
	writeJSON(dir, "result.json", result{Exit: 1})
	if err := a.Reply("", ref, "yes"); err == nil || !strings.Contains(err.Error(), "start a new run") {
		t.Fatalf("no session: %v", err)
	}
	if err := a.Reply("", ref, "  "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty: %v", err)
	}
}

// Two quick replies start one watcher, not two.
func TestTwoRepliesStartOneWatcher(t *testing.T) {
	claudeSays(t, `SOUS: needs you which one?`, 0)
	a, ref := watchedRun(t, "claude")
	waitFor(t, func() bool { st, _ := a.Status("", ref); return st.State == NeedsYou && !watcherAlive(a.dir("u3")) })
	testutil.FakeBin(t, "claude", `sleep 30`)
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- a.Reply("", ref, "the new one") }()
	}
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) {
		t.Fatalf("exactly one reply must win: %v, %v", e1, e2)
	}
	a.Stop("", ref)
}

func TestReplyCarriesOn(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	claudeSays(t, `SOUS: needs you which one?`, 0)
	a.Watch(dir, false)
	if err := a.Reply("", ref, "the new one"); err != nil {
		t.Fatal(err)
	}
	var m runMeta
	readJSON(dir, "run.json", &m)
	if m.Answer != "the new one" || m.Session != "s-1" {
		t.Fatalf("%+v", m)
	}
	if _, err := os.Stat(filepath.Join(dir, "result.json")); !os.IsNotExist(err) {
		t.Fatal("the old result must go")
	}
	// The resumed watcher passes --resume and the answer.
	testutil.FakeBin(t, "claude", `echo "$*" > args; printf '{"result":"SOUS: done ok","session_id":"s-1"}\n'`)
	a.Watch(dir, true)
	if args := readString(m.Worktree, "args"); !strings.Contains(args, "--resume s-1") || !strings.HasSuffix(args, "the new one") {
		t.Fatalf("args %q", args)
	}
	if st, _ := a.Status("", ref); st.State != Done {
		t.Fatalf("%+v", st)
	}
}

// watchedRun starts a run with a real watcher: this test binary, through
// the runner door (see TestMain in conformance_test.go).
func watchedRun(t *testing.T, name string) (*Agent, string) {
	t.Helper()
	exe, _ := os.Executable()
	home := t.TempDir()
	t.Setenv("SOUS_HOME", home)
	a, _ := Builtin(name, home, exe, time.Minute)
	ref, err := a.Start(Request{ID: 3, UID: "u3", Project: gitRepo(t), Brief: "task"})
	if err != nil {
		t.Fatal(err)
	}
	return a, ref
}

func TestStopAndClean(t *testing.T) {
	testutil.FakeBin(t, "claude", "sleep 30")
	a, ref := watchedRun(t, "claude")
	dir := a.dir("u3")
	if err := a.Clean("", ref); err == nil {
		t.Fatal("clean must refuse a running run")
	}
	for range 2 {
		if err := a.Stop("", ref); err != nil {
			t.Fatal(err)
		}
	}
	if st, _ := a.Status("", ref); st.State != Failed || st.Text != "stopped" {
		t.Fatalf("%+v", st)
	}
	var m runMeta
	readJSON(dir, "run.json", &m)
	if err := a.Clean(m.Project, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.Worktree); !os.IsNotExist(err) {
		t.Fatal("worktree still there")
	}
	if out, _ := exec.Command("git", "-C", m.Project, "branch", "--list", m.Branch).Output(); len(out) != 0 {
		t.Fatalf("an empty branch goes too: %s", out)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the run's folder (log, result) goes too: nothing is left behind")
	}
	if err := a.Clean(m.Project, ref); err != nil {
		t.Fatalf("cleaning twice is fine: %v", err)
	}
}

func TestCleanKeepsABranchWithCommits(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	var m runMeta
	readJSON(dir, "run.json", &m)
	exec.Command("git", "-C", m.Worktree, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "work").Run()
	writeJSON(dir, "result.json", result{})
	if err := a.Clean(m.Project, ref); err != nil {
		t.Fatal(err)
	}
	if out, _ := exec.Command("git", "-C", m.Project, "branch", "--list", m.Branch).Output(); len(out) == 0 {
		t.Fatal("a branch with commits must be kept")
	}
}

// Work left uncommitted (a Codex sandbox cannot commit) is
// never thrown away by clean, and status says it is there.
func TestUncommittedWorkIsKeptAndShown(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	testutil.FakeBin(t, "claude", `echo fixed > fix.txt; printf '{"result":"SOUS: done fixed it","session_id":"s"}\n'`)
	a.Watch(dir, false)
	st, _ := a.Status("", ref)
	if st.State != Done || !strings.Contains(st.Text, "fixed it") || !strings.Contains(st.Text, "not committed") {
		t.Fatalf("%+v", st)
	}
	var m runMeta
	readJSON(dir, "run.json", &m)
	if err := a.Clean(m.Project, ref); err == nil || !strings.Contains(err.Error(), "not committed") {
		t.Fatalf("clean: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.Worktree, "fix.txt")); err != nil {
		t.Fatal("the work is gone")
	}
}

// fakeAgent records its arguments and says it needs the person, in each
// built in agent's own words (Claude Code's result, Codex's thread,
// Antigravity's conversation), so every runner finds a session to resume.
const fakeAgent = `for x in "$@"; do echo "$x"; done > args
printf '{"result":"SOUS: needs you q","session_id":"s"}\n'
echo '{"thread_id":"s"}'
echo '{"conversation_id":"s","response":"SOUS: needs you q"}'`

// A brief or answer that starts with a dash is text, never a flag: it
// follows "--", or is the value of a flag (-p=…).
func TestPromptsAreNeverFlags(t *testing.T) {
	for _, name := range Registry.Names() {
		a, ref, dir := startedRun(t, name, time.Minute)
		testutil.FakeBin(t, name, fakeAgent)
		a.Watch(dir, false)
		a.Reply("", ref, "--help is fine")
		var m runMeta
		readJSON(dir, "run.json", &m)
		a.Watch(dir, true)
		args := strings.Split(readString(m.Worktree, "args"), "\n")
		n := len(args)
		afterDashes := n >= 2 && args[n-1] == "--help is fine" && (args[n-2] == "--" || args[n-3] == "--")
		if !afterDashes && args[n-1] != "-p=--help is fine" {
			t.Errorf("%s: %q", name, args)
		}
	}
}

// A watcher killed outright leaves its agent working; the run
// still reads as running, and stop still stops the agent.
func TestStopReachesAnAgentWhoseWatcherDied(t *testing.T) {
	testutil.FakeBin(t, "claude", "sleep 30")
	a, ref := watchedRun(t, "claude")
	dir := a.dir("u3")
	waitFor(t, func() bool { return readString(dir, "agent.pgid") != "" && readString(dir, "watcher.pid") != "" })
	watcher, _ := strconv.Atoi(readString(dir, "watcher.pid"))
	syscall.Kill(watcher, syscall.SIGKILL)
	time.Sleep(200 * time.Millisecond)
	if st, _ := a.Status("", ref); st.State != Running {
		t.Fatalf("the agent is still at it: %+v", st)
	}
	if err := a.Stop("", ref); err != nil {
		t.Fatal(err)
	}
	pgid, _ := strconv.Atoi(readString(dir, "agent.pgid"))
	if syscall.Kill(-pgid, 0) == nil {
		t.Fatal("the agent is still running")
	}
	if st, _ := a.Status("", ref); st.State != Failed || st.Text != "stopped" {
		t.Fatalf("%+v", st)
	}
}

// Two starts of the same run make one run, and neither undoes
// the other.
func TestConcurrentStartsOfOneRun(t *testing.T) {
	testutil.FakeBin(t, "claude", "")
	exe, calls := fakeSous(t)
	a, _ := Builtin("claude", t.TempDir(), exe, time.Hour)
	repo := gitRepo(t)
	errs := make(chan error, 4)
	for range 4 {
		go func() { _, err := a.Start(Request{ID: 5, UID: "u5", Project: repo, Brief: "b"}); errs <- err }()
	}
	for range 4 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(a.dir("u5"), "run.json")); err != nil {
		t.Fatal("the run is gone")
	}
	waitFor(t, func() bool { b, _ := os.ReadFile(calls); return strings.Count(string(b), "watch") >= 1 })
	time.Sleep(100 * time.Millisecond)
	if b, _ := os.ReadFile(calls); strings.Count(string(b), "watch") != 1 {
		t.Fatalf("watchers: %s", b)
	}
}

// Real runs showed a reply cannot grant permissions, so each agent is given
// exactly what committing on its own branch needs, and no more: Claude the
// git commands that commit, Codex git's objects, refs and logs and this
// worktree's own folder (never the repo's hooks or config).
func TestAgentsMayCommitAndNoMore(t *testing.T) {
	for _, name := range Registry.Names() {
		a, ref, dir := startedRun(t, name, time.Minute)
		testutil.FakeBin(t, name, fakeAgent)
		var m runMeta
		readJSON(dir, "run.json", &m)
		for _, resume := range []bool{false, true} {
			if resume {
				a.Reply("", ref, "go on")
			}
			a.Watch(dir, resume)
			args := readString(m.Worktree, "args")
			common := filepath.Join(m.Project, ".git")
			switch name {
			case "claude":
				if !strings.Contains(args, "Bash(git commit:*)") || strings.Contains(args, "Bash(git push") || strings.Contains(args, "Bash(git:*)") {
					t.Errorf("claude resume=%v: %s", resume, args)
				}
			case "codex":
				if !strings.Contains(args, "sandbox_workspace_write.writable_roots") || !strings.Contains(args, common+"/objects") || strings.Contains(args, common+"/hooks") || strings.Contains(args, `"`+common+`"`) {
					t.Errorf("codex resume=%v: %s", resume, args)
				}
			case "agy":
				// Edits accepted; commands only as the person's settings allow.
				if !strings.Contains(args, "accept-edits") || strings.Contains(args, "dangerously") {
					t.Errorf("agy resume=%v: %s", resume, args)
				}
			default:
				t.Errorf("%s: say here what a run of it may do", name)
			}
		}
	}
}

// A built in run hands its agent where the person left off (here_file),
// in the prompt itself: the file may be outside what the agent may read.
func TestStartPutsWhereThePersonLeftOffInThePrompt(t *testing.T) {
	testutil.FakeBin(t, "claude", "")
	repo := gitRepo(t)
	// A watcher that does nothing: this is about the prompt, and one that
	// wrote after the test ended would race its cleanup.
	exe := testutil.Script(t, t.TempDir(), "sous", "exit 0")
	here := filepath.Join(t.TempDir(), "here.txt")
	os.WriteFile(here, []byte("api · main · last commit 2h ago\n  1  check the index  2h\n"), 0o600)
	a, _ := Builtin("claude", t.TempDir(), exe, time.Hour)
	a.Start(Request{ID: 8, UID: "u8", Project: repo, Brief: "Fix it.", HereFile: here})
	var m runMeta
	b, _ := os.ReadFile(filepath.Join(a.Home, "runs", "u8", "run.json"))
	json.Unmarshal(b, &m)
	i, j := strings.Index(m.Prompt, "check the index"), strings.Index(m.Prompt, "Fix it.")
	if i < 0 || j < i {
		t.Fatalf("the context comes before the task:\n%s", m.Prompt)
	}
	a.Start(Request{ID: 9, UID: "u9", Project: repo, Brief: "No context.", HereFile: filepath.Join(t.TempDir(), "gone")})
	b, _ = os.ReadFile(filepath.Join(a.Home, "runs", "u9", "run.json"))
	json.Unmarshal(b, &m)
	if !strings.HasSuffix(m.Prompt, "The task:\n\nNo context.") {
		t.Fatalf("a missing file adds nothing:\n%s", m.Prompt)
	}
}

// A watcher that fails before the agent ends says why: its own output is
// kept beside the run, and a run stopped without a result names it.
func TestAWatcherThatFailsSaysWhy(t *testing.T) {
	testutil.FakeBin(t, "claude", "")
	repo := gitRepo(t)
	exe := testutil.Script(t, t.TempDir(), "sous", `echo "watch: run.json: permission denied" >&2; exit 1`)
	a, _ := Builtin("claude", t.TempDir(), exe, time.Hour)
	ref, err := a.Start(Request{ID: 3, UID: "u3", Project: repo, Brief: "Fix it."})
	if err != nil {
		t.Fatal(err)
	}
	var st Status
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if st, _ = a.Status(repo, ref); st.State == Failed {
			break
		}
	}
	if st.State != Failed || !strings.Contains(st.Text, "permission denied") {
		t.Fatalf("%+v", st)
	}
}
