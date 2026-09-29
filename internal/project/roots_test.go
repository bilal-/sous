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
