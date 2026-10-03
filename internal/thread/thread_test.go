package thread

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
)

func TestNoteLifecycle(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	remote := "github.com/acme/chime"
	p := project.Project{Path: "/ws/acme/chime", Org: "acme", Name: "chime", Remote: &remote}
	q := project.Project{Path: "/ws/studio/work", Org: "studio", Name: "work"}

	id, _, err := Note(s, q, Me, "need final copy for the pricing page", "human", now)
	if err != nil || id != 1 {
		t.Fatal(id, err)
	}
	id, _, _ = Note(s, p, Idea, "notifications need context, not 'unused terminal'", "agent", now)
	if id != 2 {
		t.Fatal(id)
	}
	if _, _, err := Note(s, p, Kind("urgent"), "x", "human", now); err == nil {
		t.Fatal("bad kind accepted")
	}
	if _, _, err := Note(s, p, Idea, "", "human", now); err == nil {
		t.Fatal("empty text accepted")
	}

	open, _ := Open(s, now.Add(6*24*time.Hour))
	if len(open) != 2 || !open[0].Since.Equal(now) || open[1].Source != "agent" || *open[1].Remote != remote {
		t.Fatalf("open: %+v", open)
	}

	if err := Edit(s, 1, "chase designer Friday"); err != nil {
		t.Fatal(err)
	}
	if err := SetKind(s, 2, Me); err != nil {
		t.Fatal(err)
	}
	if err := Snooze(s, 2, 3, now); err != nil {
		t.Fatal(err)
	}
	open, _ = Open(s, now.Add(time.Hour))
	if len(open) != 2 || open[0].Snoozed || !open[1].Snoozed {
		t.Fatalf("a snoozed note is open, marked snoozed: %+v", open)
	}
	open, _ = Open(s, now.Add(4*24*time.Hour))
	if len(open) != 2 || open[1].Snoozed {
		t.Fatal("snooze should expire after 3 days")
	}
	fp, _ := ForProject(s, "/elsewhere/chime", remote, now.Add(time.Hour))
	if len(fp) != 1 || !fp[0].Snoozed {
		t.Fatalf("ForProject by remote incl. snoozed: %+v", fp)
	}
	if err := Done(s, 1, now); err != nil {
		t.Fatal(err)
	}
	if err := Done(s, 1, now); !errors.Is(err, ErrNotFound) {
		t.Fatal("done twice should be ErrNotFound")
	}
	if err := Done(s, 99, now); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing id should be ErrNotFound")
	}
}

func TestConcurrentNotes(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: "/ws/x", Org: "ws", Name: "x"}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); Note(s, p, Idea, fmt.Sprintf("race %d", i), "human", time.Now()) }()
		go func() { defer wg.Done(); Note(s, p, Idea, "the same retry", "human", time.Now()) }()
	}
	wg.Wait()
	doc, _ := store.Load[Doc](s, "threads", Migrator{})
	ids := map[int]bool{}
	for _, th := range doc.Threads {
		ids[th.ID] = true
	}
	if len(doc.Threads) != 11 || len(ids) != 11 {
		t.Fatalf("ten notes and one retried ten times: %d threads, %d ids", len(doc.Threads), len(ids))
	}
}

// Notes are one line. Internal newlines collapse; whitespace-only is rejected.
func TestNoteNormalizesWhitespace(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: "/ws/x", Org: "ws", Name: "x"}
	id, _, err := Note(s, p, Idea, "has a real\nnewline   inside\t", "human", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	open, _ := Open(s, time.Now())
	if open[0].ID != id || open[0].Text != "has a real newline inside" {
		t.Fatalf("%q", open[0].Text)
	}
	if _, _, err := Note(s, p, Idea, "  \n\t ", "human", time.Now()); err == nil {
		t.Fatal("whitespace-only must be rejected")
	}
}

