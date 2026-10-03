package cli

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

// cmdNote: -p names the project (fuzzy; default the current one), -k the
// kind (default idea), --file also files it in the project's tracker.
func cmdNote(e *Env, a argv) int {
	proj, kind := a.value("p"), thread.Idea
	if a.has("k") {
		kind = thread.Kind(a.value("k")) // an explicit "" fails validation
	}
	p, code := resolveProject(e, proj)
	if code != 0 {
		return code
	}
	id, existed, err := thread.Note(e.store(), p, kind, a.pos[0], e.Source, time.Now())
	if err != nil {
		return threadErr(e, err)
	}
	did, said := didNoted, fmt.Sprintf("noted %d in %s", id, p.Name)
	if existed {
		did, said = didAlreadyNoted, fmt.Sprintf("already noted as %d in %s", id, p.Name)
	}
	c := changedJSON{ID: fmt.Sprint(id), Did: did, Next: noteNext(e, id)}
	if !a.has("file") {
		return e.changed(c, said)
	}
	ref, code, msg := fileNote(e, id, proj != "")
	if code != 0 {
		// Saved all the same: one answer says so, and why it is not filed.
		c.Error = "not filed: " + msg
		if e.JSON {
			e.writeJSON(c)
			return code
		}
		e.changed(c, said)
		fmt.Fprintf(e.Stderr, "sous: %s\nsous: note saved as %d but not filed\n", msg, id)
		return code
	}
	c.Did, c.Ref, c.Next = didFiled, &ref, noteNext(e, id)
	return e.changed(c, fmt.Sprintf("filed %d as %s", id, ref))
}

func threadID(e *Env, s string) (int, int) {
	id, err := strconv.Atoi(s)
	if err != nil || id <= 0 {
		return 0, fail(e, exitUsage, "thread id must be a number, got %q", s)
	}
	return id, 0
}

// threadErr: validation problems are usage (2); anything the store or the
// data says is a failure (1). An agent reading 2 assumes its arguments were
// wrong; reading 1 it knows sous itself could not proceed.
func threadErr(e *Env, err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, thread.ErrNotFound) || errors.Is(err, store.ErrNewer) {
		return fail(e, exitFailed, "%v", err)
	}
	var vErr thread.ValidationError
	if errors.As(err, &vErr) {
		return fail(e, exitUsage, "%v", err)
	}
	return fail(e, exitFailed, "%v", err)
}

// The four thread-maintenance verbs, one function each: the verb table
// routes to them directly.

func cmdEdit(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	if err := thread.Edit(e.store(), id, a.pos[1]); err != nil {
		return threadErr(e, err)
	}
	return e.changed(changedJSON{ID: fmt.Sprint(id), Did: didEdited, Next: noteNext(e, id)}, fmt.Sprintf("edited %d", id))
}

func cmdKind(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	if err := thread.SetKind(e.store(), id, thread.Kind(a.pos[1])); err != nil {
		return threadErr(e, err)
	}
	return e.changed(changedJSON{ID: fmt.Sprint(id), Did: didKind, Next: noteNext(e, id)}, fmt.Sprintf("%d is %s now", id, a.pos[1]))
}

// cmdSnooze hides a thread for N days, or a signal (s:…) until it changes.
func cmdSnooze(e *Env, a argv) int {
	args := a.pos
	if signal.IsID(args[0]) {
		if len(args) != 1 {
			return fail(e, exitUsage, "a signal is snoozed until it changes; days do not apply")
		}
		if err := signal.Snooze(e.store(), args[0]); err != nil {
			return fail(e, exitFailed, "%v", err)
		}
		return e.changed(changedJSON{ID: args[0], Did: didSnoozed, Next: []string{"sous"}}, "snoozed "+args[0]+" until it changes")
	}
	id, code := threadID(e, args[0])
	if code != 0 {
		return code
	}
	days := 7
	if len(args) == 2 {
		d, err := strconv.Atoi(args[1])
		if err != nil || d <= 0 {
			return fail(e, exitUsage, "days must be a positive number")
		}
		days = d
	}
	now := time.Now()
	if err := thread.Snooze(e.store(), id, days, now); err != nil {
		return threadErr(e, err)
	}
	until := now.Add(time.Duration(days) * 24 * time.Hour)
	return e.changed(changedJSON{ID: fmt.Sprint(id), Did: didSnoozed, Next: noteNext(e, id)}, fmt.Sprintf("snoozed %d until %s", id, text.When(until)))
}

// cmdDone closes a thread locally; --close closes it in its tracker first,
// and a failure there leaves the thread open. A run is stopped first, and
// with --clean its worktree removed.
func cmdDone(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	th, err := thread.Get(e.store(), id)
	if err != nil {
		return threadErr(e, err)
	}
	// Each step is safe to repeat, so done again (or with --close or
	// --clean after a plain done) does what is left, and says so.
	if c := stopRun(e, id, a.has("clean")); c != 0 {
		return c
	}
	if a.has("close") {
		if c := closeUpstream(e, id); c != 0 {
			return c
		}
	}
	if th.Closed != nil {
		return e.changed(changedJSON{ID: fmt.Sprint(id), Did: didAlreadyClosed, Ref: th.Ref, Next: []string{"sous"}},
			fmt.Sprintf("%d was closed %s", id, text.Ago(time.Now(), *th.Closed)))
	}
	if err := thread.Done(e.store(), id, time.Now()); err != nil {
		return threadErr(e, err)
	}
	return e.changed(changedJSON{ID: fmt.Sprint(id), Did: didClosed, Ref: th.Ref, Next: []string{"sous"}}, fmt.Sprintf("closed %d", id))
}
