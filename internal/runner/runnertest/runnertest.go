// Package runnertest checks a runner against the contract, driving it the
// way sous does. Run it from a test in a sous checkout:
//
//	r := runner.Registry.Discover("", nil, []string{"/path/to/sous-runner-x"})[0]
//	runnertest.RunDoor(t, r, "/path/to/a/git/project", finish)
package runnertest

import (
	"context"
	"testing"

	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/testutil"
)

// RunDoor starts a run, checks start is safe to repeat and status answers,
// then calls finish(ref) to let the run end and waits (up to a minute) for
// done or needs_you. Finally it stops the run twice (stopping twice is fine).
// It is runner.Check, the same check `sous runner check` makes, as a test.
func RunDoor(t *testing.T, r runner.Runner, project string, finish func(ref string)) {
	t.Helper()
	for _, bad := range runner.Check(context.Background(), r, project, runner.CheckOptions{Finish: finish}) {
		t.Error(bad)
	}
}

// Fake is a runner plugin whose calls are the shell case arms in cases,
// for tests of the code that calls runners.
func Fake(t *testing.T, cases string) runner.Runner {
	t.Helper()
	p := testutil.Script(t, t.TempDir(), "sous-runner-fake", "case \"$1\" in\n"+cases+"\nesac")
	return runner.Registry.Discover("", nil, []string{p})[0]
}
