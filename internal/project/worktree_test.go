package project

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

// A linked worktree belongs to its main repo; a repo, a submodule and a
// worktree of a bare repo do not belong to anything else.
func TestMainOf(t *testing.T) {
	ws, _ := filepath.EvalSymlinks(t.TempDir())
	app := filepath.Join(ws, "acme/app")
	mkrepo(t, app)
	wt := filepath.Join(ws, "acme/app-feature")
	testutil.Git(t, app, "worktree", "add", "-q", "-b", "feat/x", wt)

	if main, ok := MainOf(wt); !ok || main != app {
		t.Fatalf("worktree: %q %v", main, ok)
	}
	if _, ok := MainOf(app); ok {
		t.Fatal("the main repo is not a worktree")
	}

	lib := filepath.Join(ws, "acme/lib")
	mkrepo(t, lib)
	testutil.Git(t, app, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "vendor/lib")
	if _, ok := MainOf(filepath.Join(app, "vendor/lib")); ok {
		t.Fatal("a submodule is its own repo")
	}

	bare := testutil.Bare(t, filepath.Join(ws, "acme/bare.git"))
	testutil.Git(t, app, "push", "-q", bare, "HEAD:main")
	bwt := filepath.Join(ws, "acme/from-bare")
	testutil.Git(t, bare, "worktree", "add", "-q", bwt, "main")
	if _, ok := MainOf(bwt); ok {
		t.Fatal("a bare repo's worktree has no main folder to belong to")
	}
}

// Discovery lists a repo once, however many of its worktrees sit beside
// it, and a worktree whose repo lives elsewhere brings that repo in.
func TestDiscoverFoldsWorktrees(t *testing.T) {
	ws, _ := filepath.EvalSymlinks(t.TempDir())
	app := filepath.Join(ws, "acme/app")
	mkrepo(t, app)
	testutil.Git(t, app, "worktree", "add", "-q", "-b", "feat/a", filepath.Join(ws, "acme/app-a"))
	testutil.Git(t, app, "worktree", "add", "-q", "-b", "feat/b", filepath.Join(ws, "acme/app-b"))
	away := filepath.Join(ws, "elsewhere/api")
	mkrepo(t, away)
	testutil.Git(t, away, "worktree", "add", "-q", "-b", "feat/c", filepath.Join(ws, "acme/api-c"))

	ps, _ := Discover([]string{filepath.Join(ws, "acme")}, nil, io.Discard)
	var got []string
	for _, p := range ps {
		got = append(got, p.Path)
	}
	if len(got) != 2 || got[0] != app || got[1] != away {
		t.Fatalf("%q", got)
	}
}

// Inside a worktree, the project is the main repo's.
func TestForPathInAWorktree(t *testing.T) {
	ws, _ := filepath.EvalSymlinks(t.TempDir())
	app := filepath.Join(ws, "acme/app")
	mkrepo(t, app)
	wt := filepath.Join(ws, "acme/app-feature")
	testutil.Git(t, app, "worktree", "add", "-q", "-b", "feat/x", wt)
	os.MkdirAll(filepath.Join(wt, "src"), 0o755)
	if root, ok := ForPath(filepath.Join(wt, "src"), ""); !ok || root != app {
		t.Fatalf("%q %v", root, ok)
	}
}

// Worktrees lists the linked ones that still exist, with their branches.
func TestWorktrees(t *testing.T) {
	ws, _ := filepath.EvalSymlinks(t.TempDir())
	app := filepath.Join(ws, "app")
	mkrepo(t, app)
	a, gone := filepath.Join(ws, "a"), filepath.Join(ws, "gone")
	testutil.Git(t, app, "worktree", "add", "-q", "-b", "feat/a", a)
	testutil.Git(t, app, "worktree", "add", "-q", "--detach", gone)
	os.RemoveAll(gone)
	wts, err := Worktrees(app)
	if err != nil || len(wts) != 1 || wts[0] != (Worktree{Path: a, Branch: "feat/a"}) {
		t.Fatalf("%+v %v", wts, err)
	}
}
