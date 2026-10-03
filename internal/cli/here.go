package cli

import (
	"context"
	"path/filepath"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/thread"
)

func cmdHere(e *Env, a argv) int {
	start := e.Cwd
	if len(a.pos) == 1 {
		start = a.pos[0]
	}
	root, ok := project.ForPath(start, e.UserHome)
	switch {
	case !ok && len(a.pos) == 1:
		return fail(e, exitUsage, "%s is not in a project (a git repo); sous projects lists them", start)
	case !ok:
		return fail(e, exitUsage, "not inside a project; use sous here <path> or sous <project>")
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	now := time.Now()
	ctx := e.ctx()
	d, err := board.BuildHere(ctx, board.Inputs{
		Store: e.store(), Cfg: e.Cfg, Signals: signal.Registry.Offline(e.Exe),
		Timeout: e.pluginTimeout(), Warn: e.Stderr, Now: now,
		Reconcile: func(ctx context.Context, v []thread.View) []thread.View { return reconcile(e, ctx, v, now, true) },
	}, root)
	if err != nil {
		return fail(e, exitFailed, "%v", err)
	}
	if e.JSON {
		return e.writeJSON(d.JSON())
	}
	board.RenderHere(e.Stdout, d, now, e.Brief)
	return 0
}
