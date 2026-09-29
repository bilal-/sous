package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
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
	id, err := thread.Note(e.store(), p, kind, a.pos[0], e.Source, time.Now())
	if err != nil {
		return threadErr(e, err)
	}
	fmt.Fprintln(e.Stdout, id)
	if a.has("file") {
		if c := fileThread(e, id, proj != ""); c != 0 {
			fmt.Fprintf(e.Stderr, "sous: note saved as %d but not filed\n", id)
			return c
		}
	}
	return 0
}

func threadID(e *Env, s string) (int, int) {
	id, err := strconv.Atoi(s)
	if err != nil || id <= 0 {
		return 0, fail(e, 2, "thread id must be a number, got %q", s)
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
		return fail(e, 1, "%v", err)
	}
	var vErr thread.ValidationError
	if errors.As(err, &vErr) {
		return fail(e, 2, "%v", err)
	}
	return fail(e, 1, "%v", err)
}

// The four thread-maintenance verbs, one function each: the verb table
// routes to them directly.

func cmdEdit(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	return threadErr(e, thread.Edit(e.store(), id, a.pos[1]))
}

func cmdKind(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	return threadErr(e, thread.SetKind(e.store(), id, thread.Kind(a.pos[1])))
}

// cmdSnooze hides a thread for N days, or a signal (s:…) until it changes.
func cmdSnooze(e *Env, a argv) int {
	args := a.pos
	if strings.HasPrefix(args[0], "s:") {
		if len(args) != 1 {
			return fail(e, 2, "a signal is snoozed until it changes; days do not apply")
		}
		if err := signal.Snooze(e.store(), args[0]); err != nil {
			return fail(e, 1, "%v", err)
		}
		return 0
	}
	id, code := threadID(e, args[0])
	if code != 0 {
		return code
	}
	days := 7
	if len(args) == 2 {
		d, err := strconv.Atoi(args[1])
		if err != nil || d <= 0 {
			return fail(e, 2, "days must be a positive number")
		}
		days = d
	}
	return threadErr(e, thread.Snooze(e.store(), id, days, time.Now()))
}

// cmdDone closes a thread locally; --close closes it in its tracker first,
// and a failure there leaves the thread open.
func cmdDone(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	if a.has("close") {
		if c := closeUpstream(e, id); c != 0 {
			return c
		}
	}
	return threadErr(e, thread.Done(e.store(), id, time.Now()))
}
