// Package cli routes `sous` subcommands. Bare `sous` is always the board.
package cli

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/tracker"
)

// Version is set at build time: -ldflags "-X github.com/bilal-/sous/internal/cli.Version=v0.1.0".
var Version = "0.1.0-dev"

const usageHeader = `sous %s: one list of what is waiting on you, across every project.

  sous              the board: what's waiting on you across all projects
  sous <folder>     board scoped to a folder of repos
  sous <project>    where was I, for a named project or repo path
`

const usageFooter = `
Flags: --json (read verbs) · --brief (here) · --ambient · --cached · --refresh · --menubar
Exit:  0 ok · 1 failure · 2 usage or ambiguity · 3 no board yet (--cached, --ambient)
Put -- before a note that starts with a dash.

For agents:
  Notes are the user's. Keep them private unless the user asks to share one.
  sous file, note --file and done --close write where other people can see.
    Before running one, ask the user, end your turn, and run it only after
    they say yes in their next message.
  When a project name matches more than one project, sous lists them. Show
    the list and ask; never pick one yourself.
  Set SOUS_SOURCE=agent when you write a note on your own initiative.
  Never create FOLLOWUPS.md, edit ~/.sous/config.toml or the <!-- sous:... -->
    markers to make filing work. If sous says there is no tracker, say so.
  sous done means the user's task is finished, not that you wrote code.
  (ref missing) and (status unavailable) mean sous could not confirm, not done.
  When work is put off, the user waits on someone, or an idea belongs to
    another project, offer to note it in one line. Write it only on a yes.
`

// Env carries everything a subcommand needs; nothing reads globals. Config
// is loaded once per invocation and the store is one value, so every verb
// sees the same world.
type Env struct {
	Home     string // SOUS_HOME
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	JSON     bool
	Brief    bool
	Exe      string // path to this binary, for re-exec of built-in plugins
	Cfg      *config.Config
	Store    *store.Store
	Cwd      string // the working directory, read once; tests set it instead of chdir
	UserHome string // the person's home folder, read once
	Shell    string // $SHELL
	Zdotdir  string // $ZDOTDIR, where zsh keeps its startup files when set
	Source   string // SOUS_SOURCE: who is writing notes ("agent"), read once
	// Timeout for one signal plugin. The session hook shortens it so a slow
	// plugin cannot eat the hook's 5 s guard.
	PluginTimeout time.Duration
	// Deadline, when set, bounds the whole verb (plugins and tracker status
	// probes together); the session hook sets it under its guard.
	Deadline time.Time
	cfgErr   error
	vctx     context.Context
	cancel   context.CancelFunc
}

func (e *Env) pluginTimeout() time.Duration {
	if e.PluginTimeout > 0 {
		return e.PluginTimeout
	}
	return 15 * time.Second
}

// ctx: the verb's context, bounded by Deadline when one is set. Made once
// per Env and cancelled by close when the verb returns.
func (e *Env) ctx() context.Context {
	if e.vctx == nil {
		e.vctx, e.cancel = context.Background(), func() {}
		if !e.Deadline.IsZero() {
			e.vctx, e.cancel = context.WithDeadline(context.Background(), e.Deadline)
		}
	}
	return e.vctx
}

func (e *Env) close() {
	if e.cancel != nil {
		e.cancel()
	}
}

// writeJSON prints v as indented JSON on stdout. Every read verb's --json.
func (e *Env) writeJSON(v any) int {
	enc := json.NewEncoder(e.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fail(e, 1, "%v", err)
	}
	return 0
}

// config returns the loaded config or the exit code for a broken one.
func (e *Env) config() (*config.Config, int) {
	if e.cfgErr != nil {
		return nil, fail(e, 1, "config: %v", e.cfgErr)
	}
	return e.Cfg, 0
}

func (e *Env) store() *store.Store { return e.Store }

// child: the same world, different I/O — for verbs that run another verb
// internally (hooks and go run `here`).
func (e *Env) child(stdout, stderr io.Writer, brief bool) *Env {
	return &Env{Home: e.Home, Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr, Brief: brief, Exe: e.Exe, Cfg: e.Cfg, Store: e.Store, Cwd: e.Cwd, UserHome: e.UserHome, Shell: e.Shell, Zdotdir: e.Zdotdir, Source: e.Source, PluginTimeout: e.PluginTimeout, Deadline: e.Deadline, cfgErr: e.cfgErr}
}

