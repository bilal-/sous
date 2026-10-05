package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func TestReaderRefusesMissingAndNewerSnapshots(t *testing.T) {
	st := &store.Store{Home: t.TempDir()}
	r := Reader{Store: st, Configured: true, Now: time.Now()}
	if _, err := r.Read(context.Background()); !errors.Is(err, ErrNotReady) {
		t.Fatalf("missing cache: %v", err)
	}
	path := filepath.Join(st.Home, "cache.json")
	raw := []byte(`{"version":999,"rendered_at":null,"board":null}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read(context.Background()); !errors.Is(err, store.ErrNewer) {
		t.Fatalf("newer cache: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(raw) {
		t.Fatal("a newer cache was overwritten")
	}
}

func TestReaderWithoutRootsKeepsPrivateNotesAndSaysUnconfigured(t *testing.T) {
	st := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: t.TempDir(), Name: "api"}
	now := time.Now().UTC()
	if _, _, err := thread.Note(st, p, thread.Me, "check retry fix", "human", now); err != nil {
		t.Fatal(err)
	}
	s, err := (Reader{Store: st, Now: now}).Read(context.Background())
	if err != nil || s.Configured || s.Available || s.Complete || s.AsOf != nil || len(s.Problems) == 0 || len(s.Items) != 1 {
		t.Fatalf("unconfigured must keep known commitments and say what is unknown: %+v %v", s, err)
	}
}

func TestReaderKeepsTasksWhenProjectDisappears(t *testing.T) {
	st := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: filepath.Join(t.TempDir(), "acme", "api"), Name: "api", Org: "acme"}
	if err := os.MkdirAll(p.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, _, err := thread.Note(st, p, thread.Me, "check retry fix", "human", now); err != nil {
		t.Fatal(err)
	}
	views, _ := thread.Open(st, now)
	if err := board.WriteCache(st, &board.Data{Projects: []project.Project{p}, Threads: views, Checked: 1, RenderedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p.Path); err != nil {
		t.Fatal(err)
	}
	s, err := (Reader{Store: st, Configured: true, Now: now.Add(time.Hour)}).Read(context.Background())
	if err != nil || s.Complete || len(s.Items) != 1 || !s.Items[0].Stale || len(s.Problems) == 0 || !s.AsOf.Equal(now) {
		t.Fatalf("missing project must preserve known work and uncertainty: %+v %v", s, err)
	}
	if slices.Contains(s.Items[0].Actions, "open") {
		t.Fatal("cannot open a missing checkout")
	}
}

func TestReaderMarksFiledStatusGapsIncomplete(t *testing.T) {
	for _, state := range []string{"error", "unknown"} {
		t.Run(state, func(t *testing.T) {
			st := &store.Store{Home: t.TempDir()}
			p := project.Project{Path: t.TempDir(), Name: "api"}
			now := time.Now().UTC()
			if _, _, err := thread.Note(st, p, thread.Me, "check retry timeout", "human", now); err != nil {
				t.Fatal(err)
			}
			views, _ := thread.Open(st, now)
			if err := board.WriteCache(st, &board.Data{Projects: []project.Project{p}, Threads: views, Checked: 1, RenderedAt: now}); err != nil {
				t.Fatal(err)
			}
			r := Reader{Store: st, Configured: true, Now: now, Reconcile: func(_ context.Context, views []thread.View) []thread.View {
				views[0].Upstream = state
				if state == "error" {
					views[0].UpstreamErr = "filed note could not be read"
				}
				return views
			}}
			s, err := r.Read(context.Background())
			if err != nil || s.Complete || len(s.Problems) == 0 || len(s.Items) != 1 || s.Items[0].Upstream.State != state {
				t.Fatalf("filed status uncertainty must stay explicit: %+v, %v", s, err)
			}
		})
	}
}

func TestReaderUsesLocalReconciliationAndStableNoteKeys(t *testing.T) {
	st := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: t.TempDir(), Name: "api"}
	now := time.Now().UTC()
	id, _, err := thread.NoteRun(st, p, "fix retry timeout", "retry", "codex", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRun(st, id, func(r *thread.Run) {
		r.Runner, r.Ref, r.State = "codex", "codex:example", thread.RunRunning
	}); err != nil {
		t.Fatal(err)
	}
	views, _ := thread.Open(st, now)
	if err := board.WriteCache(st, &board.Data{Projects: []project.Project{p}, Threads: views, Checked: 1, RenderedAt: now}); err != nil {
		t.Fatal(err)
	}
	called := false
	r := Reader{Store: st, Configured: true, Now: now.Add(time.Hour), Reconcile: func(ctx context.Context, views []thread.View) []thread.View {
		called = true
		views[0].Run.State, views[0].Run.Text = thread.RunNeedsYou, "which timeout?"
		return views
	}}
	s, err := r.Read(context.Background())
	if err != nil || !called || len(s.Items) != 1 || s.Items[0].Section != "on_you" || s.Items[0].Run.State != thread.RunNeedsYou || !slices.Contains(s.Items[0].Actions, "reply") {
		t.Fatalf("local run state not reflected: %+v %v", s, err)
	}
	if s.Items[0].Key != "n:"+views[0].UID || !s.AsOf.Equal(now) {
		t.Fatalf("identity or remote freshness changed: %+v", s.Items[0])
	}
}

func TestRevisionIgnoresPollingTimesWithoutMutatingTheSnapshot(t *testing.T) {
	now := time.Now().UTC()
	s := Snapshot{Kind: "snapshot", V: 0, Provider: "sous", ObservedAt: now,
		Items: []Item{{Key: "n:example", Item: board.Item{ID: "1", Run: &board.RunItem{State: thread.RunRunning, CheckedAt: &now}}}}}
	if err := seal(&s); err != nil {
		t.Fatal(err)
	}
	before := s.Revision
	later := now.Add(time.Second)
	s.ObservedAt, s.Items[0].Run.CheckedAt = later, &later
	if err := seal(&s); err != nil || s.Revision != before || s.Items[0].Run.CheckedAt == nil {
		t.Fatalf("polling clocks changed identity or were erased: %+v %v", s, err)
	}
	s.Items[0].Run.State = thread.RunNeedsYou
	if err := seal(&s); err != nil || s.Revision == before {
		t.Fatalf("a run transition must change revision: %v", err)
	}
}
