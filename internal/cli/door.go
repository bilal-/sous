package cli

import (
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/launcher"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/signal"
)

// The doors to the built in plugins, `sous <axis> <name> <call> [args]`:
// how sous calls its own built ins, exactly as it calls a plugin program,
// and a way to try one by hand. Each is its axis's registry, served with
// what its built ins are made from.

func cmdSignal(e *Env, a argv) int {
	return signal.Registry.Serve(e.Cfg, a.pos, e.Stdin, e.Stdout, e.Stderr)
}

func cmdBackend(e *Env, a argv) int {
	return backend.Registry.Serve(backend.Deps{Home: e.Home, Cfg: e.Cfg}, a.pos, e.Stdin, e.Stdout, e.Stderr)
}

func cmdLauncher(e *Env, a argv) int {
	return launcher.Registry.Serve(launcher.Deps{Exec: func(dir, path string, argv []string) error {
		return execIn(dir, path, argv)
	}}, a.pos, e.Stdin, e.Stdout, e.Stderr)
}

func cmdRunner(e *Env, a argv) int {
	return runner.Registry.Serve(runner.Deps{Home: e.Home, Exe: e.Exe, Limit: e.Cfg.RunLimit()}, a.pos, e.Stdin, e.Stdout, e.Stderr)
}

// execIn replaces sous with argv in dir, setting env on our environment.
// It returns only when it could not.
func execIn(dir, path string, argv []string, env ...string) error {
	if err := os.Chdir(dir); err != nil {
		return err
	}
	return execProgram(path, argv, withEnv(os.Environ(), env))
}

// execProgram replaces this process with another. Tests that run sous in
// their own process swap it, so no test can end the test run.
var execProgram = syscall.Exec

// withEnv sets each "K=V" in set on base, replacing any inherited K: exec
// keeps duplicates and programs read the first.
func withEnv(base, set []string) []string {
	out := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(set, func(s string) bool { return strings.HasPrefix(s, k+"=") }) {
			out = append(out, kv)
		}
	}
	return append(out, set...)
}
