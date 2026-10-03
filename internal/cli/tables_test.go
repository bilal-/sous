package cli

import (
	"slices"
	"testing"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/launcher"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/tracker"
)

// The tables agree: every built in tracker has a backend that files into
// it and a signal that finds work in it, with a link for its items; every
// agent tool is a launcher, and a runner when it can run headless. A new
// row in one table that is missing from another fails here.
func TestTablesAgree(t *testing.T) {
	for _, k := range tracker.Kinds {
		if !slices.Contains(backend.Registry.Names(), k.Name) || !slices.Contains(signal.Registry.Names(), k.Name) {
			t.Errorf("tracker %s needs a built in backend and signal of that name", k.Name)
		}
		if k.Tool == "" || k.URL == nil || k.Check == nil || len(k.PublicHosts) == 0 {
			t.Errorf("tracker %s is incomplete: %+v", k.Name, k)
		}
	}
	for _, h := range harness.All {
		if !slices.Contains(launcher.Registry.Names(), h.Name) {
			t.Errorf("%s is not a launcher", h.Name)
		}
		if runs := slices.Contains(runner.Registry.Names(), h.Name); runs != (h.Headless != nil) {
			t.Errorf("%s: a runner exactly when it runs headless (runner %v)", h.Name, runs)
		}
	}
}
