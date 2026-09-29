// Package signaltest checks a signal plugin against the contract every
// signal shares, the way backendtest checks backends. It runs the plugin
// exactly as sous does, through the same door.
package signaltest

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/signal"
)

// Run scans projects twice with the plugin at argv (for example
// {"/path/to/sous-signal-todo", "scan"}) and checks the contract:
//
//   - it exits 0 and prints at least one finding (give it projects that
//     have something to report, so an empty plugin cannot pass)
//   - every line is a finding in contract version 0
//   - ids start with "s:" and are unique in one scan
//   - every finding is for one of the projects given
//   - kinds are me, them, unfinished or info
//   - the same projects give the same ids and texts again (ids are stable)
func Run(t *testing.T, argv []string, projects []string) {
	t.Helper()
	first := scan(t, argv, projects)
	if len(first) == 0 {
		t.Fatal("the plugin reported nothing: give it projects that have something to report")
	}
	seen := map[string]bool{}
	for _, s := range first {
		switch {
		case !strings.HasPrefix(s.ID, "s:"):
			t.Errorf("id %q does not start with s:", s.ID)
		case seen[s.ID]:
			t.Errorf("id %q appears twice in one scan", s.ID)
		case !slices.Contains(projects, s.Project):
			t.Errorf("%s: project %q was not one it was given", s.ID, s.Project)
		case !slices.Contains([]signal.Kind{signal.Me, signal.Them, signal.Unfinished, signal.Info}, s.Kind):
			t.Errorf("%s: kind %q is not me, them, unfinished or info", s.ID, s.Kind)
		case s.Text == "":
			t.Errorf("%s: no text", s.ID)
		}
		seen[s.ID] = true
	}
	// Ids must be stable; the order of lines is not part of the contract.
	byID := func(sigs []signal.Signal) map[string]string {
		m := map[string]string{}
		for _, s := range sigs {
			m[s.ID] = s.Text
		}
		return m
	}
	if a, b := byID(first), byID(scan(t, argv, projects)); !maps.Equal(a, b) {
		t.Errorf("a second scan of the same projects differs: ids must be stable\nfirst:  %v\nsecond: %v", a, b)
	}
}

func scan(t *testing.T, argv []string, projects []string) []signal.Signal {
	t.Helper()
	res := plugin.Exec(context.Background(), argv, []byte(strings.Join(projects, "\n")+"\n"), 15*time.Second)
	if res.TimedOut || res.Err != nil || res.Code != 0 {
		t.Fatalf("the plugin failed: exit %d, %v, %s", res.Code, res.Err, res.Stderr)
	}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		var raw map[string]json.RawMessage
		if line != "" && (json.Unmarshal([]byte(line), &raw) != nil || string(raw["v"]) != "0") {
			t.Fatalf("not a finding in contract version 0 (every line carries \"v\":0):\n%s", line)
		}
	}
	sigs, bad := signal.ReadLinesLenient(strings.NewReader(res.Stdout))
	if bad > 0 {
		t.Fatalf("%d lines were not findings in contract version 0:\n%s", bad, res.Stdout)
	}
	return sigs
}
