package board

import (
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
	"testing"
	"time"
)

func TestCacheDoesNotRestoreFailureAcrossRunMutation(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	id, _, err := thread.NoteRun(s, project.Project{Path: "/code/acme/api"}, "task", "task", "fake", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRun(s, id, func(r *thread.Run) { r.Ref, r.State, r.Checked = "fake:1", thread.RunRunning, &now }); err != nil {
		t.Fatal(err)
	}
	views, err := thread.Open(s, now)
	if err != nil {
		t.Fatal(err)
	}
	views[0].RunErr = "earlier runner failure"
	if err := WriteCache(s, &Data{Threads: views, RenderedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRun(s, id, func(*thread.Run) {}); err != nil {
		t.Fatal(err)
	}
	cached, err := ReadCurrentCache(s, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := cached.Data.Threads[0].RunErr; got != "" {
		t.Fatalf("cached failure crossed an accepted action: %s", got)
	}
}
