package tracker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/testutil"
)

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
	testutil.FakeBin(t, "gh", `case "$*" in "auth token --user work-account") echo tok-acme;; "auth token --user nobody") echo "no oauth token found" >&2; exit 1;; *) echo "TOKEN=$GH_TOKEN PROMPT=$GH_PROMPT_DISABLED args=$*";; esac`)
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
	testutil.FakeBin(t, "glab", `echo x >> `+calls+`; echo "git.example.org"; echo "  ✓ Logged in to git.example.org as dev"; echo "  x git.other.dev: not logged in"`)
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
	testutil.FakeBin(t, "glab", `echo "could not reach keyring" >&2; exit 1`)
	if hs, err := GitLabHosts(&config.Config{}); err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Fatalf("%v %v", hs, err)
	}
	testutil.FakeBin(t, "glab", `echo "git.example.org"; echo "  ✓ Logged in to git.example.org as dev"`)
	if hs, err := GitLabHosts(&config.Config{}); err != nil || len(hs) != 1 {
		t.Fatalf("recovery: %v %v", hs, err)
	}
	ResetCache()
	testutil.FakeBin(t, "glab", `echo "No hosts are configured on this machine." >&2; exit 1`)
	if hs, err := GitLabHosts(&config.Config{}); err != nil || len(hs) != 0 {
		t.Fatalf("no hosts logged in is an answer, not a failure: %v %v", hs, err)
	}
	ResetCache()
	t.Setenv("PATH", t.TempDir())
	if hs, err := GitLabHosts(&config.Config{GitLabHosts: []string{"gitlab.example.com"}}); err != nil || len(hs) != 1 {
		t.Fatalf("no glab installed is config hosts only, not an error: %v %v", hs, err)
	}
}

// A damaged install-id is an error, never silently replaced: every marker
// sous left in a tracker carries it, and a new one would orphan them all.
func TestInstallIDRefusesToReplaceADamagedFile(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "install-id"), []byte("oops\n"), 0o644)
	if _, err := InstallID(home); err == nil {
		t.Fatal("damaged install-id must be an error")
	}
	if b, _ := os.ReadFile(filepath.Join(home, "install-id")); string(b) != "oops\n" {
		t.Fatalf("must not overwrite: %q", b)
	}
	fresh := t.TempDir()
	id, err := InstallID(fresh)
	if err != nil || len(id) != 8 {
		t.Fatal(id, err)
	}
	if again, _ := InstallID(fresh); again != id {
		t.Fatal("stable")
	}
}

// Reading tokens is one at a time across processes, so a gh upgrade asks
// for keychain access once rather than once per parallel check.
func TestTokenReadsNeverOverlap(t *testing.T) {
	Init(t.TempDir(), t.TempDir(), nil)
	t.Cleanup(func() { Init("", "", nil) })
	lock := filepath.Join(t.TempDir(), "busy")
	testutil.FakeBin(t, "gh", `mkdir "`+lock+`" 2>/dev/null || { echo overlap >&2; exit 1; }; sleep 0.2; rmdir "`+lock+`"; echo tok`)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := readToken(fmt.Sprint("acct", i)); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// An ~/.ssh/config alias reads as its real host, from the file alone:
// nothing is run (ssh -G would run Match exec lines) and nothing leaves
// the machine.
func TestParseRemoteResolvesSSHAliases(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh", "conf.d"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(`
Include conf.d/*
Match exec "touch `+filepath.Join(home, "ran")+`"
  User nobody
Host gh-work gh-* !gh-private
  HostName github.com
  User git
Host github.com-work
  HostName "github.com"
Host LAB
    hostname GIT.EXAMPLE.ORG
Host github.com
  HostName ssh.github.com
Host *
  User git
`), 0o600)
	os.WriteFile(filepath.Join(home, ".ssh", "conf.d", "work"), []byte("Host work-lab\n  HostName git.example.org\n"), 0o600)
	Init(home, t.TempDir(), nil)
	t.Cleanup(func() { Init("", "", nil) })
	for in, want := range map[string]string{
		"gh-work/acme/api":         "github.com",
		"gh-home/acme/api":         "github.com",
		"gh-private/acme/api":      "",
		"github.com-work/acme/api": "github.com",
		"lab/team/web":             "git.example.org",
		"work-lab/team/web":        "git.example.org",
		"github.com/a/b":           "github.com", // never remapped (ssh.github.com is a port trick)
		"unknown/team/web":         "",
	} {
		if host, _ := ParseRemote(in); host != want {
			t.Errorf("%q → %q, want %q", in, host, want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "ran")); err == nil {
		t.Fatal("ssh config must be read, never executed")
	}
}

// Review: tabs separate ssh config words too; an Include inside a Host
// block belongs to that block; a real tracker host (gitlab.com moved to
// port 443 as altssh.gitlab.com) and wildcard patterns never rename a host
// that already has a dot.
func TestSSHConfigTabsIncludesAndRealHosts(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host\twork\n\tHostName\tgithub.com\nHost lab\n  Include lab.conf\nHost gitlab.com\n  HostName altssh.gitlab.com\n  Port 443\nHost *.example.org\n  HostName proxy.example.net\n"), 0o600)
	os.WriteFile(filepath.Join(home, ".ssh", "lab.conf"), []byte("HostName git.example.org\n"), 0o600)
	Init(home, t.TempDir(), nil)
	t.Cleanup(func() { Init("", "", nil) })
	for in, want := range map[string]string{
		"work/a/b":            "github.com",
		"lab/a/b":             "git.example.org",
		"gitlab.com/a/b":      "gitlab.com",
		"git.example.org/a/b": "git.example.org",
	} {
		if host, _ := ParseRemote(in); host != want {
			t.Errorf("%q → %q, want %q", in, host, want)
		}
	}
}
