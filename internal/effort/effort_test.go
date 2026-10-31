package effort

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func TestNewerOrBrokenEffortsRemainUnavailable(t *testing.T) {
	for _, raw := range []string{`{"version":99}`, `broken`} {
		s := &store.Store{Home: t.TempDir()}
		p := filepath.Join(s.Home, "efforts.json")
		os.WriteFile(p, []byte(raw), 0600)
		if _, err := All(s); err == nil {
			t.Fatal("unavailable looked empty")
		}
		if _, _, err := Create(s, "Launch invitations", time.Now()); err == nil {
			t.Fatal("overwrote state")
		}
		b, _ := os.ReadFile(p)
		if string(b) != raw {
			t.Fatal("state changed")
		}
	}
}

func TestLinksAreRetryableAndUseStableNoteIdentity(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	c := config.Default()
	c.Roots = []string{"/code"}
	p := project.Project{Path: "/code/acme/api"}
	n, _, _ := thread.Note(s, p, thread.Me, "Publish invitation contract", "human", now)
	r, _, _ := memory.Add(s, memory.Input{Scope: memory.ForProject(c, p), Kind: memory.Decision, Text: "Invitations expire in a day"}, now)
	e, existed, err := Create(s, "Launch invitations", now)
	if err != nil || existed {
		t.Fatalf("%v %v", existed, err)
	}
	again, existed, err := Create(s, "launch invitations", now)
	if err != nil || !existed || again.ID != e.ID {
		t.Fatal("duplicate effort")
	}
	change := Changes{Notes: []int{n}, Memories: []string{r.ID}, Refs: []string{"design.md#invitations"}}
	for range 2 {
		if err := Link(s, c, e.ID, change, false); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := Get(s, e.ID)
	if len(got.Notes) != 1 || got.Notes[0].UID == "" || len(got.Memories) != 1 || len(got.Projects) != 1 || len(got.Refs) != 1 {
		t.Fatalf("%#v", got)
	}
	if err := Link(s, c, e.ID, change, true); err != nil {
		t.Fatal(err)
	}
	got, _ = Get(s, e.ID)
	if len(got.Notes)+len(got.Memories)+len(got.Refs) != 0 {
		t.Fatal("unlink retained references")
	}
	if err := Archive(s, e.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if err := Archive(s, e.ID, false, now); err != nil {
		t.Fatal(err)
	}
}

func TestDependencyCyclesRejectedAndNeverChangeCommitments(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	c := config.Default()
	p := project.Project{Path: "/code/acme/api"}
	a, _, _ := thread.Note(s, p, thread.Me, "Publish API", "human", now)
	b, _, _ := thread.Note(s, p, thread.Them, "Approve design", "human", now)
	e, _, _ := Create(s, "Launch", now)
	if err := Link(s, c, e.ID, Changes{Notes: []int{a, b}}, false); err != nil {
		t.Fatal(err)
	}
	if err := Depend(s, e.ID, a, b, false); err != nil {
		t.Fatal(err)
	}
	if err := Depend(s, e.ID, a, b, false); err != nil {
		t.Fatal("dependency retry failed")
	}
	if err := Depend(s, e.ID, b, a, false); err == nil {
		t.Fatal("accepted dependency cycle")
	}
	if err := Depend(s, e.ID, a, 999, false); err == nil {
		t.Fatal("accepted unlinked blocker")
	}
	before, _ := thread.Get(s, a)
	if before.Kind != thread.Me || before.Closed != nil {
		t.Fatal("dependency changed commitment")
	}
	if err := Depend(s, e.ID, a, b, true); err != nil {
		t.Fatal(err)
	}
	got, _ := Get(s, e.ID)
	if len(got.Dependencies) != 0 {
		t.Fatal("dependency not removed")
	}
}