// The v0→v1 migration is the one that can meet real data; pin it.
func TestMigrateV0(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	v0 := `{"threads":[{"id":3,"project":"/p","text":"t","kind":"me","since":"2026-01-01T00:00:00Z"},{"id":7,"project":"/q","text":"u","kind":"idea","since":"2026-01-02T00:00:00Z","source":"agent"}]}`
	os.WriteFile(filepath.Join(s.Home, "threads.json"), []byte(v0), 0o644)
	d, err := store.Load[Doc](s, "threads", Migrator{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != 3 || d.NextID != 8 || d.Threads[0].UID == "" || d.Threads[0].Source != "human" || d.Threads[1].Source != "agent" {
		t.Fatalf("%+v", d)
	}
	if d.Threads[0].Remote != nil || d.Threads[0].Closed != nil || d.Threads[0].SnoozedUntil != nil || d.Threads[0].Ref != nil {
		t.Fatalf("backfilled fields must be null: %+v", d.Threads[0])
	}
	if _, err := (Migrator{}).Migrate(5, nil); err == nil {
		t.Fatal("unknown from-version must error")
	}
	if _, err := (Migrator{}).Migrate(0, []byte("not json")); err == nil {
		t.Fatal("bad v0 JSON must error")
	}
}

func TestValidationErrorAndNotFoundMessages(t *testing.T) {
	if ValidationError("x").Error() != "x" {
		t.Fatal("ValidationError.Error")
	}
	s := &store.Store{Home: t.TempDir()}
	err := Edit(s, 42, "y")
	if err == nil || !strings.Contains(err.Error(), "no open note 42") {
		t.Fatalf("%v", err)
	}
}

func TestFileAtomicallyAndUpstreamClose(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	p := project.Project{Path: "/ws/x", Org: "ws", Name: "x"}
	id, _, _ := Note(s, p, Me, "x", "human", now)
	calls := 0
	ref, err := FileAtomically(s, id, false, func(th Thread) (string, error) { calls++; return "md:FOLLOWUPS.md:1:abcd1234", nil })
	if err != nil || ref != "md:FOLLOWUPS.md:1:abcd1234" || calls != 1 {
		t.Fatal(ref, err, calls)
	}
	ref2, _ := FileAtomically(s, id, false, func(Thread) (string, error) { calls++; return "other", nil })
	if ref2 != ref || calls != 1 {
		t.Fatal("second file must not call the backend")
	}
	if _, err := FileAtomically(s, id, false, func(Thread) (string, error) { return "", errors.New("boom") }); err != nil {
		t.Fatal("already filed: backend error irrelevant")
	}
	id2, _, _ := Note(s, p, Idea, "y", "human", now)
	if _, err := FileAtomically(s, id2, false, func(Thread) (string, error) { return "", errors.New("boom") }); err == nil {
		t.Fatal("backend error propagates")
	}
	if th, _ := Get(s, id2); th.Ref != nil {
		t.Fatal("failed file must not record a ref")
	}
	if err := CloseUpstreamClosed(s, id, now); err != nil {
		t.Fatal(err)
	}
	th, _ := Get(s, id)
	if th.Closed == nil || th.ClosedBy != "upstream" {
		t.Fatalf("%+v", th)
	}
	rc, _ := RecentlyClosedUpstream(s, "/ws/x", "", now.Add(time.Hour), 7*24*time.Hour)
	if len(rc) != 1 || rc[0].ID != id {
		t.Fatalf("%+v", rc)
	}
	if rc, _ := RecentlyClosedUpstream(s, "/ws/x", "", now.Add(8*24*time.Hour), 7*24*time.Hour); len(rc) != 0 {
		t.Fatal("old upstream closes drop out")
	}
	if err := CloseUpstreamClosed(s, id, now); !errors.Is(err, ErrNotFound) {
		t.Fatal("closing twice is not found")
	}
}

// v1 to v2: every note gets a stable random uid, used in filing markers so
// two installs' note 7 can never collide in a shared FOLLOWUPS.md.
func TestMigrateV1GivesEveryNoteAUID(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	v1 := `{"version":1,"next_id":3,"threads":[{"id":1,"project":"/p","text":"a","kind":"me","since":"2026-01-01T00:00:00Z","source":"human"},{"id":2,"project":"/p","text":"b","kind":"idea","since":"2026-01-01T00:00:00Z","source":"human"}]}`
	os.WriteFile(filepath.Join(s.Home, "threads.json"), []byte(v1), 0o644)
	d, err := store.Load[Doc](s, "threads", Migrator{})
	if err != nil {
		t.Fatal(err)
	}
	a, b := d.Threads[0].UID, d.Threads[1].UID
	if d.Version != 3 || len(a) != 12 || len(b) != 12 || a == b || d.Threads[0].ID != 1 {
		t.Fatalf("%+v", d)
	}
	id, _, _ := Note(s, project.Project{Path: "/p"}, Me, "c", "", time.Now())
	th, _ := Get(s, id)
	if len(th.UID) != 12 || th.UID == a {
		t.Fatalf("new notes get their own uid: %+v", th)
	}
}

// Upgrading is done in memory on every read until the next write, so the
// uid given to an existing note must be the same every time.
func TestMigratedUIDIsTheSameOnEveryRead(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	v1 := `{"version":1,"next_id":2,"threads":[{"id":1,"project":"/p","text":"a","kind":"me","since":"2026-01-01T00:00:00Z","source":"human"}]}`
	os.WriteFile(filepath.Join(s.Home, "threads.json"), []byte(v1), 0o644)
	a, _ := store.Load[Doc](s, "threads", Migrator{})
	b, _ := store.Load[Doc](s, "threads", Migrator{})
	if a.Threads[0].UID == "" || a.Threads[0].UID != b.Threads[0].UID {
		t.Fatalf("%q vs %q", a.Threads[0].UID, b.Threads[0].UID)
	}
}

// Which notes predate uids is recorded by the upgrade that gave
// them one, not guessed from the date.
func TestLegacyIsRecordedByTheUpgrade(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	late := UIDSince.Add(48 * time.Hour).Format(time.RFC3339) // an old binary, used after 0.1.1 came out
	v1 := `{"version":1,"next_id":2,"threads":[{"id":1,"project":"/p","text":"a","kind":"me","since":"` + late + `","source":"human"}]}`
	os.WriteFile(filepath.Join(s.Home, "threads.json"), []byte(v1), 0o644)
	d, _ := store.Load[Doc](s, "threads", Migrator{})
	if !d.Threads[0].Legacy {
		t.Fatal("a note upgraded from v1 had no uid, so it is legacy whatever its date")
	}
	id, _, _ := Note(s, project.Project{Path: "/p"}, Me, "new", "", UIDSince.Add(-time.Hour))
	if th, _ := Get(s, id); th.Legacy {
		t.Fatal("a note made with a uid is never legacy")
	}
}

func TestNoteRunIsIdempotentByKey(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: "/code/acme/billing"}
	now := time.Now()
	id, existed, err := NoteRun(s, p, "Fix the flaky test\nIt fails 1 in 5.", "flaky", "claude", "agent", now)
	if err != nil || existed {
		t.Fatal(id, existed, err)
	}
	again, existed, _ := NoteRun(s, p, "different words", "flaky", "claude", "agent", now)
	if again != id || !existed {
		t.Fatalf("same key: %d %v", again, existed)
	}
	other, existed, _ := NoteRun(s, p, "another", "", "claude", "agent", now)
	if other == id || existed {
		t.Fatal("no key is always new")
	}
	th, _ := Get(s, id)
	if th.Text != "Fix the flaky test" || th.Kind != Them || th.Run.Runner != "claude" || th.Run.State != "starting" {
		t.Fatalf("%+v %+v", th, th.Run)
	}
	if err := SetRun(s, id, func(r *Run) { r.Ref, r.State = "claude:u", "running" }); err != nil {
		t.Fatal(err)
	}
	if err := SetRun(s, other+100, func(*Run) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	plain, _, _ := Note(s, p, Me, "a plain note", "human", now)
	if err := SetRun(s, plain, func(*Run) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a note without a run: %v", err)
	}
	Done(s, id, now)
	if _, existed, _ := NoteRun(s, p, "x", "flaky", "claude", "agent", now); existed {
		t.Fatal("a closed run's key is free again")
	}
	if err := SetRun(s, id, func(*Run) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("closed: %v", err)
	}
	rs, _ := Runs(s, time.Now())
	if len(rs) != 2 {
		t.Fatalf("open runs: %d", len(rs))
	}
	// A run that never started is started again by a retry, on its note.
	failed, _, _ := NoteRun(s, p, "x", "retry", "claude", "", now)
	SetRun(s, failed, func(r *Run) { r.State, r.Text = "failed", "did not start: no claude" })
	if again, existed, _ := NoteRun(s, p, "x", "retry", "claude", "", now); existed || again != failed {
		t.Fatalf("a retry starts the failed start again: %d %v", again, existed)
	}
	if _, _, err := NoteRun(s, p, "  \n ", "", "claude", "", now); err == nil {
		t.Fatal("an empty brief is refused")
	}
}

// A run's kind follows its state, so it cannot be set by hand.
func TestSetKindRefusesARun(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	id, _, _ := NoteRun(s, project.Project{Path: "/code/acme/billing"}, "fix it", "", "fake", "human", time.Now())
	err := SetKind(s, id, Me)
	if !errors.As(err, new(ValidationError)) || !strings.Contains(err.Error(), "is a run") {
		t.Fatal(err)
	}
}

// A note is safe to retry: the same text, kind and project as an open
// note is that note. Once it is closed, the same text is a new note.
func TestNoteIsSafeToRetry(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: "/code/acme/api"}
	now := time.Now()
	id, existed, err := Note(s, p, Me, "fix the  index", "human", now)
	if err != nil || existed {
		t.Fatal(id, existed, err)
	}
	again, existed, _ := Note(s, p, Me, "fix the index", "agent", now)
	if again != id || !existed {
		t.Fatalf("a retry is the same note: %d %d %v", id, again, existed)
	}
	if same, existed, _ := Note(s, p, Them, "fix the index", "human", now); same != id || !existed {
		t.Fatal("the same text under another kind is still that note")
	}
	Done(s, id, now)
	if after, existed, _ := Note(s, p, Me, "fix the index", "human", now); after == id || existed {
		t.Fatal("after it closes, the same text is a new note")
	}
}

// A run is safe to retry too: without a key, the brief is its key.
func TestNoteRunWithoutAKeyUsesTheBrief(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: "/code/acme/api"}
	id, _, _ := NoteRun(s, p, "fix the flaky test\nmore", "", "fake", "human", time.Now())
	SetRun(s, id, func(r *Run) { r.Ref, r.State = "fake:1", RunRunning })
	again, existed, _ := NoteRun(s, p, "fix the flaky test\nmore", "", "fake", "human", time.Now())
	if again != id || !existed {
		t.Fatalf("%d %d %v", id, again, existed)
	}
	if other, existed, _ := NoteRun(s, p, "fix another test", "", "fake", "human", time.Now()); other == id || existed {
		t.Fatal("another brief is another run")
	}
}

