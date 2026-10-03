package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/testutil"

	"github.com/bilal-/sous/docs"
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
	testutil.Contains(t, got, `"rows":[{"tags":[]`, `"ptr":{"tags":[]`, `"nil":null`, `"any":{"xs":[]}`, `"map":{"a":[]}`, `"err":null`)
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
		keys := []string{"projects", "plugins", "on_you", "on_others", "unfinished", "ideas", "snoozed", "attention", "recently_closed",
			"new_on_you", "new_on_others", "new_ideas", "closed", "worked", "next"}
		for _, k := range keys {
			if strings.Contains(out, `"`+k+`": null`) {
				t.Errorf("%v: %s is null:\n%s", args, k, out)
			}
		}
	}
}

// A command asked for --json that fails says why on stdout too, as JSON
// with its exit code, and on stderr as always.
func TestErrorsUnderJSON(t *testing.T) {
	f := fixture(t)
	out, errs, code := f.run("show", "99", "--json")
	var got errorJSON
	if code != exitUsage || json.Unmarshal([]byte(out), &got) != nil || got.Exit != exitUsage || !strings.Contains(got.Error, "99") || !strings.Contains(errs, "99") || len(got.Next) == 0 {
		t.Fatalf("a note that is not there is the wrong number, with somewhere to look: %d %q %q", code, out, errs)
	}
	if out, _, code := f.run("show", "99"); code != exitUsage || out != "" {
		t.Fatalf("without --json stdout stays empty: %q", out)
	}
}

// Under --json every answer is one JSON value, failures included: a
// program reads it whole. A note that is saved but not filed says both.
func TestJSONIsOneValueEvenWhenItFails(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	for _, args := range [][]string{{"note", "--file", "kept here", "--json"}, {"show", "99", "--json"}, {"done", "99", "--json"}, {"go", "nope", "--run", "x", "--json"}} {
		out, _, code := f.runIn(p, args...)
		dec := json.NewDecoder(strings.NewReader(out))
		var first map[string]any
		if err := dec.Decode(&first); err != nil || dec.More() || code == 0 {
			t.Errorf("%v: %d, want one JSON value:\n%s", args, code, out)
		}
	}
	out, _, _ := f.runIn(p, "note", "--file", "kept here too", "--json")
	var c changedJSON
	json.Unmarshal([]byte(out), &c)
	if c.ID == "" || c.Did != "noted" || !strings.Contains(c.Error, "not filed") {
		t.Fatalf("%+v", c)
	}
}

// Every did a change can answer with is in docs/commands.md, so a program
// can know them all.
func TestEveryDidIsDocumented(t *testing.T) {
	for _, d := range dids {
		if !strings.Contains(docs.Commands, "`"+d+"`") {
			t.Errorf("docs/commands.md does not list did %q", d)
		}
	}
}

// --brief is never silently ignored: the board has no short form, and
// --json is always whole.
func TestBriefWhereItMeansNothing(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	for _, args := range [][]string{{"--brief"}, {"--json", "--brief"}, {"here", p, "--brief", "--json"}, {"--cached", "--brief"}} {
		if _, _, code := f.run(args...); code != exitUsage {
			t.Errorf("%v: %d", args, code)
		}
	}
	if _, _, code := f.run("here", p, "--brief"); code != 0 {
		t.Errorf("here --brief: %d", code)
	}
}

// Help is wherever an agent asks for it: --help anywhere before --, and
// for the commands plugins and hooks call too.
func TestHelpAnywhere(t *testing.T) {
	f := fixture(t)
	for _, args := range [][]string{{"show", "2", "--help"}, {"note", "x", "-h"}, {"hook", "--help"}, {"help", "hook"}, {"help", "runner"}} {
		out, errs, code := f.run(args...)
		if code != 0 || !strings.HasPrefix(out, "usage: sous "+args[0]) && !strings.HasPrefix(out, "usage: sous "+args[1]) {
			t.Errorf("%v: %d %q %q", args, code, out, errs)
		}
	}
	if out, _, _ := f.run("note", "--", "--help"); strings.HasPrefix(out, "usage:") {
		t.Errorf("after -- it is a note's text: %q", out)
	}
}
