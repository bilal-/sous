package memory

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
)

var moment = time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
var api = Scope{Workspace: "work", Project: "/code/acme/api", Remote: "git.example.org/acme/api"}
var site = Scope{Workspace: "work", Project: "/code/acme/site", Remote: "git.example.org/acme/site"}
var personal = Scope{Workspace: "personal", Project: "/home/sam/code/library"}

func TestUnavailableAndNewerStateAreNeverEmptyOrOverwritten(t *testing.T) {
	for _, body := range []string{`{broken`, `{"version":9,"records":[]}`} {
		s := &store.Store{Home: t.TempDir()}
		path := filepath.Join(s.Home, "memory.json")
		os.WriteFile(path, []byte(body), 0600)
		if _, err := Search(s, Query{Scope: api}); err == nil {
			t.Fatal("unavailable looked empty")
		}
		if _, _, err := Add(s, Input{Scope: api, Kind: Decision, Text: "Use cursors"}, moment); err == nil {
			t.Fatal("overwrote unreadable state")
		}
		after, _ := os.ReadFile(path)
		if string(after) != body {
			t.Fatal("changed unavailable state")
		}
	}
}

func TestScopeAndExplicitSharingSurviveAgentChanges(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	r, _, err := Add(s, Input{Scope: api, Kind: Lesson, Text: "Use idempotency keys", Source: "STATUS.md#retry", Author: "agent"}, moment)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range []Scope{site, personal, {Workspace: "personal", Project: api.Project, Remote: api.Remote}} {
		got, err := Search(s, Query{Scope: sc})
		if err != nil || len(got) != 0 {
			t.Fatalf("leaked into %#v: %v %v", sc, got, err)
		}
	}
	if err := Share(s, r.ID, site, true); err != nil {
		t.Fatal(err)
	}
	got, _ := Search(s, Query{Scope: site, Text: "idempotency"})
	if len(got) != 1 || got[0].Source != "STATUS.md#retry" || got[0].Scope != api {
		t.Fatalf("lost provenance: %#v", got)
	}
	if err := Share(s, r.ID, site, false); err != nil {
		t.Fatal(err)
	}
	got, _ = Search(s, Query{Scope: site})
	if len(got) != 0 {
		t.Fatal("unsharing left context visible")
	}
	moved := api
	moved.Project = "/other/acme/api"
	got, _ = Search(s, Query{Scope: moved})
	if len(got) != 1 {
		t.Fatal("same project remote lost its history")
	}
}

func TestSupersessionRetryVerificationAndForget(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	old, _, _ := Add(s, Input{Scope: api, Kind: Decision, Text: "Use offset pagination", Source: "design.md#old"}, moment)
	in := Input{Scope: api, Kind: Decision, Text: "Use cursor pagination", Source: "design.md#new", Key: "pagination-v2", Supersedes: old.ID}
	current, already, err := Add(s, in, moment.Add(time.Hour))
	if err != nil || already {
		t.Fatalf("%v %v", already, err)
	}
	again, already, err := Add(s, in, moment.Add(2*time.Hour))
	if err != nil || !already || again.ID != current.ID {
		t.Fatal("retry duplicated decision")
	}
	changed := in
	changed.Text = "Use something else"
	if _, _, err := Add(s, changed, moment); !errors.Is(err, ErrConflict) {
		t.Fatalf("key conflict: %v", err)
	}
	got, _ := Search(s, Query{Scope: api, Text: "pagination"})
	if len(got) != 1 || got[0].ID != current.ID {
		t.Fatalf("obsolete decision recalled: %#v", got)
	}
	history, _ := Search(s, Query{Scope: api, History: true})
	if len(history) != 2 {
		t.Fatal("lost earlier decision")
	}
	if err := Verify(s, current.ID, "tests/pagination_test.go", moment.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	verified, _ := Get(s, current.ID)
	if verified.VerifiedAt == nil || verified.VerificationSource != "tests/pagination_test.go" {
		t.Fatal("no verification evidence")
	}
	if err := Forget(s, current.ID, moment); err != nil {
		t.Fatal(err)
	}
	if err := Forget(s, current.ID, moment); err != nil {
		t.Fatal("forget is not retryable")
	}
	forgotten, _ := Get(s, current.ID)
	if forgotten.Text != "" || forgotten.Source != "" || forgotten.VerificationSource != "" {
		t.Fatal("forgotten content retained")
	}
	_, already, err = Add(s, in, moment)
	if err != nil || !already {
		t.Fatal("replay resurrected forgotten content")
	}
	got, _ = Search(s, Query{Scope: api})
	if len(got) != 0 {
		t.Fatal("forget resurrected superseded decision")
	}
}

func TestCrossProjectSupersessionAndBadKindsAreRejected(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	r, _, _ := Add(s, Input{Scope: api, Kind: Decision, Text: "Use cursors"}, moment)
	for _, in := range []Input{{Scope: site, Kind: Decision, Text: "Use offsets", Supersedes: r.ID}, {Scope: api, Kind: "task", Text: "Do work"}, {Scope: api, Kind: Lesson, Text: " "}} {
		if _, _, err := Add(s, in, moment); err == nil {
			t.Fatalf("accepted %#v", in)
		}
	}
}

func TestConcurrentIdenticalCapturesProduceOneRecord(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := Add(s, Input{Scope: api, Kind: Handoff, Text: "Tests passed; review next"}, moment); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := Search(s, Query{Scope: api})
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestWorkspaceUsesExplicitSettingOrContainingRoot(t *testing.T) {
	c := config.Default()
	c.Roots = []string{"/code", "/code/work"}
	p := project.Project{Path: "/code/work/acme/api", Org: "acme", Name: "api"}
	if got := ForProject(c, p); got.Workspace != "/code/work" {
		t.Fatalf("%#v", got)
	}
	c.Projects = map[string]map[string]string{"acme/*": {"workspace": "work"}}
	if got := ForProject(c, p); got.Workspace != "work" {
		t.Fatalf("%#v", got)
	}
}
