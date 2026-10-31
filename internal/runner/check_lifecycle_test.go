package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/harness/harnesstest"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/thread"
)

func TestStopCancelsProjectCheck(t *testing.T) {
	exerciseStopProjectCheck(t, false)
}

func TestStopCancelsCheckAfterWatcherDies(t *testing.T) {
	exerciseStopProjectCheck(t, true)
}

func exerciseStopProjectCheck(t *testing.T, killWatcher bool) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("SOUS_HOME", home)
	testutil.FakeBin(t, "claude", `
echo committed > check-change
git add check-change
git -c user.name=Sam -c user.email=sam@example.invalid commit -qm work
`+harnesstest.Says("SOUS: done committed work"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Builtin("claude", home, exe, time.Minute)
	markers := t.TempDir()
	entered, survived := filepath.Join(markers, "entered"), filepath.Join(markers, "survived")
	req := Request{ID: 1, UID: "checking", Project: gitRepo(t), Brief: "task",
		Check: fmt.Sprintf("echo $$ > %q; sleep 3; echo survived > %q", entered, survived)}
	ref, err := a.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Stop("", ref)
	waitFor(t, func() bool {
		pid, err := os.ReadFile(entered)
		return err == nil && strings.TrimSpace(string(pid)) == readString(a.dir(req.UID), "agent.pgid")
	})
	deadline := time.Now().Add(3500 * time.Millisecond)
	if killWatcher {
		watcherPID, err := strconv.Atoi(readString(a.dir(req.UID), "watcher.pid"))
		if err != nil || watcherPID <= 0 {
			t.Fatalf("watcher pid: %d %v", watcherPID, err)
		}
		if err := syscall.Kill(watcherPID, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := a.Stop("", ref); err != nil {
		t.Fatal(err)
	}
	if st, err := a.Status("", ref); err != nil || st.State != Failed || st.Text != "stopped" {
		t.Fatalf("stopped check status: %+v %v", st, err)
	}
	// The bounded fake check exits after three seconds even when the bug occurs.
	time.Sleep(time.Until(deadline))
	if _, err := os.Stat(survived); err == nil {
		t.Fatal("project check kept executing after Stop returned")
	}
}

func TestProjectCheckHonorsWatchDeadline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	limit := 2 * time.Second
	a, ref, dir := startedRun(t, "claude", limit)
	_, m, err := a.meta(ref)
	if err != nil {
		t.Fatal(err)
	}
	entered := filepath.Join(t.TempDir(), "entered")
	m.Check = fmt.Sprintf("echo entered > %q; sleep 5", entered)
	if err := writeJSON(dir, "run.json", m); err != nil {
		t.Fatal(err)
	}
	testutil.FakeBin(t, "claude", `
echo committed > check-change
git add check-change
git -c user.name=Sam -c user.email=sam@example.invalid commit -qm work
`+harnesstest.Says("SOUS: done committed work"))
	started := time.Now()
	if err := a.Watch(dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entered); err != nil {
		t.Fatalf("fixture did not enter the check before the run deadline: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*limit {
		t.Errorf("project check outlived the run deadline: %s", elapsed)
	}
	if st, err := a.Status("", ref); err != nil || st.State != Failed || !strings.Contains(st.Text, "ran out of time") {
		t.Fatalf("expired check status: %+v %v", st, err)
	}
}

func TestProjectCheckStopsIfOwnershipCannotBeRecorded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, ref, dir := startedRun(t, "claude", time.Minute)
	_, m, err := a.meta(ref)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "survived")
	m.Check = fmt.Sprintf("sleep 0.2; echo survived > %q", marker)
	if err := writeJSON(dir, "run.json", m); err != nil {
		t.Fatal(err)
	}
	pgid := filepath.Join(dir, "agent.pgid")
	testutil.FakeBin(t, "claude", fmt.Sprintf(`
 while [ ! -f %q ]; do sleep 0.01; done
 rm %q
 mkdir %q
 echo committed > check-change
 git add check-change
 git -c user.name=Sam -c user.email=sam@example.invalid commit -qm work
 `, pgid, pgid, pgid)+harnesstest.Says("SOUS: done committed work"))
	if err := a.Watch(dir, false); err != nil {
		t.Fatal(err)
	}
	st, err := a.Status("", ref)
	if err != nil || st.Phase != thread.PhaseFailed || st.Result == nil || len(st.Result.Checks) != 1 || st.Result.Checks[0].Status != "failed" {
		t.Fatalf("unowned check was accepted: %+v %v", st, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("unowned check kept executing: %v", err)
	}
}
