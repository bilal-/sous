package signal

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/tracker/trackertest"
)

func notification(id, reason, typ, url, title, updated string) string {
	return `{"id":"` + id + `","reason":"` + reason + `","updated_at":"` + updated + `","repository":{"full_name":"acme/api"},"subject":{"type":"` + typ + `","url":` + url + `,"title":"` + title + `"}}`
}

// 0.2: unread notifications addressed to you (mentions, team mentions,
// assignments) are on you, one row per thread. Review requests are left to
// the review search; releases, discussions and CI mail are left out.
func TestScanGitHubNotifications(t *testing.T) {
	app := repo(t, filepath.Join(t.TempDir(), "acme/api"), true)
	git(t, app, "remote", "add", "origin", "git@github.com:acme/api.git")
	page1 := `[` + strings.Join([]string{
		notification("1", "mention", "Issue", `"https://api.github.com/repos/acme/api/issues/12"`, "crash on start", "2026-09-25T10:00:00Z"),
		notification("2", "assign", "PullRequest", `"https://api.github.com/repos/acme/api/pulls/14"`, "json api", "2026-09-25T11:00:00Z"),
		notification("3", "review_requested", "PullRequest", `"https://api.github.com/repos/acme/api/pulls/15"`, "x", "2026-09-25T11:00:00Z"),
	}, ",") + `]`
	page2 := `[` + strings.Join([]string{
		notification("4", "team_mention", "Issue", `"https://api.github.com/repos/acme/api/issues/16"`, "infra", "2026-09-25T12:00:00Z"),
		notification("5", "mention", "Release", `"https://api.github.com/repos/acme/api/releases/1"`, "v1", "2026-09-25T12:00:00Z"),
		notification("6", "mention", "Discussion", `null`, "idea", "2026-09-25T12:00:00Z"),
	}, ",") + `]`
	trackertest.Fake(t, "gh", `case "$*" in
  "auth status") exit 0;;
  *"--slurp"*notifications*) printf '%s' '[`+page1+`,`+page2+`]';;
  *"graphql"*) printf '{"data":{"search":{"nodes":[]}}}';;
  *) printf '[]';;
esac`)
	var out bytes.Buffer
	if err := ScanGitHub(&config.Config{})([]string{app}, &out, io.Discard, time.Now()); err != nil {
		t.Fatal(err)
	}
	sigs, _ := readLines(&out)
	var got []string
	for _, s := range sigs {
		got = append(got, s.Text+"|"+s.State+"|"+deref(s.Ref))
	}
	want := []string{
		"mentioned · issue #12 crash on start|2026-09-25T10:00:00Z|github:acme/api#12",
		"assigned · PR #14 json api|2026-09-25T11:00:00Z|github:acme/api#14",
		"team mentioned · issue #16 infra|2026-09-25T12:00:00Z|github:acme/api#16",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
}

// Notifications need a classic token; when GitHub refuses, the other
// GitHub findings still come through, the scan is failed (not empty), and
// the reason says what to do.
func TestNotificationsRefusedIsAFailureWithAHint(t *testing.T) {
	app := repo(t, filepath.Join(t.TempDir(), "acme/api"), true)
	git(t, app, "remote", "add", "origin", "git@github.com:acme/api.git")
	trackertest.Fake(t, "gh", `case "$*" in
  "auth status") exit 0;;
  *notifications*) echo "gh: Resource not accessible by personal access token (HTTP 403)" >&2; exit 1;;
  *--review-requested=@me*) printf '[{"repository":{"nameWithOwner":"acme/api"},"number":3,"title":"review me","updatedAt":"2026-09-25T10:00:00Z"}]';;
  *"graphql"*) printf '{"data":{"search":{"nodes":[]}}}';;
  *) printf '[]';;
esac`)
	var out, warn bytes.Buffer
	err := ScanGitHub(&config.Config{})([]string{app}, &out, &warn, time.Now())
	if err == nil || !strings.Contains(out.String(), "review me") || !strings.Contains(warn.String(), "gh auth refresh -s notifications") {
		t.Fatalf("%v %q %q", err, out.String(), warn.String())
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
