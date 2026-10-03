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
	"github.com/bilal-/sous/internal/tracker"
	"github.com/bilal-/sous/internal/tracker/trackertest"
)

func TestScanGitLab(t *testing.T) {
	ws := t.TempDir()
	app := repo(t, filepath.Join(ws, "oss/app-next"), true)
	git(t, app, "remote", "add", "origin", "https://git.example.org/frontend/app-next.git")
	other := repo(t, filepath.Join(ws, "oss/library"), true)
	git(t, other, "remote", "add", "origin", "https://git.example.org/backend/library-api.git")
	gh := repo(t, filepath.Join(ws, "o/r"), true)
	git(t, gh, "remote", "add", "origin", "git@github.com:o/r.git")
	calls := filepath.Join(t.TempDir(), "calls")
	trackertest.Fake(t, "glab", `echo "$*" >> `+calls+`
case "$*" in
  *"auth status"*) echo "git.example.org"; exit 0;;
  *"--hostname git.example.org api user"*) printf '{"id":37,"username":"dev"}';;
  *"reviewer_username=dev"*) printf '[{"references":{"full":"frontend/app-next!9"},"iid":9,"title":"review me","updated_at":"2026-09-25T10:00:00Z","web_url":"https://git.example.org/frontend/app-next/-/merge_requests/9"},{"references":{"full":"elsewhere/x!1"},"iid":1,"title":"not local","updated_at":"2026-09-25T10:00:00Z"}]';;
  *"scope=created_by_me"*) printf '[{"references":{"full":"backend/library-api!3"},"iid":3,"title":"schema fields","updated_at":"2026-09-20T10:00:00Z"}]';;
  *) echo "unexpected $*" >&2; exit 1;;
esac`)
	cfg := &config.Config{GitLabHosts: []string{"git.example.org"}}
	var out, warn bytes.Buffer
	if err := ScanGitLab(cfg)([]string{app, other, gh}, &out, &warn, time.Now()); err != nil {
		t.Fatal(err, warn.String())
	}
	sigs, _ := readLines(&out)
	if len(sigs) != 2 {
		t.Fatalf("%+v", sigs)
	}
	r := find(sigs, "review requested")
	if r == nil || r.Kind != Me || r.Project != app || r.Text != "review requested · MR !9 review me" || *r.Ref != "gitlab:git.example.org/frontend/app-next!9" {
		t.Fatalf("%+v", r)
	}
	a := find(sigs, "awaiting review")
	if a == nil || a.Kind != Them || a.Project != other || *a.Ref != "gitlab:git.example.org/backend/library-api!3" {
		t.Fatalf("authored MRs are on others: %+v", a)
	}
	b, _ := os.ReadFile(calls)
	if strings.Count(string(b), "merge_requests?") != 2 {
		t.Fatalf("one call per query per host:\n%s", b)
	}
	// glab missing: reported, not fatal to the board.
	testutil.OnlyGit(t)
	out.Reset()
	warn.Reset()
	if err := ScanGitLab(cfg)([]string{app}, &out, &warn, time.Now()); err == nil || !strings.Contains(warn.String(), "glab not installed") {
		t.Fatalf("%v %q", err, warn.String())
	}
}

// No GitLab at all is "not set up": nothing is reported, and the board
// stays quiet about it (see board's not-set-up rule).
func TestScanGitLabWithoutHostsIsNotSetUp(t *testing.T) {
	trackertest.Fake(t, "glab", `echo "No hosts are configured" >&2; exit 1`)
	var out bytes.Buffer
	if err := ScanGitLab(&config.Config{})([]string{t.TempDir()}, &out, io.Discard, time.Now()); !errors.Is(err, ErrNotSetUp) || out.Len() != 0 {
		t.Fatalf("%v %q", err, out.String())
	}
}

// Real glab prints deprecation and update notices on stderr next to valid
// JSON on stdout; only stdout is data.
func TestScanGitLabIgnoresStderrNotices(t *testing.T) {
	ws := t.TempDir()
	r := repo(t, filepath.Join(ws, "a/r"), true)
	git(t, r, "remote", "add", "origin", "https://git.example.org/g/r.git")
	trackertest.Fake(t, "glab", `echo "DEPRECATION WARNING: something" >&2
case "$*" in
  *"auth status"*) echo "git.example.org";;
  *"api user"*) printf '{"username":"me"}';;
  *"scope=created_by_me"*) printf '[{"references":{"full":"g/r!2"},"iid":2,"title":"mine","updated_at":"2026-09-25T10:00:00Z"}]';;
  *) printf '[]';;
esac`)
	var out, warn bytes.Buffer
	if err := ScanGitLab(&config.Config{GitLabHosts: []string{"git.example.org"}})([]string{r}, &out, &warn, time.Now()); err != nil {
		t.Fatalf("%v %s", err, warn.String())
	}
	if !strings.Contains(out.String(), "awaiting review · MR !2 mine") {
		t.Fatalf("%s", out.String())
	}
}

// Glab failing to say which hosts it knows is a failed scan, so the
// runner keeps last findings stale instead of dropping them as resolved.
func TestScanGitLabFailsWhenHostsUnknown(t *testing.T) {
	trackertest.Fake(t, "glab", `echo "could not reach keyring" >&2; exit 1`)
	var out, warn bytes.Buffer
	if err := ScanGitLab(&config.Config{})(nil, &out, &warn, time.Now()); err == nil || !strings.Contains(warn.String(), "keyring") {
		t.Fatalf("err=%v warn=%q", err, warn.String())
	}
}

// Glab logged out or missing, with nothing in config, is "not set
// up" (rows seen before are kept stale), never a quiet empty success that
// drops them.
func TestScanGitLabNotSetUp(t *testing.T) {
	for name, glab := range map[string]string{
		"logged out": `echo "No hosts are configured on this machine." >&2; exit 1`,
		"missing":    "",
	} {
		tracker.ResetCache()
		if glab == "" {
			testutil.OnlyGit(t)
		} else {
			trackertest.Fake(t, "glab", glab)
		}
		if err := ScanGitLab(&config.Config{})(nil, io.Discard, io.Discard, time.Now()); !errors.Is(err, ErrNotSetUp) {
			t.Errorf("%s: %v", name, err)
		}
	}
	tracker.ResetCache()
	testutil.OnlyGit(t)
	if err := ScanGitLab(&config.Config{GitLabHosts: []string{"git.example.org"}})(nil, io.Discard, io.Discard, time.Now()); err == nil || errors.Is(err, ErrNotSetUp) {
		t.Fatalf("a configured host without glab is a real failure: %v", err)
	}
}
