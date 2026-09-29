package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
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
	t.Setenv("PATH", filepath.Join(home, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_AUTHOR_NAME", "Sous Tests")
	t.Setenv("GIT_AUTHOR_EMAIL", "sous-tests@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Sous Tests")
	t.Setenv("GIT_COMMITTER_EMAIL", "sous-tests@example.invalid")
	// Built-in plugins re-exec this binary as `sous`; TestMain honours this.
	t.Setenv("SOUS_TEST_AS_BINARY", "1")
	// Built-in plugins re-exec this binary as `sous`; TestMain honours this.
	// No test may reach GitHub: gh is stubbed as "not logged in" unless a test
	// installs its own fake. This is also why fixtures always show
	// "github failed" in headlines.
	os.WriteFile(filepath.Join(home, "bin", "gh"), []byte("#!/bin/sh\necho 'You are not logged into any GitHub hosts' >&2; exit 1\n"), 0o755)
	os.WriteFile(filepath.Join(home, "bin", "glab"), []byte("#!/bin/sh\necho 'No hosts are configured' >&2; exit 1\n"), 0o755)
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
	os.Exit(m.Run())
}
