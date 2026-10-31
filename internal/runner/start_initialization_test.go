package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/testutil"
)

func TestStartRefusesOrphanWithoutDeletingWork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.FakeBin(t, "claude", "")
	exe, calls := fakeSous(t)
	a, _ := Builtin("claude", t.TempDir(), exe, time.Minute)
	dir := a.dir("orphan")
	if err := os.MkdirAll(filepath.Join(dir, "worktree"), 0700); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(dir, "worktree", "uncommitted")
	if err := os.WriteFile(kept, []byte("keep this work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ref, err := a.Start(Request{ID: 1, UID: "orphan", Project: gitRepo(t), Brief: "task"})
	if err == nil || ref != "" {
		t.Fatalf("orphan accepted as initialized run: %q %v", ref, err)
	}
	b, err := os.ReadFile(kept)
	if err != nil || string(b) != "keep this work\n" {
		t.Fatalf("orphan work was changed: %q %v", b, err)
	}
	if b, _ := os.ReadFile(calls); len(b) != 0 {
		t.Fatalf("orphan launched watcher: %s", b)
	}
}

func TestStartWaitsForInitialization(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testutil.FakeBin(t, "claude", "")
	exe, _ := fakeSous(t)
	a, _ := Builtin("claude", t.TempDir(), exe, time.Minute)
	repo := gitRepo(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	gate := t.TempDir()
	ready, proceed := filepath.Join(gate, "ready"), filepath.Join(gate, "proceed")
	testutil.FakeBin(t, "git", fmt.Sprintf(`
case "$*" in
  *"worktree add"*) : > %q; while [ ! -f %q ]; do sleep 0.01; done;;
esac
exec %q "$@"
`, ready, proceed, realGit))
	defer os.WriteFile(proceed, nil, 0600)
	req := Request{ID: 1, UID: "initializing", Project: repo, Brief: "task"}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := a.Start(req); first <- err }()
	waitFor(t, func() bool { _, err := os.Stat(ready); return err == nil })
	go func() { _, err := a.Start(req); second <- err }()
	var early *error
	select {
	case err := <-second:
		early = &err
	case <-time.After(100 * time.Millisecond):
	}
	if err := os.WriteFile(proceed, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if early != nil {
		t.Fatalf("second Start answered before initialization completed: %v", *early)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
}

func TestStartRefusesUnreadableInitializationMetadata(t *testing.T) {
	for _, kind := range []string{"malformed JSON", "directory"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			testutil.FakeBin(t, "claude", "")
			exe, calls := fakeSous(t)
			a, _ := Builtin("claude", t.TempDir(), exe, time.Minute)
			dir := a.dir("partial")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			meta := filepath.Join(dir, "run.json")
			if kind == "directory" {
				if err := os.Mkdir(meta, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(meta, []byte(`{"name":`), 0600); err != nil {
				t.Fatal(err)
			}
			kept := filepath.Join(dir, "private-work")
			if err := os.WriteFile(kept, []byte("keep this work\n"), 0600); err != nil {
				t.Fatal(err)
			}
			ref, err := a.Start(Request{ID: 1, UID: "partial", Project: gitRepo(t), Brief: "task"})
			if err == nil || ref != "" {
				t.Fatalf("unreadable metadata accepted as initialized run: %q %v", ref, err)
			}
			if b, err := os.ReadFile(kept); err != nil || string(b) != "keep this work\n" {
				t.Fatalf("private work changed: %q %v", b, err)
			}
			if kind == "directory" {
				if st, err := os.Stat(meta); err != nil || !st.IsDir() {
					t.Fatalf("metadata directory changed: %v %v", st, err)
				}
			} else if b, err := os.ReadFile(meta); err != nil || string(b) != `{"name":` {
				t.Fatalf("metadata changed: %q %v", b, err)
			}
			if b, _ := os.ReadFile(calls); len(b) != 0 {
				t.Fatalf("unreadable run launched watcher: %s", b)
			}
		})
	}
}
