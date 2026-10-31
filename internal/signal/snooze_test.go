package signal

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

func TestSnoozeReceiptSurvivesPruningAndNewPrefixMatch(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	c := collected("old work", "git")
	c.Signals[0].ID = "s:abc111"
	if _, err := Observe(s, c, []string{"/p"}, now); err != nil {
		t.Fatal(err)
	}
	id, already, err := SnoozeOnce(s, "s:abc", "first-op")
	if err != nil || already || id != "s:abc111" {
		t.Fatal(id, already, err)
	}
	// Remove all observations, then reuse the old prefix for unrelated work.
	c.Signals = nil
	if _, err := Observe(s, c, []string{"/p"}, now.Add(PruneAfter+time.Hour)); err != nil {
		t.Fatal(err)
	}
	d, err := store.Load[ObsDoc](s, "observed", ObsFile)
	if err != nil || len(d.Signals) != 0 || len(d.SnoozeOps) != 1 {
		t.Fatal(d, err)
	}
	c = collected("new work", "git")
	c.Signals[0].ID = "s:abc222"
	if _, err := Observe(s, c, []string{"/p"}, now.Add(PruneAfter+2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	id, already, err = SnoozeOnce(s, "s:abc", "first-op")
	if err != nil || !already || id != "s:abc111" {
		t.Fatal(id, already, err)
	}
	d, err = store.Load[ObsDoc](s, "observed", ObsFile)
	if err != nil || d.Signals["s:abc222"].SnoozedHash != nil {
		t.Fatal("new match hidden", d, err)
	}
	if _, _, err := SnoozeOnce(s, "s:abc222", "first-op"); !errors.Is(err, ErrOpReused) {
		t.Fatal(err)
	}
}

func TestObservationReceiptUpgradeAndFutureRefusal(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	hash := "known-content"
	old := ObsDoc{Version: 1, Signals: map[string]ObsEntry{"s:abc111": {Project: "/code/acme/api", Text: "work", Plugin: "git", Hash: hash, FirstSeen: at, LastSeen: at, Stale: true, SnoozedHash: &hash}}}
	raw, _ := json.Marshal(old)
	path := filepath.Join(s.Home, "observed.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SnoozeOnce(s, "s:abc", "upgrade-op"); err != nil {
		t.Fatal(err)
	}
	d, err := store.Load[ObsDoc](s, "observed", ObsFile)
	e := d.Signals["s:abc111"]
	if err != nil || d.Version != 2 || len(d.SnoozeOps) != 1 || !e.FirstSeen.Equal(at) || !e.LastSeen.Equal(at) || !e.Stale || *e.SnoozedHash != hash || e.Text != "work" {
		t.Fatal(d, err)
	}
	future := []byte(`{"version":99,"signals":{}}`)
	if err := os.WriteFile(path, future, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SnoozeOnce(s, "s:abc", "future-op"); !errors.Is(err, store.ErrNewer) {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, future) {
		t.Fatal("future state changed", err)
	}
}
