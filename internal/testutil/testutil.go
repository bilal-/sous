// Package testutil holds the fixtures every package's tests reach for:
// throwaway git repos with a fixed identity, and fake binaries on PATH.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Git runs git in dir with a fixed author and fails the test on error.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Sous Tests", "GIT_AUTHOR_EMAIL=sous-tests@example.invalid",
		"GIT_COMMITTER_NAME=Sous Tests", "GIT_COMMITTER_EMAIL=sous-tests@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// Repo creates dir as a git repo on main with one empty commit unless
// commit is false; remote, if non-empty, becomes origin.
func Repo(t *testing.T, dir string, commit bool, remote string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q", "-b", "main")
	if commit {
		Git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	}
	if remote != "" {
		Git(t, dir, "remote", "add", "origin", remote)
	}
	return dir
}

// FakeBin puts an executable sh script named name on the front of PATH for
// the rest of the test and returns its path.
func FakeBin(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p
}
