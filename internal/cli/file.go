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
	return &filing.Filer{Store: e.Store, Cfg: e.Cfg, Backends: backend.Registry.All(e.Exe, e.Cfg.Plugins), Warn: e.Stderr}
}

// fileThread promotes a local note into its project's tracker and answers
// with its ref. explicit (-p / --force) allows worktrees.
func fileThread(e *Env, id int, explicit bool) int {
	ref, code, msg := fileNote(e, id, explicit)
	if code != 0 {
		return fail(e, code, "%s", msg)
	}
	return e.changed(changedJSON{ID: fmt.Sprint(id), Did: "filed", Ref: &ref, Next: noteNext(e, id)}, fmt.Sprintf("filed %d as %s", id, ref))
}

// fileNote files note id and returns its ref, or the exit code and what to
// tell the person.
func fileNote(e *Env, id int, explicit bool) (ref string, code int, msg string) {
	if _, code := e.config(); code != 0 {
		return "", code, "config could not be read"
	}
	ref, err := e.filer().File(e.ctx(), id, explicit)
	switch {
	case err == nil:
		return ref, 0, ""
	case errors.Is(err, filing.ErrWorktree):
		return "", exitFailed, err.Error() + "; markers filed there can vanish with the branch. Use --force to file it anyway"
	case errors.Is(err, filing.ErrNoTracker):
		return "", exitFailed, err.Error() + "; the note stays local. To file it, the project needs a FOLLOWUPS.md, or a GitHub or GitLab remote with gh or glab logged in (sous doctor); ask the person first"
	}
	return "", exitFailed, err.Error()
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
	if err := e.filer().CloseUpstream(e.ctx(), id); err != nil {
		if errors.Is(err, thread.ErrNotFound) {
			return threadErr(e, err)
		}
		return fail(e, exitFailed, "%v (thread %d left open)", err, id)
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
