package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

func TestLikelyRoots(t *testing.T) {
	home := t.TempDir()
	testutil.Repo(t, filepath.Join(home, "code", "acme", "api"), false, "")
	testutil.Repo(t, filepath.Join(home, "src", "web"), false, "")
	os.MkdirAll(filepath.Join(home, "projects", "empty"), 0o755)
	got := LikelyRoots(home)
	if len(got) != 2 || got[0] != filepath.Join(home, "code") || got[1] != filepath.Join(home, "src") {
		t.Fatalf("%v", got)
	}
}

// On a case-insensitive disk ~/projects and ~/Projects are one
// folder; it is a root once. Symlinked repos count, as in Discover.
func TestLikelyRootsDedupesAndFollowsLinks(t *testing.T) {
	home := t.TempDir()
	testutil.Repo(t, filepath.Join(home, "projects", "api"), false, "")
	if _, err := os.Stat(filepath.Join(home, "Projects")); err == nil { // case-insensitive disk
		if got := LikelyRoots(home); len(got) != 1 {
			t.Fatalf("%v", got)
		}
	}
	h2 := t.TempDir()
	elsewhere := testutil.Repo(t, filepath.Join(t.TempDir(), "web"), false, "")
	os.MkdirAll(filepath.Join(h2, "code"), 0o755)
	os.Symlink(elsewhere, filepath.Join(h2, "code", "web"))
	if got := LikelyRoots(h2); len(got) != 1 {
		t.Fatalf("a symlinked repo makes a root, as Discover sees it: %v", got)
	}
}
