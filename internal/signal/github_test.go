package signal

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/tracker/trackertest"
)

func fakeGH(t *testing.T, body string) { trackertest.Fake(t, "gh", body) }

func TestScanGitHub(t *testing.T) {
	ws := t.TempDir()
	app := repo(t, filepath.Join(ws, "oss/app-next"), true)
	git(t, app, "remote", "add", "origin", "git@github.com:oss/app-next.git")
	shell := repo(t, filepath.Join(ws, "acme/chime"), true)
	git(t, shell, "remote", "add", "origin", "https://github.com/acme/chime.git")
	local := repo(t, filepath.Join(ws, "local/nogit"), true)
	paths := []string{app, shell, local}

	// No gh on PATH (git must stay reachable for project.Remote).
	testutil.OnlyGit(t)
	var out, warn bytes.Buffer
	if err := ScanGitHub(&config.Config{})(paths, &out, &warn, time.Now()); err == nil || !strings.Contains(warn.String(), "gh not installed") {
		t.Fatalf("no gh: err=%v warn=%q", err, warn.String())
	}

	// Not logged in.
	fakeGH(t, `case "$1 $2" in "auth status") echo "not logged in" >&2; exit 1;; esac`)
	out.Reset()
	warn.Reset()
	if err := ScanGitHub(&config.Config{})(paths, &out, &warn, time.Now()); err == nil || !strings.Contains(warn.String(), "not logged in") {
		t.Fatalf("unauth: err=%v warn=%q", err, warn.String())
	}

	// Authenticated with two fake searches; count calls.
	calls := filepath.Join(t.TempDir(), "calls")
	fakeGH(t, `echo "$*" >> `+calls+`
case "$*" in
  "auth status") exit 0;;
  *--review-requested=@me*) printf '[{"repository":{"nameWithOwner":"oss/app-next"},"number":14,"title":"json api","updatedAt":"2026-09-25T10:00:00Z"},{"repository":{"nameWithOwner":"other/elsewhere"},"number":3,"title":"x","updatedAt":"2026-09-25T10:00:00Z"}]';;
  *--slurp*notifications*) printf '[[]]';;
  "api --hostname github.com graphql"*) printf '{"data":{"search":{"nodes":[]}}}';;
  *--review=changes_requested*) printf '[{"repository":{"nameWithOwner":"acme/chime"},"number":7,"title":"pairing flow","updatedAt":"2026-09-20T10:00:00Z"}]';;
  *--review=*) echo 'invalid argument for "--review" flag' >&2; exit 1;;
  *) echo "unexpected $*" >&2; exit 1;;
esac`)
	out.Reset()
	warn.Reset()
	if err := ScanGitHub(&config.Config{})(paths, &out, &warn, time.Now()); err != nil {
		t.Fatal(err, warn.String())
	}
	sigs, err := readLines(&out)
	if err != nil || len(sigs) != 2 {
		t.Fatalf("signals: %+v %v", sigs, err)
	}
	b, _ := os.ReadFile(calls)
	if strings.Count(string(b), "search prs") != 2 {
		t.Fatalf("expected exactly 2 searches:\n%s", b)
	}
	r := find(sigs, "review requested")
	if r == nil || r.Text != "review requested · PR #14 json api" || r.Kind != Me || r.Project != app || *r.Ref != "github:oss/app-next#14" || r.Observed.Format(time.RFC3339) != "2026-09-25T10:00:00Z" {
		t.Fatalf("review: %+v", r)
	}
	c := find(sigs, "changes requested")
	if c == nil || c.Project != shell || *c.Ref != "github:acme/chime#7" {
		t.Fatalf("changes (case-insensitive owner): %+v", c)
	}
}

// One query failing must not lose the other query's findings: the board
// would otherwise claim "0 on you" while review requests exist.
func TestScanGitHubPartialFailure(t *testing.T) {
	ws := t.TempDir()
	app := repo(t, filepath.Join(ws, "oss/app-next"), true)
	git(t, app, "remote", "add", "origin", "git@github.com:oss/app-next.git")
	fakeGH(t, `case "$*" in
  "auth status") exit 0;;
  *--review-requested=@me*) printf '[{"repository":{"nameWithOwner":"oss/app-next"},"number":14,"title":"json api","updatedAt":"2026-09-25T10:00:00Z"}]';;
  *) echo "rate limited" >&2; exit 1;;
esac`)
	var out, warn bytes.Buffer
	err := ScanGitHub(&config.Config{})([]string{app}, &out, &warn, time.Now())
	sigs, _ := readLines(&out)
	if len(sigs) != 1 || find(sigs, "review requested") == nil {
		t.Fatalf("surviving query's findings must be emitted: %+v", sigs)
	}
	if err == nil || !strings.Contains(warn.String(), "rate limited") {
		t.Fatalf("a failed query must still be reported: err=%v warn=%q", err, warn.String())
	}
}

