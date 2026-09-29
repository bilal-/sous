// Package launcher takes the user to a project. Every launcher is an
// executable run as `<launcher> run <path>`; built-ins are adapters reached
// through `sous launcher <name> run <path>`, so they get no special path.
// sous never wraps the launcher: it execs it, so the terminal, Ctrl-C and
// the exit code all belong to the agent.
package launcher

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/store"
)

type Launcher struct {
	Name string
	Argv []string
}

func Launchers(exe string, builtins, thirdParty []string) []Launcher {
	var out []Launcher
	for _, p := range plugin.Discover(exe, "launcher", builtins, thirdParty) {
		out = append(out, Launcher(p))
	}
	return out
}

// Builtins: the agent CLIs the user already runs, by launcher name → binary.
// Each is exec'd in the project directory with the user's terminal.
var Builtins = map[string]string{"claude": "claude", "codex": "codex"}

// BuiltinNames, sorted, from Builtins.
func BuiltinNames() []string { return slices.Sorted(maps.Keys(Builtins)) }

func Find(ls []Launcher, name string) (Launcher, bool) {
	for _, l := range ls {
		if l.Name == name {
			return l, true
		}
	}
	return Launcher{}, false
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

// WriteContext saves the resume summary handed to an agent started in
// project, as SOUS_HERE_FILE: one file per project under home/here,
// replaced each time, so nothing piles up.
func WriteContext(home, project string, body []byte) (string, error) {
	sum := sha256.Sum256([]byte(project))
	name := filepath.Join(home, "here", hex.EncodeToString(sum[:6])+".txt")
	return name, store.WriteFile(name, body, 0o600)
}
