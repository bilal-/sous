// Package trackertest helps tests that stand in for gh or glab.
package trackertest

import (
	"testing"

	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/tracker"
)

// Fake installs a fake gh or glab for the test and forgets what tracker
// cached about the real one (tokens, GitLab hosts), before and after.
func Fake(t *testing.T, tool, body string) string {
	t.Helper()
	tracker.ResetCache()
	t.Cleanup(tracker.ResetCache)
	return testutil.FakeBin(t, tool, body)
}
