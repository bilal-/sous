// Package install puts sous where people and agents will find it: agent
// hooks and skills, and one line in the shell's startup file. Every step is
// safe to repeat and reports what it did, in words for the person.
package install

import (
	"cmp"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/harness"
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

// hookSpec is one agent hook sous installs.
type hookSpec struct {
	harness.Harness
	harness.Hook
}

func (x hookSpec) file(home string) string { return x.HookFile(home) }

// here are the harnesses on this machine: setup sets them up, and doctor
// checks them.
func here(home string) []harness.Harness {
	var hs []harness.Harness
	for _, h := range harness.All {
		if h.Here(home) {
			hs = append(hs, h)
		}
	}
	return hs
}

// hookSpecs are the hooks of every harness here, optional ones included.
// Setup installs the ones it should; doctor checks them all.
func hookSpecs(home string) []hookSpec {
	var hs []hookSpec
	for _, h := range here(home) {
		for _, k := range h.Hooks {
			hs = append(hs, hookSpec{h, k})
		}
	}
	return hs
}

// Hooks adds the session hooks for the sous at exe: every harness's, and
// the optional ones only when their flag is in flags.
func Hooks(home, exe string, flags []string) ([]string, error) {
	extra := map[string]bool{} // harness → an optional end hook is on
	for _, x := range hookSpecs(home) {
		if x.Flag != "" && !slices.Contains(flags, x.Flag) {
			continue
		}
		if _, err := x.Format.Place(x.file(home), x.Event, harness.Cmd{Exe: exe, Role: x.Role, Agent: x.Name}); err != nil {
			return nil, fmt.Errorf("%s: %w", x.file(home), err)
		}
		extra[x.Name] = extra[x.Name] || x.Flag != "" && x.Role == harness.RoleEnd
	}
	var done []string
	for _, h := range here(home) {
		note := ""
		if extra[h.Name] {
			note = " and when it ends"
		}
		done = append(done, h.Display+": sees where you left off when a session starts"+note+", and has the sous skill")
	}
	return done, nil
}

// skillPlaces: each harness's own skill folder (its hook line already
// says it has the skill), then the shared ones that apply here.
func skillPlaces(home string) []harness.SkillFolder {
	var places []harness.SkillFolder
	for _, h := range here(home) {
		places = append(places, harness.SkillFolder{Who: h.Display, Dir: h.SkillDir})
	}
	for _, f := range harness.SharedSkills {
		if f.When == nil || f.When(home) {
			places = append(places, f)
		}
	}
	return places
}

// skillFile is where the sous skill goes in a skill folder.
func skillFile(home string, f harness.SkillFolder) string {
	return filepath.Join(f.Dir(home), "sous", "SKILL.md")
}

// Skills writes the sous skill in every skill folder, and reports the agents
// not already covered by Hooks.
func Skills(home string, skill []byte) ([]string, error) {
	var done []string
	for _, p := range skillPlaces(home) {
		if err := store.WriteFile(skillFile(home, p), skill, 0o644); err != nil {
			return nil, fmt.Errorf("writing the skill: %w", err)
		}
		if p.Says != "" {
			done = append(done, p.Says)
		}
	}
	return done, nil
}

// Shell adds one line to the startup file of shell (zsh, bash or fish) so
// new terminals show the board, and says what it did. goos picks bash's
// file (macOS Terminal opens login shells, which read .bash_profile);
// zdotdir is $ZDOTDIR, where zsh keeps its files when set.
func Shell(home, sousHome, shell, goos, zdotdir string) (string, error) {
	files, line, ok := shellTarget(home, sousHome, shell, goos, zdotdir)
	if !ok {
		return fmt.Sprintf("shell: %s is not one sous knows. To show the board in new shells, run sous --ambient from its startup file", cmp.Or(shell, "your shell")), nil
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

// shellTarget is where a shell's line goes and what it says; false for a
// shell sous does not know. Setup writes it; doctor checks it.
func shellTarget(home, sousHome, shell, goos, zdotdir string) (files []string, line string, ok bool) {
	switch shell {
	case "zsh":
		dir := home
		if zdotdir != "" {
			dir = zdotdir
		}
		line = `source "` + strings.Replace(config.Tilde(home, filepath.Join(sousHome, "sous.zsh")), "~", "$HOME", 1) + `"`
		return []string{filepath.Join(dir, ".zshrc")}, line, true
	case "bash":
		files = []string{filepath.Join(home, ".bashrc")}
		if goos == "darwin" {
			if f := bashLoginFile(home); f != "" {
				files = append(files, f)
			}
		}
		return files, bashLine, true
	case "fish":
		return []string{filepath.Join(home, ".config", "fish", "conf.d", "sous.fish")}, fishLine, true
	}
	return nil, "", false
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
		if sourcesBashrc.Match(b) {
			return ""
		}
		return p
	}
	return filepath.Join(home, ".bash_profile")
}

// sourcesBashrc: a command (at the start of a line, or after ;, &&, || or
// then) that runs ~/.bashrc itself.
var sourcesBashrc = regexp.MustCompile(`(?m)(?:^\s*|[;&|]\s*|\bthen\s+)(?:source|\.)\s+"?(?:~|\$HOME|\$\{HOME\})/\.bashrc"?(?:[\s;&|]|$)`)

// shellComment marks the line sous adds.
const shellComment = "# sous: show what is waiting on you in new shells"

// sourcesSnippet is a line that sources a sous.zsh, wherever SOUS_HOME was
// and however the home folder is written.
var sourcesSnippet = regexp.MustCompile(`^(?:source|\.)\s+"?(?:\$HOME|~|\$\{HOME\})?/[^"\s]*/sous\.zsh"?$`)

// The lines setup adds for bash and fish. Both print only in interactive
// shells, and bash's always succeeds (a startup file must not end failing).
const (
	bashLine = "if [[ $- == *i* ]] && command -v sous >/dev/null; then sous --ambient 2>/dev/null; fi"
	fishLine = "status is-interactive; and command -q sous; and sous --ambient 2>/dev/null"
)

// knownLines are the other startup lines sous has written, or told people
// to write, to show the board.
var knownLines = []string{
	"sous --ambient", "sous --ambient 2>/dev/null", "sous --cached",
	"command -v sous >/dev/null && sous --ambient 2>/dev/null",
	bashLine, fishLine,
}

// isSousLine: a startup line sous put there (or told the person to add).
// A line that merely mentions sous is someone else's.
func isSousLine(l string) bool {
	l = strings.TrimSpace(l)
	return sourcesSnippet.MatchString(l) || slices.Contains(knownLines, l)
}

// addLine puts line into file once: an earlier sous line is replaced in
// place (so upgrades reach it); otherwise line is appended with a comment
// saying why. It reports whether the file changed.
func addLine(file, line string) (bool, error) {
	changed := false
	err := store.EditFile(file, 0o644, func(b []byte) ([]byte, error) {
		lines := strings.Split(string(b), "\n")
		for i, l := range lines {
			if strings.TrimSpace(l) != line && !isSousLine(l) {
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
