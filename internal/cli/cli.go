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
	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/tracker"
)

// The exit codes, the same ones plugins speak (sous help lists them).
const (
	exitFailed = plugin.ExitFailed
	// exitUsage: the wrong command, flag or arguments, or a name that
	// matches more than one project.
	exitUsage = plugin.ExitRefused
	// exitNotReady: asked for the cached board before there was one (a
	// first one is being built), or the agent or runner sous go needs is
	// not set up.
	exitNotReady = plugin.ExitNotSetUp
)

// Version is set at build time: -ldflags "-X github.com/bilal-/sous/internal/cli.Version=v0.1.0".
// A plain go build says dev, never a release it is not.
var Version = "dev"

const usageHeader = `sous %s: one list of what is waiting on you, across every project.

  sous              the board: what's waiting on you across all projects
  sous <folder>     board scoped to a folder of repos
  sous <project>    where was I, for a named project or repo path
`

const usageFooter = `
Flags: --json (what a command shows or changed) · --brief (here) · --ambient · --cached · --refresh · --menubar · --version
Exit:  0 ok · 1 failure · 2 usage or ambiguity · 3 not ready: no board yet (--cached, --ambient), or the agent or runner is not set up (go)
Put -- before a note that starts with a dash.

For agents:
  Notes are the user's. Keep them private unless the user asks to share one.
  sous file, note --file and done --close write where other people can see.
    Before running one, ask the user, end your turn, and run it only after
    they say yes in their next message.
  When a project name matches more than one project, sous lists them. Show
    the list and ask; never pick one yourself.
  Set SOUS_SOURCE=agent when you write a note on your own initiative.
  Never create FOLLOWUPS.md, change settings (sous config) or edit the
    <!-- sous:... --> markers to make filing work unless the user asks. If
    sous says there is no tracker, say so.
  sous done means the user's task is finished, not that you wrote code.
  When sous shows a ? or a source failed, run sous doctor for the cause and
    the fix, and tell the user.
  (ref missing) and (status unavailable) mean sous could not confirm, not done.
  When work is put off, the user waits on someone, or an idea belongs to
    another project, offer to note it in one line. Write it only on a yes.
  To hand the user's task to a background agent: sous go <project> --run -
    with a full brief on stdin (what, why, and what done means). A retry
    with the same brief finds the run already started; give --key only to
    start a second run of the same brief. Check with sous show <n> --json.
    When a run needs the user, ask them, then pass the answer with
    sous reply <n> "<answer>". A run never pushes; the user reviews its
    branch. sous done <n> --clean when they are finished with it.
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
	Shell    string // the shell's name, from $SHELL: "zsh", "bash", "fish"
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
	return plugin.Timeout
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

// config returns the loaded config or the exit code for a broken one.
func (e *Env) config() (*config.Config, int) {
	if e.cfgErr != nil {
		return nil, fail(e, exitFailed, "config: %v", e.cfgErr)
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
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintln(e.Stderr, "sous: "+msg)
	if e.JSON {
		// A caller that asked for JSON reads stdout: the error is there too.
		// Written directly, never through writeJSON, which fails through here.
		b, _ := json.MarshalIndent(errorJSON{Error: msg, Exit: code}, "", "  ")
		fmt.Fprintf(e.Stdout, "%s\n", b)
	}
	return code
}

// errorJSON is what --json prints when a command fails: the same line as
// standard error, and the exit code (sous help lists them).
type errorJSON struct {
	Error string `json:"error"`
	Exit  int    `json:"exit"`
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
	e.Zdotdir = os.Getenv("ZDOTDIR")
	if sh := os.Getenv("SHELL"); sh != "" {
		e.Shell = filepath.Base(sh)
	}
	cache, _ := os.UserCacheDir()
	tracker.Init(e.UserHome, cache, os.Environ())
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
	case "--version":
		return cmdVersion(e, argv{})
	}
	return fail(e, exitUsage, "unknown flag: %s (try: sous help)", flag)
}

// dispatch runs a verb, or treats an unknown word as a folder, repo or
// project name to show.
func dispatch(e *Env, cmd string, rest []string) int {
	v, ok := findVerb(cmd)
	if !ok {
		if len(rest) == 0 {
			return cmdPath(e, cmd)
		}
		return fail(e, exitUsage, "unknown subcommand: %s (try: sous help)", cmd)
	}
	// A verb that has no use for --json or --brief refuses it: silently
	// ignoring it would let a caller believe it took effect.
	switch {
	case e.JSON && !v.json:
		return fail(e, exitUsage, "%s does not take --json", cmd)
	case e.Brief && !v.brief:
		return fail(e, exitUsage, "%s does not take --brief", cmd)
	}
	if v.args == nil {
		return v.run(e, argv{pos: rest})
	}
	if len(rest) > 0 && (rest[0] == "--help" || rest[0] == "-h") {
		return verbHelp(e, v)
	}
	a, err := parseArgs(*v.args, rest)
	if err != nil {
		return fail(e, exitUsage, "%v\nusage: %s", err, v.synopsis())
	}
	return v.run(e, a)
}
