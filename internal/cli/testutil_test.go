package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/thread"
)

type fx struct {
	t        *testing.T
	Home     string
	SousHome string
	WS       string
}

// fixture: fresh HOME, SOUS_HOME, empty workspace root, and a PATH that starts
// with $HOME/bin so tests can shadow gh. Git identity is fixed.
func fixture(t *testing.T) *fx {
	t.Helper()
	home, _ := filepath.EvalSymlinks(t.TempDir()) // physical path: git reports /private/var on macOS
	f := &fx{t: t, Home: home, SousHome: filepath.Join(home, ".sous"), WS: filepath.Join(home, "workspace")}
	os.MkdirAll(f.SousHome, 0o755)
	os.MkdirAll(f.WS, 0o755)
	os.MkdirAll(filepath.Join(home, "bin"), 0o755)
	t.Setenv("HOME", home)
	t.Setenv("SOUS_HOME", f.SousHome)
	t.Setenv("SHELL", "/bin/zsh") // setup edits the shell file; never depend on the real one
	t.Setenv("PATH", filepath.Join(home, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_AUTHOR_NAME", "Sous Tests")
	t.Setenv("GIT_AUTHOR_EMAIL", "sous-tests@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Sous Tests")
	t.Setenv("GIT_COMMITTER_EMAIL", "sous-tests@example.invalid")
	// Built-in plugins re-exec this binary as `sous`; TestMain honours this.
	t.Setenv("SOUS_TEST_AS_BINARY", "1")
	// No test may reach GitHub: gh is stubbed as "not logged in" unless a test
	// installs its own fake. This is also why fixtures always show
	// "github failed" in headlines.
	os.WriteFile(filepath.Join(home, "bin", "gh"), []byte("#!/bin/sh\necho 'You are not logged into any GitHub hosts' >&2; exit 1\n"), 0o755)
	os.WriteFile(filepath.Join(home, "bin", "glab"), []byte("#!/bin/sh\necho 'No hosts are configured' >&2; exit 1\n"), 0o755)
	// Nor may a test start a real agent: every harness's program is a fake
	// that does nothing, unless a test installs its own.
	for _, h := range harness.All {
		os.WriteFile(filepath.Join(home, "bin", h.Bin), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	}
	f.writeConfig("roots = [\"" + f.WS + "\"]\n")
	return f
}

func (f *fx) writeConfig(s string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.SousHome, "config.toml"), []byte(s), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fx) git(dir string, args ...string) string { return testutil.Git(f.t, dir, args...) }

// mkrepo creates WS/<rel> as a git repo on main with one empty commit unless commit is false.
func (f *fx) mkrepo(rel string, commit bool) string {
	return testutil.Repo(f.t, filepath.Join(f.WS, rel), commit, "")
}

func (f *fx) run(args ...string) (string, string, int) { return f.runIn("", args...) }

func (f *fx) runIn(dir string, args ...string) (string, string, int) {
	f.t.Helper()
	if dir == "" {
		dir = f.Home
	}
	var out, errb bytes.Buffer
	code := run(args, dir, bytes.NewReader(nil), &out, &errb)
	return out.String(), errb.String(), code
}

func (f *fx) runStdin(stdin string, args ...string) (string, string, int) {
	f.t.Helper()
	var out, errb bytes.Buffer
	code := run(args, f.Home, bytes.NewBufferString(stdin), &out, &errb)
	return out.String(), errb.String(), code
}

// TestMain lets the test binary behave as `sous` when re-executed by the
// plugin runner: `SOUS_TEST_AS_BINARY=1 <test-binary> signal git scan`.
func TestMain(m *testing.M) {
	if os.Getenv("SOUS_TEST_AS_BINARY") == "1" {
		os.Exit(Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	// sous go and sous launcher replace the process with the agent. Inside
	// the test process that would end the run early and still report ok,
	// so here they fail instead; tests that need the exec run sous as a
	// separate program (sousCmd).
	execProgram = func(path string, _, _ []string) error {
		return fmt.Errorf("test: would exec %s in the test process", path)
	}
	os.Exit(m.Run())
}

// brokenGH: gh is logged in but GitHub errors, a real failure (the default
// fixture gh is logged out, which means GitHub is simply not set up).
func (f *fx) brokenGH(githubRepos ...string) {
	for _, r := range githubRepos {
		f.git(r, "remote", "add", "origin", "git@github.com:acme/"+filepath.Base(r)+".git")
	}
	os.WriteFile(filepath.Join(f.Home, "bin", "gh"), []byte("#!/bin/sh\n[ \"$1 $2\" = \"auth status\" ] && exit 0\necho 'HTTP 502: bad gateway' >&2; exit 1\n"), 0o755)
}

// fileAs records note id as filed at ref, through the thread API, as if a
// backend had filed it there.
func (f *fx) fileAs(id int, ref string) {
	f.t.Helper()
	st := &store.Store{Home: f.SousHome}
	if _, err := thread.FileAtomically(st, id, true, func(thread.Thread) (string, error) { return ref, nil }); err != nil {
		f.t.Fatal(err)
	}
}

// plugin writes an executable plugin under $HOME/plugins, lists it in
// config.toml's plugins, and returns its path.
func (f *fx) plugin(name, body string) string {
	f.t.Helper()
	dir := filepath.Join(f.Home, "plugins")
	os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		f.t.Fatal(err)
	}
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + p + "\"]\n")
	return p
}
