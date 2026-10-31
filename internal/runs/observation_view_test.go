package runs

import (
	"bytes"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func observationView(t *testing.T) (*Dispatcher, thread.View) {
	t.Helper()
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	id, _, err := thread.NoteRun(s, project.Project{Path: "/code/acme/api"}, "task", "task", "fake", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRun(s, id, func(r *thread.Run) { r.Ref = "fake:1"; r.State = thread.RunRunning }); err != nil {
		t.Fatal(err)
	}
	th, err := thread.Get(s, id)
	if err != nil {
		t.Fatal(err)
	}
	return &Dispatcher{Store: s, Now: func() time.Time { return now }}, thread.View{Thread: th}
}

func TestObservationKeepsCurrentNoteFields(t *testing.T) {
	d, v := observationView(t)
	if err := thread.Edit(d.Store, v.ID, "current wording"); err != nil {
		t.Fatal(err)
	}
	if err := thread.Snooze(d.Store, v.ID, 7, d.Now()); err != nil {
		t.Fatal(err)
	}
	d.record(&v, runner.Status{State: runner.Done, Text: "done"})
	if v.Text != "current wording" || !v.Snoozed || v.Run.State != thread.RunDone || v.RunErr != "" {
		t.Fatalf("view lost current note state: %+v", v)
	}
}

func TestFailedObservationWriteKeepsKnownState(t *testing.T) {
	d, v := observationView(t)
	path := filepath.Join(d.Store.Home, "threads.json")
	future := []byte(`{"version":99,"next_id":2,"threads":[]}`)
	if err := os.WriteFile(path, future, 0600); err != nil {
		t.Fatal(err)
	}
	d.record(&v, runner.Status{State: runner.Done, Text: "unrecorded result"})
	if v.Run.State != thread.RunRunning || v.Run.Text != "" || v.RunErr == "" {
		t.Fatalf("unrecorded status escaped into view: %+v", v)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, future) {
		t.Fatal("future state changed")
	}
}
