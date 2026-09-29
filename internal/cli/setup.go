package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/install"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
)

// cmdSetup sets everything up and says what it did: where the projects
// are, agent hooks and skills, the shell line, and the menu bar script.
// Folders on the line become the roots. Safe to run again.
func cmdSetup(e *Env, a argv) int {
	if a.has("print-skill") {
		fmt.Fprint(e.Stdout, skillMD)
		return 0
	}
	var done []string
	roots, code := setupRoots(e, a.pos)
	if code != 0 {
		return code
	}
	done = append(done, roots)
	skills, err := install.Skills(e.UserHome, []byte(skillMD))
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	done = append(done, skills...)
	hooks, err := install.Hooks(e.UserHome, e.Exe, a.has("codex-session-end"))
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	done = append(done, hooks...)
	if err := store.WriteFile(filepath.Join(e.Home, "sous.zsh"), []byte(shellSnippet), 0o644); err != nil {
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
	menubar := filepath.Join(e.Home, "sous.5m.sh")
	if err := store.WriteFile(menubar, []byte(strings.Replace(swiftbarPlugin, swiftbarExePlaceholder, e.Exe, 1)), 0o755); err != nil {
		return fail(e, 1, "%v", err)
	}
	fmt.Fprintln(e.Stdout, "sous is set up:")
	for _, d := range done {
		fmt.Fprintf(e.Stdout, "  ✓ %s\n", d)
	}
	fmt.Fprintf(e.Stdout, "\nOpen a new terminal to see your board, or run sous now.\n")
	fmt.Fprintf(e.Stdout, "Menu bar (optional, needs SwiftBar): link %s into SwiftBar's plugin folder.\n", config.Tilde(e.UserHome, menubar))
	return 0
}

// setupRoots: folders given replace the roots; otherwise keep what config
// has, or use the usual places that hold repos. It returns the line to show.
func setupRoots(e *Env, given []string) (string, int) {
	if e.cfgErr != nil {
		return "", fail(e, 1, "%s/config.toml could not be read (%v); fix it, then run sous setup again", e.Home, e.cfgErr)
	}
	var roots []string
	switch {
	case len(given) > 0:
		for _, g := range given {
			abs, err := filepath.Abs(config.Expand(e.UserHome, g))
			if st, serr := os.Stat(abs); err != nil || serr != nil || !st.IsDir() {
				return "", fail(e, 2, "%s is not a folder", g)
			}
			roots = append(roots, abs)
		}
	case len(e.Cfg.Roots) > 0:
		return "projects: looking in " + shownRoots(e, e.Cfg.Roots) + " (change with sous setup <folder>)", 0
	default:
		roots = project.LikelyRoots(e.UserHome)
		if len(roots) == 0 {
			return "projects: " + noProjectsHint, 0
		}
	}
	written := make([]string, len(roots))
	for i, r := range roots {
		written[i] = config.Tilde(e.UserHome, r)
	}
	if err := config.SetRoots(e.Home, written); err != nil {
		return "", fail(e, 1, "%v", err)
	}
	return "projects: looking in " + shownRoots(e, roots) + " (change with sous setup <folder>)", 0
}

func shownRoots(e *Env, roots []string) string {
	shown := make([]string, len(roots))
	for i, r := range roots {
		shown[i] = config.Tilde(e.UserHome, r)
	}
	return strings.Join(shown, ", ")
}
