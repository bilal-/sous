package backend

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/tracker"
	"github.com/bilal-/sous/internal/tracker/trackertest"
)

// fakeGH logs each call's arguments to calls, then runs body.
func fakeGH(t *testing.T, calls string, body string) {
	trackertest.Fake(t, "gh", "echo \"$*\" >> "+calls+"\n"+body)
}

// repoWithRemote creates <tmp>/<org>/<name> with the given origin.
func repoWithRemote(t *testing.T, org, name, remote string) string {
	return testutil.Repo(t, filepath.Join(t.TempDir(), org, name), true, remote)
}

func TestGitHubBackendLifecycle(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "install-id"), []byte("abcd1234\n"), 0o644)
	calls := filepath.Join(t.TempDir(), "calls")
	fakeGH(t, calls, `
case "$*" in
  "auth status"*) exit 0;;
  "auth token --user work-account") echo tok;;
  *"search issues"*"--state all"*) echo 'invalid argument "all" for "--state" flag: valid values are {open|closed}' >&2; exit 1;;
  *"issue list"*"--state all"*"--json number,body"*) printf '[{"number":40,"body":"unrelated <!-- sous:zzzzzzzz:8 -->"},{"number":41,"body":"again <!-- sous:abcd1234:8 -->"}]';;
  *"issue create"*) echo "https://github.com/acme/chime/issues/42";;
  *"issue view 42"*"--json state,url"*) printf '{"state":"OPEN","url":"https://github.com/acme/chime/issues/42"}';;
  *"issue view 43"*) echo "GraphQL: Could not resolve to an issue or pull request with the number of 43. (repository.issue)" >&2; exit 1;;
  *"issue view 45"*) echo "GraphQL: Could not resolve to a Repository with the name 'acme/chime'. (repository)" >&2; exit 1;;
  *"issue view 44"*) echo "HTTP 403: rate limit" >&2; exit 1;;
  *"issue close 42"*) exit 0;;
esac`)
	cfg := &config.Config{Projects: map[string]map[string]string{"acme/*": {"github_account": "work-account"}}}
	g := GitHub(home, cfg)
	p := repoWithRemote(t, "acme", "chime", "git@github.com:acme/chime.git")

	var warn bytes.Buffer
	if !g.Detect(p, &warn) {
		t.Fatalf("detect: %s", warn.String())
	}
	ref, err := g.File(Request{ID: 7, UID: "000000000007", Project: p, Text: "notifications need context, not \"unused terminal\"", Kind: "me"})
	if err != nil || ref != "github:acme/chime#42" {
		t.Fatal(ref, err)
	}
	b, _ := os.ReadFile(calls)
	if !strings.Contains(string(b), "issue create") || !strings.Contains(string(b), "-R acme/chime") || !strings.Contains(string(b), "sous:000000000007") || !strings.Contains(string(b), "auth token --user work-account") {
		t.Fatalf("create call shape / account:\n%s", b)
	}
	ref8, err := g.File(Request{ID: 8, UID: "000000000008", Project: p, Text: "again", Kind: "idea"})
	if err != nil || ref8 != "github:acme/chime#41" {
		t.Fatal(ref8, err)
	}
	if b, _ = os.ReadFile(calls); strings.Count(string(b), "issue create") != 1 {
		t.Fatalf("must not create twice:\n%s", b)
	}
	if st, err := g.Status(p, "github:acme/chime#42"); err != nil || st != "open" {
		t.Fatal(st, err)
	}
	if st, err := g.Status(p, "github:acme/chime#43"); err != nil || st != "unknown" {
		t.Fatalf("not found is unknown, not an error: %s %v", st, err)
	}
	if _, err := g.Status(p, "github:acme/chime#44"); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("other failures are errors: %v", err)
	}
	// Wrong account on a private repo: the repo "does not resolve". That is an
	// error naming the account, never "unknown" (which would mean ref missing).
	if _, err := g.Status(p, "github:acme/chime#45"); err == nil || !strings.Contains(err.Error(), "work-account") {
		t.Fatalf("repo not visible must be an error naming the account: %v", err)
	}
	if u, err := g.URL(p, "github:acme/chime#42"); err != nil || u != "https://github.com/acme/chime/issues/42" {
		t.Fatal(u, err)
	}
	if err := g.Close(p, "github:acme/chime#42"); err != nil {
		t.Fatal(err)
	}
	if st, _ := g.Status(p, "gitlab:x/y#1"); st != "unknown" {
		t.Fatal("foreign ref")
	}
}

