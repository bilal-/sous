package tracker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/testutil"
)

func fakeBin(t *testing.T, name, body string) string { return testutil.FakeBin(t, name, body) }

func TestInstallIDIsStable(t *testing.T) {
	home := t.TempDir()
	a, err := InstallID(home)
	if err != nil || len(a) != 8 {
		t.Fatal(a, err)
	}
	b, _ := InstallID(home)
	if a != b {
		t.Fatal("must persist")
	}
	c, _ := InstallID(t.TempDir())
	if c == a {
		t.Fatal("must differ per install")
	}
}

func TestGitHubAccountFromOrgGlob(t *testing.T) {
	cfg := &config.Config{Projects: map[string]map[string]string{
		"acme/*":       {"github_account": "work-account"},
		"acme/special": {"github_account": "other"},
	}}
	if got := GitHubAccount(cfg, "acme/sous"); got != "work-account" {
		t.Fatal(got)
	}
	if got := GitHubAccount(cfg, "acme/special"); got != "other" {
		t.Fatal("exact beats glob:", got)
	}
	if got := GitHubAccount(cfg, "personal/x"); got != "" {
		t.Fatal(got)
	}
}

func TestGHUsesAccountToken(t *testing.T) {
	fakeBin(t, "gh", `case "$*" in "auth token --user work-account") echo tok-acme;; "auth token --user nobody") echo "no oauth token found" >&2; exit 1;; *) echo "TOKEN=$GH_TOKEN PROMPT=$GH_PROMPT_DISABLED args=$*";; esac`)
	cmd, err := GH("work-account", "api", "user")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), "TOKEN=tok-acme") || !strings.Contains(string(out), "PROMPT=1") {
		t.Fatalf("%q %v", out, err)
	}
	t.Setenv("GH_TOKEN", "")
	cmd, _ = GH("", "api", "user")
	out, _ = cmd.Output()
	if !strings.Contains(string(out), "TOKEN= ") {
		t.Fatalf("no account → gh's own auth: %q", out)
	}
	if _, err := GH("nobody", "api", "user"); err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Fatalf("configured account without a token is an error, never a silent fallback: %v", err)
	}
}

func TestGitLabHostsUnion(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)
	calls := filepath.Join(t.TempDir(), "calls")
	fakeBin(t, "glab", `echo x >> `+calls+`; echo "git.example.org"; echo "  ✓ Logged in to git.example.org as dev"; echo "  x git.other.dev: not logged in"`)
	cfg := &config.Config{GitLabHosts: []string{"gitlab.example.com"}}
	for range 2 {
		hs, err := GitLabHosts(cfg)
		if err != nil || strings.Join(hs, ",") != "git.example.org,gitlab.example.com" {
			t.Fatalf("%v %v", hs, err)
		}
	}
	if b, _ := os.ReadFile(calls); string(b) != "x\n" {
		t.Fatalf("glab auth status must run once per process, ran %q", b)
	}
	c := GLab("git.example.org", "api", "user")
	if !strings.Contains(strings.Join(c.Args, " "), "--hostname git.example.org") {
		t.Fatalf("%v", c.Args)
	}
	for _, e := range c.Env {
		if strings.HasPrefix(e, "GLAB_NO_PROMPT=1") {
			return
		}
	}
	t.Fatal("glab must be non-interactive")
}

func TestParseRemote(t *testing.T) {
	h, p := ParseRemote("git.example.org/frontend/app-next")
	if h != "git.example.org" || p != "frontend/app-next" {
		t.Fatal(h, p)
	}
	if h, p := ParseRemote("nohost"); h != "" || p != "" {
		t.Fatal(h, p)
	}
}

// Review: glab failing without naming a host is not "no GitLab" — that
// would drop every GitLab finding as if resolved. It is an error, and not
// cached, so the next call asks again.
func TestGitLabHostsFailureIsAnErrorNotAnAnswer(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)
	fakeBin(t, "glab", `echo "could not reach keyring" >&2; exit 1`)
	if hs, err := GitLabHosts(&config.Config{}); err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Fatalf("%v %v", hs, err)
	}
	fakeBin(t, "glab", `echo "git.example.org"; echo "  ✓ Logged in to git.example.org as dev"`)
	if hs, err := GitLabHosts(&config.Config{}); err != nil || len(hs) != 1 {
		t.Fatalf("recovery: %v %v", hs, err)
	}
	ResetCache()
	fakeBin(t, "glab", `echo "No hosts are configured on this machine." >&2; exit 1`)
	if hs, err := GitLabHosts(&config.Config{}); err != nil || len(hs) != 0 {
		t.Fatalf("no hosts logged in is an answer, not a failure: %v %v", hs, err)
	}
	ResetCache()
	t.Setenv("PATH", t.TempDir())
	if hs, err := GitLabHosts(&config.Config{GitLabHosts: []string{"gitlab.example.com"}}); err != nil || len(hs) != 1 {
		t.Fatalf("no glab installed is config hosts only, not an error: %v %v", hs, err)
	}
}
