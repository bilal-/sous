package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func newEffortReaderFixture(t *testing.T, now time.Time) (*store.Store, project.Project, thread.Thread, string) {
	t.Helper()
	s := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: t.TempDir(), Name: "api"}
	id, _, err := thread.Note(s, p, thread.Me, "Publish schema", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := thread.FileAtomically(s, id, false, func(thread.Thread) (string, error) {
		return "md:FOLLOWUPS.md:1:abcd1234", nil
	}); err != nil {
		t.Fatal(err)
	}
	n, err := thread.Get(s, id)
	if err != nil {
		t.Fatal(err)
	}
	group, _, err := effort.Create(s, "Client launch", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := effort.Link(s, &config.Config{}, group.ID, effort.Changes{Notes: []int{id}}, false); err != nil {
		t.Fatal(err)
	}
	return s, p, n, group.ID
}

func TestReaderEffortsUseCurrentLocalTrackerObservation(t *testing.T) {
	for _, tc := range []struct {
		name, cached, current string
		unavailable           int
	}{
		{name: "recovery", cached: "error", current: "open"},
		{name: "new failure", cached: "open", current: "error", unavailable: 1},
		{name: "without cached board", current: "open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			asOf := now.Add(-time.Hour)
			s, p, n, _ := newEffortReaderFixture(t, now)
			if tc.cached != "" {
				view := thread.View{Thread: n, Upstream: tc.cached}
				if tc.cached == "error" {
					view.UpstreamErr = "previous local read failed"
				}
				if err := board.WriteCache(s, &board.Data{Projects: []project.Project{p}, Threads: []thread.View{view}, Checked: 1, RenderedAt: asOf}); err != nil {
					t.Fatal(err)
				}
			}
			r := Reader{Store: s, Configured: tc.cached != "", V: 1, Now: now,
				Reconcile: func(_ context.Context, views []thread.View) []thread.View {
					if len(views) != 1 || views[0].UID != n.UID {
						t.Fatalf("wrong reconciliation input: %+v", views)
					}
					views[0].Upstream, views[0].UpstreamErr = tc.current, ""
					if tc.current == "error" {
						views[0].UpstreamErr = "current local read failed"
					}
					return views
				}}
			got, err := r.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Items) != 1 || got.Items[0].Upstream == nil || got.Items[0].Upstream.State != tc.current {
				t.Fatalf("item did not reflect current local observation: %+v", got)
			}
			if len(got.Efforts) != 1 || got.Efforts[0].Open != 1 || got.Efforts[0].Completed != 0 || got.Efforts[0].Unavailable != tc.unavailable {
				t.Fatalf("effort disagrees with current item observation: %+v", got)
			}
			if tc.current == "open" && len(got.Efforts[0].Attention) != 0 || tc.current == "error" && len(got.Efforts[0].Attention) != 1 {
				t.Fatalf("effort uncertainty did not follow the current observation: %+v", got)
			}
			if tc.cached == "" {
				if got.Available || got.Configured || got.AsOf != nil {
					t.Fatalf("local observation invented a remote board snapshot: %+v", got)
				}
			} else if got.AsOf == nil || !got.AsOf.Equal(asOf) {
				t.Fatalf("local observation advanced remote snapshot age: %+v", got)
			}
		})
	}
}

func TestReaderEffortsReflectUpstreamCloseInSameSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	asOf := now.Add(-time.Hour)
	s, p, prerequisite, group := newEffortReaderFixture(t, now)
	dependent, _, err := thread.Note(s, p, thread.Me, "Ship client", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := effort.Link(s, &config.Config{}, group, effort.Changes{Notes: []int{dependent}}, false); err != nil {
		t.Fatal(err)
	}
	if err := effort.Depend(s, group, dependent, prerequisite.ID, false); err != nil {
		t.Fatal(err)
	}
	views, err := thread.Open(s, now)
	if err != nil {
		t.Fatal(err)
	}
	views[0].Upstream, views[0].UpstreamErr = "error", "previous local read failed"
	if err := board.WriteCache(s, &board.Data{Projects: []project.Project{p}, Threads: views, Checked: 1, RenderedAt: asOf}); err != nil {
		t.Fatal(err)
	}
	r := Reader{Store: s, Configured: true, V: 1, Now: now,
		Reconcile: func(_ context.Context, current []thread.View) []thread.View {
			if err := thread.CloseUpstreamClosed(s, prerequisite.ID, now); err != nil {
				t.Fatal(err)
			}
			kept := []thread.View{}
			for _, view := range current {
				if view.UID != prerequisite.UID {
					kept = append(kept, view)
				}
			}
			return kept
		}}
	got, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != fmt.Sprint(dependent) {
		t.Fatalf("closed prerequisite still shown as open: %+v", got)
	}
	if len(got.Efforts) != 1 || got.Efforts[0].Open != 1 || got.Efforts[0].Completed != 1 || got.Efforts[0].Blocked != 0 || got.Efforts[0].Unavailable != 0 || len(got.Efforts[0].Attention) != 0 {
		t.Fatalf("effort did not observe the persisted upstream close: %+v", got)
	}
	if !got.Complete || got.AsOf == nil || !got.AsOf.Equal(asOf) {
		t.Fatalf("local close retained uncertainty or advanced remote age: %+v", got)
	}
}
