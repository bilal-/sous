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

// These tests model recycled identifiers by replacing saved diagnostics with
// IDs of a different process created by this test. They never force actual
// kernel PID recycling or signal an existing user process.
func TestStopDoesNotSignalStaleProcessIDs(t *testing.T) {
	for _, stage := range []string{"agent", "check"} {
		for _, fields := range [][]string{{"watcher.pid"}, {"agent.pgid"}, {"watcher.pid", "agent.pgid"}} {
			t.Run(stage+"/"+strings.Join(fields, "+"), func(t *testing.T) {
				f := ownershipFixture(t, stage, false)
				watcherPID := ownershipPID(t, filepath.Join(f.dir, "watcher.pid"))
				if err := syscall.Kill(watcherPID, syscall.SIGKILL); err != nil {
					t.Fatal(err)
				}
				time.Sleep(50 * time.Millisecond)
				if st, err := f.agent.Status("", f.ref); err != nil || st.State != Running {
					t.Fatalf("orphan workload is no longer running before Stop: %+v %v", st, err)
				}

				victim := exec.Command("sleep", "20")
				victim.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				if err := victim.Start(); err != nil {
					t.Fatal(err)
				}
				victimDone := make(chan error, 1)
				go func() { victimDone <- victim.Wait() }()
				victimObserved := false
				t.Cleanup(func() {
					_ = victim.Process.Kill() // Direct test child, never a saved run PID.
					if !victimObserved {
						select {
						case <-victimDone:
						case <-time.After(time.Second):
							t.Error("fixture victim did not exit")
						}
					}
				})
				for _, field := range fields {
					if err := os.WriteFile(filepath.Join(f.dir, field), []byte(strconv.Itoa(victim.Process.Pid)), 0600); err != nil {
						t.Fatal(err)
					}
				}

				started := time.Now()
				stopErr := f.agent.Stop("", f.ref)
				if stopErr != nil {
					t.Errorf("Stop failed to stop its actual orphan workload: %v", stopErr)
				}
				if elapsed := time.Since(started); elapsed > 5*time.Second {
					t.Errorf("Stop exceeded its bounded shutdown interval: %s", elapsed)
				}
				select {
				case err := <-victimDone:
					victimObserved = true
					t.Errorf("Stop signaled the unrelated test-owned process named by stale %v: %v", fields, err)
				case <-time.After(100 * time.Millisecond):
				}

				// A still-running fake responds to this marker. Besides proving
				// shutdown, it provides cooperative cleanup on the buggy baseline;
				// fixture cleanup never needs to trust substituted process IDs.
				if err := os.WriteFile(f.release, nil, 0600); err != nil {
					t.Fatal(err)
				}
				time.Sleep(150 * time.Millisecond)
				if _, err := os.Stat(f.advanced); !os.IsNotExist(err) {
					t.Errorf("actual %s workload continued after Stop: %v", stage, err)
				}
				if stopErr == nil {
					if st, err := f.agent.Status("", f.ref); err != nil || st.State != Failed || st.Text != "stopped" {
						t.Errorf("successful Stop did not report stopped: %+v %v", st, err)
					}
				}
			})
		}
	}
}

type ownershipRun struct {
	agent    *Agent
	ref      string
	dir      string
	release  string
	advanced string
	worker   int
}

// The fake can advance only after release is created, and otherwise exits by
// itself after ten seconds. Its markers are outside the worktree, so a missing
// ownership guard cannot be accidentally hidden by Clean's dirty-work check.
func ownershipFixture(t *testing.T, stage string, closeLock bool) ownershipRun {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("SOUS_HOME", home)
	markers := t.TempDir()
	entered := filepath.Join(markers, "entered")
	f := ownershipRun{release: filepath.Join(markers, "release"), advanced: filepath.Join(markers, "advanced")}
	preamble := ""
	if closeLock {
		preamble = "exec 3>&-\n"
	}
	work := preamble + fmt.Sprintf(`echo $$ > %q
i=0
while [ ! -f %q ] && [ "$i" -lt 200 ]; do
  i=$((i+1))
  sleep 0.05
done
if [ -f %q ]; then echo advanced > %q; fi
`, entered, f.release, f.release, f.advanced)
	agentScript := work
	check := ""
	if stage == "check" {
		agentScript = `echo fixture > check-change
git add check-change
git -c user.name=Sam -c user.email=sam@example.invalid commit -qm fixture
` + harnesstest.Says("SOUS: done committed fixture")
		check = work
	}
	testutil.FakeBin(t, "claude", agentScript)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f.agent, _ = Builtin("claude", home, exe, 30*time.Second)
	const uid = "process-ownership"
	f.ref, err = f.agent.Start(Request{ID: 1, UID: uid, Project: gitRepo(t), Brief: "exercise the bounded fixture", Check: check})
	if err != nil {
		t.Fatal(err)
	}
	f.dir = filepath.Join(home, "runs", uid)
	t.Cleanup(func() {
		_ = os.WriteFile(f.release, nil, 0600)
		// Let the cooperative fixture finish before TempDir removes its files.
		until := time.Now().Add(time.Second)
		for f.worker > 0 && syscall.Kill(f.worker, 0) == nil && time.Now().Before(until) {
			time.Sleep(10 * time.Millisecond)
		}
	})
	f.worker = ownershipPID(t, entered)
	// The check may publish entered just before its parent replaces the
	// previous agent's group diagnostic. Wait for the actual relationship,
	// rather than racing that publication or assuming worker PID equals PGID.
	waitFor(t, func() bool {
		b, err := os.ReadFile(filepath.Join(f.dir, "agent.pgid"))
		if err != nil {
			return false
		}
		group, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil || group <= 0 {
			return false
		}
		got, err := syscall.Getpgid(f.worker)
		return err == nil && got == group
	})
	return f
}

func ownershipPID(t *testing.T, path string) int {
	t.Helper()
	var pid int
	waitFor(t, func() bool {
		b, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil && pid > 0
	})
	return pid
}
