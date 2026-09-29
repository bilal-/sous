package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFindsWhatSetupLeftAndWhatIsMissing(t *testing.T) {
	home := t.TempDir()
	sousHome := filepath.Join(home, ".sous")
	exe := "/usr/local/bin/sous"
	// Nothing installed yet: every part is reported, each with a fix.
	for _, c := range Check(home, sousHome, exe, "zsh", "linux", "") {
		if c.OK || c.Fix == "" {
			t.Errorf("fresh home: %+v", c)
		}
	}
	Hooks(home, exe, false)
	Skills(home, []byte(Skill))
	Files(sousHome, exe)
	Shell(home, sousHome, "zsh", "linux", "")
	for _, c := range Check(home, sousHome, exe, "zsh", "linux", "") {
		if !c.OK {
			t.Errorf("after setup: %+v", c)
		}
	}
	// A hook for another sous, and an old skill, are caught.
	Hooks(home, "/old/place/sous", false)
	os.WriteFile(filepath.Join(home, ".codex", "skills", "sous", "SKILL.md"), []byte("---\nname: sous\n---\nold\n"), 0o644)
	var problems []string
	for _, c := range Check(home, sousHome, exe, "zsh", "linux", "") {
		if !c.OK {
			problems = append(problems, c.Name+": "+c.Detail)
		}
	}
	got := strings.Join(problems, "\n")
	if !strings.Contains(got, "/old/place/sous") || !strings.Contains(got, "Codex") || !strings.Contains(got, "out of date") {
		t.Fatalf("%s", got)
	}
}

// The shell line counts only when it is the one setup writes now, and for
// zsh when the file it sources is there.
func TestCheckShellWantsTheCurrentLineAndItsFile(t *testing.T) {
	home := t.TempDir()
	sousHome := filepath.Join(home, ".sous")
	shell := func() Result { return checkShell(home, sousHome, "zsh", "linux", "") }
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("source \"$HOME/old/sous.zsh\"\n"), 0o644)
	if r := shell(); r.OK || r.Fix != "sous setup" {
		t.Fatalf("an older line: %+v", r)
	}
	Shell(home, sousHome, "zsh", "linux", "")
	if r := shell(); r.OK || !strings.Contains(r.Detail, "sous.zsh") {
		t.Fatalf("no sous.zsh: %+v", r)
	}
	Files(sousHome, "/usr/local/bin/sous")
	if r := shell(); !r.OK {
		t.Fatalf("set up: %+v", r)
	}
}