// Requests visible only to a second GitHub account (per-org config)
// must appear: the search runs once per account and results are unioned.
func TestScanGitHubPerAccount(t *testing.T) {
	ws := t.TempDir()
	personal := repo(t, filepath.Join(ws, "personal/kit"), true)
	git(t, personal, "remote", "add", "origin", "git@github.com:me/kit.git")
	work := repo(t, filepath.Join(ws, "acme/chime"), true)
	git(t, work, "remote", "add", "origin", "git@github.com:acme/chime.git")
	calls := filepath.Join(t.TempDir(), "calls")
	fakeGH(t, `echo "TOKEN=$GH_TOKEN $*" >> `+calls+`
case "$*" in
  "auth status") exit 0;;
  "auth token --user work-account") echo tok-m;;
  *--review-requested=@me*)
    if [ "$GH_TOKEN" = tok-m ]; then printf '[{"repository":{"nameWithOwner":"acme/chime"},"number":7,"title":"work pr","updatedAt":"2026-09-25T10:00:00Z"}]'
    else printf '[{"repository":{"nameWithOwner":"me/kit"},"number":3,"title":"personal pr","updatedAt":"2026-09-25T10:00:00Z"}]'; fi;;
  *--slurp*notifications*) printf '[[]]';;
  "api --hostname github.com graphql"*) printf '{"data":{"search":{"nodes":[]}}}';;
  *--review=changes_requested*) printf '[]';;
esac`)
	cfg := &config.Config{Projects: map[string]map[string]string{"acme/*": {"github_account": "work-account"}}}
	var out, warn bytes.Buffer
	if err := ScanGitHub(cfg)([]string{personal, work}, &out, &warn, time.Now()); err != nil {
		t.Fatal(err, warn.String())
	}
	sigs, _ := readLines(&out)
	if len(sigs) != 2 || find(sigs, "work pr") == nil || find(sigs, "personal pr") == nil {
		t.Fatalf("both accounts' PRs: %+v", sigs)
	}
	b, _ := os.ReadFile(calls)
	if strings.Count(string(b), "review-requested") != 2 || !strings.Contains(string(b), "TOKEN=tok-m search") || !strings.Contains(string(b), "TOKEN= search") {
		t.Fatalf("one search pair per account:\n%s", b)
	}
}

// A broken default login must not hide what a configured account can see.
func TestScanGitHubWorksWhenOnlyAConfiguredAccountWorks(t *testing.T) {
	ws := t.TempDir()
	app := repo(t, filepath.Join(ws, "acme/api"), true)
	git(t, app, "remote", "add", "origin", "git@github.com:acme/api.git")
	fakeGH(t, `case "$*" in
  "auth status") echo "token for the active account is invalid" >&2; exit 1;;
  "auth token --user work-account") echo tok-w;;
  *--review-requested=@me*) [ "$GH_TOKEN" = tok-w ] || { echo "HTTP 401" >&2; exit 1; }
    printf '[{"repository":{"nameWithOwner":"acme/api"},"number":5,"title":"review me","updatedAt":"2026-09-25T10:00:00Z"}]';;
  *) [ "$GH_TOKEN" = tok-w ] || { echo "HTTP 401" >&2; exit 1; }; printf '[]';;
esac`)
	var out, warn bytes.Buffer
	cfg := &config.Config{Projects: map[string]map[string]string{"acme/*": {"github_account": "work-account"}}}
	ScanGitHub(cfg)([]string{app}, &out, &warn, time.Now())
	if !strings.Contains(out.String(), "review me") {
		t.Fatalf("out=%q warn=%q", out.String(), warn.String())
	}
}

// With nothing configured, gh missing or logged out means GitHub is not set
// up here: a quiet "off", not a failure on every board.
func TestScanGitHubNotSetUp(t *testing.T) {
	testutil.OnlyGit(t)
	var warn bytes.Buffer
	if err := ScanGitHub(&config.Config{})(nil, io.Discard, &warn, time.Now()); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("no gh: %v", err)
	}
	fakeGH(t, `echo "You are not logged into any GitHub hosts." >&2; exit 1`)
	if err := ScanGitHub(&config.Config{})(nil, io.Discard, &warn, time.Now()); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("logged out: %v", err)
	}
	cfg := &config.Config{Projects: map[string]map[string]string{"acme/*": {"github_account": "work-account"}}}
	testutil.OnlyGit(t)
	if err := ScanGitHub(cfg)(nil, io.Discard, &warn, time.Now()); err == nil || errors.Is(err, ErrNotSetUp) {
		t.Fatalf("a configured account without gh is a real failure: %v", err)
	}
}

// Only a missing tool or a real logout is "not set up"; a network
// or keyring failure is a failure, named in the headline.
func TestScanGitHubNetworkFailureIsAFailure(t *testing.T) {
	fakeGH(t, `echo "error connecting to api.github.com:
network is unreachable" >&2; exit 1`)
	var warn bytes.Buffer
	err := ScanGitHub(&config.Config{})(nil, io.Discard, &warn, time.Now())
	if err == nil || errors.Is(err, ErrNotSetUp) {
		t.Fatalf("%v", err)
	}
	if strings.Count(strings.TrimSpace(warn.String()), "\n") != 0 {
		t.Fatalf("the reason is one line: %q", warn.String())
	}
}
