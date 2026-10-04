package project

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDocumentsReportsUnavailableSources(t *testing.T) {
	root := t.TempDir()
	if got, err := Documents(filepath.Join(root, "missing")); err == nil || got != nil {
		t.Fatalf("a missing folder is not an empty set of docs: %+v %v", got, err)
	}
	if err := os.Symlink("missing.md", filepath.Join(root, "STATUS.md")); err != nil {
		t.Fatal(err)
	}
	got, err := Documents(root)
	if err != nil || len(got) != 1 || got[0].Path != "STATUS.md" || got[0].Error == "" {
		t.Fatalf("broken guide must remain visible: %+v %v", got, err)
	}
}

func TestDocumentsUsesExistingNamesAndFollowsFileLinks(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"README.md", "agents.md", "Status.md", "followups.md", "handoff.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("project context"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("agents.md", filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	got, err := Documents(root)
	if err != nil || len(got) != 5 || got[1].Path != "agents.md" || got[2].Path != "CLAUDE.md" || got[3].Purpose != "status" || got[4].Path != "followups.md" {
		t.Fatalf("%+v %v", got, err)
	}
	for _, doc := range got {
		if doc.Error != "" {
			t.Fatalf("%+v", doc)
		}
	}
}

func TestDocumentsDoesNotOpenSpecialFiles(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "STATUS.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Documents(root)
	if err != nil || len(got) != 1 || got[0].Error == "" {
		t.Fatalf("special files must be reported without opening them: %+v %v", got, err)
	}
}
