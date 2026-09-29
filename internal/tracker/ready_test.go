package tracker

import (
	"os"
	"strings"
	"testing"
)

// Review: "not logged in" only when the tool says so; any other failure
// (keyring, network) is shown as the tool reported it.
func TestReadyReportsTheRealReason(t *testing.T) {
	for _, c := range []struct{ stderr, want string }{
		{"You are not logged into any GitHub hosts. To log in, run: gh auth login", "not logged in (run gh auth login)"},
		{"error connecting to api.github.com: network is unreachable", "network is unreachable"},
	} {
		fakeTool(t, "gh", `echo "`+c.stderr+`" >&2; exit 1`)
		Init("", t.TempDir(), os.Environ())
		if err := GHReady(""); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v", c.stderr, err)
		}
	}
	t.Cleanup(func() { Init("", "", nil) })
}
