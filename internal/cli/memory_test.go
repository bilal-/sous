package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/harness"
)

func TestMemoryCommandsCaptureRecallReplaceAndForget(t *testing.T) {
	f := fixture(t)
	api := f.mkrepo("acme/api", true)
	site := f.mkrepo("acme/site", true)
	call := func(args ...string) changedJSON {
		t.Helper()
		out, errs, code := f.runIn(api, append(args, "--json")...)
		var got changedJSON
		if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
			t.Fatalf("%v: %d %s %s", args, code, out, errs)
		}
		return got
	}
	old := call("remember", "-k", "decision", "--source", "design.md#pagination", "Keep offsets")
	again := call("remember", "-k", "decision", "--source", "design.md#pagination", "Keep offsets")
	if old.ID != again.ID || again.Did != "already_remembered" {
		t.Fatalf("%#v %#v", old, again)
	}
	current := call("remember", "-k", "decision", "--supersedes", old.ID, "--source", "design.md#update", "Use cursors")
	out, errs, code := f.runIn(api, "recall", "--json")
	if code != 0 || strings.Contains(out, "Keep offsets") || !strings.Contains(out, "Use cursors") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, _, code = f.runIn(site, "recall")
	if code != 0 || !strings.Contains(out, "0 memories") {
		t.Fatalf("scope: %d %s", code, out)
	}
	call("memory", "share", current.ID, "--with", "acme/site")
	out, _, code = f.runIn(site, "here", "--brief")
	if code != 0 || !strings.Contains(out, "Use cursors") {
		t.Fatalf("shared briefing: %d %s", code, out)
	}
	call("memory", "verify", current.ID, "--source", "tests/pagination_test.go")
	call("memory", "forget", current.ID)
	out, _, code = f.runIn(api, "recall", "--history", "--json")
	if code != 0 || !strings.Contains(out, "Keep offsets") || strings.Contains(out, "Use cursors") {
		t.Fatalf("history: %d %s", code, out)
	}
	for _, args := range [][]string{{"recall", "--all", "-p", "acme/api"}, {"recall", "--limit", "0"}, {"memory", "verify", old.ID}, {"memory", "forget", old.ID, "--with", "acme/site"}} {
		if _, _, code := f.runIn(api, args...); code != 2 {
			t.Errorf("%v: %d", args, code)
		}
	}
}

func TestEffortCommandsKeepWorkStateAndExposeDependencies(t *testing.T) {
	f := fixture(t)
	api := f.mkrepo("acme/api", true)
	f.mkrepo("acme/site", true)
	f.runIn(api, "note", "-k", "me", "Publish API")
	f.runIn(api, "note", "-p", "acme/site", "-k", "them", "Approve design")
	for _, args := range [][]string{{"effort", "add", "Launch invitations"}, {"effort", "link", "Launch invitations", "--note", "1", "--note", "2"}, {"effort", "depend", "Launch invitations", "--note", "1", "--after", "2"}} {
		if out, errs, code := f.run(args...); code != 0 {
			t.Fatalf("%v: %d %s %s", args, code, out, errs)
		}
	}
	out, errs, code := f.run("effort", "Launch invitations", "--json")
	if code != 0 || !strings.Contains(out, `"blocked": 1`) || !strings.Contains(out, `"open": 2`) {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	f.run("done", "2")
	out, _, code = f.run("effort", "Launch invitations", "--json")
	if code != 0 || !strings.Contains(out, `"blocked": 0`) || !strings.Contains(out, `"open": 1`) {
		t.Fatalf("%d %s", code, out)
	}
	out, _, code = f.runIn(api, "here", "--brief")
	if code != 0 || !strings.Contains(out, "Launch invitations") {
		t.Fatalf("%d %s", code, out)
	}
	if _, _, code := f.run("effort", "archive", "Launch invitations"); code != 0 {
		t.Fatal("archive failed")
	}
	out, _, _ = f.run("effort")
	if !strings.Contains(out, "0 efforts") {
		t.Fatal(out)
	}
	if _, _, code := f.run("effort", "link", "Launch invitations", "--after", "2"); code != 2 {
		t.Fatal("ignored irrelevant flag")
	}
}

func TestMemoryUnavailableIsVisibleInHereAndWorkspaceIsExplicit(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	f.writeConfig(fmt.Sprintf("roots = [%q]\n[projects.\"acme/*\"]\nworkspace = \"work\"\n", f.WS))
	f.runIn(p, "remember", "-k", "lesson", "Useful technique")
	out, _, code := f.run("recall", "--workspace", "work", "--json")
	if code != 0 || !strings.Contains(out, "Useful technique") {
		t.Fatalf("%d %s", code, out)
	}
	os.WriteFile(filepath.Join(f.SousHome, "memory.json"), []byte(`{"version":999}`), 0600)
	out, _, code = f.runIn(p, "here", "--json")
	if code != 0 || !strings.Contains(out, "memory unavailable") || !strings.Contains(out, `"available": false`) {
		t.Fatalf("%d %s", code, out)
	}
}

// What an agent keeps for itself (Claude Code's memory) is found with sous
// recall and shown at session start in its project only, from any agent,
// and sous never changes it.
func TestAgentMemoryIsReadNeverWritten(t *testing.T) {
	f := fixture(t)
	api := f.mkrepo("acme/api", true)
	site := f.mkrepo("acme/site", true)
	h, _ := harness.Find("claude")
	dir := h.Memory.Dir(f.Home, api)
	os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, "cursors.md")
	body := "---\nname: cursors\ndescription: Paginate with cursors, not offsets\nmetadata:\n  type: feedback\n---\nSam's clients page deep.\n"
	os.WriteFile(file, []byte(body), 0o644)

	out, errs, code := f.runIn(api, "recall", "cursors", "--json")
	var got struct {
		Records []struct{ ID, Kind, Path string }
	}
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || len(got.Records) != 1 || got.Records[0].Kind != "constraint" || got.Records[0].Path != file {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	id := got.Records[0].ID
	if out, _, _ := f.runIn(site, "recall"); !strings.Contains(out, "0 memories") {
		t.Fatalf("another project's recall: %s", out)
	}
	if out, _, _ := f.runIn(site, "recall", "--all"); !strings.Contains(out, id) {
		t.Fatalf("recall --all: %s", out)
	}
	if out, _, code := f.run("recall", "--show", id); code != 0 || !strings.Contains(out, "Sam's clients page deep.") || !strings.Contains(out, "sous only reads it") {
		t.Fatalf("show: %d %s", code, out)
	}
	_, errs, code = f.run("memory", "forget", id)
	if b, _ := os.ReadFile(file); code != exitUsage || !strings.Contains(errs, "sous only reads it") || string(b) != body {
		t.Fatalf("forget: %d %s", code, errs)
	}
	out, _, _ = f.runStdin(hookJSON(map[string]any{"cwd": api, "source": "startup"}), "hook", "session-start", "codex")
	if !strings.Contains(out, "Paginate with cursors") {
		t.Fatalf("session start: %s", out)
	}

	f.writeConfig("roots = [\"" + f.WS + "\"]\nagent_memory = \"off\"\n")
	if out, _, _ := f.runIn(api, "recall"); !strings.Contains(out, "0 memories") {
		t.Fatalf("agent_memory off: %s", out)
	}
	if _, errs, code := f.run("config", "agent_memory", "maybe"); code != exitUsage || !strings.Contains(errs, "on or off") {
		t.Fatalf("config: %d %s", code, errs)
	}
}
