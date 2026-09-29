package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilal-/sous/internal/hook"
)

// setup is the installer: agent hooks, the /sous skill, the zsh snippet and
// the SwiftBar plugin. Idempotent; the binary is the source of truth for
// every file it writes.
func cmdSetup(e *Env, a argv) int {
	if a.has("print-skill") {
		fmt.Fprint(e.Stdout, skillMD)
		return 0
	}
	codexEnd := a.has("codex-session-end")
	home, _ := os.UserHomeDir()
	claude := filepath.Join(home, ".claude", "settings.json")
	codex := filepath.Join(home, ".codex", "hooks.json")
	type inst struct{ file, event, cmd string }
	installs := []inst{
		{claude, "SessionStart", e.Exe + " hook session-start claude"},
		{claude, "SessionEnd", e.Exe + " hook session-end claude"},
		{codex, "SessionStart", e.Exe + " hook session-start codex"},
	}
	if codexEnd {
		installs = append(installs, inst{codex, "SessionEnd", e.Exe + " hook session-end codex"})
	}
	for _, i := range installs {
		if _, err := hook.Install(i.file, i.event, i.cmd); err != nil {
			return fail(e, 1, "%s: %v", i.file, err)
		}
	}
	snippet := filepath.Join(e.Home, "sous.zsh")
	if err := os.MkdirAll(e.Home, 0o755); err != nil {
		return fail(e, 1, "%v", err)
	}
	if err := os.WriteFile(snippet, []byte(shellSnippet), 0o644); err != nil {
		return fail(e, 1, "writing %s: %v", snippet, err)
	}
	swiftbar := filepath.Join(e.Home, "sous.5m.sh")
	if err := os.WriteFile(swiftbar, []byte(swiftbarPlugin), 0o755); err != nil {
		return fail(e, 1, "writing %s: %v", swiftbar, err)
	}
	// The /sous skill, for both agents. The binary is the source of truth.
	var skillPaths []string
	for _, dir := range []string{filepath.Join(home, ".claude", "skills", "sous"), filepath.Join(home, ".codex", "skills", "sous")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail(e, 1, "%v", err)
		}
		p := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(p, []byte(skillMD), 0o644); err != nil {
			return fail(e, 1, "writing %s: %v", p, err)
		}
		skillPaths = append(skillPaths, p)
	}
	codexNote := ""
	if codexEnd {
		codexNote = ", SessionEnd"
	}
	fmt.Fprintf(e.Stdout, "Skill installed: %s\n", strings.Join(skillPaths, ", "))
	fmt.Fprintf(e.Stdout, `Hooks installed: Claude Code (SessionStart, SessionEnd), Codex (SessionStart%s).
  Codex SessionEnd: re-run with --codex-session-end once your Codex lists it in hook docs.

Add to ~/.zshrc:
  source "%s"

Make sure %s is on PATH.

Menu bar (optional, needs SwiftBar):
  ln -s "%s" "<SwiftBar plugin folder>/sous.5m.sh"
`, codexNote, snippet, filepath.Dir(e.Exe), swiftbar)
	return 0
}