func TestGitHubDetectRefusals(t *testing.T) {
	home := t.TempDir()
	cfg := &config.Config{}
	var warn bytes.Buffer
	if GitHub(home, cfg).Detect(repoWithRemote(t, "a", "b", "https://git.example.org/a/b.git"), &warn) {
		t.Fatal("gitlab remote must not detect as github")
	}
	fakeGH(t, filepath.Join(t.TempDir(), "c"), `case "$*" in "auth status"*) echo "not logged in" >&2; exit 1;; esac`)
	warn.Reset()
	if GitHub(home, cfg).Detect(repoWithRemote(t, "o", "r", "git@github.com:o/r.git"), &warn) || !strings.Contains(warn.String(), "not logged in") {
		t.Fatalf("unauth must not detect, and must say why: %q", warn.String())
	}
	testutil.OnlyGit(t)
	warn.Reset()
	if GitHub(home, cfg).Detect(repoWithRemote(t, "o", "r2", "git@github.com:o/r2.git"), &warn) || !strings.Contains(warn.String(), "gh not installed") {
		t.Fatalf("%q", warn.String())
	}
}

func TestGitHubClosedNotPlannedIsClosed(t *testing.T) {
	home := t.TempDir()
	fakeGH(t, filepath.Join(t.TempDir(), "c"), `case "$*" in *"--json state,url"*) printf '{"state":"CLOSED","stateReason":"NOT_PLANNED"}';; esac`)
	st, err := GitHub(home, &config.Config{}).Status(repoWithRemote(t, "o", "r", "git@github.com:o/r.git"), "github:o/r#5")
	if err != nil || st != "closed" {
		t.Fatal(st, err)
	}
}

// Crash recovery must fail closed: if the existing-issue lookup itself
// errors, do not create — a duplicate on a shared tracker is worse than a
// retry later.
func TestGitHubFileFailsClosedWhenLookupErrors(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "install-id"), []byte("abcd1234\n"), 0o644)
	calls := filepath.Join(t.TempDir(), "calls")
	fakeGH(t, calls, `
case "$*" in
  *"issue list"*) echo "HTTP 502: bad gateway" >&2; exit 1;;
  *"issue create"*) echo "https://github.com/o/r/issues/1";;
esac`)
	g := GitHub(home, &config.Config{})
	p := repoWithRemote(t, "o", "r", "git@github.com:o/r.git")
	if _, err := g.File(Request{ID: 1, UID: "000000000001", Project: p, Text: "x", Kind: "me"}); err == nil || !strings.Contains(err.Error(), "could not check") {
		t.Fatalf("%v", err)
	}
	if b, _ := os.ReadFile(calls); strings.Contains(string(b), "issue create") {
		t.Fatalf("must not create when the lookup failed:\n%s", b)
	}
}

// A configured account whose token cannot be fetched must never fall back
// to whoever gh's active account is: detect says why; ops error out.
func TestGitHubNoTokenForConfiguredAccount(t *testing.T) {
	home := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	fakeGH(t, calls, `
case "$*" in
  "auth token --user work-account") echo "no oauth token found for github.com account work-account" >&2; exit 1;;
  "auth status"*) exit 0;;
  *"issue create"*) echo "https://github.com/acme/chime/issues/1";;
  *"issue list"*) printf '[]';;
  *"--json state,url"*) printf '{"state":"OPEN"}';;
esac`)
	cfg := &config.Config{Projects: map[string]map[string]string{"acme/*": {"github_account": "work-account"}}}
	g := GitHub(home, cfg)
	p := repoWithRemote(t, "acme", "chime", "git@github.com:acme/chime.git")
	var warn bytes.Buffer
	if g.Detect(p, &warn) || !strings.Contains(warn.String(), "work-account") {
		t.Fatalf("detect must refuse and name the account: %q", warn.String())
	}
	if _, err := g.File(Request{ID: 1, UID: "000000000001", Project: p, Text: "x", Kind: "me"}); err == nil {
		t.Fatal("file must not run as the active account")
	}
	if _, err := g.Status(p, "github:acme/chime#1"); err == nil {
		t.Fatal("status must not run as the active account")
	}
	if b, _ := os.ReadFile(calls); strings.Contains(string(b), "issue create") || strings.Contains(string(b), "issue view") {
		t.Fatalf("no gh op may run without the account's token:\n%s", b)
	}
}

// An in-memory IssueCLI: tests Remote's own logic (marker recovery, fail
// closed, identity from the project) without a gh fake.

func fakeGLab(t *testing.T, calls, body string) {
	// Real glab prints notices on stderr beside JSON on stdout.
	testutil.FakeBin(t, "glab", "echo \"$*\" >> "+calls+"\necho 'DEPRECATION WARNING: x' >&2\n"+body)
}

