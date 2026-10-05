package board

import (
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func TestCurrentCacheAppliesSignalSnoozeWithoutRescanning(t *testing.T) {
	st := &store.Store{Home: t.TempDir()}
	now := time.Now().UTC()
	p := project.Project{Path: "/code/acme/api"}
	id := "s:000000000001"
	col := signal.Collected{Plugins: []signal.PluginStatus{{Name: "git", Status: signal.StatusOK}},
		Signals: []signal.Tagged{{Signal: signal.Signal{ID: id, Project: p.Path, Kind: signal.Unfinished, Text: "one stash"}, Plugin: "git"}}}
	observations, err := signal.Observe(st, col, []string{p.Path}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteCache(st, &Data{Projects: []project.Project{p}, Signals: observations, Checked: 1, RenderedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := signal.Snooze(st, id); err != nil {
		t.Fatal(err)
	}
	cache, err := ReadCurrentCache(st, now.Add(time.Hour))
	if err != nil || len(cache.Data.Signals) != 1 || !cache.Data.Signals[0].Snoozed || !cache.Data.RenderedAt.Equal(now) {
		t.Fatalf("a local snooze did not update the cached view: %+v %v", cache, err)
	}
	if got := Classify(cache.Data); len(got.Unfinished) != 0 || len(got.Snoozed) != 1 {
		t.Fatalf("snoozed work was lost or still visible: %+v", got)
	}
}

func TestCurrentCacheKeepsRemoteUncertaintyAndCurrentLocalNotes(t *testing.T) {
	st := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	p := project.Project{Path: "/code/acme/api"}
	id, _, err := thread.Note(st, p, thread.Me, "check the retry fix", "human", now.Add(-8*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := thread.FileAtomically(st, id, true, func(thread.Thread) (string, error) { return "github:acme/api#12", nil }); err != nil {
		t.Fatal(err)
	}
	views, _ := thread.Open(st, now)
	views[0].Upstream, views[0].UpstreamErr = "error", "tracker unavailable"
	d := &Data{Threads: views, Checked: 1, RenderedAt: now, Signals: []signal.Observed{{Tagged: signal.Tagged{Signal: signal.Signal{ID: "s:000000000001", Project: p.Path, Kind: signal.Me, Text: "review requested"}, Plugin: "github"}, FirstSeen: now, Stale: true}}}
	if err := WriteCache(st, d); err != nil {
		t.Fatal(err)
	}
	if err := thread.Keep(st, id, now); err != nil {
		t.Fatal(err)
	}
	c, err := ReadCurrentCache(st, now.Add(time.Hour))
	if err != nil || len(c.Data.Threads) != 1 || c.Data.Threads[0].ReviewedAt == nil || c.Data.Threads[0].Upstream != "error" || c.Data.Threads[0].UpstreamErr != "tracker unavailable" || !c.Data.Signals[0].Stale || !c.Data.RenderedAt.Equal(now) {
		t.Fatalf("current local notes must preserve remote uncertainty: %+v %v", c, err)
	}
	if err := thread.Done(st, id, now); err != nil {
		t.Fatal(err)
	}
	id2, _, err := thread.Note(st, p, thread.Me, "a new task", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	c, err = ReadCurrentCache(st, now)
	if err != nil || len(c.Data.Threads) != 1 || c.Data.Threads[0].ID != id2 || len(c.Data.Signals) != 1 || !c.Data.Signals[0].Stale {
		t.Fatalf("%+v %v", c, err)
	}
}
