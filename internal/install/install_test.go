package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellLines(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		shell, goos string
		files       []string
	}{
		{"zsh", "darwin", []string{".zshrc"}},
		{"bash", "linux", []string{".bashrc"}},
		{"bash", "darwin", []string{".bashrc", ".bash_profile"}}, // Terminal opens login shells
		{"fish", "linux", []string{".config/fish/conf.d/sous.fish"}},
	}
	for _, c := range cases {
		h := filepath.Join(home, c.shell+"-"+c.goos)
		msg, err := Shell(h, filepath.Join(h, ".sous"), c.shell, c.goos, "")
		if err != nil || !strings.Contains(msg, "added") {
			t.Fatalf("%s/%s: %q %v", c.shell, c.goos, msg, err)
		}
		for _, f := range c.files {
			b, err := os.ReadFile(filepath.Join(h, f))
			if err != nil || strings.Count(string(b), "sous") < 1 {
				t.Fatalf("%s/%s %s: %v %q", c.shell, c.goos, f, err, b)
			}
		}
		if msg, _ := Shell(h, filepath.Join(h, ".sous"), c.shell, c.goos, ""); !strings.Contains(msg, "already") {
			t.Fatalf("second run must add nothing: %q", msg)
		}
	}
}

// Review: bash reads .bashrc for non-interactive ssh commands; the line
// must print nothing there, or scp and rsync break.
func TestBashLineIsSilentWhenNotInteractive(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	home := t.TempDir()
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "sous"), []byte("#!/bin/sh\necho BOARD\n"), 0o755)
	Shell(home, filepath.Join(home, ".sous"), "bash", "linux", "")
	out, err := exec.Command("bash", "-c", "PATH="+bin+":$PATH; source "+filepath.Join(home, ".bashrc")).CombinedOutput()
	if err != nil || strings.Contains(string(out), "BOARD") {
		t.Fatalf("%v %q", err, out)
	}
}

// A commented out line is not an installed line; ZDOTDIR is where zsh
// looks.
func TestShellRespectsCommentsAndZdotdir(t *testing.T) {
	home := t.TempDir()
	zdot := filepath.Join(home, "zsh")
	os.MkdirAll(zdot, 0o755)
	os.WriteFile(filepath.Join(zdot, ".zshrc"), []byte("# source \"$HOME/.sous/sous.zsh\"\n"), 0o644)
	if msg, err := Shell(home, filepath.Join(home, ".sous"), "zsh", "darwin", zdot); err != nil || !strings.Contains(msg, "added") {
		t.Fatalf("%q %v", msg, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); err == nil {
		t.Fatal("with ZDOTDIR set, ~/.zshrc is not the file")
	}
}

func TestSkillsGoWhereAgentsLook(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".gemini", "antigravity"), 0o755)
	done, err := Skills(home, []byte("---\nname: sous\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".claude/skills/sous", ".codex/skills/sous", ".agents/skills/sous", ".gemini/antigravity/skills/sous"} {
		if _, err := os.Stat(filepath.Join(home, d, "SKILL.md")); err != nil {
			t.Error(d, err)
		}
	}
	if len(done) != 2 { // Claude Code and Codex are reported with their hooks
		t.Fatalf("%v", done)
	}
}
