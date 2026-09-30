package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/filing"
	"github.com/bilal-/sous/internal/thread"
)

// filer builds the filing use-case layer for this invocation.
func (e *Env) filer() *filing.Filer {
	return &filing.Filer{Store: e.Store, Cfg: e.Cfg, Backends: backend.Backends(e.Exe, backend.BuiltinNames(), e.Cfg.Plugins), Warn: e.Stderr}
}

// fileThread promotes a local thread into its project's tracker and prints
// the ref. explicit (-p / --force) allows worktrees.
func fileThread(e *Env, id int, explicit bool) int {
	if _, code := e.config(); code != 0 {
		return code
	}
	ref, err := e.filer().File(context.Background(), id, explicit)
	switch {
	case err == nil:
		fmt.Fprintln(e.Stdout, ref)
		return 0
	case errors.Is(err, thread.ErrNotFound):
		return fail(e, 1, "%v", err)
	case errors.Is(err, filing.ErrWorktree):
		return fail(e, 1, "%v; markers filed there can vanish with the branch. Use --force to file it anyway", err)
	case errors.Is(err, filing.ErrNoTracker):
		return fail(e, 1, "%v; the note stays local (see README: Backends)", err)
	}
	return fail(e, 1, "%v", err)
}

func cmdFile(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	return fileThread(e, id, a.has("force"))
}

// closeUpstream runs before a local done when --close was given. Failure
// leaves the thread open.
func closeUpstream(e *Env, id int) int {
	if _, code := e.config(); code != 0 {
		return code
	}
	if err := e.filer().CloseUpstream(context.Background(), id); err != nil {
		if errors.Is(err, thread.ErrNotFound) {
			return threadErr(e, err)
		}
		return fail(e, 1, "%v (thread %d left open)", err, id)
	}
	return 0
}

// reconcile asks trackers about filed notes, and runners about runs;
// offline keeps to backends whose items live on this machine and to the
// built in runners.
func reconcile(e *Env, ctx context.Context, views []thread.View, now time.Time, offline bool) []thread.View {
	if _, code := e.config(); code != 0 {
		return views
	}
	f := e.filer()
	f.Offline = offline
	return e.dispatcher().Refresh(ctx, f.Reconcile(ctx, views, now), offline)
}
