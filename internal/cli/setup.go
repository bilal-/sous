package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/hook"
	"github.com/bilal-/sous/internal/project"
)

// cmdSetup is the whole installation, safe to run again: where your
// projects are, the agent hooks and skill, the shell line, and the menu bar
// script. It says what it changed. Folders on the line become the roots.
func cmdSetup(e *Env, a argv) int {
	if a.has("print-skill") {
		fmt.Fprint(e.Stdout, skillMD)
		return 0
	}
	home, _ := os.UserHomeDir()
	if err := os.MkdirAll(e.Home, 0o755); err != nil {
		return fail(e, 1, "%v", err)
	}
	var done []string
	say := func(format string, args ...any) { done = append(done, fmt.Sprintf(format, args...)) }

	// 1. Where your projects are.
	if code := setupRoots(e, home, a.pos, say); code != 0 {
		return code
	}

	// 2. Agent hooks and the skill.
	claude := filepath.Join(home, ".claude", "settings.json")
	codex := filepath.Join(home, ".codex", "hooks.json")
	type inst struct{ file, event, cmd string }
	installs := []inst{
		{claude, "SessionStart", e.Exe + " hook session-start claude"},
		{claude, "SessionEnd", e.Exe + " hook session-end claude"},
		{codex, "SessionStart", e.Exe + " hook session-start codex"},
	}
	codexNote := ""
	if a.has("codex-session-end") {
		installs = append(installs, inst{codex, "SessionEnd", e.Exe + " hook session-end codex"})
		codexNote = " and when it ends"
	}
	for _, i := range installs {
		if _, err := hook.Install(i.file, i.event, i.cmd); err != nil {
			return fail(e, 1, "%s: %v", i.file, err)
		}
	}
	for _, dir := range []string{filepath.Join(home, ".claude", "skills", "sous"), filepath.Join(home, ".codex", "skills", "sous")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail(e, 1, "%v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
			return fail(e, 1, "writing the skill: %v", err)
		}
	}
	say("Claude Code: sees where you left off when a session starts, and has the sous skill")
	say("Codex: sees where you left off when a session starts%s, and has the sous skill", codexNote)

	// 3. The shell.
	snippet := filepath.Join(e.Home, "sous.zsh")
	if err := os.WriteFile(snippet, []byte(shellSnippet), 0o644); err != nil {
		return fail(e, 1, "writing %s: %v", snippet, err)
	}
	if a.has("no-shell") {
		say("shell: skipped. To show the board in new shells, run sous --ambient from your shell's startup file")
	} else if msg, err := setupShell(home, e.Home); err != nil {
		return fail(e, 1, "%v", err)
	} else {
		say("%s", msg)
	}

	// 4. The menu bar script (SwiftBar on macOS), pointed at this sous.
	swiftbar := filepath.Join(e.Home, "sous.5m.sh")
	if err := os.WriteFile(swiftbar, []byte(strings.Replace(swiftbarPlugin, "$HOME/.local/bin/sous", e.Exe, 1)), 0o755); err != nil {
		return fail(e, 1, "writing %s: %v", swiftbar, err)
	}

	fmt.Fprintln(e.Stdout, "sous is set up:")
	for _, d := range done {
		fmt.Fprintf(e.Stdout, "  ✓ %s\n", d)
	}
	fmt.Fprintf(e.Stdout, "\nOpen a new terminal to see your board, or run sous now.\n")
	fmt.Fprintf(e.Stdout, "Menu bar (optional, needs SwiftBar): link %s into SwiftBar's plugin folder.\n", tilde(home, swiftbar))
	return 0
}

// likelyRoots are where people usually keep their repos.
var likelyRoots = []string{"code", "src", "dev", "projects", "workspace", "repos", "git", "Developer", "Projects", "Documents/GitHub"}

// setupRoots: folders given replace the roots; otherwise keep what config
// has, or look in the usual places.
func setupRoots(e *Env, home string, given []string, say func(string, ...any)) int {
	var roots []string
	switch {
	case len(given) > 0:
		for _, g := range given {
			abs, err := filepath.Abs(config.Expand(g))
			if st, serr := os.Stat(abs); err != nil || serr != nil || !st.IsDir() {
				return fail(e, 2, "%s is not a folder", g)
			}
			roots = append(roots, tilde(home, abs))
		}
	case e.cfgErr == nil && len(e.Cfg.Roots) > 0:
		shown := make([]string, len(e.Cfg.Roots))
		for i, r := range e.Cfg.Roots {
			shown[i] = tilde(home, r)
		}
		say("projects: looking in %s (change with sous setup <folder>)", strings.Join(shown, ", "))
		return 0
	default:
		for _, r := range likelyRoots {
			dir := filepath.Join(home, r)
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				continue
			}
			if ps, _ := project.Discover([]string{dir}, nil, nil); len(ps) > 0 {
				roots = append(roots, "~/"+r)
			}
		}
		if len(roots) == 0 {
			say("projects: none found in the usual places. Tell sous where they are: sous setup ~/path/to/your/projects")
			return 0
		}
	}
	if e.cfgErr != nil {
		return fail(e, 1, "%s/config.toml could not be read (%v); fix it, then run sous setup again", e.Home, e.cfgErr)
	}
	if err := config.SetRoots(e.Home, roots); err != nil {
		return fail(e, 1, "%v", err)
	}
	say("projects: looking in %s (change with sous setup <folder>)", strings.Join(roots, ", "))
	return 0
}

// setupShell adds one line to the shell's startup file, once.
func setupShell(home, sousHome string) (string, error) {
	shell := filepath.Base(os.Getenv("SHELL"))
	var rc, line string
	switch shell {
	case "zsh":
		rc, line = filepath.Join(home, ".zshrc"), `source "`+tilde(home, filepath.Join(sousHome, "sous.zsh"))+`"`
		line = strings.Replace(line, "~", "$HOME", 1)
	case "bash":
		rc, line = filepath.Join(home, ".bashrc"), "command -v sous >/dev/null && sous --ambient 2>/dev/null"
	case "fish":
		rc, line = filepath.Join(home, ".config", "fish", "conf.d", "sous.fish"), "status is-interactive; and command -q sous; and sous --ambient 2>/dev/null"
	default:
		return fmt.Sprintf("shell: %s is not one sous knows. To show the board in new shells, run sous --ambient from its startup file", shell), nil
	}
	b, err := os.ReadFile(rc)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if strings.Contains(string(b), line) || strings.Contains(string(b), ".sous/sous.zsh") {
		return fmt.Sprintf("shell: new %s shells show the board (already in %s)", shell, tilde(home, rc)), nil
	}
	if err := os.MkdirAll(filepath.Dir(rc), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	prefix := ""
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		prefix = "\n"
	}
	if _, err := fmt.Fprintf(f, "%s\n# sous: show what is waiting on you in new shells\n%s\n", prefix, line); err != nil {
		return "", err
	}
	return fmt.Sprintf("shell: new %s shells show the board (added one line to %s)", shell, tilde(home, rc)), nil
}

// tilde shows a path under home as ~/..., as people write it.
func tilde(home, p string) string {
	if rest, ok := strings.CutPrefix(p, home); ok && home != "" && (rest == "" || rest[0] == '/') {
		return "~" + rest
	}
	return p
}
