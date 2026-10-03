package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

func buildBoard(e *Env, roots []string) (*board.Data, int) {
	cfg, code := e.config()
	if code != 0 {
		return nil, code
	}
	now := time.Now()
	d, err := board.Build(e.ctx(), board.Inputs{
		Store: e.store(), Cfg: cfg, Roots: roots, Signals: signal.Registry.All(e.Exe, cfg.Plugins),
		Timeout: e.pluginTimeout(), Warn: e.Stderr, Now: now,
		Reconcile: func(ctx context.Context, v []thread.View) []thread.View { return reconcile(e, ctx, v, now, false) },
	})
	if err != nil {
		return nil, fail(e, exitFailed, "%v", err)
	}
	// Only the full configured board is cached; a scoped board must never
	// become what the zsh surface prints as "the board".
	if len(roots) == 0 {
		if err := board.WriteCache(e.store(), d); err != nil {
			return nil, fail(e, exitFailed, "cache: %v", err)
		}
	}
	return d, 0
}

// unconfigured: config.toml is fine but names no project folders.
func (e *Env) unconfigured() bool { return e.cfgErr == nil && len(e.Cfg.Roots) == 0 }

func cmdBoard(e *Env, roots []string) int {
	if len(roots) == 0 && e.unconfigured() {
		if e.JSON {
			return e.writeJSON(board.BoardJSON{})
		}
		fmt.Fprintln(e.Stdout, "sous · "+config.NoRootsHint)
		return 0
	}
	d, code := buildBoard(e, roots)
	if code != 0 {
		return code
	}
	if e.JSON {
		return e.writeJSON(d.JSON())
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
		return fail(e, exitUsage, "%s is neither a repo nor a folder of repos", arg)
	}
	cfg, code := e.config()
	if code != 0 {
		return code
	}
	if len(cfg.Roots) == 0 {
		return fail(e, exitFailed, "%s", config.NoRootsHint)
	}
	p, err := project.Resolve(cfg.Roots, cfg.Ignore, arg, e.Cwd, e.UserHome, e.Stderr)
	if errors.Is(err, project.ErrNoMatch) && !strings.ContainsAny(arg, "/.") {
		// A word that is neither a command nor a project is most likely a
		// command misremembered.
		if near := nearestVerb(arg); near != "" {
			return unknownCommand(e, arg)
		}
		return fail(e, exitUsage, "%q is not a command or a project; sous help lists the commands, sous projects the projects", arg)
	}
	if err != nil {
		return projectErr(e, err)
	}
	return cmdHere(e, argv{pos: []string{p.Path}})
}

// cmdAmbient is what a new shell runs: the cached board, at most once per
// refresh window (refresh_hours in config.toml, the only setting for it).
func cmdAmbient(e *Env) int {
	code := 0
	board.Ambient{Home: e.Home}.Run(time.Now(), e.Cfg.RefreshWindow(), func() bool {
		switch {
		case e.cfgErr != nil:
			fmt.Fprintf(e.Stdout, "sous: %s could not be read: %v\n", config.Tilde(e.UserHome, config.Path(e.Home)), e.cfgErr)
		case e.unconfigured():
			fmt.Fprintln(e.Stdout, "sous · "+config.NoRootsHint)
		default:
			code = cmdCached(e)
		}
		return code != exitNotReady
	})
	return code
}

// cmdCached prints the last rendered board with its age (the zsh surface), and
// kicks off a detached refresh if it is older than the configured window.
func cmdCached(e *Env) int {
	c, err := board.ReadCache(e.store())
	if err != nil {
		return fail(e, exitFailed, "%v", err)
	}
	if c.Board == nil || c.RenderedAt == nil {
		// The forgetting user must not be told to remember: build it now,
		// detached, and exit 3 so the shell surface doesn't stamp this print.
		fmt.Fprintln(e.Stdout, "sous: building your board now. It will show in your next shell.")
		spawnRefresh(e)
		return exitNotReady
	}
	now := time.Now()
	if c.Data != nil {
		board.RenderSaved(e.Stdout, c.Data, now)
	} else {
		fmt.Fprint(e.Stdout, *c.Board) // saved by a sous before cached data
	}
	fmt.Fprintf(e.Stdout, "  (cached · %s)\n", text.Ago(now, *c.RenderedAt))
	if now.Sub(*c.RenderedAt) > e.Cfg.RefreshWindow() {
		spawnRefresh(e)
	}
	return 0
}

func spawnRefresh(e *Env) {
	cmd := exec.Command(e.Exe, "--refresh")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Start() // detached; errors are irrelevant to the caller
}
