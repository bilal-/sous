package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/runs"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/thread"
)

// dispatcher builds the runs use case layer for this invocation.
func (e *Env) dispatcher() *runs.Dispatcher {
	return &runs.Dispatcher{Store: e.store(), Runners: runner.Registry.All(e.Exe, e.Cfg.Plugins), Now: time.Now}
}

// goRun: sous go <project> --run <brief|-> [-a <runner>] [--key <text>].
// Hands the brief to a runner in the background and returns at once.
func goRun(e *Env, a argv, p project.Project, cfg *config.Config) int {
	if a.has("where") || a.has("in") {
		return fail(e, exitUsage, "--run starts work in the background; it does not go with --where or --in")
	}
	brief := a.value("run")
	if brief == "-" {
		b, err := io.ReadAll(e.Stdin)
		if err != nil {
			return fail(e, exitFailed, "reading the brief: %v", err)
		}
		brief = string(b)
	}
	if strings.TrimSpace(brief) == "" {
		return fail(e, exitUsage, "the brief is empty; say what to do, why, and what done means")
	}
	name := a.value("a")
	if name == "" {
		name = cfg.RunAgent()
	}
	d := e.dispatcher()
	hereFile := ""
	var here bytes.Buffer
	sub := e.child(&here, io.Discard, true)
	cmdHere(sub, argv{pos: []string{p.Path}})
	sub.close()
	if f, err := session.WriteContext(e.Home, p.Path, here.Bytes()); err == nil {
		hereFile = f
	}
	id, existed, err := d.Start(e.ctx(), p, brief, a.value("key"), name, e.Source, hereFile)
	switch {
	case errors.Is(err, runs.ErrNoRunner):
		return fail(e, exitUsage, "%v; the runners are: %s", err, strings.Join(d.Names(), ", "))
	case errors.As(err, new(thread.ValidationError)):
		return fail(e, exitUsage, "%v", err)
	case errors.Is(err, runner.ErrNotSetUp):
		return fail(e, exitNotReady, "%v (run %d is on the board as failed; sous doctor says what to install)", err, id)
	case err != nil && id > 0:
		return fail(e, exitFailed, "%v (run %d is on the board as failed; sous show %d)", err, id, id)
	case err != nil:
		return fail(e, exitFailed, "%v", err)
	}
	th, err := thread.Get(e.store(), id)
	if err != nil || th.Run == nil {
		return fail(e, exitFailed, "run %d started, but reading it back failed: %v (sous show %d)", id, err, id)
	}
	v := thread.View{Thread: th}
	item := board.ThreadRow(v, time.Now()).Item()
	c := changedJSON{ID: fmt.Sprint(id), Did: "started", Run: item.Run, Next: noteNext(e, id)}
	said := fmt.Sprintf("started run %d in %s (%s)", id, p.Name, th.Run.Runner)
	if existed {
		c.Did = "already_started"
		said = fmt.Sprintf("run %d in %s was already started (%s, %s)", id, p.Name, th.Run.Runner, strings.ReplaceAll(string(th.Run.State), "_", " "))
	}
	return e.changed(c, said)
}

// cmdShow: one note in full; for a run, how it is going right now.
func cmdShow(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	if _, code := e.config(); code != 0 {
		return code
	}
	v, err := e.dispatcher().Show(e.ctx(), id)
	if err != nil {
		return threadErr(e, err)
	}
	if e.JSON {
		return e.writeJSON(board.Note(v, time.Now(), board.Next(v)))
	}
	board.RenderNote(e.Stdout, v, time.Now(), e.UserHome)
	return 0
}

// cmdReply: sous reply <n> "<answer>": the answer goes to the run, which
// carries on.
func cmdReply(e *Env, a argv) int {
	id, code := threadID(e, a.pos[0])
	if code != 0 {
		return code
	}
	if _, code := e.config(); code != 0 {
		return code
	}
	err := e.dispatcher().Reply(e.ctx(), id, a.pos[1])
	switch {
	case err == nil:
		return e.changed(changedJSON{ID: fmt.Sprint(id), Did: "replied", Next: noteNext(e, id)}, fmt.Sprintf("run %d carries on", id))
	case errors.Is(err, runs.ErrNotARun):
		return fail(e, exitUsage, "%v; sous edit %d \"<text>\" changes a note", err, id)
	case errors.Is(err, runner.ErrUnsupported):
		return fail(e, exitFailed, "%v; start a new run with the answer in the brief: sous go <project> --run -", err)
	}
	return threadErr(e, err)
}

// stopRun stops a note's run before done closes it; with clean, its
// worktree goes too (runs.Dispatcher.Finish decides what that means).
func stopRun(e *Env, id int, clean bool) int {
	th, err := thread.Get(e.store(), id)
	switch {
	case err != nil:
		return threadErr(e, err)
	case th.Run == nil && !clean:
		// A plain note closes even when config.toml is broken; only a run
		// needs the runners config lists.
		return 0
	}
	if _, code := e.config(); code != 0 {
		return code
	}
	done, err := e.dispatcher().Finish(e.ctx(), id, clean)
	switch {
	case errors.Is(err, runs.ErrNotARun):
		return fail(e, exitUsage, "%v", err)
	case err != nil:
		return fail(e, exitFailed, "%v (it stays open)", err)
	case done == runs.CantClean:
		fmt.Fprintf(e.Stderr, "sous: %s cannot clean up after its runs; its files stay\n", th.Run.Runner)
	}
	return 0
}
