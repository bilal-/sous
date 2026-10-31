package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStandaloneModesRejectUnusedInput(t *testing.T) {
	f := fixture(t)
	if _, errs, code := f.run("--refresh"); code != 0 {
		t.Fatalf("cache fixture: %d %s", code, errs)
	}
	for _, mode := range modes {
		for _, extra := range []string{"unexpected", "--unknown", "--json"} {
			t.Run(mode.flag+"/"+extra, func(t *testing.T) {
				out, errs, code := f.run(mode.flag, extra)
				if code != exitUsage || errs == "" {
					t.Fatalf("unused input accepted: code %d, stdout %q, stderr %q", code, out, errs)
				}
				if extra == "--json" {
					var problem errorJSON
					if err := json.Unmarshal([]byte(out), &problem); err != nil || problem.Exit != exitUsage {
						t.Fatalf("JSON error: %s (%v)", out, err)
					}
				}
			})
		}
	}
}

func TestIntegrationSubcallsRejectUnusedFlags(t *testing.T) {
	f := fixture(t)
	for _, action := range []string{"run", "stop", "reply", "approve", "discard", "diff"} {
		unused := []string{"page", "max-bytes"}
		if action == "diff" {
			unused = []string{"op", "question", "commit", "max-bytes"}
		}
		for _, flag := range unused {
			t.Run(action+"/"+flag, func(t *testing.T) {
				args := []string{"integration", "--protocol", "1", "--json", action}
				if action != "diff" {
					args = append(args, "--op", "op-unused000000001")
				}
				if action == "approve" {
					args = append(args, "--commit", strings.Repeat("a", 40))
				}
				args = append(args, "--"+flag, "16384", "--", "1")
				out, errs, code := f.runStdin("some input", args...)
				var problem errorJSON
				if err := json.Unmarshal([]byte(out), &problem); err != nil {
					t.Fatal(err)
				}
				if code != exitUsage || !strings.Contains(problem.Error, "--"+flag+" is not used") || errs == "" {
					t.Fatalf("unused flag was not rejected before lookup: %d %s %s", code, out, errs)
				}
			})
		}
	}
}
