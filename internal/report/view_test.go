package report

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

// Lavish comes first when the person has it, and the browser is always
// there after it; without Lavish, only the browser.
func TestViewers(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("PATH", bin)
	testutil.Script(t, bin, "open", "")
	testutil.Script(t, bin, "npx", "")
	page := "/x/report.html"
	browser := []string{"open", page}

	if got := Viewers(home, page); !reflect.DeepEqual(got, [][]string{browser}) {
		t.Fatalf("no Lavish: %q", got)
	}

	// The skill alone: npx runs Lavish, the way the skill does.
	skill := filepath.Join(home, ".agents", "skills", "lavish")
	os.MkdirAll(skill, 0o755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), nil, 0o644)
	want := [][]string{{"npx", "-y", "lavish-axi", page, "--reopen"}, browser}
	if got := Viewers(home, page); !reflect.DeepEqual(got, want) {
		t.Fatalf("skill: %q", got)
	}

	// lavish-axi itself on PATH wins over npx.
	testutil.Script(t, bin, "lavish-axi", "")
	want = [][]string{{"lavish-axi", page, "--reopen"}, browser}
	if got := Viewers(home, page); !reflect.DeepEqual(got, want) {
		t.Fatalf("on PATH: %q", got)
	}
	if ViewerName(want[0]) != "Lavish" || ViewerName(browser) != "your browser" {
		t.Fatal("names")
	}
}

// The skill without npx cannot run, so it is not offered.
func TestViewersSkillWithoutNpx(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	skill := filepath.Join(home, ".claude", "skills", "lavish")
	os.MkdirAll(skill, 0o755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), nil, 0o644)
	if got := Viewers(home, "p"); len(got) != 1 || got[0][0] != "xdg-open" {
		t.Fatalf("%q", got)
	}
}
