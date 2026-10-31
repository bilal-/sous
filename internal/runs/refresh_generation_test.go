package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/runner/runnertest"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

// A status read may span an explicit resume and stop. Both completed actions
// must win even when they return the run to the state held when the read began.
func TestRefreshKeepsStopAfterResume(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	gates := t.TempDir()
	requested := filepath.Join(gates, "requested")
	resumed := filepath.Join(gates, "resumed")
	sampled := filepath.Join(gates, "sampled")
	release := filepath.Join(gates, "release")
	r := runnertest.Fake(t, fmt.Sprintf(`start) echo fake:1;;
status)
  echo requested > %q
  while [ ! -f %q ]; do sleep 0.01; done
  echo sampled > %q
  while [ ! -f %q ]; do sleep 0.01; done
  echo '{"v":0,"state":"running","text":"the resumed agent was working"}'
  ;;
reply) cat > /dev/null; echo resumed > %q;;
stop) exit 0;;`, requested, resumed, sampled, release, resumed))
	// Checked is a source-observation time, not an action-generation token.
	// A fixed clock also makes accidental timestamp-based guards insufficient.
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	d := &Dispatcher{Store: s, Runners: []runner.Runner{r}, Now: func() time.Time { return now }}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	waitFor := func(path string) {
		t.Helper()
		for {
			if _, err := os.Stat(path); err == nil {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("fake runner did not reach %s: %v", filepath.Base(path), ctx.Err())
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	id, _, err := d.Start(ctx, project.Project{Path: "/code/acme/api"}, "check resume and stop", "", "fake", "human", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	views, err := thread.Runs(s, now)
	if err != nil || len(views) != 1 || views[0].Run.State != thread.RunFailed || views[0].Run.Text != "stopped" {
		t.Fatalf("stopped baseline: %+v %v", views, err)
	}
	finished := make(chan []thread.View, 1)
	go func() { finished <- d.Refresh(ctx, views, false) }()
	waitFor(requested)
	// The in-flight status call samples a running agent after this explicit
	// resume but does not deliver its answer until the second stop completes.
	if err := d.Reply(ctx, id, "carry on"); err != nil {
		t.Fatal(err)
	}
	waitFor(sampled)
	if err := d.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	stopped, err := thread.Get(s, id)
	if err != nil || stopped.Run.State != thread.RunFailed || stopped.Run.Text != "stopped" {
		t.Fatalf("second stop: %+v %v", stopped.Run, err)
	}
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	var result []thread.View
	select {
	case result = <-finished:
	case <-ctx.Done():
		t.Fatal("refresh did not finish")
	}
	current, err := thread.Get(s, id)
	if err != nil || current.Run.State != thread.RunFailed || current.Run.Text != "stopped" {
		t.Fatalf("late status overwrote the second successful stop: saved=%+v view=%+v err=%v", current.Run, result[0].Run, err)
	}
	if result[0].Run.State != thread.RunFailed || result[0].Run.Text != "stopped" || result[0].RunErr != "" {
		t.Fatalf("refresh returned an obsolete resumed run: %+v", result[0])
	}
}
