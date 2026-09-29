package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

func TestParseAndStartRoot(t *testing.T) {
	if _, ok := Parse(strings.NewReader("not json")); ok {
		t.Fatal("garbage must not parse")
	}
	if _, ok := Parse(strings.NewReader("")); ok {
		t.Fatal("empty must not parse")
	}
	in, ok := Parse(strings.NewReader(`{"session_id":"s1","cwd":"/tmp","source":"resume"}`))
	if !ok || in.Source != "resume" {
		t.Fatal("parse")
	}
}

func TestLastAssistantText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(`{"type":"user","message":{"content":"do it"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Done. Tests pass; next is the widget template."}]}}
{"type":"system","subtype":"x"}
`), 0o644)
	if got := LastAssistantText(p, 300); got != "Done. Tests pass; next is the widget template." {
		t.Fatalf("%q", got)
	}
	if got := LastAssistantText(p, 10); got != "Done. Test" {
		t.Fatalf("cap: %q", got)
	}
	if LastAssistantText("/nope", 300) != "" {
		t.Fatal("missing file → empty")
	}
	os.WriteFile(p, []byte("garbage\n"), 0o644)
	if LastAssistantText(p, 300) != "" {
		t.Fatal("garbage → empty")
	}
}

func TestInstallIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"/other/thing"}]}]}}`), 0o644)
	added, err := Install(p, "SessionStart", "/bin/sous hook session-start claude")
	if err != nil || !added {
		t.Fatal(added, err)
	}
	added, _ = Install(p, "SessionStart", "/bin/sous hook session-start claude")
	if added {
		t.Fatal("second install must be a no-op")
	}
	Install(p, "SessionEnd", "/bin/sous hook session-end claude")
	var doc map[string]any
	b, _ := os.ReadFile(p)
	json.Unmarshal(b, &doc)
	hooks := doc["hooks"].(map[string]any)
	if len(hooks["SessionStart"].([]any)) != 2 || len(hooks["SessionEnd"].([]any)) != 1 {
		t.Fatalf("%s", b)
	}
	if !strings.Contains(string(b), `"timeout": 10`) || !strings.Contains(string(b), "/other/thing") {
		t.Fatalf("timeout or existing hook lost:\n%s", b)
	}
	missing := filepath.Join(t.TempDir(), "new", "hooks.json")
	if added, err := Install(missing, "SessionStart", "x"); err != nil || !added {
		t.Fatal("must create the file and parents", err)
	}
}

func TestStartRoot(t *testing.T) {
	dir := t.TempDir()
	if _, ok := StartRoot(Input{CWD: dir, Source: "startup"}, ""); ok {
		t.Fatal("non-repo cwd must not resolve")
	}
	for _, src := range []string{"resume", "clear", "compact"} {
		if _, ok := StartRoot(Input{CWD: dir, Source: src}, ""); ok {
			t.Fatalf("%s must not inject", src)
		}
	}
	if _, ok := StartRoot(Input{}, dir); ok {
		t.Fatal("empty cwd falls back to the caller's directory, which is not a repo here")
	}
	repo := testutil.Repo(t, filepath.Join(dir, "chime"), false, "")
	if root, ok := StartRoot(Input{}, repo); !ok || filepath.Base(root) != "chime" {
		t.Fatalf("fallback to the caller's directory: %q %v", root, ok)
	}
}

// A huge transcript line (a big tool result) must not stop the reader
// early and leave an older message as "how the session ended".
func TestLastAssistantTextSurvivesHugeLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	huge := `{"type":"user","message":{"content":"` + strings.Repeat("x", 17<<20) + `"}}`
	body := `{"type":"assistant","message":{"content":[{"type":"text","text":"old"}]}}` + "\n" + huge + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"the real last words"}]}}` + "\n"
	os.WriteFile(p, []byte(body), 0o644)
	if got := LastAssistantText(p, 300); got != "the real last words" {
		t.Fatalf("%q", got)
	}
}

// Moving sous (source build to Homebrew, say) must update its hook, not add
// a second one that runs sous twice per session.
func TestInstallReplacesAHookFromAnotherPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"other-tool start"}]},{"hooks":[{"type":"command","command":"/old/bin/sous hook session-start claude","timeout":10}]}]}}`), 0o644)
	changed, err := Install(p, "SessionStart", "/new/bin/sous hook session-start claude")
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	b, _ := os.ReadFile(p)
	if strings.Count(string(b), "hook session-start claude") != 1 || !strings.Contains(string(b), "/new/bin/sous") || !strings.Contains(string(b), "other-tool start") {
		t.Fatalf("%s", b)
	}
	if changed, _ := Install(p, "SessionStart", "/new/bin/sous hook session-start claude"); changed {
		t.Fatal("second install changes nothing")
	}
}
