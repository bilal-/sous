package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/thread"
)

func buildBoard(e *Env, roots []string) (*board.Data, int) {
	cfg, code := e.config()
	if code != 0 {
		return nil, code
	}
	now := time.Now()
	d, err := board.Build(e.ctx(), board.Inputs{
		Store: e.store(), Cfg: cfg, Roots: roots, Exe: e.Exe, Builtins: signal.BuiltinNames(),
		Timeout: e.pluginTimeout(), Warn: e.Stderr, Now: now,
		Reconcile: func(ctx context.Context, v []thread.View) []thread.View { return reconcile(e, ctx, v, now, false) },
	})
	if err != nil {
		return nil, fail(e, 1, "%v", err)
	}
	// Only the full configured board is cached; a scoped board must never
	// become what the zsh surface prints as "the board".
	if len(roots) == 0 {
		if err := board.WriteCache(e.store(), d); err != nil {
			return nil, fail(e, 1, "cache: %v", err)
		}
	}
	return d, 0
}

// noProjectsHint is what to do when sous knows no project folders yet.
const noProjectsHint = "none yet. Run sous setup to find them, or sous setup ~/path/to/your/projects"

// unconfigured: config.toml is fine but names no project folders.
func (e *Env) unconfigured() bool { return e.cfgErr == nil && len(e.Cfg.Roots) == 0 }

func cmdBoard(e *Env, roots []string) int {
	if len(roots) == 0 && e.unconfigured() {
		if e.JSON {
			return e.writeJSON(map[string]any{"projects": []any{}, "configured": false})
		}
		fmt.Fprintln(e.Stdout, "sous · projects: "+noProjectsHint)
		return 0
	}
	d, code := buildBoard(e, roots)
	if code != 0 {
		return code
	}
	if e.JSON {
		return e.writeJSON(d)
	}
	board.Render(e.Stdout, d)
	return 0
}

// cmdPath handles `sous <something>` that isn't a subcommand: a folder of
// repos → scoped board; a repo path or project name → here.
func cmdPath(e *Env, arg string) int {
	// Relative to the invocation's cwd, not the process's.
	candidate := arg
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(e.Cwd, candidate)
	}
	if st, err := os.Stat(candidate); err == nil && st.IsDir() {
		arg = candidate
		if _, ok := project.ForPath(arg, e.UserHome); ok {
			return cmdHere(e, argv{pos: []string{arg}})
		}
		if ps, _ := project.Discover([]string{arg}, nil, io.Discard); len(ps) > 0 {
			return cmdBoard(e, []string{arg})
		}
		return fail(e, 2, "%s is neither a repo nor a folder of repos", arg)
	}
	p, code := resolveProject(e, arg)
	if code != 0 {
		return code
	}
	return cmdHere(e, argv{pos: []string{p.Path}})
}

// cmdAmbient is what a new shell runs: the cached board, at most once per
// refresh window (refresh_hours in config.toml, the only setting for it).
func cmdAmbient(e *Env) int {
	amb := board.Ambient{Home: e.Home}
	if !amb.Due(time.Now(), e.Cfg.RefreshWindow()) {
		return 0
	}
	code := 0
	switch {
	case e.cfgErr != nil:
		fmt.Fprintf(e.Stdout, "sous: %s/config.toml could not be read: %v\n", config.Tilde(e.UserHome, e.Home), e.cfgErr)
	case e.unconfigured():
		// Nothing to build yet: say once what to do, then stay quiet.
		fmt.Fprintln(e.Stdout, "sous · projects: "+noProjectsHint)
	default:
		code = cmdCached(e)
	}
	if code == 0 { // no board yet: leave it due, so the next shell shows it
		amb.Mark()
	}
	return code
}

// cmdCached prints the last rendered board with its age (the zsh surface), and
// kicks off a detached refresh if it is older than the configured window.
func cmdCached(e *Env) int {
	c, err := board.ReadCache(e.store())
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	if c.Board == nil || c.RenderedAt == nil {
		// The forgetting user must not be told to remember: build it now,
		// detached, and exit 3 so the shell surface doesn't stamp this print.
		fmt.Fprintln(e.Stdout, "sous: building your board now. It will show in your next shell.")
		spawnRefresh(e)
		return exitNoBoardYet
	}
	now := time.Now()
	if c.Data != nil {
		board.RenderSaved(e.Stdout, c.Data, now)
	} else {
		fmt.Fprint(e.Stdout, *c.Board) // saved by a sous before cached data
	}
	fmt.Fprintf(e.Stdout, "  (cached · %s)\n", project.Ago(now, *c.RenderedAt))
	if now.Sub(*c.RenderedAt) > e.Cfg.RefreshWindow() {
		spawnRefresh(e)
	}
	return 0
}

// exitNoBoardYet: --cached or --ambient was asked for the board before
// there was one; a first one is being built.
const exitNoBoardYet = 3

func spawnRefresh(e *Env) {
	cmd := exec.Command(e.Exe, "--refresh")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Start() // detached; errors are irrelevant to the caller
}
