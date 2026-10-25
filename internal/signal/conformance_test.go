package signal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/signal/signaltest"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/tracker"
	"github.com/bilal-/sous/internal/tracker/trackertest"
)

// TestMain lets this test binary act as `sous signal <name> scan`, so the
// built ins are driven through the same door as a plugin program.
func TestMain(m *testing.M) {
	if len(os.Args) > 3 && os.Args[1] == "signal" {
		cfg, _ := config.Load(os.Getenv("SOUS_HOME"), os.Getenv("HOME"))
		os.Exit(signal.Registry.Serve(cfg, os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// Every built in signal, driven through `sous signal <name> scan`, passes
// the v0 signal contract. A built in with no case here fails.
func TestBuiltinsConform(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, c := range []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{
			name: "git",
			setup: func(t *testing.T) string {
				r := testutil.Repo(t, filepath.Join(t.TempDir(), "acme", "chime"), true, "")
				os.WriteFile(filepath.Join(r, "dirty.txt"), []byte("x"), 0o644)
				return r
			},
		},
		{
			name: tracker.GitHub,
			setup: func(t *testing.T) string {
				trackertest.Fake(t, "gh", `case "$*" in
  "auth status") exit 0;;
  *--review-requested=@me*) printf '[{"repository":{"nameWithOwner":"acme/chime"},"number":14,"title":"json api","updatedAt":"2026-09-25T10:00:00Z"}]';;
  *--slurp*notifications*) printf '[[]]';;
  "api --hostname github.com graphql"*) printf '{"data":{"search":{"nodes":[]}}}';;
  *) printf '[]';;
esac`)
				return testutil.Repo(t, filepath.Join(t.TempDir(), "acme", "chime"), true, "git@github.com:acme/chime.git")
			},
		},
		{
			name: tracker.GitLab,
			setup: func(t *testing.T) string {
				trackertest.Fake(t, "glab", `case "$*" in
  *"auth status"*) echo "git.example.org"; exit 0;;
  *"api user"*) printf '{"id":37,"username":"dev"}';;
  *"reviewer_username=dev"*) printf '[{"references":{"full":"acme/chime!9"},"iid":9,"title":"review me","updated_at":"2026-09-25T10:00:00Z"}]';;
  *) printf '[]';;
esac`)
				return testutil.Repo(t, filepath.Join(t.TempDir(), "acme", "chime"), true, "https://git.example.org/acme/chime.git")
			},
		},
	} {
		covered[c.name] = true
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SOUS_HOME", home)
			project := c.setup(t)
			p := signal.Registry.Discover(exe, []string{c.name}, nil)[0]
			signaltest.Run(t, append(p.Argv, "scan"), []string{project})
		})
	}
	for _, name := range signal.Registry.Names() {
		if !covered[name] {
			t.Errorf("signal %s has no conformance case here", name)
		}
	}
}
