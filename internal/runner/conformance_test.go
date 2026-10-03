package runner_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/harness/harnesstest"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/runner/runnertest"
	"github.com/bilal-/sous/internal/testutil"
)

// TestMain lets this test binary act as `sous runner <name> <op>`, so the
// built ins are driven through the same door as a plugin program.
func TestMain(m *testing.M) {
	if len(os.Args) > 3 && os.Args[1] == "runner" {
		os.Exit(runner.Registry.Serve(runner.Deps{Home: os.Getenv("SOUS_HOME"), Exe: os.Args[0], Limit: time.Minute}, os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// fakeAgent waits for the test to say go, then finishes the way every
// built in agent does.
var fakeAgent = "while [ ! -f go ]; do sleep 0.05; done\n" + harnesstest.Says("SOUS: done nothing to do")

func TestBuiltinsConform(t *testing.T) {
	for _, name := range runner.Registry.Names() {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("SOUS_HOME", home)
			testutil.FakeBin(t, name, fakeAgent)
			repo := testutil.Repo(t, t.TempDir(), true, "")
			exe, _ := os.Executable()
			r := runner.Registry.Discover(exe, []string{name}, nil)[0]
			runnertest.RunDoor(t, r, repo, func(ref string) {
				uid := ref[len(name)+1:]
				os.WriteFile(filepath.Join(home, "runs", uid, "worktree", "go"), nil, 0o600)
			})
		})
	}
}
