package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNoteWithATicketThatExists(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/api", true)
	out, errs, code := f.run("note", "-p", "api", "-k", "me", "--ref", "https://github.com/acme/api/issues/12", "--json", "fix the login timeout")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var c changedJSON
	if err := json.Unmarshal([]byte(out), &c); err != nil || c.Did != didNoted || c.Ref == nil || *c.Ref != "github:acme/api#12" {
		t.Fatalf("%s %v", out, err)
	}
	// A retry finds the same note, linked the same way.
	out, _, code = f.run("note", "-p", "api", "-k", "me", "--ref", "github:acme/api#12", "--json", "fix the login timeout")
	if code != 0 || !strings.Contains(flat(out), `"did":"already_noted"`) {
		t.Fatalf("exit %d: %s", code, out)
	}
	// Another ticket for the same note is refused, not swapped in.
	_, errs, code = f.run("note", "-p", "api", "-k", "me", "--ref", "github:acme/api#13", "fix the login timeout")
	if code != exitUsage || !strings.Contains(errs, "sous unlink") {
		t.Fatalf("exit %d: %s", code, errs)
	}
	out, _, _ = f.run("show", "1")
	if !strings.Contains(out, "ticket: github:acme/api#12") {
		t.Fatalf("show does not name the ticket:\n%s", out)
	}
}

func TestNoteRefusesWhatItCannotFollow(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/api", true)
	for _, ref := range []string{"https://github.com/acme/api/pull/9", "ENG-4", "md:FOLLOWUPS.md:0123456789ab"} {
		_, errs, code := f.run("note", "-p", "api", "--ref", ref, "x")
		if code != exitUsage {
			t.Errorf("--ref %s: exit %d: %s", ref, code, errs)
		}
	}
	if _, errs, code := f.run("note", "-p", "api", "--ref", "github:acme/api#1", "--file", "x"); code != exitUsage {
		t.Errorf("--file with --ref: exit %d: %s", code, errs)
	}
	if out, _, _ := f.run("projects", "--json"); strings.Contains(flat(out), `"x"`) {
		t.Fatal(out)
	}
	if out, _, _ := f.run("--json"); strings.Contains(flat(out), `"text":"x"`) {
		t.Fatalf("a refused note was saved:\n%s", out)
	}
}

