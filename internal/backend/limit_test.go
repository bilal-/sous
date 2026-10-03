package backend

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/tracker/trackertest"
)

// If the list of issues sous checks for an earlier try may
// be cut short, filing refuses rather than risk a duplicate.
func TestGitHubRecoveryRefusesWhenTheListIsFull(t *testing.T) {
	var page strings.Builder
	page.WriteString("[")
	for i := range recoveryLimit {
		if i > 0 {
			page.WriteString(",")
		}
		fmt.Fprintf(&page, `{"number":%d,"body":"x"}`, i+1)
	}
	page.WriteString("]")
	trackertest.Fake(t, "gh", `case "$*" in "auth status") exit 0;; *"issue list"*) printf '%s' '`+page.String()+`';; *) echo created; esac`)
	p := repoWithRemote(t, "acme", "api", "git@github.com:acme/api.git")
	_, err := GitHub(t.TempDir(), &config.Config{}).File(Request{ID: 1, UID: "000000000001", Project: p, Text: "x", Kind: "me"})
	if err == nil || !strings.Contains(err.Error(), "could not check") {
		t.Fatalf("%v", err)
	}
}
