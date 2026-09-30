// Package runnertest checks a runner against the contract, driving it the
// way sous does. Run it from a test in a sous checkout:
//
//	r := runner.Runners("", nil, []string{"/path/to/sous-runner-x"})[0]
//	runnertest.RunDoor(t, r, "/path/to/a/git/project", finish)
package runnertest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/runner"
)

// RunDoor starts a run, checks start is safe to repeat and status answers,
// then calls finish(ref) to let the run end and waits (up to a minute) for
// done or needs_you. Finally it stops the run twice (stopping twice is fine).
func RunDoor(t *testing.T, r runner.Runner, project string, finish func(ref string)) {
	t.Helper()
	ctx := context.Background()
	req := runner.Request{ID: 1, UID: "runnertest01", Project: project, Brief: "runnertest: make no changes and finish"}
	ref, err := runner.Start(ctx, r, req)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if again, err := runner.Start(ctx, r, req); err != nil || again != ref {
		t.Fatalf("start again with the same uid gave %q, %v; want %q", again, err, ref)
	}
	if !strings.HasPrefix(ref, r.Name+":") {
		t.Errorf("ref %q must start with %q", ref, r.Name+":")
	}
	st, err := runner.GetStatus(ctx, r, project, ref)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	finish(ref)
	for deadline := time.Now().Add(time.Minute); st.State == runner.Running && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if st, err = runner.GetStatus(ctx, r, project, ref); err != nil {
			t.Fatalf("status: %v", err)
		}
	}
	if st.State != runner.Done && st.State != runner.NeedsYou {
		t.Fatalf("after finish: %+v", st)
	}
	for range 2 {
		if err := runner.Stop(ctx, r, project, ref); err != nil {
			t.Fatalf("stop: %v", err)
		}
	}
}
