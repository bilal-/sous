package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

func TestRecordUpserts(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	p1, m1 := "fix auth", "wired cursor pagination"
	Record(s, "/ws/p", Session{Agent: "claude", SessionID: "a", Branch: "feat/auth", Prompt: &p1, LastMessage: &m1, Ended: now, Files: []string{"auth.go"}, Commit: "init"})
	// Same session ending cleanly without repeating prompt/last_message preserves them.
	Record(s, "/ws/p", Session{Agent: "claude", SessionID: "a", Branch: "feat/auth", Ended: now.Add(time.Minute)})
	// New session on a different branch keeps history of both.
	p2, m2 := "bump deps", "updated go.mod"
	Record(s, "/ws/p", Session{Agent: "codex", SessionID: "b", Branch: "main", Prompt: &p2, LastMessage: &m2, Ended: now.Add(2 * time.Minute)})

	all, err := All(s)
	if err != nil || len(all) != 1 || all["/ws/p"].Agent != "codex" {
		t.Fatalf("%+v %v", all, err)
	}
	hist, err := ForProject(s, "/ws/p")
	if err != nil || len(hist) != 2 {
		t.Fatalf("history = %+v, err=%v", hist, err)
	}
	if hist[0].Branch != "main" || hist[1].Branch != "feat/auth" || hist[1].Prompt == nil || *hist[1].Prompt != "fix auth" || hist[1].LastMessage == nil || *hist[1].LastMessage != "wired cursor pagination" {
		t.Fatalf("preserved context lost: %+v", hist)
	}
}

func TestMigratesV1AndPreservesSessions(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	os.WriteFile(filepath.Join(s.Home, "sessions.json"), []byte(`{"version":1,"sessions":{"/ws/p":{"agent":"claude","session_id":"s1","ended":"2026-10-01T10:00:00Z","last_message":"done"}}}`), 0o600)
	all, err := All(s)
	if err != nil || all["/ws/p"].SessionID != "s1" {
		t.Fatalf("v1 all: %+v %v", all, err)
	}
	hist, err := ForProject(s, "/ws/p")
	if err != nil || len(hist) != 1 || *hist[0].LastMessage != "done" {
		t.Fatalf("v1 hist: %+v %v", hist, err)
	}
}

func TestMarkStartedTracksOncePerSession(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Now()
	if !MarkStarted(s, "kimi:s1", now) {
		t.Fatal("first start must return true")
	}
	if MarkStarted(s, "kimi:s1", now) {
		t.Fatal("second start for same session must return false")
	}
	if !MarkStarted(s, "kimi:s2", now) {
		t.Fatal("new session must return true")
	}
}

func TestMigratorRefusesUnknownVersions(t *testing.T) {
	if _, err := sessionsFile.Migrate(0, nil); err == nil {
		t.Fatal("sessions")
	}
}
