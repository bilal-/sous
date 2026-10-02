package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// AXI 5: an empty list is [], never null, whatever the command hands
// writeJSON: nested, behind pointers and interfaces, inside maps.
func TestWriteJSONEmptyListsAreNotNull(t *testing.T) {
	type inner struct {
		Tags []string `json:"tags"`
		When time.Time
	}
	type outer struct {
		Rows   []inner          `json:"rows"`
		Ptr    *inner           `json:"ptr"`
		Nil    *inner           `json:"nil"`
		Any    any              `json:"any"`
		Map    map[string][]int `json:"map"`
		Hidden []int            `json:"hidden,omitempty"`
		Err    error            `json:"err"`
		secret []int
	}
	var out bytes.Buffer
	e := &Env{Stdout: &out}
	v := outer{Rows: []inner{{}}, Ptr: &inner{}, Any: map[string]any{"xs": []int(nil)}, Map: map[string][]int{"a": nil}, secret: []int{1}}
	if code := e.writeJSON(v); code != 0 {
		t.Fatal(code)
	}
	got := strings.Join(strings.Fields(out.String()), "")
	for _, want := range []string{`"rows":[{"tags":[]`, `"ptr":{"tags":[]`, `"nil":null`, `"any":{"xs":[]}`, `"map":{"a":[]}`, `"err":null`} {
		if !strings.Contains(got, want) {
			t.Errorf("want %s in %s", want, got)
		}
	}
	if strings.Contains(got, "hidden") {
		t.Errorf("omitempty still omits: %s", got)
	}
	if v.Rows[0].Tags != nil {
		t.Error("writeJSON must not change what it was given")
	}
}

// Every read command's --json, on a fresh setup with one project and one
// note: no list comes out as null.
func TestReadCommandsJSONHasNoNullLists(t *testing.T) {
	f := fixture(t)
	repo := f.mkrepo("acme/api", true)
	if _, errs, code := f.runIn(repo, "note", "check the index"); code != 0 {
		t.Fatal(errs)
	}
	for _, args := range [][]string{{}, {"here", repo}, {"report"}, {"projects"}, {"config"}, {"doctor"}, {"show", "1"}} {
		out, errs, _ := f.run(append(args, "--json")...)
		var v any
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Errorf("%v: not JSON: %v %q %q", args, err, out, errs)
			continue
		}
		keys := []string{"projects", "signals", "threads", "plugins", "new_me", "new_them", "new_idea", "worked", "attention"}
		if len(args) == 1 && args[0] == "report" {
			keys = append(keys, "closed") // elsewhere closed is a time, null while open
		}
		for _, k := range keys {
			if strings.Contains(out, `"`+k+`": null`) {
				t.Errorf("%v: %s is null:\n%s", args, k, out)
			}
		}
	}
}