func sousHome() string {
	if h := os.Getenv("SOUS_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".sous")
}

func usage(w io.Writer) {
	fmt.Fprintf(w, usageHeader, Version)
	for _, v := range verbs {
		if v.usage != "" {
			fmt.Fprintf(w, "  %s\n", v.usage)
		}
	}
	fmt.Fprint(w, usageFooter)
}

func cmdVersion(e *Env, _ argv) int { fmt.Fprintf(e.Stdout, "sous %s\n", Version); return 0 }
func cmdHelp(e *Env, _ argv) int    { usage(e.Stdout); return 0 }

// stableExe is the path hooks and scripts should name for this sous: the
// sous found on PATH when it is this same program (Homebrew's
// /opt/homebrew/bin/sous survives upgrades; the versioned folder it links
// to does not), else exe as given.
func stableExe(exe string) string {
	onPath, err := exec.LookPath("sous")
	if err != nil {
		return exe
	}
	a, err1 := filepath.EvalSymlinks(onPath)
	b, err2 := filepath.EvalSymlinks(exe)
	if err1 == nil && err2 == nil && a == b {
		if abs, err := filepath.Abs(onPath); err == nil {
			return abs
		}
	}
	return exe
}

func fail(e *Env, code int, format string, a ...any) int {
	fmt.Fprintf(e.Stderr, "sous: "+format+"\n", a...)
	return code
}

// Run is the whole CLI. Returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cwd, _ := os.Getwd()
	return run(args, cwd, stdin, stdout, stderr)
}

// run is Run with the working directory explicit, so tests never chdir the
// process and can run in parallel.
func run(args []string, cwd string, stdin io.Reader, stdout, stderr io.Writer) int {
	e := newEnv(cwd, stdin, stdout, stderr)
	defer e.close()
	args = e.takeReadFlags(args)
	if len(args) > 0 && strings.HasPrefix(args[0], "-") {
		return runMode(e, args[0])
	}
	if len(args) == 0 {
		return cmdBoard(e, nil)
	}
	return dispatch(e, args[0], args[1:])
}

// newEnv reads the environment, once, for the whole invocation. Nothing
// below cli reads it.
func newEnv(cwd string, stdin io.Reader, stdout, stderr io.Writer) *Env {
	exe, _ := os.Executable()
	home := sousHome()
	e := &Env{Home: home, Stdin: stdin, Stdout: stdout, Stderr: stderr, Exe: stableExe(exe), Store: &store.Store{Home: home}, Cwd: cwd}
	e.Source = os.Getenv("SOUS_SOURCE")
	e.UserHome, _ = os.UserHomeDir()
	e.Shell, e.Zdotdir = os.Getenv("SHELL"), os.Getenv("ZDOTDIR")
	cache, _ := os.UserCacheDir()
	tracker.Init(e.UserHome, cache)
	e.Cfg, e.cfgErr = config.Load(home, e.UserHome)
	if e.Cfg == nil {
		e.Cfg = config.Default()
	}
	return e
}

// takeReadFlags removes --json and --brief wherever they are (before any
// --): `sous here --json` reads as naturally as `sous --json here`.
func (e *Env) takeReadFlags(args []string) []string {
	kept := args[:0:0]
	for i, a := range args {
		switch a {
		case "--":
			return append(kept, args[i:]...) // the rest is text
		case "--json":
			e.JSON = true
		case "--brief":
			e.Brief = true
		default:
			kept = append(kept, a)
		}
	}
	return kept
}

// runMode handles the options that stand alone and come first: the shell
// and menu bar surfaces, and help.
func runMode(e *Env, flag string) int {
	switch flag {
	case "--refresh":
		_, code := buildBoard(e, nil)
		return code
	case "--cached":
		return cmdCached(e)
	case "--ambient":
		return cmdAmbient(e)
	case "--menubar":
		return cmdMenubar(e)
	case "--help", "-h":
		usage(e.Stdout)
		return 0
	}
	return fail(e, 2, "unknown flag: %s (try: sous help)", flag)
}

// dispatch runs a verb, or treats an unknown word as a folder, repo or
// project name to show.
func dispatch(e *Env, cmd string, rest []string) int {
	v, ok := findVerb(cmd)
	if !ok {
		if len(rest) == 0 {
			return cmdPath(e, cmd)
		}
		return fail(e, 2, "unknown subcommand: %s (try: sous help)", cmd)
	}
	// --json / --brief belong to read verbs only; a write verb given one is
	// a caller mistake and must fail loud rather than silently succeed.
	if (e.JSON || e.Brief) && !v.read {
		flagName := "--json"
		if e.Brief && !e.JSON {
			flagName = "--brief"
		}
		return fail(e, 2, "%s does not take %s", cmd, flagName)
	}
	if v.args == nil {
		return v.run(e, argv{pos: rest})
	}
	if len(rest) > 0 && (rest[0] == "--help" || rest[0] == "-h") {
		return verbHelp(e, v)
	}
	a, err := parseArgs(*v.args, rest)
	if err != nil {
		return fail(e, 2, "%v\nusage: %s", err, v.synopsis())
	}
	return v.run(e, a)
}

func verbHelp(e *Env, v verb) int {
	fmt.Fprintf(e.Stdout, "usage: %s\n", v.synopsis())
	if _, about, ok := strings.Cut(v.usage, "  "); ok {
		fmt.Fprintf(e.Stdout, "  %s\n", strings.TrimSpace(about))
	}
	return 0
}
