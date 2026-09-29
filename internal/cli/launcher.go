package cli

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"

	"github.com/bilal-/sous/internal/launcher"
)

// cmdLauncher: `sous launcher <name> run <path>`.
func cmdLauncher(e *Env, a argv) int {
	args := a.pos
	if len(args) != 3 || args[1] != "run" {
		return fail(e, 2, "usage: sous launcher <name> run <path>")
	}
	bin, ok := launcher.Builtins[args[0]]
	if !ok {
		return fail(e, 2, "no built-in launcher: %s", args[0])
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return fail(e, 1, "%s not found on PATH", bin)
	}
	return execIn(e, args[2], path, []string{bin})
}

var lookPath = exec.LookPath

// execIn replaces sous with argv in dir, setting env on our environment.
// Returns only on failure.
func execIn(e *Env, dir, path string, argv []string, env ...string) int {
	if err := os.Chdir(dir); err != nil {
		return fail(e, 1, "%v", err)
	}
	if err := syscall.Exec(path, argv, withEnv(os.Environ(), env)); err != nil {
		return fail(e, 1, "exec %s: %v", path, err)
	}
	return 0
}

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
