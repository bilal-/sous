// Package install puts sous where people and agents will find it: agent
// hooks and skills, and one line in the shell's startup file. Every step is
// safe to repeat and reports what it did, in words for the person.
package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/hook"
	"github.com/bilal-/sous/internal/store"
)

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
			files = append(files, filepath.Join(home, ".bash_profile"))
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

// addLine appends line to file, with a comment saying why, unless an
// uncommented copy is already there.
func addLine(file, line string) (bool, error) {
	b, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == line {
			return false, nil
		}
	}
	prefix := ""
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		prefix = "\n"
	}
	body := string(b) + prefix + "\n# sous: show what is waiting on you in new shells\n" + line + "\n"
	return true, store.WriteFile(file, []byte(body), 0o644)
}
