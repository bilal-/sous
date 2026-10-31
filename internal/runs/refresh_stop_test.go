package runs

import (
	"context"
	"fmt"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/runner/runnertest"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewRefreshCannotOverwriteStop(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	gates := t.TempDir()
	entered := filepath.Join(gates, "entered")
	release := filepath.Join(gates, "release")
	r := runnertest.Fake(t, fmt.Sprintf(`start) echo fake:1;;
status)
  echo entered > %q
  i=0
  while [ ! -f %q ] && [ "$i" -lt 200 ]; do sleep 0.01; i=$((i+1)); done
  echo '{"v":0,"state":"running","phase":"working","text":"earlier observation"}'
  ;;
stop) exit 0;;`, entered, release))
	d := &Dispatcher{Store: s, Runners: []runner.Runner{r}, Now: time.Now}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, _, err := d.Start(ctx, project.Project{Path: "/code/acme/api"}, "review lifecycle", "", "fake", "human", "")
	if err != nil {
		t.Fatal(err)
	}
	views, err := thread.Runs(s, time.Now())
	if err != nil || len(views) != 1 {
		t.Fatalf("views: %+v %v", views, err)
	}
	finished := make(chan []thread.View, 1)
	go func() { finished <- d.Refresh(ctx, views, false) }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(entered); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake status never entered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err = d.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	stopped, err := thread.Get(s, id)
	if err != nil || stopped.Run.State != thread.RunFailed || stopped.Run.Text != "stopped" {
		t.Fatalf("stop: %+v %v", stopped.Run, err)
	}
	if err = os.WriteFile(release, []byte("release"), 0600); err != nil {
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
		t.Fatalf("late status overwrote successful Stop: saved=%+v view=%+v err=%v", current.Run, result[0].Run, err)
	}
	if result[0].Run.State != thread.RunFailed || result[0].Run.Text != "stopped" {
		t.Fatalf("late status returned obsolete working view: %+v", result[0].Run)
	}
}
