package integration

import (
	"context"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/effort"
	"github.com/bilal-/sous/internal/store"
)

func TestLocalEffortsChangeSnapshotRevisionAndWatchMetadata(t *testing.T) {
	s := &store.Store{Home: t.TempDir()}
	r := Reader{Store: s, V: 1, Now: time.Now()}
	before, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := effort.Create(s, "Account launch", r.Now); err != nil {
		t.Fatal(err)
	}
	after, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Efforts) != 1 || before.Revision == after.Revision {
		t.Fatalf("missing effort change: %#v", after)
	}
	m := meta(before, after)
	if m == nil || m.Efforts == nil || len(*m.Efforts) != 1 {
		t.Fatalf("missing watch metadata: %#v", m)
	}
	if err := effort.Archive(s, "Account launch", true, r.Now); err != nil {
		t.Fatal(err)
	}
	last, err := r.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m = meta(after, last)
	if m == nil || m.Efforts == nil || len(*m.Efforts) != 0 {
		t.Fatalf("missing removal: %#v", m)
	}
}
