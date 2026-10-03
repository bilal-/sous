package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/runs"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/thread"
)

// dispatcher builds the runs use case layer for this invocation.
func (e *Env) dispatcher() *runs.Dispatcher {
	return &runs.Dispatcher{Store: e.store(), Runners: runner.Runners(e.Exe, runner.BuiltinNames(), e.Cfg.Plugins), Now: time.Now}
}

// goRun: sous go <project> --run <brief|-> [-a <runner>] [--key <text>].
// Hands the brief to a runner in the background and returns at once.
func goRun(e *Env, a argv, p project.Project, cfg *config.Config) int {
	if a.has("where") || a.has("in") {
		return fail(e, 2, "--run starts work in the background; it does not go with --where or --in")
	}
	brief := a.value("run")
	if brief == "-" {
		b, err := io.ReadAll(e.Stdin)
		if err != nil {
			return fail(e, 1, "reading the brief: %v", err)
		}
		brief = string(b)
	}
	if strings.TrimSpace(brief) == "" {
		return fail(e, 2, "the brief is empty; say what to do, why, and what done means")
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
		return fail(e, 2, "%v; the runners are: %s", err, strings.Join(d.Names(), ", "))
	case errors.As(err, new(thread.ValidationError)):
		return fail(e, 2, "%v", err)
	case errors.Is(err, runner.ErrNotSetUp):
		return fail(e, 3, "%v (run %d is on the board as failed; sous doctor says what to install)", err, id)
	case err != nil && id > 0:
		return fail(e, 1, "%v (run %d is on the board as failed; sous show %d)", err, id, id)
	case err != nil:
		return fail(e, 1, "%v", err)
	}
	th, err := thread.Get(e.store(), id)
	if err != nil || th.Run == nil {
		return fail(e, 1, "run %d started, but reading it back failed: %v (sous show %d)", id, err, id)
	}
	if e.JSON {
		return e.writeJSON(map[string]any{"id": id, "runner": th.Run.Runner, "state": th.Run.State, "existed": existed, "next": []string{fmt.Sprintf("sous show %d --json", id)}})
	}
	verb := "started"
	if existed {
		verb = "already started as"
	}
	fmt.Fprintf(e.Stdout, "%s run %d in %s (%s) · sous show %d to check\n", verb, id, p.Name, th.Run.Runner, id)
	return 0
}

// showView is sous show --json.
type showView struct {
	thread.View
	Next []string `json:"next"`
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
	next := nextFor(v)
	if e.JSON {
		return e.writeJSON(showView{View: v, Next: next})
	}
	now := time.Now()
	state := string(v.Kind) + " · " + project.Ago(now, v.Since)
	if v.Closed != nil {
		state = "closed " + project.Ago(now, *v.Closed)
	}
	fmt.Fprintf(e.Stdout, "%d  %s  %s  (%s)\n", v.ID, projectName(v.Project), v.Text, state)
	if v.Ref != nil {
		fmt.Fprintf(e.Stdout, "   filed: %s\n", *v.Ref)
	}
	if r := v.Run; r != nil {
		line := "   run: " + strings.ReplaceAll(r.State, "_", " ")
		if r.Text != "" {
			line += " · " + r.Text
		}
		fmt.Fprintf(e.Stdout, "%s (%s)\n", line, r.Runner)
		if v.RunErr != "" {
			fmt.Fprintf(e.Stdout, "   status unavailable: %s\n", v.RunErr)
		}
		var where []string
		for _, kv := range [][2]string{{"branch", r.Branch}, {"worktree", r.Worktree}, {"log", r.Log}} {
			if kv[1] != "" {
				where = append(where, kv[0]+" "+config.Tilde(e.UserHome, kv[1]))
			}
		}
		if len(where) > 0 {
			fmt.Fprintf(e.Stdout, "   %s\n", strings.Join(where, " · "))
		}
	}
	if len(next) > 0 {
		fmt.Fprintf(e.Stdout, "   next: %s\n", strings.Join(next, " · "))
	}
	return 0
}

func projectName(path string) string { return project.Describe(path).Name }

// nextFor: the commands that make sense for a note in its state.
func nextFor(v thread.View) []string {
	if v.Closed != nil {
		return []string{}
	}
	id := v.ID
	if v.Run == nil {
		return []string{fmt.Sprintf("sous done %d", id)}
	}
	switch v.Run.State {
	case string(runner.NeedsYou):
		return []string{fmt.Sprintf(`sous reply %d "<answer>"`, id), fmt.Sprintf("sous done %d", id)}
	case string(runner.Done), string(runner.Failed):
		return []string{fmt.Sprintf("sous done %d --clean", id)}
	}
	return []string{fmt.Sprintf("sous show %d", id), fmt.Sprintf("sous done %d", id)}
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
		fmt.Fprintf(e.Stdout, "run %d carries on · sous show %d to check\n", id, id)
		return 0
	case errors.Is(err, runs.ErrNotARun):
		return fail(e, 2, "%v; sous edit %d \"<text>\" changes a note", err, id)
	case errors.Is(err, runner.ErrUnsupported):
		return fail(e, 1, "%v; start a new run with the answer in the brief: sous go <project> --run -", err)
	}
	return threadErr(e, err)
}

// stopRun stops a note's run before done closes it; with clean, its
// worktree goes too. A note without a run is left alone (and clean is
// then a usage mistake).
func stopRun(e *Env, id int, clean bool) int {
	th, err := thread.Get(e.store(), id)
	if err != nil {
		return threadErr(e, err)
	}
	if th.Run == nil {
		if clean {
			return fail(e, 2, "note %d is not a run; --clean is for runs", id)
		}
		return 0
	}
	if th.Run.Ref == "" {
		return 0 // never started: nothing to stop
	}
	if _, code := e.config(); code != 0 {
		return code
	}
	d := e.dispatcher()
	if err := d.Stop(e.ctx(), id); err != nil {
		return fail(e, 1, "stopping run %d: %v (it stays open)", id, err)
	}
	if clean {
		switch err := d.Clean(e.ctx(), id); {
		case errors.Is(err, runner.ErrUnsupported):
			fmt.Fprintf(e.Stderr, "sous: %s cannot clean up after its runs; its files stay\n", th.Run.Runner)
		case err != nil:
			return fail(e, 1, "cleaning up run %d: %v (it stays open)", id, err)
		}
	}
	return 0
}
