package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func TestEffortProjectionPreservesHistoryAndIdentity(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now().UTC()
	p := project.Project{Path: t.TempDir()}
	sc := memory.ForProject(&config.Config{}, p)
	add := func(text string) thread.Thread {
		id, _, err := thread.Note(s, p, thread.Me, text, "human", now)
		if err != nil {
			t.Fatal(err)
		}
		n, err := thread.Get(s, id)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	open := add("open")
	closed := add("closed")
	snoozed := add("snoozed")
	if err := thread.Done(s, closed.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := thread.Snooze(s, snoozed.ID, 1, now); err != nil {
		t.Fatal(err)
	}
	_, err := thread.FileAtomically(s, open.ID, false, func(thread.Thread) (string, error) { return "github:acme/api#1", nil })
	if err != nil {
		t.Fatal(err)
	}
	open, err = thread.Get(s, open.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteCache(s, &Data{Threads: []thread.View{{Thread: open, Upstream: "unknown"}}, RenderedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	old, _, err := memory.Add(s, memory.Input{Scope: sc, Kind: memory.Decision, Text: "old", Author: "human"}, now)
	if err != nil {
		t.Fatal(err)
	}
	newer, _, err := memory.Add(s, memory.Input{Scope: sc, Kind: memory.Decision, Text: "new", Author: "human", Supersedes: old.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	forgotten, _, err := memory.Add(s, memory.Input{Scope: sc, Kind: memory.Lesson, Text: "retired", Author: "human"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.Forget(s, forgotten.ID, now); err != nil {
		t.Fatal(err)
	}
	e := effort.Effort{ID: "e:1", Name: "Launch", Notes: []effort.NoteRef{{ID: open.ID, UID: open.UID}, {ID: closed.ID, UID: closed.UID}, {ID: snoozed.ID, UID: snoozed.UID}, {ID: open.ID, UID: "wrong-uid"}}, Memories: []effort.MemoryRef{{ID: old.ID}, {ID: forgotten.ID}}, Dependencies: []effort.Dependency{{NoteUID: open.UID, AfterUID: closed.UID}, {NoteUID: snoozed.UID, AfterUID: open.UID}}}
	got := DescribeEffort(s, e, now)
	if got.Open != 2 || got.Completed != 1 || got.Unavailable != 2 || got.Blocked != 1 || len(got.Items) != 3 || len(got.Memories) != 2 {
		t.Fatalf("counts/history changed: %+v", got)
	}
	if got.Items[0].ID != "1" || got.Items[1].ClosedAt == nil || !got.Items[2].Snoozed || !got.Blockers[0].Unknown || got.AsOf == nil || !got.AsOf.Equal(now.Add(-time.Hour)) {
		t.Fatalf("identity/order/state changed: %+v", got)
	}
	if got.Memories[0].SupersededBy != newer.ID || got.Memories[1].ForgottenAt == nil {
		t.Fatal("effort links lost memory history")
	}
}
func TestEffortDoesNotConsultUnlinkedStores(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	for _, name := range []string{"threads", "memory"} {
		if err := os.WriteFile(filepath.Join(s.Home, name+".json"), []byte(`{"version":99}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := effort.Effort{ID: "e:1", Name: "Empty"}
	got := DescribeEffort(s, e, time.Now())
	if got.Unavailable != 0 || len(got.Attention) != 0 {
		t.Fatalf("unlinked corrupt stores leaked into effort: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(s.Home, "cache.json"), []byte(`{"version":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	got = DescribeEffort(s, e, time.Now())
	if len(got.Attention) != 1 || !strings.HasPrefix(got.Attention[0], "cached tracker state unavailable:") {
		t.Fatalf("cache uncertainty disappeared: %+v", got)
	}
}
