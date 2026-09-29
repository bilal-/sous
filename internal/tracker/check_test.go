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
		lines = append(lines, r.Name+"|"+map[bool]string{true: "ok", false: "no"}[r.OK]+"|"+map[bool]string{true: "needed", false: "optional"}[r.Needed]+"|"+r.Fix)
	}
	got := strings.Join(lines, "\n")
	// Each account's notifications are checked with its own token; what
	// config.toml names is needed, the default login is optional.
	for _, want := range []string{
		"gh|ok|optional|",
		"GitHub account work-account|ok|needed|",
		"GitHub account gone|no|needed|gh auth login",
		"GitHub notifications|no|optional|gh auth refresh -s notifications",
		"GitHub notifications (work-account)|no|needed|gh auth refresh -s notifications",
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
	if r := CheckGitHub([]string{"work-account"}); r[0].OK || !r[0].Needed {
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
