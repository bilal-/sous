package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Intended for the owner/anchor implementation. It uses filesystem diagnostics
// and existing Agent APIs, without referring to owner implementation structs.
func TestUncertainOwnershipSurvivesReleasedLockAndLateResult(t *testing.T) {
	for _, stage := range []string{"agent", "check"} {
		for _, loseWatcher := range []bool{false, true} {
			name := "watcher-observes-owner-loss"
			if loseWatcher {
				name = "all-supervisors-lost"
			}
			t.Run(stage+"/"+name, func(t *testing.T) {
				f := ownershipFixture(t, stage, true)
				ownerPID := ownershipPID(t, filepath.Join(f.dir, "owner.pid"))
				anchorPID := ownershipPID(t, filepath.Join(f.dir, "agent.pgid"))
				watcherPID := ownershipPID(t, filepath.Join(f.dir, "watcher.pid"))
				if loseWatcher {
					if err := syscall.Kill(watcherPID, syscall.SIGKILL); err != nil {
						t.Fatal(err)
					}
				}
				// These are only this fixture's still-running supervisors. Kill
				// the anchor alone, not its group: the fake worker must survive.
				if err := syscall.Kill(ownerPID, syscall.SIGKILL); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Kill(anchorPID, syscall.SIGKILL); err != nil {
					t.Fatal(err)
				}
				waitFor(t, func() bool { return ownershipLockFree(f.dir) })
				if err := syscall.Kill(f.worker, 0); err != nil {
					t.Fatalf("fixture worker should remain alive after closing fd 3: %v", err)
				}
				if st, err := f.agent.Status("", f.ref); err == nil {
					t.Errorf("Status guessed a finished/running state after losing ownership: %+v", st)
				}
				if err := f.agent.Stop("", f.ref); err == nil {
					t.Error("Stop reported success with a free lock but a live unowned worker")
				}
				if _, err := os.Stat(filepath.Join(f.dir, "uncertain.json")); err != nil {
					t.Fatalf("missing outcome did not preserve durable uncertainty: %v", err)
				}

				// Make the ordinary completed-run/Reply prerequisites true using
				// established runner-owned JSON fields. An apparently successful
				// late result must not silently restore lost process ownership.
				var meta map[string]any
				b, err := os.ReadFile(filepath.Join(f.dir, "run.json"))
				if err != nil || json.Unmarshal(b, &meta) != nil {
					t.Fatalf("reading fixture metadata: %v", err)
				}
				meta["session"] = "fixture-session"
				ownershipWriteJSON(t, filepath.Join(f.dir, "run.json"), meta)
				ownershipWriteJSON(t, filepath.Join(f.dir, "result.json"), map[string]any{
					"exit": 0, "message": "SOUS: done fixture", "session": "fixture-session", "ended": time.Now().UTC(),
				})
				if st, err := f.agent.Status("", f.ref); err == nil {
					t.Errorf("late success file erased uncertain status: %+v", st)
				}
				if err := f.agent.Stop("", f.ref); err == nil {
					t.Error("late success file made Stop accept uncertain ownership")
				}
				if err := f.agent.Reply("", f.ref, "use the bounded fixture"); err == nil {
					t.Error("Reply resumed a run whose previous worker may still be alive")
				}
				if err := f.agent.Clean("", f.ref); err == nil {
					t.Error("Clean removed a run whose worker may still be alive")
				}
				if _, err := os.Stat(filepath.Join(f.dir, "worktree")); err != nil {
					t.Errorf("uncertain run's worktree was removed: %v", err)
				}
				if _, err := os.Stat(filepath.Join(f.dir, "uncertain.json")); err != nil {
					t.Errorf("operation silently erased uncertainty: %v", err)
				}
			})
		}
	}
}

func ownershipLockFree(dir string) bool {
	f, err := os.OpenFile(filepath.Join(dir, "watcher.lock"), os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	// A separate open-file description, never the inherited lock: close
	// releases this probe only. Do not use LOCK_UN on a shared description.
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

func ownershipWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
