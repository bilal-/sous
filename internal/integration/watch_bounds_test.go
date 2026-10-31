package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
)

func TestStreamBoundsMetadataAndErrors(t *testing.T) {
	normal := Snapshot{Kind: "snapshot", V: 1, Provider: Provider, Revision: "a"}
	big := normal
	big.Revision = "b"
	for i := range 200 {
		big.Efforts = append(big.Efforts, board.EffortSummary{ID: fmt.Sprint("e:", i), Name: strings.Repeat("x", 200), Attention: []string{}})
	}
	hugeError := errors.New(strings.Repeat("x", 17<<10))
	errorEvent := Event{Kind: "event", V: 1, Provider: Provider, Type: "unavailable", Error: hugeError.Error(), Exit: 1, Changes: []Change{}}
	errorJSON, err := json.Marshal(errorEvent)
	if err != nil {
		t.Fatal(err)
	}
	type reading struct {
		snapshot Snapshot
		err      error
	}
	for _, tc := range []struct {
		name              string
		reads             []reading
		maxBytes, emitted int
		wantError         bool
	}{
		{"snapshot metadata", []reading{{big, nil}}, 16 << 10, 0, true},
		{"delta metadata", []reading{{normal, nil}, {big, nil}}, 16 << 10, 1, true},
		{"unavailable", []reading{{Snapshot{}, hugeError}}, 16 << 10, 0, true},
		{"newline counts", []reading{{Snapshot{}, hugeError}}, len(errorJSON), 0, true},
		{"exact bound", []reading{{Snapshot{}, hugeError}}, len(errorJSON) + 1, 1, false},
		{"unlimited", []reading{{big, nil}}, 0, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			i, emitted := 0, 0
			err := (Stream{V: 1, MaxBytes: tc.maxBytes, Interval: time.Millisecond}).Run(ctx, func(context.Context) (Snapshot, error) {
				if i == len(tc.reads) {
					cancel()
					return Snapshot{}, nil
				}
				r := tc.reads[i]
				i++
				return r.snapshot, r.err
			}, func(e Event) error {
				emitted++
				raw, err := json.Marshal(e)
				if err != nil {
					return err
				}
				if tc.maxBytes > 0 && len(raw)+1 > tc.maxBytes {
					t.Errorf("emitted %d bytes, limit %d", len(raw)+1, tc.maxBytes)
				}
				return nil
			})
			if (err != nil) != tc.wantError || emitted != tc.emitted {
				t.Fatalf("emitted %d events, error %v; want %d, error %v", emitted, err, tc.emitted, tc.wantError)
			}
			if tc.wantError && !strings.Contains(err.Error(), "--max-bytes") {
				t.Fatalf("error must explain the limit: %v", err)
			}
		})
	}
}
