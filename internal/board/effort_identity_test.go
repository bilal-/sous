package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/memory"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

func writeEffortFixture(t *testing.T, s *store.Store, name string, doc any) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Home, name+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEffortProjectionUsesFirstDuplicateRecord(t *testing.T) {
	for _, laterUID := range []string{"first-note", "other-note"} {
		t.Run(laterUID, func(t *testing.T) {
			s := &store.Store{Home: t.TempDir()}
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			p := t.TempDir()
			writeEffortFixture(t, s, "threads", thread.Doc{Version: 4, NextID: 8, Threads: []thread.Thread{
				{ID: 7, UID: "first-note", Project: p, Kind: thread.Me, Text: "first note", Since: now},
				{ID: 7, UID: laterUID, Project: p, Kind: thread.Me, Text: "later note", Since: now},
			}})
			writeEffortFixture(t, s, "memory", memory.Doc{Version: 1, NextID: 2, Records: []memory.Record{
				{ID: "m:1", Text: "first memory", Kind: memory.Lesson, CreatedAt: now},
				{ID: "m:1", Text: "later memory", Kind: memory.Lesson, CreatedAt: now},
			}})
			e := effort.Effort{ID: "e:1", Name: "Launch", Notes: []effort.NoteRef{{ID: 7, UID: "first-note"}}, Memories: []effort.MemoryRef{{ID: "m:1"}}}
			got := DescribeEffort(s, e, now)
			if got.Unavailable != 0 || len(got.Attention) != 0 || len(got.Items) != 1 || got.Items[0].Text != "first note" || len(got.Memories) != 1 || got.Memories[0].Text != "first memory" {
				t.Fatalf("first matching record was not retained: %+v", got)
			}
		})
	}
}

func TestEffortCachedTrackerSuccessRequiresMatchingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, uid, ref string
		want           string
	}{
		{"matching", "note-uid", "github:acme/api#7", "open"},
		{"different uid", "other-uid", "github:acme/api#7", "error"},
		{"different ref", "note-uid", "github:acme/api#8", "error"},
		{"missing ref", "note-uid", "", "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &store.Store{Home: t.TempDir()}
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			ref := "github:acme/api#7"
			n := thread.Thread{ID: 7, UID: "note-uid", Project: t.TempDir(), Kind: thread.Me, Text: "Review schema", Since: now, Ref: &ref}
			writeEffortFixture(t, s, "threads", thread.Doc{Version: 4, NextID: 8, Threads: []thread.Thread{n}})
			old := n
			old.UID = tc.uid
			old.Ref = nil
			if tc.ref != "" {
				ref := tc.ref
				old.Ref = &ref
			}
			if err := WriteCache(s, &Data{Threads: []thread.View{{Thread: old, Upstream: "open"}}, RenderedAt: now.Add(-time.Hour)}); err != nil {
				t.Fatal(err)
			}
			got := DescribeEffort(s, effort.Effort{ID: "e:1", Name: "Launch", Notes: []effort.NoteRef{{ID: 7, UID: n.UID}}}, now)
			if len(got.Items) != 1 || got.Items[0].Upstream == nil || got.Items[0].Upstream.State != tc.want {
				t.Fatalf("cached state from mismatched identity: %+v", got)
			}
			if tc.want == "error" {
				if got.Unavailable != 1 || got.Items[0].Upstream.Error != "not checked; run sous --refresh" || !reflect.DeepEqual(got.Attention, []string{"note 7 tracker status unavailable"}) {
					t.Fatalf("mismatched cache did not remain uncertain: %+v", got)
				}
			} else if got.Unavailable != 0 || len(got.Attention) != 0 {
				t.Fatalf("matching cached success was lost: %+v", got)
			}
		})
	}
}

func TestEffortAttentionFollowsSourceAndLinkOrder(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	writeEffortFixture(t, s, "cache", map[string]int{"version": 99})
	writeEffortFixture(t, s, "threads", thread.Doc{Version: 4, NextID: 1, Threads: []thread.Thread{}})
	writeEffortFixture(t, s, "memory", memory.Doc{Version: 1, NextID: 1, Records: []memory.Record{}})
	root := t.TempDir()
	missingZ := filepath.Join(root, "zeta")
	missingA := filepath.Join(root, "alpha")
	e := effort.Effort{ID: "e:1", Name: "Launch", Notes: []effort.NoteRef{{ID: 17, UID: "missing-z"}, {ID: 2, UID: "missing-a"}}, Memories: []effort.MemoryRef{{ID: "m:17"}, {ID: "m:2"}}, Projects: []memory.Scope{{Project: missingZ}, {Project: missingA}}}
	got := DescribeEffort(s, e, now)
	want := []string{"note 17 unavailable", "note 2 unavailable", "memory m:17 unavailable", "memory m:2 unavailable", "project unavailable: " + missingZ, "project unavailable: " + missingA}
	if len(got.Attention) != len(want)+1 || !strings.HasPrefix(got.Attention[0], "cached tracker state unavailable: ") || !reflect.DeepEqual(got.Attention[1:], want) || got.Unavailable != 6 {
		t.Fatalf("attention order or unavailable count changed: %+v", got)
	}
}

func TestEffortEmptySelectionsIgnoreUnlinkedStores(t *testing.T) {
	for _, archived := range []bool{false, true} {
		name := "empty"
		if archived {
			name = "archived only"
		}
		t.Run(name, func(t *testing.T) {
			s := &store.Store{Home: t.TempDir()}
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			es := []effort.Effort{}
			if archived {
				es = append(es, effort.Effort{ID: "e:1", Name: "Past launch", ArchivedAt: &now, Notes: []effort.NoteRef{{ID: 1, UID: "note-uid"}}, Memories: []effort.MemoryRef{{ID: "m:1"}}})
			}
			writeEffortFixture(t, s, "efforts", effort.Doc{Version: 1, NextID: 2, Efforts: es})
			for _, name := range []string{"threads", "memory", "cache"} {
				writeEffortFixture(t, s, name, map[string]int{"version": 99})
			}
			views, problems := EffortSummaries(s, now, nil)
			if views == nil || len(views) != 0 || problems == nil || len(problems) != 0 {
				t.Fatalf("unselected records consulted private stores: %+v %v", views, problems)
			}
		})
	}
	t.Run("other project", func(t *testing.T) {
		s := &store.Store{Home: t.TempDir()}
		now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		a := memory.Scope{Workspace: "acme", Project: filepath.Join(t.TempDir(), "api")}
		b := memory.Scope{Workspace: "acme", Project: filepath.Join(t.TempDir(), "web")}
		writeEffortFixture(t, s, "efforts", effort.Doc{Version: 1, NextID: 2, Efforts: []effort.Effort{{ID: "e:1", Name: "API launch", Projects: []memory.Scope{a}, Notes: []effort.NoteRef{{ID: 1, UID: "note-uid"}}, Memories: []effort.MemoryRef{{ID: "m:1"}}}}})
		writeEffortFixture(t, s, "memory", memory.Doc{Version: 1, NextID: 1, Records: []memory.Record{}})
		for _, name := range []string{"threads", "cache"} {
			writeEffortFixture(t, s, name, map[string]int{"version": 99})
		}
		got := ProjectContext(s, b, now, nil)
		if !got.Available || len(got.Efforts) != 0 || len(got.Attention) != 0 {
			t.Fatalf("unrelated effort leaked unavailable state: %+v", got)
		}
	})
}
