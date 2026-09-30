package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func fakeRunner(t *testing.T, body string) runner.Runner {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sous-runner-fake")
	os.WriteFile(p, []byte("#!/bin/sh\ncase \"$1\" in\n"+body+"\nesac\n"), 0o755)
	return runner.Runners("", nil, []string{p})[0]
}

func TestStartRecordsTheRunAndAFailedStartStaysVisible(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	ok := fakeRunner(t, `start) echo fake:1;; status) echo '{"v":0,"state":"running"}';;`)
	d := &Dispatcher{Store: s, Runners: []runner.Runner{ok}, Now: time.Now}
	p := project.Project{Path: "/code/acme/billing"}
	id, _, err := d.Start(context.Background(), p, "fix it", "", "fake", "agent", "")
	th, _ := thread.Get(s, id)
	if err != nil || th.Run.Ref != "fake:1" || th.Run.State != "running" {
		t.Fatalf("%+v %v", th.Run, err)
	}
	bad := fakeRunner(t, `start) echo "no agent here" >&2; exit 1;;`)
	d.Runners = []runner.Runner{bad}
	id, _, err = d.Start(context.Background(), p, "fix it", "", "fake", "agent", "")
	th, _ = thread.Get(s, id)
	if err == nil || th.Closed != nil || th.Run.State != "failed" || !strings.Contains(th.Run.Text, "no agent here") {
		t.Fatalf("%+v %v", th.Run, err)
	}
	if _, _, err := d.Start(context.Background(), p, "x", "", "nope", "", ""); err == nil {
		t.Fatal("an unknown runner is refused before any note is made")
	}
}

// Review Focus 4.
func TestRefreshNeverGuesses(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	hang := fakeRunner(t, `start) echo fake:1;; status) sleep 30;;`)
	d := &Dispatcher{Store: s, Runners: []runner.Runner{hang}, Now: time.Now}
	id, _, _ := d.Start(context.Background(), project.Project{Path: "/p"}, "b", "", "fake", "", "")
	views, _ := thread.Runs(s)
	start := time.Now()
	got := d.Refresh(context.Background(), views, false)
	if time.Since(start) > 5*time.Second || got[0].Run.State != "running" || got[0].RunErr == "" {
		t.Fatalf("%v %+v", time.Since(start), got[0])
	}
	th, _ := thread.Get(s, id)
	if th.Run.State != "running" {
		t.Fatal("a failed status must not change the stored state")
	}
}

func TestRefreshRecordsTheAnswer(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	r := fakeRunner(t, `start) echo fake:1;; status) echo '{"v":0,"state":"done","text":"fixed","branch":"sous/run-1"}';;`)
	d := &Dispatcher{Store: s, Runners: []runner.Runner{r}, Now: time.Now}
	id, _, _ := d.Start(context.Background(), project.Project{Path: "/p"}, "b", "", "fake", "", "")
	views, _ := thread.Runs(s)
	got := d.Refresh(context.Background(), views, false)
	th, _ := thread.Get(s, id)
	if got[0].Run.State != "done" || th.Run.State != "done" || th.Run.Text != "fixed" || th.Run.Branch != "sous/run-1" || th.Run.Checked == nil {
		t.Fatalf("%+v %+v", got[0].Run, th.Run)
	}
	// local: only built ins are asked; a plugin's run is left as it was.
	views, _ = thread.Runs(s)
	views[0].Run.State = "running"
	if got := d.Refresh(context.Background(), views, true); got[0].Run.State != "running" || got[0].RunErr != "" {
		t.Fatalf("local refresh asked a plugin: %+v", got[0])
	}
}
