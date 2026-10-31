package board

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
)

func TestBriefRetainsDecisionsAndConstraintsAcrossNewHandoffs(t *testing.T) {
	c := Context{Available: true}
	for i := 0; i < 8; i++ {
		c.Memories = append(c.Memories, memory.Record{ID: "m:handoff", Kind: memory.Handoff, Text: "Recent handoff"})
	}
	c.Memories = append(c.Memories, memory.Record{ID: "m:decision", Kind: memory.Decision, Text: "Use cursors"}, memory.Record{ID: "m:constraint", Kind: memory.Constraint, Text: "Stay offline"})
	var out bytes.Buffer
	renderContext(&out, c, true)
	if !strings.Contains(out.String(), "Use cursors") || !strings.Contains(out.String(), "Stay offline") || strings.Count(out.String(), "m:") != 6 {
		t.Fatalf("brief lost durable knowledge or exceeded its bound: %s", out.String())
	}
}

func TestMemoryListsNameTheOriginProject(t *testing.T) {
	var out bytes.Buffer
	RenderMemories(&out, memory.Result{TotalCount: 1, Records: []memory.Record{{ID: "m:1", Kind: memory.Decision, Scope: memory.Scope{Workspace: "work", Project: "/code/acme/api"}, Text: "Use cursors", Source: "docs/design.md"}}})
	if !strings.Contains(out.String(), "acme/api") || !strings.Contains(out.String(), "Use cursors") {
		t.Fatalf("ambiguous cross-project memory: %s", out.String())
	}
}

func TestContextFailureIsExplicitAndBriefIsBounded(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	sc := memory.Scope{Workspace: "work", Project: "/code/acme/api"}
	os.WriteFile(filepath.Join(s.Home, "memory.json"), []byte(`{"version":99}`), 0600)
	ctx := ProjectContext(s, sc, now, nil)
	if ctx.Available || len(ctx.Attention) == 0 {
		t.Fatalf("%#v", ctx)
	}
	os.Remove(filepath.Join(s.Home, "memory.json"))
	for i := range 8 {
		memory.Add(s, memory.Input{Scope: sc, Kind: memory.Decision, Text: strings.Repeat("important decision ", 30) + string(rune('a'+i)), Source: "design.md#decision"}, now.Add(time.Duration(i)*time.Minute))
	}
	ctx = ProjectContext(s, sc, now, nil)
	var b bytes.Buffer
	renderContext(&b, ctx, true)
	if len(ctx.Memories) != 8 || strings.Count(b.String(), "  m:") != 5 || !strings.Contains(b.String(), "sous recall") {
		t.Fatalf("%s", b.String())
	}
	for _, line := range strings.Split(b.String(), "\n") {
		if utf8.RuneCountInString(line) > text.LineRunes {
			t.Fatalf("long brief: %s", line)
		}
	}
}

func TestEffortProjectsDoNotImplicitlyShareMemory(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	c := config.Default()
	c.Roots = []string{"/code"}
	a := project.Project{Path: "/code/acme/api"}
	b := project.Project{Path: "/code/acme/site"}
	r, _, _ := memory.Add(s, memory.Input{Scope: memory.ForProject(c, a), Kind: memory.Lesson, Text: "private implementation detail"}, now)
	e, _, _ := effort.Create(s, "Launch", now)
	if err := effort.Link(s, c, e.ID, effort.Changes{Projects: []project.Project{b}, Memories: []string{r.ID}}, false); err != nil {
		t.Fatal(err)
	}
	ctx := ProjectContext(s, memory.ForProject(c, b), now, nil)
	if len(ctx.Efforts) != 1 || len(ctx.Memories) != 0 {
		t.Fatalf("implicit sharing: %#v", ctx)
	}
}

func TestEffortBlockersFollowCurrentNotesAndMissingMeansUnknown(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	c := config.Default()
	p := project.Project{Path: t.TempDir()}
	a, _, _ := thread.Note(s, p, thread.Me, "Publish invitations", "human", now)
	b, _, _ := thread.Note(s, p, thread.Them, "Approve design", "human", now)
	e, _, _ := effort.Create(s, "Launch", now)
	effort.Link(s, c, e.ID, effort.Changes{Notes: []int{a, b}}, false)
	effort.Depend(s, e.ID, a, b, false)
	e, _ = effort.Get(s, e.ID)
	v := DescribeEffort(s, e, now)
	if v.Open != 2 || v.Blocked != 1 {
		t.Fatalf("%#v", v)
	}
	thread.Done(s, b, now)
	v = DescribeEffort(s, e, now)
	if v.Open != 1 || v.Blocked != 0 || v.Completed != 1 {
		t.Fatalf("%#v", v)
	}
	os.WriteFile(filepath.Join(s.Home, "threads.json"), []byte(`{"version":99}`), 0600)
	v = DescribeEffort(s, e, now)
	if v.Unavailable == 0 || len(v.Attention) == 0 {
		t.Fatal("missing notes looked complete")
	}
}

func TestEffortUnavailableTrackerKeepsDependencyUnknown(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	c := &config.Config{}
	p := project.Project{Path: t.TempDir()}
	now := time.Now()
	a, _, _ := thread.Note(s, p, thread.Me, "Publish schema", "human", now)
	b, _, _ := thread.Note(s, p, thread.Me, "Ship client", "human", now)
	if _, err := thread.FileAtomically(s, a, false, func(thread.Thread) (string, error) { return "github:acme/api#12", nil }); err != nil {
		t.Fatal(err)
	}
	e, _, _ := effort.Create(s, "Client launch", now)
	if err := effort.Link(s, c, e.ID, effort.Changes{Notes: []int{a, b}}, false); err != nil {
		t.Fatal(err)
	}
	if err := effort.Depend(s, e.ID, b, a, false); err != nil {
		t.Fatal(err)
	}
	e, _ = effort.Get(s, e.ID)
	v := DescribeEffort(s, e, now)
	if v.Unavailable != 1 || len(v.Blockers) != 1 || !v.Blockers[0].Unknown {
		t.Fatalf("tracker failure looked known: %#v", v)
	}
}

func TestScopedEffortsHideUnrelatedFailureDetailsAndRetainStaleSummary(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	views := []EffortSummary{
		{ID: "e:1", Name: "API launch", Projects: []memory.Scope{{Project: "/code/acme/api"}}, Attention: []string{"note 1 unavailable"}},
		{ID: "e:2", Name: "Private research", Projects: []memory.Scope{{Project: "/other/research"}}, Attention: []string{"private document missing"}},
	}
	if err := WriteCache(s, &Data{Efforts: views, RenderedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Home, "efforts.json"), []byte(`{"version":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, problems := EffortSummaries(s, now, nil)
	got, problems = scopeEfforts(got, problems, map[string]bool{"/code/acme/api": true})
	if len(got) != 1 || got[0].ID != "e:1" || !got[0].Stale || len(problems) != 2 || strings.Contains(strings.Join(problems, " "), "private") {
		t.Fatalf("lost stale data or leaked other scope: %#v %v", got, problems)
	}
}
