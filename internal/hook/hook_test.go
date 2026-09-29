package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/store"
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
	if added, err := Install(missing, "SessionStart", "/bin/sous hook session-start claude"); err != nil || !added {
		t.Fatal("must create the file and parents", err)
	}
}

func TestStartRoot(t *testing.T) {
	dir := t.TempDir()
	if _, ok := StartRoot(Input{CWD: dir, Source: "startup"}, "", ""); ok {
		t.Fatal("non-repo cwd must not resolve")
	}
	for _, src := range []string{"resume", "clear", "compact"} {
		if _, ok := StartRoot(Input{CWD: dir, Source: src}, "", ""); ok {
			t.Fatalf("%s must not inject", src)
		}
	}
	if _, ok := StartRoot(Input{}, dir, ""); ok {
		t.Fatal("empty cwd falls back to the caller's directory, which is not a repo here")
	}
	repo := testutil.Repo(t, filepath.Join(dir, "chime"), false, "")
	if root, ok := StartRoot(Input{}, repo, ""); !ok || filepath.Base(root) != "chime" {
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

// Review: only a sous hook is replaced: the program must be sous and the
// arguments exactly ours. Duplicates collapse to one; paths with spaces are
// quoted.
func TestInstallMatchesOnlyRealSousHooks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{"hooks":{"SessionStart":[
{"hooks":[{"type":"command","command":"/home/dev/code/sous-tools/wrap hook session-start claude"}]},
{"hooks":[{"type":"command","command":"/old/bin/sous hook session-start claude"}]},
{"hooks":[{"type":"command","command":"/older/bin/sous hook session-start claude"}]}]}}`), 0o644)
	exe := "/Applications/My Tools/sous"
	if _, err := Install(p, "SessionStart", Command(exe, "session-start", "claude")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, "sous-tools/wrap hook session-start claude") {
		t.Fatalf("someone else's hook must be left alone:\n%s", s)
	}
	if strings.Count(s, "bin/sous hook") != 0 || strings.Count(s, `My Tools/sous'`) != 1 {
		t.Fatalf("old sous hooks become one, quoted:\n%s", s)
	}
	if !IsOurs(Command(exe, "session-start", "claude"), "session-start", "claude") || IsOurs("/x/sous-tools/wrap hook session-start claude", "session-start", "claude") {
		t.Fatal("IsOurs")
	}
}

// Review: the last message is found by reading the transcript's end, so a
// huge transcript is read in bounded time; an assistant line longer than
// the tail window still counts.
func TestLastAssistantTextReadsOnlyTheTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	f, _ := os.Create(p)
	for i := 0; i < 40; i++ { // about 40 MB of tool output before the end
		f.WriteString(`{"type":"user","message":{"content":"` + strings.Repeat("x", 1<<20) + `"}}` + "\n")
	}
	f.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"done for today"}]}}` + "\n")
	f.WriteString(`{"type":"user","message":{"content":"` + strings.Repeat("y", 3<<20) + `"}}` + "\n")
	f.Close()
	start := time.Now()
	got := LastAssistantText(p, 300)
	if got != "done for today" {
		t.Fatalf("%q", got)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("took %v", el)
	}
}

// Recording a session end, with its guard, lives here, not in the CLI.
func TestRecordEndStoresTheLastMessage(t *testing.T) {
	repo := testutil.Repo(t, filepath.Join(t.TempDir(), "api"), true, "")
	tr := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(tr, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"all green"}]}}`+"\n"), 0o644)
	s := &store.Store{Home: t.TempDir()}
	RecordEnd(s, Input{CWD: repo, TranscriptPath: tr}, "claude", "", "", time.Now())
	all, _ := session.All(s)
	if len(all) != 1 {
		t.Fatalf("%v", all)
	}
	for _, sess := range all {
		if sess.LastMessage == nil || *sess.LastMessage != "all green" || sess.SessionID != "unknown" {
			t.Fatalf("%+v", sess)
		}
	}
}

// Review: odd settings (a non-object entry, an empty group) must neither
// crash setup nor be written back as null; an old unquoted hook whose path
// has a space is still recognized; hooks under different matchers are kept.
func TestInstallCopesWithOddSettings(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{"hooks":{"SessionStart":[null, "x", {"hooks":[]},
{"matcher":"startup","hooks":[{"type":"command","command":"/Users/Sam Smith/bin/sous hook session-start claude"}]},
{"matcher":"resume","hooks":[{"type":"command","command":"/opt/sous/bin/sous hook session-start claude"}]}]}}`), 0o644)
	if _, err := Install(p, "SessionStart", Command("/opt/new/sous", "session-start", "claude")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if strings.Contains(s, `"hooks": null`) || !strings.Contains(s, `"x"`) {
		t.Fatalf("an empty group must stay [] and odd entries must stay:\n%s", s)
	}
	if strings.Contains(s, "Sam Smith") || strings.Count(s, "/opt/new/sous hook session-start claude") != 2 {
		t.Fatalf("old hooks replaced per matcher, spaced path recognized:\n%s", s)
	}
}

// Review: one very long line (a big tool result) must not be copied again
// for every chunk read before it; reading stays linear.
func TestLastAssistantTextIsLinearOnALongLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	f, _ := os.Create(p)
	f.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"before the long line"}]}}` + "\n")
	f.WriteString(`{"type":"user","message":{"content":"` + strings.Repeat("z", 120<<20) + `"}}` + "\n")
	f.Close()
	start := time.Now()
	if got := LastAssistantText(p, 300); got != "before the long line" {
		t.Fatalf("%q", got)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("took %v", el)
	}
}
