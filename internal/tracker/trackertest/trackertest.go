// Package trackertest helps tests that stand in for gh or glab.
package trackertest

import (
	"os"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/tracker"
)

// Fake installs a fake gh or glab for the test and forgets what tracker
// cached about the real one (tokens, GitLab hosts), before and after.
//
// tracker is given the test's environment (so the fake is found and can run
// ordinary commands) and no home folder, so no real ~/.ssh/config is read.
func Fake(t *testing.T, tool, body string) string {
	t.Helper()
	tracker.ResetCache()
	path := testutil.FakeBin(t, tool, body)
	tracker.Init("", t.TempDir(), os.Environ())
	t.Cleanup(func() { tracker.ResetCache(); tracker.Init("", "", nil) })
	return path
}
