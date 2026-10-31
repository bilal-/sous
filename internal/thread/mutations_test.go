package thread

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
)

func TestPrivateMutationReceiptsSurviveLaterActions(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	id, _, err := Note(s, project.Project{Path: "/code/acme/api"}, Me, "check result", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	if already, err := SnoozeOnce(s, id, 7, now, "snooze-first"); err != nil || already {
		t.Fatal(already, err)
	}
	if err := Snooze(s, id, 30, now); err != nil {
		t.Fatal(err)
	}
	later, _ := Get(s, id)
	if already, err := SnoozeOnce(s, id, 7, now.Add(time.Hour), "snooze-first"); err != nil || !already {
		t.Fatal(already, err)
	}
	got, _ := Get(s, id)
	if !got.SnoozedUntil.Equal(*later.SnoozedUntil) {
		t.Fatal("replay undid later snooze")
	}
	if _, err := SnoozeOnce(s, id, 8, now, "snooze-first"); !errors.Is(err, ErrOpReused) {
		t.Fatal(err)
	}
	if _, err := DoneOnce(s, id, now, "snooze-first"); !errors.Is(err, ErrOpReused) {
		t.Fatal(err)
	}
	closed := now.Add(2 * time.Hour)
	_, err = store.Modify[Doc](s, name, Migrator{}, func(d *Doc) error {
		th, _ := d.find(id, false)
		th.Closed = &closed
		th.ClosedBy = "upstream"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if already, err := DoneOnce(s, id, now.Add(3*time.Hour), "done-first"); err != nil || !already {
		t.Fatal(already, err)
	}
	if already, err := DoneRecorded(s, id, "done-first"); err != nil || !already {
		t.Fatal(already, err)
	}
	if already, err := SnoozeOnce(s, id, 7, now.Add(4*time.Hour), "snooze-first"); err != nil || !already {
		t.Fatal(already, err)
	}
	got, _ = Get(s, id)
	if got.ClosedBy != "upstream" || !got.Closed.Equal(closed) {
		t.Fatal("closure provenance changed", got)
	}
	if _, err := DoneRecorded(s, id+1, "done-first"); !errors.Is(err, ErrOpReused) {
		t.Fatal(err)
	}
}

func TestMutationReceiptUpgradeAndFutureRefusal(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ref := "md:FOLLOWUPS.md:abcdef123456"
	th := Thread{ID: 7, UID: "abcdef123456", Project: "/code/acme/api", Kind: Me, Text: "inspect result", Since: at, Ref: &ref, Run: &Run{Revision: 9, Runner: "fake", Ref: "ref", State: RunDone}}
	old := Doc{Version: 5, NextID: 8, Threads: []Thread{th}}
	raw, _ := json.Marshal(old)
	path := filepath.Join(s.Home, "threads.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if already, err := SnoozeOnce(s, 7, 7, at, "upgrade-snooze"); err != nil || already {
		t.Fatal(already, err)
	}
	d, err := store.Load[Doc](s, name, Migrator{})
	if err != nil || d.Version != 6 || d.NextID != 8 || len(d.MutationOps) != 1 || d.Threads[0].Run.Revision != 9 || *d.Threads[0].Ref != ref {
		t.Fatal(d, err)
	}
	future := []byte(`{"version":99,"next_id":8,"threads":[]}`)
	if err := os.WriteFile(path, future, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := DoneOnce(s, 7, at, "future-done"); !errors.Is(err, store.ErrNewer) {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, future) {
		t.Fatal("future state changed", err)
	}
}
