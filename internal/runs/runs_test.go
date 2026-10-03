package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/runner/runnertest"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func TestStartRecordsTheRunAndAFailedStartStaysVisible(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	ok := runnertest.Fake(t, `start) echo fake:1;; status) echo '{"v":0,"state":"running"}';;`)
	d := &Dispatcher{Store: s, Runners: []runner.Runner{ok}, Now: time.Now}
	p := project.Project{Path: "/code/acme/billing"}
	id, _, err := d.Start(context.Background(), p, "fix it", "", "fake", "agent", "")
	th, _ := thread.Get(s, id)
	if err != nil || th.Run.Ref != "fake:1" || th.Run.State != "running" {
		t.Fatalf("%+v %v", th.Run, err)
	}
	bad := runnertest.Fake(t, `start) echo "no agent here" >&2; exit 1;;`)
	d.Runners = []runner.Runner{bad}
	id, _, err = d.Start(context.Background(), p, "fix the other thing", "", "fake", "agent", "")
	th, _ = thread.Get(s, id)
	if err == nil || th.Closed != nil || th.Run.State != "failed" || !strings.Contains(th.Run.Text, "no agent here") {
		t.Fatalf("%+v %v", th.Run, err)
	}
	if _, _, err := d.Start(context.Background(), p, "x", "", "nope", "", ""); err == nil {
		t.Fatal("an unknown runner is refused before any note is made")
	}
}

func TestRefreshNeverGuesses(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	hang := runnertest.Fake(t, `start) echo fake:1;; status) sleep 30;;`)
	d := &Dispatcher{Store: s, Runners: []runner.Runner{hang}, Now: time.Now}
	id, _, _ := d.Start(context.Background(), project.Project{Path: "/p"}, "b", "", "fake", "", "")
	views, _ := thread.Runs(s, time.Now())
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
	r := runnertest.Fake(t, `start) echo fake:1;; status) echo '{"v":0,"state":"done","text":"fixed","branch":"sous/run-1"}';;`)
	d := &Dispatcher{Store: s, Runners: []runner.Runner{r}, Now: time.Now}
	id, _, _ := d.Start(context.Background(), project.Project{Path: "/p"}, "b", "", "fake", "", "")
	views, _ := thread.Runs(s, time.Now())
	got := d.Refresh(context.Background(), views, false)
	th, _ := thread.Get(s, id)
	if got[0].Run.State != "done" || th.Run.State != "done" || th.Run.Text != "fixed" || th.Run.Branch != "sous/run-1" || th.Run.Checked == nil {
		t.Fatalf("%+v %+v", got[0].Run, th.Run)
	}
	// local: only built ins are asked; a plugin's run is left as it was.
	views, _ = thread.Runs(s, time.Now())
	views[0].Run.State = "running"
	if got := d.Refresh(context.Background(), views, true); got[0].Run.State != "running" || got[0].RunErr != "" {
		t.Fatalf("local refresh asked a plugin: %+v", got[0])
	}
}

// A run that never got a ref (sous stopped mid start) ends as
// failed once its start has had time to answer, instead of starting forever.
func TestStuckStartEndsAsFailed(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	then := time.Now().Add(-time.Hour)
	id, _, _ := thread.NoteRun(s, project.Project{Path: "/p"}, "b", "k", "fake", "", then)
	fresh, _, _ := thread.NoteRun(s, project.Project{Path: "/p"}, "c", "", "fake", "", time.Now())
	d := &Dispatcher{Store: s, Now: time.Now}
	views, _ := thread.Runs(s, time.Now())
	d.Refresh(context.Background(), views, true)
	if th, _ := thread.Get(s, id); th.Run.State != "failed" || !strings.Contains(th.Run.Text, "did not start") {
		t.Fatalf("%+v", th.Run)
	}
	if th, _ := thread.Get(s, fresh); th.Run.State != "starting" {
		t.Fatalf("a start still in progress is left alone: %+v", th.Run)
	}
}

// Finish is what sous done does to a run before the note closes: stop it,
// and with clean remove what it left. A note that is not a run is left
// alone, and clean is then a mistake; a runner that cannot clean is not.
func TestFinish(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	r := runnertest.Fake(t, `start) echo fake:1;; stop) echo stopped >> "$(dirname "$0")/calls";; clean) echo cleaned >> "$(dirname "$0")/calls";;`)
	calls := filepath.Join(filepath.Dir(r.Argv[0]), "calls")
	d := &Dispatcher{Store: s, Runners: []runner.Runner{r}, Now: time.Now}
	id, _, err := d.Start(context.Background(), project.Project{Path: "/code/acme/billing"}, "fix it", "", "fake", "human", "")
	if err != nil {
		t.Fatal(err)
	}
	if done, err := d.Finish(context.Background(), id, true); err != nil || done != Cleaned {
		t.Fatal(done, err)
	}
	if b, _ := os.ReadFile(calls); string(b) != "stopped\ncleaned\n" {
		t.Fatalf("%q", b)
	}
	note, _, _ := thread.Note(s, project.Project{Path: "/code/acme/billing"}, thread.Me, "not a run", "human", time.Now())
	if done, err := d.Finish(context.Background(), note, false); err != nil || done != NoRun {
		t.Fatal("a note without a run has nothing to finish:", err)
	}
	if _, err := d.Finish(context.Background(), note, true); !errors.Is(err, ErrNotARun) {
		t.Fatal("clean is for runs:", err)
	}
	noclean := runnertest.Fake(t, `start) echo fake:2;; stop) exit 0;; clean) exit 2;;`)
	d.Runners = []runner.Runner{noclean}
	id, _, _ = d.Start(context.Background(), project.Project{Path: "/code/acme/billing"}, "fix that", "", "fake", "human", "")
	if done, err := d.Finish(context.Background(), id, true); err != nil || done != CantClean {
		t.Fatal("a runner that cannot clean still finishes:", done, err)
	}
	stuck := runnertest.Fake(t, `start) echo fake:3;; stop) echo "still busy" >&2; exit 1;;`)
	d.Runners = []runner.Runner{stuck}
	id, _, _ = d.Start(context.Background(), project.Project{Path: "/code/acme/billing"}, "fix more", "", "fake", "human", "")
	if _, err := d.Finish(context.Background(), id, false); err == nil || !strings.Contains(err.Error(), "still busy") {
		t.Fatal(err)
	}
}
