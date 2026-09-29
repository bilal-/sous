package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

func TestCheckFindsWhatSetupLeftAndWhatIsMissing(t *testing.T) {
	testutil.FakeBin(t, "sous", "")
	home := t.TempDir()
	sousHome := filepath.Join(home, ".sous")
	exe := "/usr/local/bin/sous"
	// Nothing installed yet: every part is reported, each with a fix,
	// except the one hook setup adds only when asked.
	for _, c := range Check(home, sousHome, exe, "zsh", "linux", "") {
		if (c.OK || c.Fix == "") && c.Detail != "not installed (optional)" {
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
	// The Codex end hook is asked for with a flag: missing is fine, but
	// one that runs another sous is not.
	for _, c := range Check(home, sousHome, exe, "zsh", "linux", "") {
		if strings.Contains(c.Name, "Codex hook (session end)") && (!c.OK || !strings.Contains(c.Detail, "optional")) {
			t.Errorf("codex end: %+v", c)
		}
	}
	// A hook for another sous, and an old skill, are caught.
	Hooks(home, "/old/place/sous", true)
	os.WriteFile(filepath.Join(home, ".codex", "skills", "sous", "SKILL.md"), []byte("---\nname: sous\n---\nold\n"), 0o644)
	var problems []string
	for _, c := range Check(home, sousHome, exe, "zsh", "linux", "") {
		if !c.OK {
			problems = append(problems, c.Name+": "+c.Detail)
		}
	}
	got := strings.Join(problems, "\n")
	if !strings.Contains(got, "/old/place/sous") || !strings.Contains(got, "Codex hook (session end)") || !strings.Contains(got, "out of date") {
		t.Fatalf("%s", got)
	}
}

// The shell line counts only when it is the one setup writes now, and for
// zsh when the file it sources is there.
func TestCheckShellWantsTheCurrentLineAndItsFile(t *testing.T) {
	home := t.TempDir()
	sousHome := filepath.Join(home, ".sous")
	shell := func() Result { return checkShell(home, sousHome, "/usr/local/bin/sous", "zsh", "linux", "") }
	testutil.OnlyGit(t) // no sous on PATH
	if r := shell(); r.OK || !r.Optional {
		t.Fatalf("no line is a choice setup offers: %+v", r)
	}
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("source \"$HOME/old/sous.zsh\"\n"), 0o644)
	if r := shell(); r.OK || r.Fix != "sous setup" {
		t.Fatalf("an older line: %+v", r)
	}
	Shell(home, sousHome, "zsh", "linux", "")
	if r := shell(); r.OK || !strings.Contains(r.Detail, "sous.zsh") {
		t.Fatalf("no sous.zsh: %+v", r)
	}
	Files(sousHome, "/usr/local/bin/sous")
	if r := shell(); r.OK || !strings.Contains(r.Detail, "PATH") || r.Fix != "add /usr/local/bin to PATH" {
		t.Fatalf("sous not on PATH: %+v", r)
	}
	testutil.FakeBin(t, "sous", "")
	if r := shell(); !r.OK {
		t.Fatalf("set up: %+v", r)
	}
}
