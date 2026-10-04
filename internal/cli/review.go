package cli

import (
	"fmt"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/thread"
)

func cmdReview(e *Env, a argv) int {
	if a.has("keep") {
		if a.has("p") {
			return fail(e, exitUsage, "--keep and -p cannot be used together")
		}
		id, code := threadID(e, a.value("keep"))
		if code != 0 {
			return code
		}
		if err := thread.Keep(e.store(), id, time.Now()); err != nil {
			return threadErr(e, err)
		}
		return e.changed(changedJSON{ID: fmt.Sprint(id), Did: didReviewed, Next: []string{"sous", fmt.Sprintf("sous show %d", id)}}, fmt.Sprintf("kept %d; review again in a week", id))
	}
	cfg, code := e.config()
	if code != 0 {
		return code
	}
	var roots []string
	if a.has("p") {
		p, code := resolveProject(e, a.value("p"))
		if code != 0 {
			return code
		}
		roots = []string{p.Path}
	}
	var d *board.Data
	if len(cfg.Roots) == 0 && len(roots) == 0 {
		now := time.Now()
		views, err := thread.Open(e.store(), now)
		if err != nil {
			return threadErr(e, err)
		}
		views = reconcile(e, e.ctx(), views, now, true)
		d = &board.Data{Threads: views, RenderedAt: now.UTC()}
	} else {
		d, code = buildBoard(e, roots)
		if code != 0 {
			return code
		}
	}
	sessions, err := session.All(e.store())
	if err != nil {
		return fail(e, exitFailed, "%v", err)
	}
	r := board.Review(d, sessions)
	if e.JSON {
		return e.writeJSON(r)
	}
	board.RenderReview(e.Stdout, r)
	return 0
}