// A run whose start failed is started again by a retry, on the same note,
// rather than leaving a failed note behind for every try.
func TestNoteRunRetriesAFailedStart(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: "/code/acme/api"}
	id, _, _ := NoteRun(s, p, "fix it", "", "claude", "human", time.Now())
	SetRun(s, id, func(r *Run) { r.State, r.Text = RunFailed, "did not start: claude is not installed" })
	again, existed, err := NoteRun(s, p, "fix it", "", "codex", "human", time.Now())
	th, _ := Get(s, id)
	if err != nil || again != id || existed || th.Run.State != RunStarting || th.Run.Runner != "codex" || th.Run.Text != "" {
		t.Fatalf("%d %d %v %+v", id, again, existed, th.Run)
	}
}

// A run keeps its whole brief, so it can be handed over again; its note's
// text is the first line.
func TestNoteRunKeepsTheBrief(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	id, _, _ := NoteRun(s, project.Project{Path: "/code/acme/api"}, "Fix the flaky test.\nIt fails 1 in 5.\n", "", "fake", "human", time.Now())
	th, _ := Get(s, id)
	if th.Text != "Fix the flaky test." || th.Run.Brief != "Fix the flaky test.\nIt fails 1 in 5." {
		t.Fatalf("%q %q", th.Text, th.Run.Brief)
	}
}
