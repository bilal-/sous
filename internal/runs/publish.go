package runs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/thread"
)

var (
	// ErrCommitChanged: the commit being approved is no longer the result.
	ErrCommitChanged = errors.New("the result moved on")
	// ErrNoResult: the run has no result to approve.
	ErrNoResult = errors.New("has no result to approve")
	// ErrPrivateState: the work carries a runner's private state, which
	// must never be published.
	ErrPrivateState = errors.New("carries a runner's private state")
	// ErrBadBranch: the runner named a branch outside its own namespace.
	ErrBadBranch = errors.New("names a branch it may not use")
)

// PublishError says which stage of publishing failed: push or pr.
type PublishError struct {
	Stage string
	Err   error
}

func (e PublishError) Error() string { return e.Stage + ": " + e.Err.Error() }
func (e PublishError) Unwrap() error { return e.Err }

// privatePaths are folders a runner keeps its own state in, which sous
// refuses to publish.
var privatePaths = []string{".orchid/"}

// Approve publishes commit, the run's result, once the person approved it:
// the runner accepts it and names its branch, sous pushes exactly that
// commit to that branch (never forced), then opens a draft PR, or finds the
// one it opened before. Each stage is recorded as it is reached, so calling
// again after a failure carries on where it stopped.
func (d *Dispatcher) Approve(ctx context.Context, id int, commit, op string) (thread.RunPublication, error) {
	th, r, err := d.run(id)
	if err != nil {
		return thread.RunPublication{}, err
	}
	pub := th.Run.Publication
	if pub != nil && pub.Commit != commit {
		return *pub, fmt.Errorf("run %d already published %s: %w", id, pub.Commit, ErrCommitChanged)
	}
	if pub == nil {
		v := d.Refresh(ctx, []thread.View{{Thread: th}}, false)[0]
		switch res := v.Run.Result; {
		case v.RunErr != "":
			return thread.RunPublication{}, fmt.Errorf("asking run %d for its result: %s", id, v.RunErr)
		case res == nil:
			return thread.RunPublication{}, fmt.Errorf("run %d %w", id, ErrNoResult)
		case res.Commit != commit:
			return thread.RunPublication{}, fmt.Errorf("run %d's result is now %s: %w", id, res.Commit, ErrCommitChanged)
		}
		ap, err := runner.Approve(ctx, r, th.Project, th.Run.Ref, commit, op)
		switch {
		case errors.Is(err, runner.ErrConflict):
			return thread.RunPublication{}, fmt.Errorf("%w: %v", ErrCommitChanged, err)
		case err != nil:
			return thread.RunPublication{}, err
		case !strings.HasPrefix(ap.Branch, "sous/") && !strings.HasPrefix(ap.Branch, r.Name+"/"):
			return thread.RunPublication{}, fmt.Errorf("%s %q (want sous/… or %s/…): %w", r.Name, ap.Branch, r.Name, ErrBadBranch)
		}
		pub = &thread.RunPublication{Stage: thread.PubAccepted, Commit: commit, Base: ap.Base, Branch: ap.Branch}
		if err := d.savePub(id, pub); err != nil {
			return *pub, err
		}
	}
	if pub.Stage == thread.PubAccepted {
		if err := d.push(th.Project, pub); err != nil {
			pub.Error = err.Error()
			d.savePub(id, pub)
			return *pub, err
		}
		pub.Stage, pub.Error = thread.PubPushed, ""
		if err := d.savePub(id, pub); err != nil {
			return *pub, err
		}
	}
	if pub.Stage == thread.PubPushed {
		url, err := d.openDraft(th.Project, pub.Branch, th.Text, th.Run.Brief)
		if err != nil {
			pub.Error = err.Error()
			d.savePub(id, pub)
			return *pub, PublishError{"pr", err}
		}
		pub.Stage, pub.PR, pub.Error = thread.PubPublished, url, ""
		if err := d.savePub(id, pub); err != nil {
			return *pub, err
		}
	}
	return *pub, nil
}

func (d *Dispatcher) savePub(id int, pub *thread.RunPublication) error {
	p := *pub
	return thread.SetRun(d.Store, id, func(run *thread.Run) { run.Publication = &p })
}

// push checks the branch holds exactly the approved commit and nothing
// private on the way to it, then pushes that commit, never forced.
func (d *Dispatcher) push(repo string, pub *thread.RunPublication) error {
	at, err := project.Git(repo, "rev-parse", "--verify", "--quiet", pub.Branch+"^{commit}")
	if strings.TrimSpace(at) != pub.Commit || err != nil {
		return PublishError{"push", fmt.Errorf("branch %s is at %q, not the approved %s", pub.Branch, strings.TrimSpace(at), pub.Commit)}
	}
	span := pub.Commit
	if pub.Base != "" {
		span = pub.Base + ".." + pub.Commit
	}
	files, err := project.Git(repo, "log", "--format=", "--name-only", span)
	if err != nil {
		return PublishError{"push", err}
	}
	for _, f := range strings.Split(files, "\n") {
		for _, p := range privatePaths {
			if strings.HasPrefix(strings.TrimSpace(f), p) {
				return fmt.Errorf("%s: %w (%s)", pub.Branch, ErrPrivateState, strings.TrimSpace(f))
			}
		}
	}
	if pub.Remote == "" {
		pub.Remote = pushRemote(repo)
	}
	if pub.Remote == "" {
		return PublishError{"push", errors.New("the project has no remote to push to")}
	}
	if _, err := project.Git(repo, "push", "--quiet", pub.Remote, pub.Commit+":refs/heads/"+pub.Branch); err != nil {
		return PublishError{"push", err}
	}
	return nil
}

// pushRemote is origin when there is one, else the only remote.
func pushRemote(repo string) string {
	out, err := project.Git(repo, "remote")
	if err != nil {
		return ""
	}
	remotes := strings.Fields(out)
	for _, r := range remotes {
		if r == "origin" {
			return r
		}
	}
	if len(remotes) == 1 {
		return remotes[0]
	}
	return ""
}

// openDraft opens the draft PR, through OpenDraft when set.
func (d *Dispatcher) openDraft(repo, branch, title, body string) (string, error) {
	if d.OpenDraft == nil {
		return "", errors.New("no way to open a pull request is set up")
	}
	return d.OpenDraft(repo, branch, title, body)
}

// Discard turns a run's result down; the work stays until clean.
func (d *Dispatcher) Discard(ctx context.Context, id int) error {
	th, r, err := d.run(id)
	if err != nil {
		return err
	}
	if th.Run.Publication != nil {
		return fmt.Errorf("run %d was approved and %s: %w", id, th.Run.Publication.Stage, ErrCommitChanged)
	}
	if err := runner.Discard(ctx, r, th.Project, th.Run.Ref); err != nil {
		return err
	}
	return thread.SetRun(d.Store, id, func(run *thread.Run) {
		run.State, run.Text = runner.Failed, "discarded"
		if run.Phase != "" {
			run.Phase = thread.PhaseDiscarded
		}
	})
}
