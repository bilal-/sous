// Package install puts sous where people and agents will find it: agent
// hooks and skills, and one line in the shell's startup file. Every step is
// safe to repeat and reports what it did, in words for the person.
package install

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/hook"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
)

// Skill is the sous skill: a few lines saying when to reach for sous and to
// run sous help. The CLI teaches the rest.
//
//go:embed assets/SKILL.md
var Skill string

//go:embed assets/sous.zsh
var zshSnippet string

//go:embed assets/sous.5m.sh
var menuBarScript string

// Files writes what the shell line and the menu bar run, into sousHome:
// the zsh snippet, and the SwiftBar script pointed at the sous at exe.
// It returns the menu bar script's path.
func Files(sousHome, exe string) (string, error) {
	if err := store.WriteFile(filepath.Join(sousHome, "sous.zsh"), []byte(zshSnippet), 0o644); err != nil {
		return "", err
	}
	menubar := filepath.Join(sousHome, "sous.5m.sh")
	if err := store.WriteFile(menubar, []byte(strings.Replace(menuBarScript, "@SOUS@", exe, 1)), 0o755); err != nil {
		return "", err
	}
	return menubar, os.Chmod(menubar, 0o755) // SwiftBar runs it, whatever mode it had
}

// Roots picks the project folders: the ones given, else the ones config
// already has (kept is true), else the usual places that hold projects.
// Nothing is written here.
func Roots(userHome string, given, configured []string) (roots []string, kept bool, err error) {
	switch {
	case len(given) > 0:
		for _, g := range given {
			abs, err := filepath.Abs(config.Expand(userHome, g))
			if st, serr := os.Stat(abs); err != nil || serr != nil || !st.IsDir() {
				return nil, false, fmt.Errorf("%s is not a folder", g)
			}
			roots = append(roots, abs)
		}
		return roots, false, nil
	case len(configured) > 0:
		return configured, true, nil
	}
	return project.LikelyRoots(userHome), false, nil
}

// Hooks adds the session hooks for Claude Code (start and end) and Codex
// (start; end too when codexEnd), for the sous at exe.
func Hooks(home, exe string, codexEnd bool) ([]string, error) {
	claude := filepath.Join(home, ".claude", "settings.json")
	codex := filepath.Join(home, ".codex", "hooks.json")
	type h struct{ file, event, role, agent string }
	hs := []h{
		{claude, "SessionStart", "session-start", "claude"},
		{claude, "SessionEnd", "session-end", "claude"},
		{codex, "SessionStart", "session-start", "codex"},
	}
	codexNote := ""
	if codexEnd {
		hs = append(hs, h{codex, "SessionEnd", "session-end", "codex"})
		codexNote = " and when it ends"
	}
	for _, x := range hs {
		if _, err := hook.Install(x.file, x.event, hook.Command(exe, x.role, x.agent)); err != nil {
			return nil, fmt.Errorf("%s: %w", x.file, err)
		}
	}
	return []string{
		"Claude Code: sees where you left off when a session starts, and has the sous skill",
		"Codex: sees where you left off when a session starts" + codexNote + ", and has the sous skill",
	}, nil
}

// Skills writes the sous skill where agents look for skills: Claude
// Code's and Codex's folders, the shared ~/.agents/skills that Gemini CLI,
// Kimi, Cursor and others read, and Antigravity's folder when Antigravity
// is installed. It reports the agents not already covered by Hooks.
func Skills(home string, skill []byte) ([]string, error) {
	type place struct{ dir, report string }
	places := []place{
		{dir: filepath.Join(home, ".claude", "skills")},
		{dir: filepath.Join(home, ".codex", "skills")},
		{filepath.Join(home, ".agents", "skills"), "Gemini CLI, Kimi, Cursor and other agents: have the sous skill (in ~/.agents/skills, the shared folder they read)"},
	}
	if st, err := os.Stat(filepath.Join(home, ".gemini", "antigravity")); err == nil && st.IsDir() {
		places = append(places, place{filepath.Join(home, ".gemini", "antigravity", "skills"), "Antigravity: has the sous skill"})
	}
	var done []string
	for _, p := range places {
		if err := store.WriteFile(filepath.Join(p.dir, "sous", "SKILL.md"), skill, 0o644); err != nil {
			return nil, fmt.Errorf("writing the skill: %w", err)
		}
		if p.report != "" {
			done = append(done, p.report)
		}
	}
	return done, nil
}

