package integration

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStreamV1RunProjectChanges(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after []string
	}{
		{"grant", nil, []string{"/code/acme/api"}},
		{"replace", []string{"/code/acme/api"}, []string{"/code/acme/web"}},
		{"revoke", []string{"/code/acme/api"}, []string{}},
		{"revoke nil", []string{"/code/acme/api"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := Snapshot{Kind: "snapshot", V: 1, Provider: Provider, Revision: "a", RunProjects: tc.before}
			b := a
			b.Revision, b.RunProjects = "b", tc.after
			events := collect(t, Stream{V: 1}, a, b)
			if len(events) != 2 || events[1].Snapshot != nil {
				t.Fatal("expected a baseline and a delta")
			}
			raw, err := json.Marshal(events[1])
			if err != nil {
				t.Fatal(err)
			}
			var wire struct{ Meta map[string]json.RawMessage }
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			replacement, ok := wire.Meta["run_projects"]
			if !ok || string(replacement) == "null" {
				t.Fatalf("run projects must be replaced with an array: %s", raw)
			}
			state := append([]string{}, tc.before...)
			if err := json.Unmarshal(replacement, &state); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(state, append([]string{}, tc.after...)) {
				t.Fatalf("host kept %v, want %v", state, tc.after)
			}
		})
	}
}
