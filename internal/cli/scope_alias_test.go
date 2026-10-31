package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/board"
)

func f3SymlinkProject(t *testing.T) (*fx, string) {
	t.Helper()
	f := fixture(t)
	initial := f.mkrepo("other/checkout", true)
	physical := filepath.Join(f.Home, "checkout")
	if err := os.Rename(initial, physical); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(f.WS, "acme", "api")
	if err := os.MkdirAll(filepath.Dir(alias), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	return f, physical
}

func TestF3NamedBriefingRetainsMemoryScope(t *testing.T) {
	f, _ := f3SymlinkProject(t)
	if out, errs, code := f.run("remember", "-p", "acme/api", "Named context"); code != 0 {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, errs, code := f.run("acme/api", "--json")
	var got board.HereData
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	if len(got.Context.Memories) != 1 {
		t.Fatalf("named project lost its scope: %s", out)
	}
}

func TestF3ImplicitRecallRefusesDifferentAliasScope(t *testing.T) {
	f, physical := f3SymlinkProject(t)
	if out, errs, code := f.run("remember", "-p", "acme/api", "Named context"); code != 0 {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, errs, code := f.runIn(physical, "recall", "--json")
	if code != 2 || !strings.Contains(out, `"error"`) || !strings.Contains(errs, "ambig") || strings.Contains(out, "Named context") {
		t.Fatalf("ambiguous context looked available: %d %s %s", code, out, errs)
	}
}

func TestF3ExplicitScopesRetainIndependentRetryKeys(t *testing.T) {
	f, physical := f3SymlinkProject(t)
	var ids []string
	for _, selector := range []string{"acme/api", physical} {
		var first changedJSON
		for i := 0; i < 2; i++ {
			out, errs, code := f.run("remember", "-p", selector, "--key", "handoff", "Existing context", "--json")
			var got changedJSON
			if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
				t.Fatalf("%d %s %s", code, out, errs)
			}
			if i == 0 {
				first = got
			} else if got.ID != first.ID || got.Did != "already_remembered" {
				t.Fatalf("retry changed identity: %#v %#v", first, got)
			}
		}
		ids = append(ids, first.ID)
	}
	if ids[0] == ids[1] {
		t.Fatalf("different workspaces merged: %v", ids)
	}
}

func TestF3ImplicitBriefingAndCaptureFailClosed(t *testing.T) {
	f, physical := f3SymlinkProject(t)
	f.run("remember", "-p", "acme/api", "Private named context")
	before, _ := os.ReadFile(filepath.Join(f.SousHome, "memory.json"))
	for _, args := range [][]string{{"here", "--json"}, {"here", "--brief"}} {
		out, errs, code := f.runIn(physical, args...)
		if code != 0 || strings.Contains(out, "Private named context") || !strings.Contains(out, "ambiguous private context") {
			t.Fatalf("%v: %d %s %s", args, code, out, errs)
		}
		if args[1] == "--json" && !strings.Contains(out, `"available": false`) {
			t.Fatal(out)
		}
	}
	for _, args := range [][]string{{"remember", "Do not save"}, {"remember", "-p", ".", "Do not save"}, {"effort", "--project", "."}} {
		out, errs, code := f.runIn(physical, append(args, "--json")...)
		if code != 2 || !strings.Contains(out, `"error"`) || !strings.Contains(errs, "ambiguous") {
			t.Fatalf("%v: %d %s %s", args, code, out, errs)
		}
	}
	after, _ := os.ReadFile(filepath.Join(f.SousHome, "memory.json"))
	if string(before) != string(after) {
		t.Fatal("scope selection rewrote private state")
	}
	out, errs, code := f.run("here", physical, "--json")
	if code != 0 || !strings.Contains(out, `"available": true`) || strings.Contains(out, "Private named context") {
		t.Fatalf("explicit physical scope: %d %s %s", code, out, errs)
	}
}

func TestF3MissingRootMakesPrivateContextUnavailable(t *testing.T) {
	f := fixture(t)
	physical := f.mkrepo("acme/api", true)
	f.writeConfig("roots = [\"" + filepath.Join(f.Home, "missing") + "\"]\n")
	out, errs, code := f.runIn(physical, "recall", "--json")
	if code != 1 || !strings.Contains(out, `"error"`) || !strings.Contains(errs, "unavailable") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	out, errs, code = f.runIn(physical, "here", "--json")
	if code != 0 || !strings.Contains(out, `"available": false`) {
		t.Fatalf("%d %s %s", code, out, errs)
	}
}

func TestF3GoAndHooksCarryTheSelection(t *testing.T) {
	f, physical := f3SymlinkProject(t)
	f.run("remember", "-p", "acme/api", "Private named context")
	f.bin("claude", "cat \"$SOUS_HERE_FILE\"")
	out, err := sousCmd(f, f.Home, "go", "acme/api").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Private named context") {
		t.Fatalf("named go: %v %s", err, out)
	}
	out, err = sousCmd(f, physical, "go", ".").CombinedOutput()
	if err != nil || strings.Contains(string(out), "Private named context") || !strings.Contains(string(out), "ambiguous private context") {
		t.Fatalf("implicit go: %v %s", err, out)
	}
	text, errs, code := f.runStdin(hookJSON(map[string]any{"cwd": physical, "source": "startup"}), "hook", "session-start", "claude")
	if code != 0 || errs != "" || strings.Contains(text, "Private named context") || !strings.Contains(text, "ambiguous private context") {
		t.Fatalf("hook: %d %s %s", code, text, errs)
	}
}

func TestF3BadConfigDoesNotMakeImplicitContextAvailable(t *testing.T) {
	f := fixture(t)
	p := f.mkrepo("acme/api", true)
	f.runIn(p, "remember", "Physical memory")
	f.writeConfig("[broken")
	for _, args := range [][]string{{"here", "--json"}, {"here", "--brief"}} {
		out, _, code := f.runIn(p, args...)
		if code != 0 || strings.Contains(out, "Physical memory") || !strings.Contains(out, "private context unavailable") {
			t.Fatalf("%d %s", code, out)
		}
	}
	out, errs, code := f.runStdin(hookJSON(map[string]any{"cwd": p, "source": "startup"}), "hook", "session-start", "claude")
	if code != 0 || errs != "" || strings.Contains(out, "Physical memory") || !strings.Contains(out, "private context unavailable") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
}

func TestF3RunBriefingsCarrySelection(t *testing.T) {
	f, physical := f3SymlinkProject(t)
	request := filepath.Join(f.Home, "request.json")
	fake := f.plugin("sous-runner-fake", fmt.Sprintf(`case "$1" in
 start) cat > %q; echo fake:1;;
 status) echo '{"v":0,"state":"running"}';;
 esac`, request))
	f.writeConfig(fmt.Sprintf("roots = [%q]\nplugins = [%q]\nrunner = \"fake\"\n[projects.%q]\nremote_runs = \"true\"\n", f.WS, fake, filepath.Base(filepath.Dir(physical))+"/"+filepath.Base(physical)))
	f.run("remember", "-p", "acme/api", "Private named context")
	for i, args := range [][]string{
		{"go", "acme/api", "--run", "Named task", "--json"},
		{"go", ".", "--run", "Implicit task", "--json"},
		{"integration", "--protocol", "1", "--json", "run", "--op", "op-scope000000000001", "--", "."},
	} {
		var out, errs bytes.Buffer
		code := run(args, physical, strings.NewReader("Integration task"), &out, &errs)
		if code != 0 {
			t.Fatalf("%v: %d %s %s", args, code, &out, &errs)
		}
		data, err := os.ReadFile(request)
		var req struct {
			HereFile string `json:"here_file"`
		}
		if err != nil || json.Unmarshal(data, &req) != nil {
			t.Fatalf("request: %s %v", data, err)
		}
		briefing, err := os.ReadFile(req.HereFile)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && !strings.Contains(string(briefing), "Private named context") {
			t.Fatalf("named run: %s", briefing)
		}
		if i > 0 && (strings.Contains(string(briefing), "Private named context") || !strings.Contains(string(briefing), "ambiguous private context")) {
			t.Fatalf("implicit run: %s", briefing)
		}
	}
}

func TestF3AliasHistoryHintsExecuteFromAnotherDirectory(t *testing.T) {
	f, _ := f3SymlinkProject(t)
	f.run("remember", "-p", "acme/api", "Named decision")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOUS_HINT_BINARY", exe)
	f.bin("sous", `export SOUS_TEST_AS_BINARY=1; exec "$SOUS_HINT_BINARY" "$@"`)
	for _, args := range [][]string{{"acme/api", "--brief"}, {"recall", "--show", "m:1"}} {
		out, errs, code := f.run(args...)
		if code != 0 || !strings.Contains(out, "workspace history") {
			t.Fatalf("%v: %d %s %s", args, code, out, errs)
		}
		var hint string
		for _, line := range strings.Split(out, "\n") {
			if start := strings.Index(line, "sous recall"); start >= 0 && strings.Contains(line[start:], "--history") {
				hint = strings.Split(line[start:], "--history")[0] + "--history"
				break
			}
		}
		if hint == "" {
			t.Fatal(out)
		}
		cmd := exec.Command("/bin/sh", "-c", hint+" --json")
		cmd.Dir = f.Home
		got, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(got), "Named decision") {
			t.Fatalf("%s: %v %s", hint, err, got)
		}
	}
}
