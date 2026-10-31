package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIntegrationActionsWriteOneOutcomeOnLookupFailure(t *testing.T) {
	for _, action := range []string{"run", "stop", "reply", "approve", "discard", "done", "snooze"} {
		t.Run(action+"/config", func(t *testing.T) {
			f := fixture(t)
			f.writeConfig("roots = [")
			args := []string{"integration", "--protocol", "1", "--json", action, "--op", "op-config000000001"}
			if action == "approve" {
				args = append(args, "--commit", strings.Repeat("a", 40))
			}
			args = append(args, "--", "1")
			out, errs, code := f.runStdin("some input", args...)
			var o outcome
			if err := json.Unmarshal([]byte(out), &o); err != nil {
				t.Fatalf("one outcome required: %v: %q", err, out)
			}
			if action == "done" || action == "snooze" {
				// Private notes do not require configuration. The missing
				// target is still reported in exactly one outcome.
				if code != exitUsage || o.Kind != "outcome" || o.Outcome != "refused" || o.Reason != "unknown_item" || strings.Contains(errs, "config:") {
					t.Fatalf("private outcome: %+v, code %d, stderr %q", o, code, errs)
				}
				return
			}
			if code != exitNotReady || o.Kind != "outcome" || o.Outcome != "not_ready" || o.Reason != "config" || !strings.Contains(errs, "config:") {
				t.Fatalf("outcome: %+v, code %d, stderr %q", o, code, errs)
			}
		})
	}
	for _, ambiguous := range []bool{false, true} {
		name := "missing"
		if ambiguous {
			name = "ambiguous"
		}
		t.Run("run/"+name, func(t *testing.T) {
			f := fixture(t)
			if ambiguous {
				f.mkrepo("acme/api", true)
				f.mkrepo("example/api", true)
			}
			out, errs, code := f.runStdin("some input", "integration", "--protocol", "1", "--json", "run", "--op", "op-project00000001", "--", "api")
			var o outcome
			if err := json.Unmarshal([]byte(out), &o); err != nil {
				t.Fatalf("one outcome required: %v: %q", err, out)
			}
			if code != exitUsage || o.Kind != "outcome" || o.Outcome != "refused" || o.Reason != "bad_input" || errs == "" {
				t.Fatalf("outcome: %+v, code %d, stderr %q", o, code, errs)
			}
		})
	}
}
