package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// Review Focus 3: a push from inside the run fails, and the repo's config
// is untouched.
func TestPushIsBlocked(t *testing.T) {
	repo := gitRepo(t)
	before, _ := os.ReadFile(filepath.Join(repo, ".git", "config"))
	for _, remote := range []string{"origin", "https://example.invalid/acme/billing.git", "ssh://git@example.invalid/acme/billing.git", "git@example.invalid:acme/billing.git"} {
		cmd := exec.Command("git", "-C", repo, "push", remote, "HEAD:refs/heads/x")
		cmd.Env = append(os.Environ(), pushBlock()...)
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

// Review Focus 5.
func TestReplyRefusals(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	os.WriteFile(filepath.Join(dir, "watcher.pid"), []byte(fmt.Sprint(os.Getpid())), 0o600)
	if err := a.Reply("", ref, "yes"); err == nil || !strings.Contains(err.Error(), "still working") {
		t.Fatalf("running: %v", err)
	}
	os.Remove(filepath.Join(dir, "watcher.pid"))
	writeJSON(dir, "result.json", result{Exit: 1})
	if err := a.Reply("", ref, "yes"); err == nil || !strings.Contains(err.Error(), "start a new run") {
		t.Fatalf("no session: %v", err)
	}
	if err := a.Reply("", ref, "  "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty: %v", err)
	}
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

func TestStopAndClean(t *testing.T) {
	a, ref, dir := startedRun(t, "claude", time.Minute)
	sleeper := exec.Command("sleep", "30")
	sleeper.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	sleeper.Start()
	os.WriteFile(filepath.Join(dir, "watcher.pid"), []byte(fmt.Sprint(sleeper.Process.Pid)), 0o600)
	if err := a.Clean("", ref); err == nil {
		t.Fatal("clean must refuse a running run")
	}
	for range 2 {
		if err := a.Stop("", ref); err != nil {
			t.Fatal(err)
		}
	}
	sleeper.Wait()
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