// Shell adds one line to the startup file of shell (zsh, bash or fish) so
// new terminals show the board, and says what it did. goos picks bash's
// file (macOS Terminal opens login shells, which read .bash_profile);
// zdotdir is $ZDOTDIR, where zsh keeps its files when set.
func Shell(home, sousHome, shell, goos, zdotdir string) (string, error) {
	var files []string
	var line string
	switch shell {
	case "zsh":
		dir := home
		if zdotdir != "" {
			dir = zdotdir
		}
		files = []string{filepath.Join(dir, ".zshrc")}
		line = `source "` + strings.Replace(config.Tilde(home, filepath.Join(sousHome, "sous.zsh")), "~", "$HOME", 1) + `"`
	case "bash":
		files = []string{filepath.Join(home, ".bashrc")}
		if goos == "darwin" {
			if f := bashLoginFile(home); f != "" {
				files = append(files, f)
			}
		}
		line = `if [[ $- == *i* ]] && command -v sous >/dev/null; then sous --ambient 2>/dev/null; fi`
	case "fish":
		files = []string{filepath.Join(home, ".config", "fish", "conf.d", "sous.fish")}
		line = "status is-interactive; and command -q sous; and sous --ambient 2>/dev/null"
	default:
		return fmt.Sprintf("shell: %s is not one sous knows. To show the board in new shells, run sous --ambient from its startup file", shell), nil
	}
	var added []string
	for _, f := range files {
		changed, err := addLine(f, line)
		if err != nil {
			return "", err
		}
		if changed {
			added = append(added, config.Tilde(home, f))
		}
	}
	if len(added) == 0 {
		return fmt.Sprintf("shell: new %s shells show the board (already set up)", shell), nil
	}
	return fmt.Sprintf("shell: new %s shells show the board (added one line to %s)", shell, strings.Join(added, " and ")), nil
}

// bashLoginFile is where macOS Terminal's login bash will run the line:
// the first of .bash_profile, .bash_login and .profile that exists (bash
// reads only that one), or a new .bash_profile when none does. "" when that
// file already sources .bashrc, which has the line.
func bashLoginFile(home string) string {
	for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
		p := filepath.Join(home, name)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), ".bashrc") {
			return ""
		}
		return p
	}
	return filepath.Join(home, ".bash_profile")
}

// shellComment marks the line sous adds.
const shellComment = "# sous: show what is waiting on you in new shells"

// isSousLine: a line an earlier sous (or the person) added to show the
// board: it runs sous --ambient or sources sous.zsh, and is not a comment.
func isSousLine(l string) bool {
	l = strings.TrimSpace(l)
	return !strings.HasPrefix(l, "#") && (strings.Contains(l, "sous --ambient") || strings.Contains(l, "sous.zsh"))
}

// addLine puts line into file once: an earlier sous line is replaced in
// place (so upgrades reach it); otherwise line is appended with a comment
// saying why. It reports whether the file changed.
func addLine(file, line string) (bool, error) {
	changed := false
	err := store.EditFile(file, 0o644, func(b []byte) ([]byte, error) {
		lines := strings.Split(string(b), "\n")
		for i, l := range lines {
			if !isSousLine(l) {
				continue
			}
			if strings.TrimSpace(l) == line {
				return nil, nil
			}
			lines[i], changed = line, true
			return []byte(strings.Join(lines, "\n")), nil
		}
		prefix := ""
		if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
			prefix = "\n"
		}
		changed = true
		return []byte(string(b) + prefix + "\n" + shellComment + "\n" + line + "\n"), nil
	})
	return changed, err
}
