package cli

import (
	"errors"
	"fmt"

	"github.com/bilal-/sous/internal/runs"
)

// cmdApprove: sous approve <n> [--commit <sha>]. Publishes a run's result:
// the runner accepts it, sous pushes exactly that commit to the run's
// branch and opens a draft PR. Without --commit it approves the result the
// run holds now; a host names the commit the person reviewed.
func cmdApprove(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	if _, code := e.config(); code != 0 {
		return code
	}
	d := e.dispatcher()
	commit := a.value("commit")
	if commit == "" {
		v, err := d.Show(e.ctx(), id)
		if err != nil {
			return threadErr(e, err)
		}
		if v.Run == nil || v.Run.Result == nil {
			return fail(e, exitFailed, "run %d has no result to approve; sous show %d", id, id)
		}
		commit = v.Run.Result.Commit
	}
	pub, err := d.Approve(e.ctx(), id, commit, "")
	var pe runs.PublishError
	switch {
	case err == nil:
		said := fmt.Sprintf("run %d approved: %s pushed, draft PR %s", id, pub.Branch, pub.PR)
		return e.changed(changedJSON{ID: fmt.Sprint(id), Did: didApproved, Publication: &pub, Next: noteNext(e, id)}, said)
	case errors.As(err, &pe):
		return failWith(e, errorJSON{Error: fmt.Sprintf("%v (got as far as %s; sous approve %d carries on from there)", err, pub.Stage, id), Exit: exitFailed, ID: fmt.Sprint(id), Next: []string{fmt.Sprintf("sous approve %d", id)}})
	case errors.Is(err, runs.ErrNotARun):
		return fail(e, exitUsage, "%v", err)
	case errors.Is(err, runs.ErrCommitChanged), errors.Is(err, runs.ErrNoResult), errors.Is(err, runs.ErrPrivateState), errors.Is(err, runs.ErrBadBranch):
		return fail(e, exitFailed, "%v; sous show %d", err, id)
	}
	return threadErr(e, err)
}

// cmdDiscard: sous discard <n>. Turns a run's result down; its work stays
// until sous done <n> --clean.
func cmdDiscard(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	if _, code := e.config(); code != 0 {
		return code
	}
	switch err := e.dispatcher().Discard(e.ctx(), id); {
	case err == nil:
		return e.changed(changedJSON{ID: fmt.Sprint(id), Did: didDiscarded, Next: []string{fmt.Sprintf("sous done %d --clean", id)}}, fmt.Sprintf("run %d discarded; its work stays until sous done %d --clean", id, id))
	case errors.Is(err, runs.ErrNotARun):
		return fail(e, exitUsage, "%v", err)
	case errors.Is(err, runs.ErrCommitChanged):
		return fail(e, exitFailed, "%v", err)
	default:
		return threadErr(e, err)
	}
}
