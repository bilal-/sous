package thread

import (
	"bytes"
	"encoding/json"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func observationFixture(t *testing.T) (*store.Store, Thread) {
	t.Helper()
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	id, _, err := NoteRun(s, project.Project{Path: "/code/acme/api"}, "task", "task", "fake", SourceHuman, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetRun(s, id, func(r *Run) { r.Ref, r.State = "fake:1", RunRunning }); err != nil {
		t.Fatal(err)
	}
	th, err := Get(s, id)
	if err != nil {
		t.Fatal(err)
	}
	return s, th
}

func TestRunObservationUsesOwnedRevision(t *testing.T) {
	s, before := observationFixture(t)
	if err := SetRun(s, before.ID, func(*Run) {}); err != nil {
		t.Fatal(err)
	}
	called := false
	current, matched, err := ObserveRun(s, before, func(r *Run) { called = true; r.State = RunDone })
	if err != nil || matched || called || current.Run.State != RunRunning || current.Run.Revision != before.Run.Revision+1 {
		t.Fatalf("stale observation: %+v %v %v called=%v", current.Run, matched, err, called)
	}
	path := filepath.Join(s.Home, "threads.json")
	saved, _ := os.ReadFile(path)
	unchanged, matched, err := ObserveRun(s, current, nil)
	after, _ := os.ReadFile(path)
	if err != nil || !matched || unchanged.Run.Revision != current.Run.Revision || !bytes.Equal(saved, after) {
		t.Fatalf("failed poll changed state: %+v %v %v", unchanged.Run, matched, err)
	}
	accepted, matched, err := ObserveRun(s, current, func(r *Run) { r.State = RunDone; r.Revision = 0 })
	if err != nil || !matched || accepted.Run.State != RunDone || accepted.Run.Revision != current.Run.Revision+1 {
		t.Fatalf("accepted observation: %+v %v %v", accepted.Run, matched, err)
	}
}

func TestRunObservationKeepsCurrentNoteAndClosure(t *testing.T) {
	s, before := observationFixture(t)
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if err := Edit(s, before.ID, "current wording"); err != nil {
		t.Fatal(err)
	}
	if err := Snooze(s, before.ID, 7, now); err != nil {
		t.Fatal(err)
	}
	current, matched, err := ObserveRun(s, before, func(r *Run) { r.Text = "current status" })
	if err != nil || !matched || current.Text != "current wording" || current.SnoozedUntil == nil {
		t.Fatalf("note edit lost: %+v %v %v", current, matched, err)
	}
	before = current
	if err := Done(s, current.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := MarkCleaned(s, current.ID, now); err != nil {
		t.Fatal(err)
	}
	called := false
	current, matched, err = ObserveRun(s, before, func(*Run) { called = true })
	if err != nil || matched || called || current.Closed == nil || current.Run.Cleaned == nil {
		t.Fatalf("closed run changed: %+v %v %v", current, matched, err)
	}
}

func TestRetryOfNeverStartedRunAdvancesRevision(t *testing.T) {
	s, th := observationFixture(t)
	if err := SetRun(s, th.ID, func(r *Run) { r.Ref = ""; r.State = RunFailed }); err != nil {
		t.Fatal(err)
	}
	before, _ := Get(s, th.ID)
	id, existed, err := NoteRun(s, project.Project{Path: th.Project}, "task", "task", "other", SourceHuman, time.Now())
	current, getErr := Get(s, id)
	if err != nil || getErr != nil || existed || id != th.ID || current.Run.Revision != before.Run.Revision+1 || current.Run.State != RunStarting {
		t.Fatalf("retry: %+v %v %v", current.Run, err, getErr)
	}
}

func TestRunObservationBindsNoteIdentityAndDoesNotWrap(t *testing.T) {
	s, before := observationFixture(t)
	wrong := before
	wrong.UID = "different-note"
	called := false
	if _, matched, err := ObserveRun(s, wrong, func(*Run) { called = true }); err != nil || matched || called {
		t.Fatalf("wrong identity: %v %v", matched, err)
	}
	if _, err := store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error { d.Threads[0].Run.Revision = ^uint64(0); return nil }); err != nil {
		t.Fatal(err)
	}
	before, _ = Get(s, before.ID)
	if err := SetRun(s, before.ID, func(*Run) { called = true }); err == nil || called {
		t.Fatalf("exhausted action: %v", err)
	}
	if _, _, err := ObserveRun(s, before, func(*Run) { called = true }); err == nil || called {
		t.Fatalf("exhausted observation: %v", err)
	}
	current, _ := Get(s, before.ID)
	if current.Run.Revision != ^uint64(0) {
		t.Fatal("revision wrapped")
	}
}

func TestRunRevisionUpgradePreservesExistingData(t *testing.T) {
	s, th := observationFixture(t)
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	th.ReviewedAt = &now
	th.Run.Checked, th.Run.Cleaned, th.Run.Tidied = &now, &now, &now
	th.Run.Publication = &RunPublication{Commit: "abc123"}
	th.Run.Revision = 0
	doc := Doc{Version: 4, NextID: th.ID + 1, Threads: []Thread{th}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Home, "threads.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	upgraded, err := store.Load[Doc](s, name, Migrator{})
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Version != (Migrator{}).Current() {
		t.Fatalf("version %d", upgraded.Version)
	}
	want, _ := json.Marshal(th)
	got, _ := json.Marshal(upgraded.Threads[0])
	if !bytes.Equal(want, got) {
		t.Fatalf("upgrade changed run/provenance: %s != %s", got, want)
	}
	future := []byte(`{"version":99,"next_id":2,"threads":[]}`)
	if err := os.WriteFile(filepath.Join(s.Home, "threads.json"), future, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetRun(s, th.ID, func(*Run) {}); err == nil {
		t.Fatal("future state accepted")
	}
	after, _ := os.ReadFile(filepath.Join(s.Home, "threads.json"))
	if !bytes.Equal(after, future) {
		t.Fatal("future state rewritten")
	}
}
