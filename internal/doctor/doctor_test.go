package doctor

import (
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/tracker"
)

// A gh and a glab that both hang are each reported once the time is up,
// and a GitHub account config.toml names is a problem, not a note.
func TestTrackerChecksDoNotWaitForever(t *testing.T) {
	testutil.FakeBin(t, "gh", "sleep 2")
	testutil.FakeBin(t, "glab", "sleep 2")
	tracker.ResetCache()
	t.Cleanup(tracker.ResetCache)
	old := trackerTimeout
	trackerTimeout = 300 * time.Millisecond
	t.Cleanup(func() { trackerTimeout = old })
	start := time.Now()
	cs := trackerChecks(&config.Config{GitLabHosts: []string{"git.example.org"}})
	if time.Since(start) > 3*time.Second {
		t.Fatalf("waited %s", time.Since(start))
	}
	var got []string
	for _, c := range cs {
		got = append(got, c.Name+"|"+string(c.Status)+"|"+c.Detail)
	}
	if len(cs) != 2 || !strings.Contains(got[0], "did not answer") || !strings.Contains(got[1], "did not answer") {
		t.Fatalf("%q", got)
	}
}

func TestConfiguredAccountFailingIsAProblem(t *testing.T) {
	testutil.FakeBin(t, "gh", `case "$*" in
  "auth status") exit 0;;
  "auth token --user work-account") exit 1;;
esac`)
	tracker.ResetCache()
	t.Cleanup(tracker.ResetCache)
	cs := trackerChecks(&config.Config{Projects: map[string]map[string]string{"acme/*": {config.KeyGitHubAccount: "work-account"}}})
	if Problems(cs) == 0 {
		t.Fatalf("%+v", cs)
	}
}
