package thread

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

func TestReviewsRequireAReasonAndRespectKeep(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	old := now.Add(-ReviewInterval)
	recent := now.Add(-time.Hour)
	closed := now.Add(-time.Minute)
	for _, tc := range []struct {
		name string
		view View
		want bool
	}{
		{"old", View{Thread: Thread{Since: old}}, true},
		{"fresh", View{Thread: Thread{Since: recent}}, false},
		{"closed", View{Thread: Thread{Since: old, Closed: &closed}}, false},
		{"snoozed", View{Thread: Thread{Since: old}, Snoozed: true}, false},
		{"running", View{Thread: Thread{Since: old, Run: &Run{State: RunRunning}}}, false},
		{"starting", View{Thread: Thread{Since: old, Run: &Run{State: RunStarting}}}, false},
		{"run done", View{Thread: Thread{Since: recent, Run: &Run{State: RunDone}}}, true},
		{"run failed", View{Thread: Thread{Since: recent, Run: &Run{State: RunFailed}}}, true},
		{"missing", View{Thread: Thread{Since: recent}, Upstream: "unknown"}, true},
		{"unavailable", View{Thread: Thread{Since: recent}, Upstream: "error"}, true},
		{"kept", View{Thread: Thread{Since: old, ReviewedAt: &recent}}, false},
		{"kept outcome", View{Thread: Thread{Since: old, ReviewedAt: &recent}, Upstream: "unknown"}, false},
		{"week elapsed", View{Thread: Thread{Since: old.Add(-time.Hour), ReviewedAt: &old}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Reviews([]View{tc.view}, now)
			if (len(got) == 1) != tc.want || len(got) == 1 && got[0].Reason == "" {
				t.Fatalf("%+v", got)
			}
		})
	}
	views := []View{
		{Thread: Thread{ID: 2, Since: old}},
		{Thread: Thread{ID: 3, Since: old.Add(-time.Hour)}},
		{Thread: Thread{ID: 1, Since: old}},
	}
	got := Reviews(views, now)
	if got[0].ID != 3 || got[1].ID != 1 || got[2].ID != 2 {
		t.Fatalf("oldest review first, id breaks ties: %+v", got)
	}
}

func TestMigrateV3KeepsNotesAndStartsReviewFromTheirAge(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	raw := `{"version":3,"next_id":8,"threads":[{"id":7,"uid":"000000000007","project":"/code/acme/api","text":"verify the fix","kind":"me","since":"2026-09-01T12:00:00Z","source":"human","ref":"md:FOLLOWUPS.md:000000000007"}]}`
	if err := os.WriteFile(filepath.Join(s.Home, "threads.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := store.Load[Doc](s, "threads", Migrator{})
	if err != nil || d.Version != 4 || d.NextID != 8 || len(d.Threads) != 1 || d.Threads[0].UID != "000000000007" || d.Threads[0].ReviewedAt != nil || d.Threads[0].Ref == nil {
		t.Fatalf("%+v %v", d, err)
	}
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if got := Reviews([]View{{Thread: d.Threads[0]}}, now); len(got) != 1 {
		t.Fatalf("existing notes must participate in review: %+v", got)
	}
	if err := Keep(s, 7, now); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.Home, "threads.json"))
	var saved Doc
	if err := json.Unmarshal(b, &saved); err != nil || saved.Threads[0].ReviewedAt == nil || !saved.Threads[0].ReviewedAt.Equal(now) || saved.Threads[0].Closed != nil {
		t.Fatalf("%s %v", b, err)
	}
}
