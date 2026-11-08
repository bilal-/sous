package agentmem

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/memory"
)

func setup(t *testing.T) (home, api string) {
	t.Helper()
	home, _ = filepath.EvalSymlinks(t.TempDir())
	api = filepath.Join(home, "code", "acme", "api")
	os.MkdirAll(api, 0o755)
	h, _ := harness.Find("claude")
	dir := h.Memory.Dir(home, api)
	os.MkdirAll(dir, 0o755)
	files := map[string]string{
		"MEMORY.md":    "- [Cursors](cursors.md) — the index, not a memory\n",
		"cursors.md":   "---\nname: cursors\ndescription: \"Paginate with cursors, not offsets\"\nmetadata:\n  type: feedback\n---\n\n**Why:** Sam's clients page deep.\n",
		"release.md":   "---\nname: release\ndescription: Where the release stands\ntype: project\n---\nTagged, not pushed.\n",
		"plain.md":     "No front matter at all.\n",
		"empty.md":     "---\nname: empty\n---\n",
		"notes.txt":    "not markdown",
		"reference.md": "---\ndescription: The dashboard is at dash.example.org\nmetadata:\n  type: reference\n---\n",
	}
	for name, body := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
	return home, api
}

func TestAnAgentsMemoryIsReadAsRecords(t *testing.T) {
	home, api := setup(t)
	c := &config.Config{Roots: []string{filepath.Join(home, "code")}}
	got := map[string]memory.Record{}
	for _, r := range ForProject(home, c, api) {
		got[r.Source] = r
	}
	if len(got) != 4 {
		t.Fatalf("want cursors, release, plain and reference only: %v", got)
	}
	cur := got["cursors.md"]
	if cur.Kind != memory.Constraint || cur.Text != "Paginate with cursors, not offsets\n\n**Why:** Sam's clients page deep." || cur.Author != "claude" {
		t.Fatalf("got %+v", cur)
	}
	if !Owned(cur.ID) || cur.Path == "" || cur.VerifiedAt != nil || cur.Scope.Project != api || cur.Scope.Workspace != filepath.Join(home, "code") {
		t.Fatalf("got %+v", cur)
	}
	if got["release.md"].Kind != memory.Handoff || got["reference.md"].Kind != memory.Lesson || got["plain.md"].Text != "No front matter at all." {
		t.Fatalf("got %+v", got)
	}
	again, err := Get(home, c, cur.ID)
	if err != nil || again.Text != cur.Text {
		t.Fatalf("get: %+v %v", again, err)
	}
	if _, err := Get(home, c, "a:nothing"); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if n := len(All(home, c)); n != 4 {
		t.Fatalf("all: %d", n)
	}
}

func TestAgentMemoryOffReadsNothing(t *testing.T) {
	home, api := setup(t)
	c := &config.Config{AgentMemory: Off}
	if len(ForProject(home, c, api)) != 0 || len(All(home, c)) != 0 {
		t.Fatal("read with agent_memory off")
	}
}