func TestANoteWaitsOnAnotherUntilItCloses(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/api", true)
	f.mkrepo("acme/site", true)
	f.run("note", "-p", "api", "-k", "me", "ship the endpoint")
	out, errs, code := f.run("note", "-p", "site", "-k", "me", "--after", "1", "ship the form")
	if code != 0 || !strings.Contains(out, "waits on 1") {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	board, _, _ := f.run()
	if !strings.Contains(board, "ship the form") || !strings.Contains(board, "(waits on 1)") {
		t.Fatalf("the board does not say what it waits on:\n%s", board)
	}
	js, _, _ := f.run("--json")
	if !strings.Contains(flat(js), `"waits_on":["1"]`) {
		t.Fatalf("--json does not say what it waits on:\n%s", js)
	}
	if _, errs, code := f.run("link", "1", "--after", "2"); code != exitUsage {
		t.Fatalf("a loop was allowed: exit %d: %s", code, errs)
	}
	f.run("done", "1")
	board, _, _ = f.run()
	if strings.Contains(board, "waits on") {
		t.Fatalf("a closed note still blocks:\n%s", board)
	}
	out, _, _ = f.run("show", "2")
	if !strings.Contains(out, "waits on: 1 (closed)") {
		t.Fatalf("show does not say the wait is over:\n%s", out)
	}
}

func TestLinkAndUnlinkAnExistingNote(t *testing.T) {
	f := fixture(t)
	f.mkrepo("acme/api", true)
	f.run("note", "-p", "api", "-k", "me", "first")
	f.run("note", "-p", "api", "-k", "me", "second")
	out, errs, code := f.run("link", "2", "--ref", "gitlab:git.example.org/acme/api#4", "--after", "1", "--json")
	if code != 0 || !strings.Contains(flat(out), `"did":"linked"`) || !strings.Contains(flat(out), `"after":["1"]`) || !strings.Contains(flat(out), `"ref":"gitlab:git.example.org/acme/api#4"`) {
		t.Fatalf("exit %d: %s%s", code, out, errs)
	}
	if out, _, _ := f.run("link", "2", "--after", "1", "--json"); !strings.Contains(flat(out), `"did":"already_linked"`) {
		t.Fatal(out)
	}
	if _, errs, code := f.run("link", "2"); code != exitUsage {
		t.Fatalf("link with nothing to link: exit %d: %s", code, errs)
	}
	out, _, code = f.run("unlink", "2", "--ref", "--after", "1", "--json")
	if code != 0 || !strings.Contains(flat(out), `"did":"unlinked"`) || !strings.Contains(flat(out), `"ref":null`) {
		t.Fatalf("exit %d: %s", code, out)
	}
	if out, _, _ := f.run("unlink", "2", "--ref", "--json"); !strings.Contains(flat(out), `"did":"already_unlinked"`) {
		t.Fatal(out)
	}
}

// flat is JSON output with its whitespace taken out, to look for fields.
func flat(s string) string {
	var b bytes.Buffer
	if json.Compact(&b, []byte(s)) != nil {
		return s
	}
	return b.String()
}

// A host files a note, or links it to a ticket, with an op it made; a
// repeat answers the same, and the snapshot only offers both to a note
// that has no ticket yet.
func TestIntegrationFileAndLinkActions(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	f.run("note", "-p", "api", "-k", "me", "to file")
	f.run("note", "-p", "api", "-k", "me", "to link")
	if o, code := f.act("", "file", "--op", "op-file-0000000001", "--", "1"); code != exitUsage || o.Reason != "no_tracker" {
		t.Fatalf("no tracker: %+v %d", o, code)
	}
	os.WriteFile(filepath.Join(p, "FOLLOWUPS.md"), []byte("# F\n"), 0o644)
	if o, code := f.act("", "file", "--op", "op-file-0000000002", "--", "1"); code != 0 || o.Outcome != "done" {
		t.Fatalf("file: %+v %d", o, code)
	}
	if o, code := f.act("", "file", "--op", "op-file-0000000002", "--", "1"); code != 0 || o.Outcome != "already_done" {
		t.Fatalf("file again: %+v %d", o, code)
	}
	md, _ := os.ReadFile(filepath.Join(p, "FOLLOWUPS.md"))
	if strings.Count(string(md), "to file") != 1 {
		t.Fatalf("filed %d times:\n%s", strings.Count(string(md), "to file"), md)
	}
	if o, code := f.act("", "link", "--op", "op-link-0000000001", "--ref", "https://github.com/acme/api/pull/3", "--", "2"); code != exitUsage || o.Reason != "bad_input" {
		t.Fatalf("a pull request: %+v %d", o, code)
	}
	if o, code := f.act("", "link", "--op", "op-link-0000000002", "--ref", "https://github.com/acme/api/issues/3", "--", "2"); code != 0 || o.Outcome != "done" {
		t.Fatalf("link: %+v %d", o, code)
	}
	if o, code := f.act("", "link", "--op", "op-link-0000000003", "--ref", "github:acme/api#4", "--", "2"); code != exitUsage || o.Reason != "linked_elsewhere" {
		t.Fatalf("another ticket: %+v %d", o, code)
	}
	if _, errs, code := f.run("integration", "--protocol", "1", "--json", "link", "--op", "op-link-0000000004", "--", "2"); code != exitUsage {
		t.Fatalf("link without --ref: %d %s", code, errs)
	}
	f.run("note", "-p", "api", "-k", "me", "plain")
	f.run("--refresh")
	out, _, _ := f.run("integration", "--protocol", "1", "--json", "snapshot")
	var snap struct {
		Items []struct {
			ID      string   `json:"id"`
			Actions []string `json:"actions"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatal(err, out)
	}
	offers := map[string]bool{}
	for _, it := range snap.Items {
		offers[it.ID] = slices.Contains(it.Actions, "file") && slices.Contains(it.Actions, "link")
	}
	if offers["1"] || offers["2"] || !offers["3"] {
		t.Fatalf("file and link offered to %v", offers)
	}
}
