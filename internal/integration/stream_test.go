package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
)

// collect runs a stream over the snapshots given, one per tick, and
// returns what it sent.
func collect(t *testing.T, st Stream, snaps ...Snapshot) []Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	i := 0
	var events []Event
	st.Interval = time.Millisecond
	st.Run(ctx, func(context.Context) (Snapshot, error) {
		if i == len(snaps) {
			cancel()
			return snaps[len(snaps)-1], nil
		}
		i++
		return snaps[i-1], nil
	}, func(e Event) error { events = append(events, e); return nil })
	return events
}

func item(key, section, text string) Item {
	return Item{Key: key, Section: section, Item: board.Item{ID: strings.TrimPrefix(key, "n:"), Text: text}}
}

// Version 1 sends changed items only after its first snapshot, with the
// board fields that changed in meta.
func TestStreamV1SendsDeltas(t *testing.T) {
	a := Snapshot{Kind: "snapshot", V: 1, Provider: "sous", Revision: "a", Problems: []string{}, Items: []Item{item("n:1", "on_you", "one")}}
	b := a
	b.Revision, b.Items = "b", []Item{item("n:1", "on_you", "one, changed"), item("n:2", "on_you", "two")}
	c := b
	c.Revision, c.Problems = "c", []string{"github: timed out"}
	ev := collect(t, Stream{V: 1}, a, b, c)
	if len(ev) != 3 || ev[0].Type != "snapshot" || ev[0].Snapshot == nil {
		t.Fatalf("%+v", ev)
	}
	if ev[1].Type != "changes" || ev[1].Snapshot != nil || ev[1].Meta != nil || len(ev[1].Changes) != 2 || ev[1].PreviousRevision != "a" {
		t.Fatalf("delta: %+v", ev[1])
	}
	if ev[2].Meta == nil || ev[2].Meta.Problems == nil || (*ev[2].Meta.Problems)[0] != "github: timed out" || len(ev[2].Changes) != 0 {
		t.Fatalf("meta: %+v", ev[2])
	}
	// Version 0 keeps the whole snapshot in every change.
	if ev := collect(t, Stream{V: 0}, a, b); ev[1].Snapshot == nil || ev[1].Meta != nil {
		t.Fatalf("v0: %+v", ev[1])
	}
}

// No line is ever longer than MaxBytes: a snapshot leaves out the lowest
// sections first and counts what it left, keeping the board's revision; a
// delta too big becomes such a snapshot.
func TestStreamV1KeepsUnderMaxBytes(t *testing.T) {
	big := Snapshot{Kind: "snapshot", V: 1, Provider: "sous", Revision: "a"}
	for i := range 40 {
		big.Items = append(big.Items, item(fmt.Sprintf("n:u%d", i), "unfinished", strings.Repeat("x", 400)))
	}
	for i := range 5 {
		big.Items = append(big.Items, item(fmt.Sprintf("n:m%d", i), "on_you", "keep me"))
	}
	ev := collect(t, Stream{V: 1, MaxBytes: 16 << 10}, big)
	b, _ := json.Marshal(ev[0])
	if len(b) > 16<<10 || ev[0].Snapshot.Omitted["unfinished"] == 0 || ev[0].Revision != "a" {
		t.Fatalf("%d bytes, omitted %v", len(b), ev[0].Snapshot.Omitted)
	}
	for _, it := range ev[0].Snapshot.Items {
		if it.Section == "on_you" {
			goto kept
		}
	}
	t.Fatal("on_you was left out before unfinished")
kept:
	grown := big
	grown.Revision = "b"
	grown.Items = append([]Item{}, big.Items...)
	for i := range grown.Items {
		grown.Items[i].Text += "!"
	}
	ev = collect(t, Stream{V: 1, MaxBytes: 16 << 10}, big, grown)
	if len(ev) != 2 || ev[1].Type != "snapshot" || ev[1].Revision != "b" {
		t.Fatalf("a delta too big: %+v", ev[1].Type)
	}
	if b, _ := json.Marshal(ev[1]); len(b) > 16<<10 {
		t.Fatalf("%d bytes", len(b))
	}
}
