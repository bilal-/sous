package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/harness/harnesstest"
	"github.com/bilal-/sous/internal/testutil"
)

func TestCleanSerializesWithReply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, ref, dir := startedRun(t, "claude", time.Minute)
	claudeSays(t, `SOUS: needs you choose a fixture`, 0)
	if err := a.Watch(dir, false); err != nil {
		t.Fatal(err)
	}
	st, err := a.Status("", ref)
	if err != nil || st.State != NeedsYou {
		t.Fatalf("before reply: %+v %v", st, err)
	}
	a.Exe, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOUS_HOME", a.Home)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	gate := t.TempDir()
	ready, proceed := filepath.Join(gate, "ready"), filepath.Join(gate, "proceed")
	agentStarted := filepath.Join(gate, "agent-started")
	testutil.FakeBin(t, "claude", fmt.Sprintf("echo started > %q; sleep 30\n", agentStarted)+harnesstest.Says("SOUS: done nothing to do"))
	testutil.FakeBin(t, "git", fmt.Sprintf(`
case "$*" in
  *"worktree remove"*)
    : > %q
    while [ ! -f %q ]; do sleep 0.01; done
    ;;
esac
exec %q "$@"
`, ready, proceed, realGit))
	done := make(chan error, 1)
	go func() { done <- a.Clean("", ref) }()
	waitFor(t, func() bool { _, err := os.Stat(ready); return err == nil })
	defer os.WriteFile(proceed, nil, 0600)
	// A real Reply resumes a real watcher while Clean is at git removal.
	replyDone := make(chan error, 1)
	go func() { replyDone <- a.Reply("", ref, "use the small fixture") }()
	var replyErr error
	replied := false
	select {
	case replyErr = <-replyDone:
		replied = true
	case <-time.After(100 * time.Millisecond):
	}
	if replied && replyErr == nil {
		waitFor(t, func() bool {
			_, err := os.Stat(agentStarted)
			return err == nil && readString(dir, "agent.pgid") != ""
		})
		watcherPID, _ := strconv.Atoi(readString(dir, "watcher.pid"))
		agentPGID, _ := strconv.Atoi(readString(dir, "agent.pgid"))
		if watcherPID <= 0 || agentPGID <= 0 {
			t.Fatal("watcher did not record process ownership")
		}
		defer syscall.Kill(watcherPID, syscall.SIGKILL)
		defer syscall.Kill(-agentPGID, syscall.SIGKILL)
		if !watcherAlive(dir) {
			t.Fatal("reply did not retain watcher lock")
		}
	}
	if err := os.WriteFile(proceed, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cleanErr := <-done
	if !replied {
		replyErr = <-replyDone
	}
	if replyErr != nil {
		if !strings.Contains(replyErr.Error(), "still working") && !strings.Contains(replyErr.Error(), "no run") {
			t.Fatalf("unexpected reply error: %v", replyErr)
		}
		if cleanErr != nil {
			t.Fatalf("clean owning the run failed: %v", cleanErr)
		}
		if _, err := os.Stat(agentStarted); !os.IsNotExist(err) {
			t.Fatalf("refused reply launched an agent: %v", err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("serialized cleanup left the run behind: %v", err)
		}
		return
	}
	if cleanErr == nil {
		t.Fatal("clean removed the run while its watcher lock was held")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("refused clean did not preserve run: %v", err)
	}
}

// Old process IDs must not become live ownership while cleanup is running.
func TestCleanDoesNotClaimWatcherOwnership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, ref, dir := startedRun(t, "claude", time.Minute)
	if err := writeJSON(dir, "result.json", result{Exit: 0, Message: "SOUS: done nothing to do", Ended: time.Now()}); err != nil {
		t.Fatal(err)
	}
	victim := exec.Command("sleep", "10")
	if err := victim.Start(); err != nil {
		t.Fatal(err)
	}
	victimDone := make(chan error, 1)
	go func() { victimDone <- victim.Wait() }()
	defer victim.Process.Kill()
	if err := os.WriteFile(filepath.Join(dir, "watcher.pid"), []byte(strconv.Itoa(victim.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	gate := t.TempDir()
	ready, proceed := filepath.Join(gate, "ready"), filepath.Join(gate, "proceed")
	testutil.FakeBin(t, "git", fmt.Sprintf(`
case "$*" in
  *"worktree remove"*) : > %q; while [ ! -f %q ]; do sleep 0.01; done;;
esac
exec %q "$@"
`, ready, proceed, realGit))
	defer os.WriteFile(proceed, nil, 0600)
	cleanDone := make(chan error, 1)
	go func() { cleanDone <- a.Clean("", ref) }()
	waitFor(t, func() bool { _, err := os.Stat(ready); return err == nil })
	stopDone := make(chan error, 1)
	go func() { stopDone <- a.Stop("", ref) }()
	var victimErr error
	killed := false
	select {
	case victimErr = <-victimDone:
		killed = true
	case <-time.After(250 * time.Millisecond):
	}
	if err := os.WriteFile(proceed, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-cleanDone; err != nil {
		t.Fatal(err)
	}
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if killed {
		t.Fatalf("Stop signaled a stale PID while Clean held the watcher lock: %v", victimErr)
	}
}
