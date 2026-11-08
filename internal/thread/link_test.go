package thread

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
)

func linkFixture(t *testing.T) (*store.Store, time.Time, int, int) {
	t.Helper()
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	a, _, err := Note(s, project.Project{Path: "/code/acme/api"}, Me, "ship the endpoint", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := Note(s, project.Project{Path: "/code/acme/site"}, Me, "ship the form", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	return s, now, a, b
}

func TestAWaitShowsUntilItsPrerequisiteCloses(t *testing.T) {
	s, now, a, b := linkFixture(t)
	if changed, err := Link(s, b, Links{After: []int{a}}); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := Link(s, b, Links{After: []int{a}}); err != nil || changed {
		t.Fatalf("linking again should change nothing: %v %v", changed, err)
	}
	waits := func() []int {
		open, err := Open(s, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range open {
			if v.ID == b {
				return v.WaitsOn
			}
		}
		t.Fatal("note gone")
		return nil
	}
	if got := waits(); !slices.Equal(got, []int{a}) {
		t.Fatalf("waits on %v, want [%d]", got, a)
	}
	if err := Done(s, a, now); err != nil {
		t.Fatal(err)
	}
	if got := waits(); len(got) != 0 {
		t.Fatalf("a closed prerequisite still blocks: %v", got)
	}
}

func TestAWaitRefusesItselfCyclesAndClosedNotes(t *testing.T) {
	s, now, a, b := linkFixture(t)
	if _, err := Link(s, a, Links{After: []int{a}}); !errors.As(err, new(ValidationError)) {
		t.Fatalf("waiting on itself: %v", err)
	}
	if _, err := Link(s, b, Links{After: []int{a}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(s, a, Links{After: []int{b}}); !errors.As(err, new(ValidationError)) {
		t.Fatalf("a cycle was allowed: %v", err)
	}
	c, _, _ := Note(s, project.Project{Path: "/code/acme/api"}, Me, "old", "human", now)
	if err := Done(s, c, now); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(s, b, Links{After: []int{c}}); !errors.As(err, new(ClosedError)) {
		t.Fatalf("waiting on a closed note: %v", err)
	}
	if _, err := Link(s, b, Links{After: []int{99}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("waiting on no note: %v", err)
	}
}

func TestLinkingARefKeepsItAndRefusesAnother(t *testing.T) {
	s, _, a, _ := linkFixture(t)
	ref := "github:acme/api#12"
	if changed, err := Link(s, a, Links{Ref: ref}); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := Link(s, a, Links{Ref: ref}); err != nil || changed {
		t.Fatal(changed, err)
	}
	if _, err := Link(s, a, Links{Ref: "github:acme/api#13"}); !errors.Is(err, ErrLinked) {
		t.Fatalf("a second ref replaced the first: %v", err)
	}
	got, _ := Get(s, a)
	if got.Ref == nil || *got.Ref != ref {
		t.Fatalf("ref = %v", got.Ref)
	}
	if changed, err := Unlink(s, a, Links{Ref: "*"}); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if got, _ := Get(s, a); got.Ref != nil {
		t.Fatalf("ref kept after unlink: %v", *got.Ref)
	}
}

func TestUnlinkingAWait(t *testing.T) {
	s, _, a, b := linkFixture(t)
	if _, err := Link(s, b, Links{After: []int{a}}); err != nil {
		t.Fatal(err)
	}
	if changed, err := Unlink(s, b, Links{After: []int{a}}); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := Unlink(s, b, Links{After: []int{a}}); err != nil || changed {
		t.Fatal(changed, err)
	}
	if got, _ := Get(s, b); len(got.After) != 0 {
		t.Fatalf("after = %v", got.After)
	}
}

func TestARunCannotBeLinkedToATicket(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	id, _, err := NoteRun(s, project.Project{Path: "/code/acme/api"}, "fix it", "", "claude", "human", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Link(s, id, Links{Ref: "github:acme/api#1"}); !errors.As(err, new(ValidationError)) {
		t.Fatalf("a run was linked to a ticket: %v", err)
	}
}

func TestVersion6NotesUpgradeWithNoWaits(t *testing.T) {
	home := t.TempDir()
	raw := `{"version":6,"next_id":2,"threads":[{"id":1,"uid":"abc","project":"/code/acme/api","remote":null,"text":"x","kind":"me","since":"2026-10-01T00:00:00Z","source":"human","ref":null,"snoozed_until":null,"closed":null}]}`
	if err := os.WriteFile(filepath.Join(home, name+".json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &store.Store{Home: home}
	if _, err := Open(s, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Link(s, 1, Links{Ref: "github:acme/api#3"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, name+".json"))
	var d struct{ Version int }
	if err := json.Unmarshal(b, &d); err != nil || d.Version != 7 {
		t.Fatalf("version %d: %v", d.Version, err)
	}
}
