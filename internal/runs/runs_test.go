package runs

import (
	"context"
	"errors"
	"fmt"
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

// Showing a failed run reads the end of its log, so the reason is there
// without opening it.
func TestShowReadsAFailedRunsLog(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	log := filepath.Join(t.TempDir(), "log")
	os.WriteFile(log, []byte("starting\n\nstep one\nstep two\npanic: no fixture\n\n"), 0o600)
	r := runnertest.Fake(t, `start) echo fake:1;; status) echo '{"v":0,"state":"failed","text":"exit 2","log":"`+log+`"}';;`)
	d := &Dispatcher{Store: s, Runners: []runner.Runner{r}, Now: time.Now}
	id, _, _ := d.Start(context.Background(), project.Project{Path: "/code/acme/api"}, "fix it", "", "fake", "human", "")
	v, err := d.Show(context.Background(), id)
	if err != nil || strings.Join(v.LogTail, "|") != "step one|step two|panic: no fixture" {
		t.Fatalf("%q %v", v.LogTail, err)
	}
}

// A run closed by a plain done can still be cleaned up after.
func TestFinishAClosedRun(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	r := runnertest.Fake(t, `start) echo fake:1;; stop) exit 0;; clean) echo cleaned > "$(dirname "$0")/calls";;`)
	d := &Dispatcher{Store: s, Runners: []runner.Runner{r}, Now: time.Now}
	id, _, _ := d.Start(context.Background(), project.Project{Path: "/code/acme/api"}, "fix it", "", "fake", "human", "")
	thread.Done(s, id, time.Now())
	if done, err := d.Finish(context.Background(), id, true); err != nil || done != Cleaned {
		t.Fatal(done, err)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(r.Argv[0]), "calls")); string(b) != "cleaned\n" {
		t.Fatalf("%q", b)
	}
}

// A built in run closed long enough ago is cleaned up once, by sous: its
// folder does not stay forever. Recent ones, plugin runs and runs whose
// clean refuses (work not committed) are left; cleaned ones are not
// asked again.
func TestTidyCleansOldClosedRuns(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	calls := filepath.Join(t.TempDir(), "calls")
	local := runnertest.Fake(t, `start) echo fake:$$;; clean) echo "$3" >> `+calls+`;;`)
	local.Offline = true // as a built in is
	plugin := runnertest.Fake(t, `start) echo other:1;; clean) echo plugin >> `+calls+`;;`)
	plugin.Name = "other"
	d := &Dispatcher{Store: s, Runners: []runner.Runner{local, plugin}, Now: time.Now}
	p := project.Project{Path: "/code/acme/api"}
	old, _, _ := d.Start(context.Background(), p, "old", "", "fake", "human", "")
	recent, _, _ := d.Start(context.Background(), p, "recent", "", "fake", "human", "")
	viaPlugin, _, _ := d.Start(context.Background(), p, "via plugin", "", "other", "human", "")
	thread.Done(s, old, now.Add(-31*24*time.Hour))
	thread.Done(s, recent, now.Add(-time.Hour))
	thread.Done(s, viaPlugin, now.Add(-31*24*time.Hour))
	for range 2 {
		if err := d.Tidy(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(calls)
	if got := strings.Fields(string(b)); len(got) != 1 {
		t.Fatalf("one old built in run cleaned, once: %q", got)
	}
	th, _ := thread.Get(s, old)
	if th.Run.Cleaned == nil {
		t.Fatal("the clean is recorded")
	}
}

// A run whose clean refuses is asked again a day later, not on every
// board, and one board tidies only a few runs however many are due.
func TestTidyWaitsAndPaces(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	calls := filepath.Join(t.TempDir(), "calls")
	local := runnertest.Fake(t, `start) echo fake:$$;; clean) echo "$3" >> `+calls+`; exit 2;;`)
	local.Offline = true
	d := &Dispatcher{Store: s, Runners: []runner.Runner{local}, Now: func() time.Time { return now }}
	p := project.Project{Path: "/code/acme/api"}
	for i := range TidyAtOnce + 1 {
		id, _, _ := d.Start(context.Background(), p, fmt.Sprintf("run %d", i), "", "fake", "human", "")
		thread.Done(s, id, now.Add(-31*24*time.Hour))
	}
	count := func() int { b, _ := os.ReadFile(calls); return len(strings.Fields(string(b))) }
	d.Tidy(context.Background())
	if got := count(); got != TidyAtOnce {
		t.Fatalf("one board tidies %d, tried %d", TidyAtOnce, got)
	}
	d.Tidy(context.Background())
	if got := count(); got != TidyAtOnce+1 {
		t.Fatalf("the next board takes the rest and asks none again: %d", got)
	}
	d.Tidy(context.Background())
	if got := count(); got != TidyAtOnce+1 {
		t.Fatalf("a refusal is not asked again the same day: %d", got)
	}
	now = now.Add(TidyRetry + time.Minute)
	d.Tidy(context.Background())
	if got := count(); got != 2*TidyAtOnce+1 {
		t.Fatalf("a day later it is asked again: %d", got)
	}
}
