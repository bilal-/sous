package tracker

import (
	"os"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

func TestCheckGitHub(t *testing.T) {
	fakeTool(t, "gh", `case "$*" in
  "auth status") exit 0;;
  "auth token --user work-account") echo tok;;
  "auth token --user gone") exit 1;;
  *notifications*) echo "gh: Resource not accessible by personal access token (HTTP 403)" >&2; exit 1;;
esac`)
	Init("", t.TempDir(), os.Environ())
	t.Cleanup(func() { Init("", "", nil) })
	var lines []string
	for _, r := range CheckGitHub([]string{"work-account", "gone"}) {
		lines = append(lines, r.Name+"|"+map[bool]string{true: "ok", false: "no"}[r.OK]+"|"+r.Fix)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"gh|ok|",
		"GitHub account work-account|ok|",
		"GitHub account gone|no|gh auth login",
		"GitHub notifications|no|gh auth refresh -s notifications",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestCheckGitHubNotInstalledIsOptional(t *testing.T) {
	testutil.OnlyGit(t)
	Init("", t.TempDir(), os.Environ())
	t.Cleanup(func() { Init("", "", nil) })
	r := CheckGitHub(nil)
	if len(r) != 1 || !r[0].OK || !strings.Contains(r[0].Detail, "not installed") {
		t.Fatalf("%+v", r)
	}
	if r := CheckGitHub([]string{"work-account"}); r[0].OK {
		t.Fatalf("configured accounts need gh: %+v", r)
	}
}

// fakeTool installs a fake gh or glab and clears what tracker cached about
// the last one (tokens, GitLab hosts), before and after, so tests cannot
// see each other's answers.
func fakeTool(t *testing.T, tool, body string) {
	t.Helper()
	ResetCache()
	t.Cleanup(ResetCache)
	testutil.FakeBin(t, tool, body)
}
