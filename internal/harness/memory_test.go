package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// Claude Code names a project's memory folder after its path with every
// character that is not a letter or digit as a dash; sous finds the real
// folder again by walking the disk, and leaves out folders that are gone.
func TestClaudeMemoryFoldersMapBackToProjects(t *testing.T) {
	home, _ := filepath.EvalSymlinks(t.TempDir())
	projects := []string{
		filepath.Join(home, "code", "acme", "api"),
		filepath.Join(home, "code", "acme-site.example_org"),
		filepath.Join(home, ".hidden", "tool"),
	}
	for _, p := range append(projects, filepath.Join(home, "code", "gone")) {
		os.MkdirAll(claudeMemoryDir(home, p), 0o755)
	}
	for _, p := range projects {
		os.MkdirAll(p, 0o755)
	}
	os.MkdirAll(filepath.Join(home, "code", "acme-site"), 0o755) // a near miss must not win
	got := claude.Memory.Projects(home)
	if len(got) != len(projects) {
		t.Fatalf("got %v", got)
	}
	for _, p := range projects {
		if got[p] != claudeMemoryDir(home, p) {
			t.Fatalf("%s: got %q", p, got[p])
		}
	}
}

func TestClaudeMemoryWithoutAFolderIsNone(t *testing.T) {
	if got := claude.Memory.Projects(t.TempDir()); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
