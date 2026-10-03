package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/tracker"
)

// A gh and a glab that both hang are each reported once the time is up,
// and a GitHub account config.toml names is a problem, not a note.
func TestTrackerChecksDoNotWaitForever(t *testing.T) {
	testutil.FakeBin(t, "gh", "sleep 2")
	testutil.FakeBin(t, "glab", "sleep 2")
	tracker.ResetCache()
	t.Cleanup(tracker.ResetCache)
	old := trackerTimeout
	trackerTimeout = 300 * time.Millisecond
	t.Cleanup(func() { trackerTimeout = old })
	start := time.Now()
	cs := trackerChecks(&config.Config{GitLabHosts: []string{"git.example.org"}})
	if time.Since(start) > 3*time.Second {
		t.Fatalf("waited %s", time.Since(start))
	}
	var got []string
	for _, c := range cs {
		got = append(got, c.Name+"|"+string(c.Status)+"|"+c.Detail)
	}
	if len(cs) != 2 || !strings.Contains(got[0], "did not answer") || !strings.Contains(got[1], "did not answer") {
		t.Fatalf("%q", got)
	}
}

func TestConfiguredAccountFailingIsAProblem(t *testing.T) {
	testutil.FakeBin(t, "gh", `case "$*" in
  "auth status") exit 0;;
  "auth token --user work-account") exit 1;;
esac`)
	tracker.ResetCache()
	t.Cleanup(tracker.ResetCache)
	cs := trackerChecks(&config.Config{Projects: map[string]map[string]string{"acme/*": {config.KeyGitHubAccount: "work-account"}}})
	if Count(cs, Bad) == 0 {
		t.Fatalf("%+v", cs)
	}
}

// Not logged in to gh, with no accounts configured, is a note: the board
// passes over a tracker that is not set up.
func TestTrackerNotSetUpIsANote(t *testing.T) {
	testutil.OnlyGit(t) // and no glab
	testutil.FakeBin(t, "gh", `echo "You are not logged into any GitHub hosts." >&2; exit 1`)
	tracker.ResetCache()
	t.Cleanup(tracker.ResetCache)
	cs := trackerChecks(&config.Config{})
	if Count(cs, Bad) != 0 || Count(cs, Warn) != 1 {
		t.Fatalf("%+v", cs)
	}
}

// A plugin sous would skip for its name is caught, not called fine.
func TestPluginWithTheWrongName(t *testing.T) {
	dir := t.TempDir()
	good, bad := filepath.Join(dir, "sous-signal-linear"), filepath.Join(dir, "linear")
	for _, p := range []string{good, bad} {
		os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	}
	cs := pluginChecks([]string{good, bad})
	if cs[0].Status != OK || cs[1].Status != Bad || !strings.Contains(cs[1].Detail, "sous-signal-") {
		t.Fatalf("%+v", cs)
	}
}

// The sources the last refresh could not read are listed with the reason:
// that is what the board's ? means.
func TestLastRefreshFailuresAreListed(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	why := "gh: HTTP 401"
	board.WriteCache(&store.Store{Home: home}, &board.Data{RenderedAt: now, Plugins: []signal.PluginStatus{
		{Name: "git", Status: signal.StatusOK},
		{Name: "github", Status: signal.StatusFailed, Error: &why},
	}})
	cs := boardChecks(Inputs{SousHome: home, Now: now})
	if len(cs) != 2 || cs[0].Status != OK || cs[1].Name != "last refresh: github" || !strings.Contains(cs[1].Detail, why) {
		t.Fatalf("%+v", cs)
	}
}

// The agent a built in runner needs: runs are optional, so missing is a
// note, with the fix.
func TestRunnerChecks(t *testing.T) {
	testutil.OnlyGit(t)
	testutil.FakeBin(t, "codex", "")
	cs := runnerChecks(t.TempDir())
	got := map[string]Check{}
	for _, c := range cs {
		got[c.Name] = c
	}
	if got["runner claude"].Status != Warn || !strings.Contains(got["runner claude"].Fix, "sous config runner") || got["runner codex"].Status != OK {
		t.Fatalf("%+v", cs)
	}
}
