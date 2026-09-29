package signal

import (
	"bytes"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/tracker/trackertest"
)

// 0.2: your open pull requests whose checks failed are on you. Pending,
// passing or absent checks are not; a new push (a new head commit) ends a
// snooze; GraphQL errors are failures.
func TestScanGitHubFailingChecks(t *testing.T) {
	app := repo(t, filepath.Join(t.TempDir(), "acme/api"), true)
	git(t, app, "remote", "add", "origin", "git@github.com:acme/api.git")
	pr := func(n int, state, oid string) string {
		rollup := "null"
		if state != "" {
			rollup = `{"state":"` + state + `"}`
		}
		return `{"number":` + itoa(n) + `,"title":"pr ` + itoa(n) + `","updatedAt":"2026-09-25T10:00:00Z","repository":{"nameWithOwner":"acme/api"},"commits":{"nodes":[{"commit":{"oid":"` + oid + `","statusCheckRollup":` + rollup + `}}]}}`
	}
	nodes := strings.Join([]string{pr(1, "FAILURE", "aaa"), pr(2, "ERROR", "bbb"), pr(3, "PENDING", "ccc"), pr(4, "SUCCESS", "ddd"), pr(5, "", "eee")}, ",")
	trackertest.Fake(t, "gh", `case "$*" in
  "auth status") exit 0;;
  "api --hostname github.com graphql"*) printf '%s' '{"data":{"search":{"nodes":[`+nodes+`]}}}';;
  *) printf '[]';;
esac`)
	var out bytes.Buffer
	if err := ScanGitHub(&config.Config{})([]string{app}, &out, io.Discard, time.Now()); err != nil {
		t.Fatal(err)
	}
	sigs, _ := readLines(&out)
	var got []string
	for _, s := range sigs {
		got = append(got, s.Text+"|"+s.State)
	}
	want := "checks failing · PR #1 pr 1|aaa,checks failing · PR #2 pr 2|bbb"
	if strings.Join(got, ",") != want {
		t.Fatalf("%v", got)
	}
	trackertest.Fake(t, "gh", `case "$*" in
  "auth status") exit 0;;
  "api --hostname github.com graphql"*) printf '%s' '{"data":null,"errors":[{"message":"Something went wrong"}]}';;
  *) printf '[]';;
esac`)
	if err := ScanGitHub(&config.Config{})([]string{app}, io.Discard, io.Discard, time.Now()); err == nil || !strings.Contains(err.Error(), "Something went wrong") {
		t.Fatalf("a GraphQL error is a failure: %v", err)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// Review: completeness is about the pull requests GitHub had, not the
// failing ones found: 100 pull requests with one failing is a full page.
func TestFailingChecksWithMorePagesIsIncomplete(t *testing.T) {
	app := repo(t, filepath.Join(t.TempDir(), "acme/api"), true)
	git(t, app, "remote", "add", "origin", "git@github.com:acme/api.git")
	node := `{"number":1,"title":"x","updatedAt":"2026-09-25T10:00:00Z","repository":{"nameWithOwner":"acme/api"},"commits":{"nodes":[{"commit":{"oid":"aaa","statusCheckRollup":{"state":"FAILURE"}}}]}}`
	trackertest.Fake(t, "gh", `case "$*" in
  "auth status") exit 0;;
  "api --hostname github.com graphql"*) printf '%s' '{"data":{"search":{"pageInfo":{"hasNextPage":true},"nodes":[`+node+`]}}}';;
  *) printf '[]';;
esac`)
	var out bytes.Buffer
	err := ScanGitHub(&config.Config{})([]string{app}, &out, io.Discard, time.Now())
	if err == nil || !strings.Contains(err.Error(), "more") || !strings.Contains(out.String(), "checks failing") {
		t.Fatalf("%v %q", err, out.String())
	}
}
