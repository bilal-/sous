package signal

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/tracker/trackertest"
)

// A search that returns a full page may have more; the
// scan says it is incomplete, so earlier findings stay (stale) instead of
// being dropped.
func TestFullPageMeansIncomplete(t *testing.T) {
	app := repo(t, filepath.Join(t.TempDir(), "acme/api"), true)
	git(t, app, "remote", "add", "origin", "git@github.com:acme/api.git")
	var page strings.Builder
	page.WriteString("[")
	for i := range searchLimit {
		if i > 0 {
			page.WriteString(",")
		}
		fmt.Fprintf(&page, `{"repository":{"nameWithOwner":"other/r%d"},"number":%d,"title":"x","updatedAt":"2026-09-25T10:00:00Z"}`, i, i)
	}
	page.WriteString("]")
	trackertest.Fake(t, "gh", `case "$*" in "auth status") exit 0;; *) printf '%s' '`+page.String()+`';; esac`)
	err := ScanGitHub(&config.Config{})([]string{app}, io.Discard, io.Discard, time.Now())
	if err == nil || !strings.Contains(err.Error(), "some may be missing") {
		t.Fatalf("%v", err)
	}
}
