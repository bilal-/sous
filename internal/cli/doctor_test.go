package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

// doctorPath: only git, a sous and a claude (the default agent) on PATH, so
// doctor sees the same machine everywhere (no real gh or glab, the shell
// line finds sous, and runs can start).
func doctorPath(t *testing.T) {
	testutil.OnlyGit(t)
	testutil.FakeBin(t, "sous", "")
	testutil.FakeBin(t, "claude", "")
}

func TestDoctor(t *testing.T) {
	f := fixture(t)
	doctorPath(t)
	f.mkrepo("acme/api", true)
	// Fresh: nothing installed. Problems are listed with fixes; exit 1.
	out, _, code := f.run("doctor")
	if code != 1 || !strings.Contains(out, "sous doctor") || !strings.Contains(out, "✗") || !strings.Contains(out, "sous setup") {
		t.Fatalf("fresh: %d\n%s", code, out)
	}
	f.run("setup")
	out, _, code = f.run("doctor")
	if code != 0 || strings.Contains(out, "✗") || !strings.Contains(out, "1 project") {
		t.Fatalf("after setup: %d\n%s", code, out)
	}
	// A listed plugin that is gone is a problem.
	f.writeConfig("roots = [\"" + f.WS + "\"]\nplugins = [\"" + filepath.Join(f.Home, "nope") + "\"]\n")
	out, _, code = f.run("doctor")
	if code != 1 || !strings.Contains(out, "nope") {
		t.Fatalf("missing plugin: %d\n%s", code, out)
	}
	out, _, _ = f.run("doctor", "--json")
	var js struct {
		Problems int  `json:"problems"`
		Warnings *int `json:"warnings"`
		Checks   []struct {
			Name, Status, Detail, Fix string
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &js); err != nil || js.Problems == 0 || js.Warnings == nil || len(js.Checks) < 8 {
		t.Fatalf("json: %v %s", err, out)
	}
}

// A folder listed in roots that is gone is a problem, named.
func TestDoctorMissingRoot(t *testing.T) {
	f := fixture(t)
	doctorPath(t)
	f.writeConfig("roots = [\"" + filepath.Join(f.Home, "gone") + "\"]\n")
	out, _, code := f.run("doctor")
	if code != 1 || !strings.Contains(out, "gone") {
		t.Fatalf("%d\n%s", code, out)
	}
	os.Remove(filepath.Join(f.SousHome, "config.toml"))
	if out, _, _ := f.run("doctor"); !strings.Contains(out, "sous setup") {
		t.Fatalf("no roots: %s", out)
	}
}

// doctor only looks: an old notes file is read, not upgraded, and no lock
// files appear.
func TestDoctorChangesNothing(t *testing.T) {
	f := fixture(t)
	doctorPath(t)
	f.run("setup")
	notes := filepath.Join(f.SousHome, "threads.json")
	old := `{"version":1,"next_id":2,"threads":[{"id":1,"project":"/x","text":"old note","kind":"me","created":"2026-01-01T00:00:00Z"}]}`
	os.WriteFile(notes, []byte(old), 0o600)
	before, _ := os.ReadDir(f.SousHome)
	if out, _, _ := f.run("doctor"); !strings.Contains(out, "notes (threads.json): reads") {
		t.Fatalf("old notes: %s", out)
	}
	if b, _ := os.ReadFile(notes); string(b) != old {
		t.Fatalf("doctor upgraded the notes file: %s", b)
	}
	if after, _ := os.ReadDir(f.SousHome); len(after) != len(before) {
		t.Fatalf("doctor made files: %v then %v", before, after)
	}
}