func TestGitLabBackendLifecycle(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "install-id"), []byte("abcd1234\n"), 0o644)
	calls := filepath.Join(t.TempDir(), "calls")
	fakeGLab(t, calls, `
case "$*" in
  *"auth status"*) echo "git.example.org"; exit 0;;
  *"issues?scope=created_by_me&state=all"*) printf '[{"iid":40,"description":"other <!-- sous:zzzzzzzz:8 -->"},{"iid":11,"description":"again <!-- sous:abcd1234:8 -->"}]';;
  *"-X POST"*"projects/frontend%2Fapp-next/issues"*) printf '{"iid":12,"state":"opened","web_url":"https://git.example.org/frontend/app-next/-/issues/12"}';;
  *"issues/12?state_event=close"*) printf '{"iid":12,"state":"closed"}';;
  *"issues/12"*) printf '{"iid":12,"state":"opened","web_url":"https://git.example.org/frontend/app-next/-/issues/12"}';;
  *"issues/13"*) echo '{"message":"404 Not found"}' >&2; exit 1;;
  *"issues/14"*) echo '{"message":"401 Unauthorized"}' >&2; exit 1;;
  *"projects/frontend%2Fsecret/issues/1"*) echo '{"message":"404 Project Not Found"}' >&2; exit 1;;
esac`)
	cfg := &config.Config{}
	g := GitLab(home, cfg)
	p := repoWithRemote(t, "oss", "app-next", "https://git.example.org/frontend/app-next.git")
	var warn bytes.Buffer
	if !g.Detect(p, &warn) {
		t.Fatalf("detect: %s", warn.String())
	}
	ref, err := g.File(Request{ID: 7, UID: "000000000007", Project: p, Text: "json api for browse & search", Kind: "me"})
	if err != nil || ref != "gitlab:git.example.org/frontend/app-next#12" {
		t.Fatal(ref, err)
	}
	b, _ := os.ReadFile(calls)
	if !strings.Contains(string(b), "--hostname git.example.org") || !strings.Contains(string(b), "projects/frontend%2Fapp-next/issues") || !strings.Contains(string(b), "sous:000000000007") || !strings.Contains(string(b), "-f title=json api for browse & search") {
		t.Fatalf("create call shape:\n%s", b)
	}
	if ref8, _ := g.File(Request{ID: 8, UID: "000000000008", Project: p, Text: "again", Kind: "idea"}); ref8 != "gitlab:git.example.org/frontend/app-next#11" {
		t.Fatal("recovery via marker:", ref8)
	}
	if b, _ = os.ReadFile(calls); strings.Count(string(b), "-X POST") != 1 {
		t.Fatalf("must not create twice:\n%s", b)
	}
	if st, err := g.Status(p, ref); err != nil || st != "open" {
		t.Fatal(st, err)
	}
	if st, err := g.Status(p, "gitlab:git.example.org/frontend/app-next#13"); err != nil || st != "unknown" {
		t.Fatal(st, err)
	}
	if _, err := g.Status(p, "gitlab:git.example.org/frontend/app-next#14"); err == nil {
		t.Fatal("401 is an error, not unknown")
	}
	// A project this token cannot see is "invisible", never "ref missing".
	if _, err := g.Status(p, "gitlab:git.example.org/frontend/secret#1"); err == nil || !strings.Contains(err.Error(), "not visible") {
		t.Fatalf("%v", err)
	}
	if err := g.Close(p, ref); err != nil {
		t.Fatal(err)
	}
	if u, err := g.URL(p, ref); err != nil || !strings.HasSuffix(u, "/-/issues/12") {
		t.Fatal(u, err)
	}
	warn.Reset()
	if g.Detect(repoWithRemote(t, "x", "y", "https://git.other.dev/a/b.git"), &warn) || warn.Len() != 0 {
		t.Fatalf("a host that is not GitLab-looking is silently not ours: %q", warn.String())
	}
	if g.Detect(repoWithRemote(t, "o", "r", "git@github.com:o/r.git"), &warn) {
		t.Fatal("github remote is not gitlab")
	}
}

// Review F16: a project hosted elsewhere (Bitbucket, Gitea) is simply not a
// GitLab project; only a GitLab-looking host glab doesn't know gets the hint.
func TestGitLabLocateHintsOnlyForGitLabHosts(t *testing.T) {
	tracker.ResetCache()
	t.Cleanup(tracker.ResetCache)
	fakeGLab(t, filepath.Join(t.TempDir(), "calls"), `echo "No hosts are configured on this machine." >&2; exit 1`)
	g := GitLab(t.TempDir(), &config.Config{})
	var warn bytes.Buffer
	if g.Detect(repoWithRemote(t, "acme", "chime", "git@bitbucket.org:acme/chime.git"), &warn) || warn.Len() != 0 {
		t.Fatalf("bitbucket: %q", warn.String())
	}
	if g.Detect(repoWithRemote(t, "acme", "api", "https://gitlab.example.org/acme/api.git"), &warn) || !strings.Contains(warn.String(), "glab auth login --hostname gitlab.example.org") {
		t.Fatalf("gitlab-looking host: %q", warn.String())
	}
}
