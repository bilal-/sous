package session

import (
	"testing"

	"github.com/bilal-/sous/internal/store"
)

func TestRecordUpserts(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	Record(s, "/ws/p", Session{Agent: "claude", SessionID: "a"})
	Record(s, "/ws/p", Session{Agent: "codex", SessionID: "b"})
	all, err := All(s)
	if err != nil || len(all) != 1 || all["/ws/p"].Agent != "codex" {
		t.Fatalf("%+v %v", all, err)
	}
}

func TestMigratorRefusesUnknownVersions(t *testing.T) {
	if _, err := (Migrator{}).Migrate(0, nil); err == nil {
		t.Fatal("sessions")
	}
}
