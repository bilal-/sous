// Package runs is the use case layer for runs, as filing is for filed
// notes: starting one, asking runners how theirs are going, passing on
// replies, stopping and cleaning. Nothing here parses arguments or prints.
package runs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

// StatusTimeout bounds each runner's status during a refresh.
const StatusTimeout = 3 * time.Second

var (
	// ErrNoRunner: no runner by that name.
	ErrNoRunner = errors.New("no runner named")
	// ErrNotARun: the note has no run.
	ErrNotARun = errors.New("is not a run")
)

// Dispatcher holds what every run operation needs. Build one per invocation.
type Dispatcher struct {
	Store   *store.Store
	Runners []runner.Runner
	Now     func() time.Time
}

// Names are the runners there are, for messages.
func (d *Dispatcher) Names() []string {
	var names []string
	for _, r := range d.Runners {
		names = append(names, r.Name)
	}
	return names
}

// Start makes the run's note and hands the task to the runner. With a key,
// a run already made with it is returned (existed) and nothing starts. A
// runner that fails to start leaves the run on the board as failed, with
// the reason, never silently dropped.
func (d *Dispatcher) Start(ctx context.Context, p project.Project, brief, key, name, source, hereFile string) (int, bool, error) {
	r, ok := runner.Find(d.Runners, name)
	if !ok {
		return 0, false, fmt.Errorf("%w %q", ErrNoRunner, name)
	}
	id, existed, err := thread.NoteRun(d.Store, p, brief, key, name, source, d.Now())
	if err != nil || existed {
		return id, existed, err
	}
	th, err := thread.Get(d.Store, id)
	if err != nil {
		return id, false, err
	}
	ref, err := runner.Start(ctx, r, runner.Request{ID: id, UID: th.UID, Project: p.Path, Brief: brief, HereFile: hereFile})
	if err != nil {
		if serr := thread.SetRun(d.Store, id, func(run *thread.Run) { run.State, run.Text = string(runner.Failed), "did not start: "+err.Error() }); serr != nil {
			return id, false, fmt.Errorf("%w (and marking the run failed: %v)", err, serr)
		}
		return id, false, err
	}
	now := d.Now().UTC()
	return id, false, thread.SetRun(d.Store, id, func(run *thread.Run) {
		run.Ref, run.State, run.Checked = ref, string(runner.Running), &now
	})
}

// Refresh asks the runner of each open run how it is going, concurrently,
// each within StatusTimeout. An answer is stored; a failure keeps the last
// known state and says why (RunErr), never guessing. With local, only the
// built in runners are asked: the session hook and here must not wait on
// plugins.
func (d *Dispatcher) Refresh(ctx context.Context, views []thread.View, local bool) []thread.View {
	var wg sync.WaitGroup
	for i := range views {
		v := &views[i]
		if v.Run == nil || v.Closed != nil {
			continue
		}
		if v.Run.Ref == "" {
			d.giveUpOnStart(v)
			continue
		}
		r, err := runner.ByRef(d.Runners, v.Run.Ref)
		if err != nil {
			v.RunErr = err.Error()
			continue
		}
		if local && !slices.Contains(runner.BuiltinNames(), r.Name) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, StatusTimeout)
			defer cancel()
			st, err := runner.GetStatus(sctx, r, v.Project, v.Run.Ref)
			if err != nil {
				v.RunErr = err.Error()
				return
			}
			d.record(v, st)
		}()
	}
	wg.Wait()
	return views
}

// startGrace is how long a start may take to answer before a run with no
// ref is taken as never started: sous stopped before the runner replied.
const startGrace = 2 * runner.Timeout

// giveUpOnStart ends a run stuck starting, so it neither sits on the board
// forever nor holds its key.
func (d *Dispatcher) giveUpOnStart(v *thread.View) {
	if v.Run.State != "starting" || d.Now().Sub(v.Since) < startGrace {
		return
	}
	d.record(v, runner.Status{State: runner.Failed, Text: "did not start: sous stopped before the runner answered"})
}

// record stores a status answer on the note and on its view.
func (d *Dispatcher) record(v *thread.View, st runner.Status) {
	now := d.Now().UTC()
	set := func(run *thread.Run) {
		run.State, run.Text, run.Checked = string(st.State), st.Text, &now
		run.Branch, run.Worktree, run.Log = st.Branch, st.Worktree, st.Log
	}
	run := *v.Run
	set(&run)
	v.Run = &run
	if err := thread.SetRun(d.Store, v.ID, set); err != nil {
		v.RunErr = "could not record the state: " + err.Error()
	}
}

// Show is one note, and for an open run, how it is going now.
func (d *Dispatcher) Show(ctx context.Context, id int) (thread.View, error) {
	th, err := thread.Get(d.Store, id)
	if err != nil {
		return thread.View{}, err
	}
	v := []thread.View{{Thread: th}}
	return d.Refresh(ctx, v, false)[0], nil
}

// run finds an open note's run and its runner.
func (d *Dispatcher) run(id int) (thread.Thread, runner.Runner, error) {
	th, err := thread.Get(d.Store, id)
	switch {
	case err != nil:
		return th, runner.Runner{}, err
	case th.Closed != nil:
		return th, runner.Runner{}, fmt.Errorf("%w %d", thread.ErrNotFound, id)
	case th.Run == nil:
		return th, runner.Runner{}, fmt.Errorf("note %d %w", id, ErrNotARun)
	case th.Run.Ref == "":
		return th, runner.Runner{}, fmt.Errorf("run %d never started: %s", id, th.Run.Text)
	}
	r, err := runner.ByRef(d.Runners, th.Run.Ref)
	return th, r, err
}

// Reply passes the person's answer to the run; it carries on.
func (d *Dispatcher) Reply(ctx context.Context, id int, answer string) error {
	th, r, err := d.run(id)
	if err != nil {
		return err
	}
	if err := runner.Reply(ctx, r, th.Project, th.Run.Ref, answer); err != nil {
		return err
	}
	return thread.SetRun(d.Store, id, func(run *thread.Run) { run.State, run.Text = string(runner.Running), "" })
}

// Stop ends the run, if it is still going.
func (d *Dispatcher) Stop(ctx context.Context, id int) error {
	th, r, err := d.run(id)
	if err != nil {
		return err
	}
	return runner.Stop(ctx, r, th.Project, th.Run.Ref)
}

// Clean removes what the run left behind, keeping its work.
func (d *Dispatcher) Clean(ctx context.Context, id int) error {
	th, r, err := d.run(id)
	if err != nil {
		return err
	}
	return runner.Clean(ctx, r, th.Project, th.Run.Ref)
}
