package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

// sous runner check drives any runner program through the contract, so a
// plugin written in any language can be checked without a sous checkout.
func TestRunnerCheck(t *testing.T) {
	f := fixture(t)
	good := testutil.Script(t, t.TempDir(), "sous-runner-good", `case "$1" in
start) cat > /dev/null; echo good:1;;
status) echo '{"v":0,"state":"running"}';;
stop) ;;
esac`)
	if out, errs, code := f.run("runner", "check", good); code != 0 || !strings.Contains(out, "good follows the runner contract") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	bad := testutil.Script(t, t.TempDir(), "sous-runner-bad", `case "$1" in
start) cat > /dev/null; echo other:1;;
status) echo '{"v":0,"state":"running"}';;
stop) ;;
esac`)
	out, errs, code := f.run("runner", "check", bad, "--json")
	var got struct {
		Runner   string   `json:"runner"`
		OK       bool     `json:"ok"`
		Problems []string `json:"problems"`
	}
	if code != 1 || json.Unmarshal([]byte(out), &got) != nil || got.OK || got.Runner != "bad" || len(got.Problems) == 0 || !strings.Contains(got.Problems[0], "must start with") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	for _, args := range [][]string{{"runner", "check"}, {"runner", "check", "/nope/sous-runner-x"}, {"runner", "check", good, "--wait", "soon"}, {"runner", "check", "/bin/echo"}} {
		if _, _, code := f.run(args...); code != 2 {
			t.Errorf("%v: exit %d; want 2", args, code)
		}
	}
}
