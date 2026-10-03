package cli

import (
	"os"
	"slices"
	"strings"
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

// The docs name every agent tool wherever they list them: a paragraph that
// names Claude Code and Codex names them all (unless it is about one), and a list of agent names
// (`claude`, `codex`, ...) has every one. A new harness the docs do not
// mention fails here.
func TestDocsNameEveryAgent(t *testing.T) {
	for _, file := range []string{"../../README.md", "../../docs/commands.md"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, para := range strings.Split(string(b), "\n\n") {
			para = strings.Join(strings.Fields(para), " ")
			if strings.HasPrefix(para, "**") && strings.Contains(para[:min(len(para), 20)], ".**") {
				continue // a section about one agent
			}
			for _, h := range harness.All {
				if strings.Contains(para, "Claude Code") && strings.Contains(para, "Codex") && !strings.Contains(para, h.Display) {
					t.Errorf("%s: names Claude Code and Codex but not %s: %.120s", file, h.Display, para)
				}
				if strings.Contains(para, "`claude`, `codex`") && !strings.Contains(para, "`"+h.Name+"`") {
					t.Errorf("%s: lists agents without `%s`: %.120s", file, h.Name, para)
				}
			}
		}
	}
}
