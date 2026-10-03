// Package runs is the use case layer for runs, as filing is for filed
// notes: starting one, asking runners how theirs are going, passing on
// replies, stopping and cleaning. Nothing here parses arguments or prints.
package runs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
	"io"
)

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
	r, ok := plugin.Find(d.Runners, name)
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
		if serr := thread.SetRun(d.Store, id, func(run *thread.Run) { run.State, run.Text = runner.Failed, "did not start: "+err.Error() }); serr != nil {
			return id, false, fmt.Errorf("%w (and marking the run failed: %v)", err, serr)
		}
		return id, false, err
	}
	now := d.Now().UTC()
	return id, false, thread.SetRun(d.Store, id, func(run *thread.Run) {
		run.Ref, run.State, run.Checked = ref, runner.Running, &now
	})
}

// Refresh asks the runner of each open run how it is going, concurrently,
// each within plugin.StatusTimeout. An answer is stored; a failure keeps
// the last known state and says why (RunErr), never guessing. With local,
// only the built in runners are asked: the session hook and here must not
// wait on plugins.
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
		if local && !r.Offline {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, plugin.StatusTimeout)
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
	if v.Run.State != thread.RunStarting || d.Now().Sub(v.Since) < startGrace {
		return
	}
	d.record(v, runner.Status{State: runner.Failed, Text: "did not start: sous stopped before the runner answered"})
}

// record stores a status answer on the note and on its view.
func (d *Dispatcher) record(v *thread.View, st runner.Status) {
	now := d.Now().UTC()
	set := func(run *thread.Run) {
		run.State, run.Text, run.Checked = st.State, st.Text, &now
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
	v := d.Refresh(ctx, []thread.View{{Thread: th}}, false)[0]
	if v.Run != nil && v.Run.State == thread.RunFailed && v.Run.Log != "" {
		v.LogTail = lastLines(v.Run.Log, 3)
	}
	return v, nil
}

// lastLines is the last n lines of the file at path that hold anything;
// none when it cannot be read.
func lastLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	// An agent's log can run long; its end is all that is wanted.
	const tail = 16 << 10
	if st, err := f.Stat(); err == nil && st.Size() > tail {
		f.Seek(st.Size()-tail, io.SeekStart)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, text.Cut(l, text.ReasonRunes))
		}
	}
	return lines[max(len(lines)-n, 0):]
}

// run finds an open note's run and its runner.
func (d *Dispatcher) run(id int) (thread.Thread, runner.Runner, error) {
	th, err := thread.Get(d.Store, id)
	switch {
	case err != nil:
		return th, runner.Runner{}, err
	case th.Closed != nil:
		return th, runner.Runner{}, fmt.Errorf("%w %d", thread.ErrNotFound, id)
	}
	return d.runOf(th)
}

// runOf is a note's run and its runner, open or closed.
func (d *Dispatcher) runOf(th thread.Thread) (thread.Thread, runner.Runner, error) {
	id := th.ID
	switch {
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
	return thread.SetRun(d.Store, id, func(run *thread.Run) { run.State, run.Text = runner.Running, "" })
}

// on makes one call to a note's run through its runner, open or closed:
// stopping and cleaning are safe to repeat after the note is closed.
func (d *Dispatcher) on(ctx context.Context, th thread.Thread, call func(context.Context, runner.Runner, string, string) error) error {
	th, r, err := d.runOf(th)
	if err != nil {
		return err
	}
	return call(ctx, r, th.Project, th.Run.Ref)
}

// Finished is what closing a note did to its run.
type Finished int

const (
	NoRun     Finished = iota // not a run, or one that never started: nothing to do
	Stopped                   // the run is stopped
	Cleaned                   // stopped, and what it left behind removed
	CantClean                 // stopped; the runner cannot clean up, so its files stay
)

// Finish is what closing a note does to its run first: stop it, and with
// clean remove what it left behind, keeping its work. A note that is not a
// run is left alone (and asking to clean it is ErrNotARun). It works on a
// closed note too, so done --clean after done still cleans up.
func (d *Dispatcher) Finish(ctx context.Context, id int, clean bool) (Finished, error) {
	th, err := thread.Get(d.Store, id)
	switch {
	case err != nil:
		return NoRun, err
	case th.Run == nil && clean:
		return NoRun, fmt.Errorf("note %d %w; --clean is for runs", id, ErrNotARun)
	case th.Run == nil || th.Run.Ref == "":
		return NoRun, nil
	}
	if err := d.on(ctx, th, runner.Stop); err != nil {
		return NoRun, fmt.Errorf("stopping run %d: %w", id, err)
	}
	if !clean {
		return Stopped, nil
	}
	switch err := d.clean(ctx, th); {
	case errors.Is(err, runner.ErrUnsupported):
		return CantClean, nil
	case err != nil:
		return Stopped, fmt.Errorf("cleaning up run %d: %w", id, err)
	}
	return Cleaned, nil
}

// clean asks th's runner to remove what its run left behind, and records
// that it did.
func (d *Dispatcher) clean(ctx context.Context, th thread.Thread) error {
	if err := d.on(ctx, th, runner.Clean); err != nil {
		return err
	}
	if err := thread.MarkCleaned(d.Store, th.ID, d.Now()); err != nil {
		return fmt.Errorf("%w: %v", errNotRecorded, err)
	}
	return nil
}

// errNotRecorded: a run was cleaned, but saying so failed.
var errNotRecorded = errors.New("cleaned, but could not record it")

const (
	// TidyAfter is how long a run closed without --clean keeps what it
	// left behind before sous cleans it up.
	TidyAfter = 30 * 24 * time.Hour
	// TidyRetry is how long sous waits to try again on a run whose clean
	// refused, its work not committed.
	TidyRetry = 24 * time.Hour
	// TidyAtOnce caps how many runs one board tidies, so a long backlog
	// never holds it up.
	TidyAtOnce = 3
)

// Tidy cleans up runs closed more than TidyAfter ago that nobody cleaned:
// the built in runners' only, which keep their worktrees and logs on this
// machine (a plugin runner keeps its own). Each is tried at most once per
// TidyRetry, and no more than TidyAtOnce at a time. A run whose clean
// refuses is left for next time. The error is what could not be recorded.
func (d *Dispatcher) Tidy(ctx context.Context) error {
	now := d.Now()
	views, err := thread.Untidied(d.Store, now.Add(-TidyAfter), now.Add(-TidyRetry))
	if err != nil {
		return err
	}
	var errs []error
	tried := 0
	for _, v := range views {
		if r, err := runner.ByRef(d.Runners, v.Run.Ref); err != nil || !r.Offline {
			continue
		}
		if tried++; tried > TidyAtOnce {
			break
		}
		if err := thread.MarkTidied(d.Store, v.ID, now); err != nil {
			errs = append(errs, err)
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, plugin.StatusTimeout)
		if err := d.clean(sctx, v.Thread); errors.Is(err, errNotRecorded) {
			errs = append(errs, err)
		}
		cancel()
	}
	return errors.Join(errs...)
}
