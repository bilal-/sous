package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinesRound4(t *testing.T) {
	for line, want := range map[string]bool{
		`source "$HOME/.config/sous/sous.zsh"`: true, // a custom SOUS_HOME, earlier
		`source /opt/sous/sous.zsh`:            true,
		`export X="$HOME/.sous/sous.zsh"`:      false,
	} {
		if isSousLine(line) != want {
			t.Errorf("isSousLine(%q) = %v", line, !want)
		}
	}
	for text, sources := range map[string]bool{
		"[ -f ~/.bashrc ] && . ~/.bashrc\n":                true,
		"if [ -f ~/.bashrc ]; then source ~/.bashrc; fi\n": true,
		"source ~/.bashrc.local\n":                         false,
		"echo source ~/.bashrc\n":                          false,
	} {
		home := t.TempDir()
		os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(text), 0o644)
		if got := bashLoginFile(home) == ""; got != sources {
			t.Errorf("%q sources .bashrc: got %v", strings.TrimSpace(text), got)
		}
	}
}
