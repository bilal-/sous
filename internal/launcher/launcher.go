// Package launcher takes the user to a project. Every launcher is an
// executable run as `<launcher> run <path>`; built-ins are adapters reached
// through `sous launcher <name> run <path>`, so they get no special path.
// sous never wraps the launcher: it execs it, so the terminal, Ctrl-C and
// the exit code all belong to the agent.
package launcher

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/bilal-/sous/internal/plugin"
)

// Launcher is a launcher as sous calls it.
type Launcher = plugin.Plugin

// Launchers lists the named built ins, then the launcher programs among
// thirdParty.
func Launchers(exe string, builtins, thirdParty []string) []Launcher {
	return Registry.Discover(exe, builtins, thirdParty)
}

// Builtins: the agent CLIs the user already runs, by launcher name → binary.
// Each is exec'd in the project directory with the user's terminal.
var Builtins = map[string]string{"claude": "claude", "codex": "codex"}

// BuiltinNames, sorted, from Builtins.
func BuiltinNames() []string { return slices.Sorted(maps.Keys(Builtins)) }

// Deps is what the built in launchers are given: Exec replaces sous with
// the program in dir, and returns only when it could not.
type Deps struct {
	Exec func(dir, path string, argv []string) error
}

// Registry is the built in launchers, one per agent in Builtins. Each has
// one call, `run <path>`.
var Registry = plugin.Registry[Deps]{Axis: "launcher"}

func init() {
	for _, name := range BuiltinNames() {
		Registry.Builtins = append(Registry.Builtins, plugin.Builtin[Deps]{Name: name, Ops: func(d Deps) map[string]plugin.Op {
			return map[string]plugin.Op{"run": func(args []string, _ io.Reader, _, stderr io.Writer) int {
				if len(args) != 1 {
					return plugin.Usage(stderr, "run <path>")
				}
				path, err := BuiltinPath(name)
				if err == nil {
					err = d.Exec(args[0], path, []string{filepath.Base(path)})
				}
				return plugin.Exit(err, stderr)
			}}
		}})
	}
}

// ExecArgv resolves the executable and builds the argv for syscall.Exec.
func ExecArgv(l Launcher, path string) (string, []string, error) {
	argv0, err := exec.LookPath(l.Argv[0])
	if err != nil {
		return "", nil, fmt.Errorf("launcher %s: %s not found on PATH", l.Name, l.Argv[0])
	}
	argv := append(append([]string{}, l.Argv...), "run", path)
	return argv0, argv, nil
}

// Prepare finds the launcher named agent (a built-in or a listed plugin)
// and returns the program and arguments that start it in project. Anything
// missing is an error now, so it reads as a sous message rather than a
// failed exec.
func Prepare(exe string, plugins []string, agent, project string) (string, []string, error) {
	l, ok := plugin.Find(Launchers(exe, BuiltinNames(), plugins), agent)
	if !ok {
		return "", nil, fmt.Errorf("%w: %s", ErrUnknown, agent)
	}
	if _, err := BuiltinPath(l.Name); err != nil && !errors.Is(err, ErrUnknown) {
		return "", nil, fmt.Errorf("launcher %s: %w", l.Name, err)
	}
	return ExecArgv(l, project)
}

// ErrUnknown: no launcher by that name.
var ErrUnknown = errors.New("no launcher named")

// BuiltinPath is where the program a built-in launcher starts is found.
func BuiltinPath(name string) (string, error) {
	bin, ok := Builtins[name]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknown, name)
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH", bin)
	}
	return path, nil
}
