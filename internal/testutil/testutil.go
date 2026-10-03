// Package testutil holds the fixtures every package's tests reach for:
// throwaway git repos with a fixed identity, and fake binaries on PATH.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolated is the environment every test binary that uses testutil runs
// in, whatever the developer's: git with no global or system config (no
// signing, hooks or aliases of theirs) and a fixed identity, no ZDOTDIR
// (setup would write to their real .zshrc), and no SOUS_SOURCE. Tests that
// need one of these set it themselves.
var isolated = map[string]string{
	"GIT_CONFIG_GLOBAL":   os.DevNull,
	"GIT_CONFIG_NOSYSTEM": "1",
	"GIT_AUTHOR_NAME":     "Sous Tests",
	"GIT_AUTHOR_EMAIL":    "sous-tests@example.invalid",
	"GIT_COMMITTER_NAME":  "Sous Tests",
	"GIT_COMMITTER_EMAIL": "sous-tests@example.invalid",
	"ZDOTDIR":             "",
	"SOUS_SOURCE":         "",
}

func init() {
	for k, v := range isolated {
		if v == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}
}

// Git runs git in dir with a fixed author and fails the test on error.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
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

// Bare creates a bare repo at path, for a remote to push to.
func Bare(t *testing.T, path string) string {
	t.Helper()
	Git(t, filepath.Dir(path), "init", "-q", "--bare", path)
	return path
}

// Script writes an executable sh script dir/name with body, and returns
// its path: a fake tool or plugin.
func Script(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// FakeBin puts an executable sh script named name on the front of PATH for
// the rest of the test and returns its path.
func FakeBin(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := Script(t, dir, name, body)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p
}

// OnlyGit leaves git as the only program on PATH, in a folder of its own:
// git's real folder often holds gh or glab too (Homebrew, CI runners).
func OnlyGit(t *testing.T) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(git, filepath.Join(dir, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// Contains fails the test for each of wants that got does not contain,
// showing got once.
func Contains(t *testing.T, got string, wants ...string) {
	t.Helper()
	var missing []string
	for _, w := range wants {
		if !strings.Contains(got, w) {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Errorf("missing %q in:\n%s", missing, got)
	}
}
