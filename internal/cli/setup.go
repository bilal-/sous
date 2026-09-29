package cli

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/install"
)

// cmdSetup sets everything up and says what it did: where the projects
// are, agent hooks and skills, the shell line, and the menu bar script.
// Folders on the line become the roots. Safe to run again.
func cmdSetup(e *Env, a argv) int {
	if a.has("print-skill") {
		fmt.Fprint(e.Stdout, install.Skill)
		return 0
	}
	var done []string
	roots, code := setupRoots(e, a.pos)
	if code != 0 {
		return code
	}
	done = append(done, roots)
	skills, err := install.Skills(e.UserHome, []byte(install.Skill))
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	done = append(done, skills...)
	hooks, err := install.Hooks(e.UserHome, e.Exe, a.has("codex-session-end"))
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	done = append(done, hooks...)
	menubar, err := install.Files(e.Home, e.Exe)
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	if a.has("no-shell") {
		done = append(done, "shell: skipped. To show the board in new shells, run sous --ambient from your shell's startup file")
	} else {
		msg, err := install.Shell(e.UserHome, e.Home, filepath.Base(e.Shell), runtime.GOOS, e.Zdotdir)
		if err != nil {
			return fail(e, 1, "%v", err)
		}
		done = append(done, msg)
	}
	fmt.Fprintln(e.Stdout, "sous is set up:")
	for _, d := range done {
		fmt.Fprintf(e.Stdout, "  ✓ %s\n", d)
	}
	fmt.Fprintf(e.Stdout, "\nOpen a new terminal to see your board, or run sous now.\n")
	fmt.Fprintf(e.Stdout, "Menu bar (optional, needs SwiftBar): link %s into SwiftBar's plugin folder.\n", config.Tilde(e.UserHome, menubar))
	return 0
}

// setupRoots applies install.Roots and returns the line to show.
func setupRoots(e *Env, given []string) (string, int) {
	if e.cfgErr != nil {
		return "", fail(e, 1, "%s could not be read (%v); fix it, then run sous setup again", config.Tilde(e.UserHome, config.Path(e.Home)), e.cfgErr)
	}
	roots, kept, err := install.Roots(e.UserHome, given, e.Cfg.Roots)
	switch {
	case err != nil:
		return "", fail(e, 2, "%v", err)
	case len(roots) == 0:
		return "projects: " + config.NoRootsHint, 0
	}
	if !kept {
		if err := config.SetRoots(e.Home, e.UserHome, roots); err != nil {
			return "", fail(e, 1, "%v", err)
		}
	}
	shown := make([]string, len(roots))
	for i, r := range roots {
		shown[i] = config.Tilde(e.UserHome, r)
	}
	return "projects: looking in " + strings.Join(shown, ", ") + " (change with sous setup <folder>)", 0
}
