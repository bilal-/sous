package signal

import (
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

func collected(text string, okPlugins ...string) Collected {
	c := Collected{Signals: []Tagged{{Signal: Signal{ID: "s:x", Project: "/p", Kind: Unfinished, Text: text}, Plugin: "git"}}}
	for _, n := range okPlugins {
		c.Plugins = append(c.Plugins, PluginStatus{Name: n, Status: "ok"})
	}
	return c
}

// ran is what a plugin that ran fine and found sigs reports.
func ran(plugin string, sigs ...Signal) Collected {
	c := Collected{Plugins: []PluginStatus{{Name: plugin, Status: StatusOK}}}
	for _, sg := range sigs {
		c.Signals = append(c.Signals, Tagged{Signal: sg, Plugin: plugin})
	}
	return c
}

func TestObserveLifecycle(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	obs, err := Observe(s, collected("1 files uncommitted", "git"), []string{"/p"}, t0)
	if err != nil || len(obs) != 1 || !obs[0].FirstSeen.Equal(t0) || obs[0].Snoozed || obs[0].Stale {
		t.Fatalf("first: %+v %v", obs, err)
	}
	obs, _ = Observe(s, collected("1 files uncommitted", "git"), []string{"/p"}, t0.Add(48*time.Hour))
	if !obs[0].FirstSeen.Equal(t0) {
		t.Fatalf("first_seen must persist: %+v", obs[0])
	}
	if err := Snooze(s, "s:x"); err != nil {
		t.Fatal(err)
	}
	obs, _ = Observe(s, collected("1 files uncommitted", "git"), []string{"/p"}, t0.Add(49*time.Hour))
	if !obs[0].Snoozed {
		t.Fatal("snoozed should hide")
	}
	obs, _ = Observe(s, collected("2 files uncommitted", "git"), []string{"/p"}, t0.Add(50*time.Hour))
	if obs[0].Snoozed || !obs[0].FirstSeen.Equal(t0) {
		t.Fatalf("change must un-snooze but keep first_seen: %+v", obs[0])
	}
	// Signal gone (plugin ok, no findings) → dropped.
	obs, _ = Observe(s, Collected{Plugins: []PluginStatus{{Name: "git", Status: "ok"}}}, []string{"/p"}, t0.Add(51*time.Hour))
	if len(obs) != 0 {
		t.Fatalf("gone signal should drop: %+v", obs)
	}
	if err := Snooze(s, "s:x"); err == nil {
		t.Fatal("snoozing an unobserved id should error")
	}
	// Plugin failed → last-known re-emitted stale.
	Observe(s, collected("review", "git"), []string{"/p"}, t0)
	obs, _ = Observe(s, Collected{Plugins: []PluginStatus{{Name: "git", Status: "failed"}}}, []string{"/p"}, t0.Add(time.Hour))
	if len(obs) != 1 || !obs[0].Stale || obs[0].Text != "review" {
		t.Fatalf("stale: %+v", obs)
	}
}

// A scoped scan (one org folder) must not wipe snoozes or first_seen for
// projects that simply weren't scanned this run.
func TestObserveScopedScanPreservesUnscanned(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	full := ran("git",
		Signal{ID: "s:a", Project: "/ws/a", Kind: Unfinished, Text: "1 files uncommitted"},
		Signal{ID: "s:b", Project: "/ws/b", Kind: Unfinished, Text: "2 stashes"},
	)
	Observe(s, full, []string{"/ws/a", "/ws/b"}, t0)
	Snooze(s, "s:a")

	// Scoped run: only /ws/b scanned.
	scoped := Collected{Signals: full.Signals[1:], Plugins: full.Plugins}
	obs, _ := Observe(s, scoped, []string{"/ws/b"}, t0.Add(time.Hour))
	if len(obs) != 1 || obs[0].ID != "s:b" {
		t.Fatalf("scoped output must only cover scanned projects: %+v", obs)
	}

	// Full run again: s:a still snoozed, first_seen still t0.
	obs, _ = Observe(s, full, []string{"/ws/a", "/ws/b"}, t0.Add(2*time.Hour))
	var a *Observed
	for i := range obs {
		if obs[i].ID == "s:a" {
			a = &obs[i]
		}
	}
	if a == nil || !a.Snoozed || !a.FirstSeen.Equal(t0) {
		t.Fatalf("scoped scan wiped state: %+v", a)
	}
}

// first_seen is when the *condition* began, not when its text last changed:
// touching a second file must not reset "uncommitted for 3 weeks" to 0m.
// A change does clear a snooze.
func TestObserveFirstSeenSurvivesTextChange(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	Observe(s, collected("1 files uncommitted", "git"), []string{"/p"}, t0)
	Snooze(s, "s:x")
	obs, _ := Observe(s, collected("2 files uncommitted", "git"), []string{"/p"}, t0.Add(72*time.Hour))
	if obs[0].Snoozed || !obs[0].FirstSeen.Equal(t0) {
		t.Fatalf("want un-snoozed with first_seen preserved: %+v", obs[0])
	}
}

// Review 2 I3: an obligation's age is how long it has been waiting, not
// when sous first noticed it. A PR updated 2d ago is 2d old on first sight.
func TestObserveAgeUsesPluginObserved(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	c := Collected{Signals: []Tagged{{Signal: Signal{ID: "s:pr", Project: "/p", Kind: Me, Text: "review requested", Observed: now.Add(-48 * time.Hour)}, Plugin: "github"}},
		Plugins: []PluginStatus{{Name: "github", Status: "ok"}}}
	obs, _ := Observe(s, c, []string{"/p"}, now)
	if !obs[0].FirstSeen.Equal(now.Add(-48 * time.Hour)) {
		t.Fatalf("want first_seen 2d ago, got %s", obs[0].FirstSeen)
	}
	// An entry first seen before the plugin learned its real age is corrected too.
	Observe(s, Collected{Signals: []Tagged{{Signal: Signal{ID: "s:old", Project: "/p", Kind: Me, Text: "x"}, Plugin: "github"}}, Plugins: c.Plugins}, []string{"/p"}, now)
	c2 := Collected{Signals: []Tagged{{Signal: Signal{ID: "s:old", Project: "/p", Kind: Me, Text: "x", Observed: now.Add(-5 * 24 * time.Hour)}, Plugin: "github"}}, Plugins: c.Plugins}
	obs, _ = Observe(s, c2, []string{"/p"}, now)
	for _, o := range obs {
		if o.ID == "s:old" && !o.FirstSeen.Equal(now.Add(-5*24*time.Hour)) {
			t.Fatalf("existing entry must take the earlier observed: %s", o.FirstSeen)
		}
	}
}

// Observations for signals not seen in 30 days are pruned, so observed.json
// does not grow forever with vanished projects and removed plugins.
func TestObservePrunesStaleEntries(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	Observe(s, collected("old", "git"), []string{"/p"}, t0)
	// A different project's signal, from a plugin that later disappears from config.
	Observe(s, Collected{Signals: []Tagged{{Signal: Signal{ID: "s:gone", Project: "/q", Kind: Me, Text: "x"}, Plugin: "jira"}}, Plugins: []PluginStatus{{Name: "jira", Status: "ok"}}}, []string{"/q"}, t0)
	// 40 days later: /p is scanned (git ok), /q is out of scope and jira is not configured.
	Observe(s, Collected{Plugins: []PluginStatus{{Name: "git", Status: "ok"}}}, []string{"/p"}, t0.Add(40*24*time.Hour))
	d, _ := store.Load[ObsDoc](s, "observed", ObsMigrator{})
	if _, ok := d.Signals["s:gone"]; ok {
		t.Fatal("entry unseen for 40 days must be pruned")
	}
	// But within the window, out-of-scope entries survive (scoped scans).
	Observe(s, collected("kept", "git"), []string{"/p"}, t0)
	Observe(s, Collected{Plugins: []PluginStatus{{Name: "git", Status: "ok"}}}, []string{"/other"}, t0.Add(5*24*time.Hour))
	d, _ = store.Load[ObsDoc](s, "observed", ObsMigrator{})
	if _, ok := d.Signals["s:x"]; !ok {
		t.Fatal("recent out-of-scope entry must survive")
	}
}

// Review F2: what earlier scans learned about one project, derived the way
// Observe derives it (snooze included), with no other project's rows.
func TestKnown(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	c := ran("github",
		Signal{ID: "s:pr", Project: "/code/acme/chime", Kind: Me, Text: "review requested"},
		Signal{ID: "s:other", Project: "/code/acme/api", Kind: Me, Text: "review requested"},
	)
	Observe(s, c, nil, t0)
	Snooze(s, "s:pr")
	known, err := Known(s, "/code/acme/chime")
	if err != nil || len(known) != 1 || known[0].ID != "s:pr" || known[0].Plugin != "github" || !known[0].Snoozed || !known[0].FirstSeen.Equal(t0) {
		t.Fatalf("%+v %v", known, err)
	}
}

// A snooze ends when the thing changes, even if its one line summary does
// not ("1 files uncommitted" before and after more edits).
func TestSnoozeEndsWhenStateChanges(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	with := func(state string) Collected {
		c := collected("1 files uncommitted", "git")
		c.Signals[0].State = state
		return c
	}
	Observe(s, with("a"), []string{"/p"}, t0)
	Snooze(s, "s:x")
	if obs, _ := Observe(s, with("a"), []string{"/p"}, t0); !obs[0].Snoozed {
		t.Fatal("same state stays snoozed")
	}
	if obs, _ := Observe(s, with("b"), []string{"/p"}, t0); obs[0].Snoozed {
		t.Fatal("new state ends the snooze")
	}
}

func TestSnoozeByUniquePrefix(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	c := Collected{Plugins: []PluginStatus{{Name: "git", Status: "ok"}}, Signals: []Tagged{
		{Signal: Signal{ID: "s:0a70aaaaaaaa", Project: "/p", Kind: Unfinished, Text: "a"}, Plugin: "git"},
		{Signal: Signal{ID: "s:0a71bbbbbbbb", Project: "/p", Kind: Unfinished, Text: "b"}, Plugin: "git"},
	}}
	Observe(s, c, []string{"/p"}, time.Now())
	if err := Snooze(s, "s:0a7"); err == nil || !strings.Contains(err.Error(), "s:0a70aaaaaaaa") {
		t.Fatalf("ambiguous prefix must list matches: %v", err)
	}
	if err := Snooze(s, "s:0a70"); err != nil {
		t.Fatal(err)
	}
	if obs, _ := Known(s, "/p"); !obs[0].Snoozed || obs[1].Snoozed {
		t.Fatalf("%+v", obs)
	}
}

func TestSnoozePrefixTooShortSaysSo(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	Observe(s, collected("x", "git"), []string{"/p"}, time.Now())
	if err := Snooze(s, "s:x"); err != nil {
		t.Fatal("an exact id of any length works:", err)
	}
	if err := Snooze(s, "s:a"); err == nil || !strings.Contains(err.Error(), "at least 3") {
		t.Fatalf("%v", err)
	}
}
