package harness

import "testing"

// Review round 4: a command that wraps sous (env, timeout, echo) is not a
// sous hook, even when every word is a path.
func TestIsOursRejectsWrappers(t *testing.T) {
	for _, c := range []string{
		"/bin/echo /opt/sous hook session-start claude",
		"/usr/bin/env /opt/sous hook session-start claude",
		"/usr/bin/timeout 3 /opt/sous hook session-start claude",
	} {
		if IsOurs(c, RoleStart, "claude") {
			t.Errorf("%q is not ours", c)
		}
	}
}
