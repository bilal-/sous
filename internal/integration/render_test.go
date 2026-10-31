package integration

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderVersionOneChanges(t *testing.T) {
	before := Snapshot{Kind: "snapshot", V: 1, Provider: Provider, Revision: "a", Items: []Item{item("n:1", "on_you", "first")}}
	after := before
	after.Revision, after.Items = "b", []Item{item("n:1", "on_you", "changed")}
	events := collect(t, Stream{V: 1}, before, after)
	if len(events) != 2 || events[1].Snapshot != nil {
		t.Fatal("expected a baseline and a delta")
	}
	var out bytes.Buffer
	for _, event := range events {
		if err := RenderEvent(&out, event); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(out.String(), "snapshot · 1 items · a") || !strings.Contains(out.String(), "changes · 1 change · b") {
		t.Fatal(out.String())
	}
}
