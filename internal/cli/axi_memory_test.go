package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestAXI4RecallKeepsTotalAndRetrievalHint(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	for i := range 3 {
		if _, errs, code := f.runIn(p, "remember", fmt.Sprintf("Decision %d", i)); code != 0 {
			t.Fatal(code, errs)
		}
	}
	out, _, code := f.runIn(p, "recall", "Decision", "--limit", "1")
	if code != 0 || !strings.Contains(out, "3 memories") || !strings.Contains(out, "showing 1") || !strings.Contains(out, "without --limit") {
		t.Fatalf("%d %s", code, out)
	}
	out, _, _ = f.runIn(p, "recall", "--limit", "1", "--json")
	var result struct {
		Total     int               `json:"total_count"`
		Truncated bool              `json:"truncated"`
		Records   []json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Total != 3 || !result.Truncated || len(result.Records) != 1 {
		t.Fatalf("%v %s", err, out)
	}
}

// Execute the printed hint from another repo, including shell quoting.
func TestAXI9MemoryHintsKeepTheirProject(t *testing.T) {
	f := fixture(t)
	api := f.mkrepo("acme/api's service", true)
	site := f.mkrepo("acme/site", true)
	f.runIn(api, "remember", "API decision")
	for _, args := range [][]string{{"here", api, "--brief"}, {"recall", "--show", "m:1"}} {
		out, _, code := f.runIn(site, args...)
		if code != 0 {
			t.Fatal(code, out)
		}
		var hint string
		for _, line := range strings.Split(out, "\n") {
			if start := strings.Index(line, "sous recall"); start >= 0 && strings.Contains(line[start:], "--history") {
				hint, _, _ = strings.Cut(line[start:], " for ")
			}
		}
		if hint == "" {
			t.Fatalf("no history hint: %s", out)
		}
		// A fake sous delegates to this test binary, never the installed CLI.
		f.bin("sous", "exec \"$SOUS_HINT_BINARY\" \"$@\"")
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("SOUS_HINT_BINARY", binary)
		cmd := exec.Command("/bin/sh", "-c", hint)
		cmd.Dir = site
		got, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(got), "API decision") {
			t.Fatalf("hint %q: %v %s", hint, err, got)
		}
	}
}

func TestAXI6EmptyRecallSelectorsFail(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	f.runIn(p, "remember", "API decision")
	for _, flag := range []string{"--show", "--workspace", "-p", "--effort", "--kind"} {
		out, errs, code := f.runIn(p, "recall", flag, "", "--json")
		if code != 2 || !strings.Contains(out, `"error"`) || errs == "" {
			t.Fatalf("%s: %d %s %s", flag, code, out, errs)
		}
	}
}

func TestAXI3RememberRejectsTruncatedInput(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/api", true)
	body := "first" + strings.Repeat(" ", 32770) + "DO NOT DROP"
	if out, errs, code := f.runStdin(body, "remember", "-p", "acme/api", "-", "--json"); code != 2 || !strings.Contains(out, `"error"`) {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	if out, _, _ := f.run("recall", "--all"); !strings.Contains(out, "0 memories") {
		t.Fatalf("partial input was saved: %s", out)
	}
}
