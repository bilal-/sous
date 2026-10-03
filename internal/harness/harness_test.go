package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Install puts a hook, written as its command line, in a Claude Code style
// settings file.
func Install(file, event, command string) (bool, error) {
	args := shellSplit(command)
	return HooksJSON{}.Place(file, event, Cmd{Exe: args[0], Role: args[2], Agent: args[3]})
}

// Command and IsOurs are Cmd's two sides, the way the tests read best.
func Command(exe, role, agent string) string  { return Cmd{exe, role, agent}.String() }
func IsOurs(command, role, agent string) bool { return Cmd{Role: role, Agent: agent}.Ours(command) }

// LastAssistantText reads a Claude Code transcript.
func LastAssistantText(path string, max int) string { return claude.Last(path, max) }

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
// for every chunk read before it. Measured in bytes allocated, not time,
// so a slow machine cannot make it flaky: linear is about twice the line,
// quadratic would be many times more.
func TestLastAssistantTextIsLinearOnALongLine(t *testing.T) {
	const size = 32 << 20
	p := filepath.Join(t.TempDir(), "t.jsonl")
	f, _ := os.Create(p)
	f.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"before the long line"}]}}` + "\n")
	f.WriteString(`{"type":"user","message":{"content":"` + strings.Repeat("z", size) + `"}}` + "\n")
	f.Close()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := LastAssistantText(p, 300)
	runtime.ReadMemStats(&after)
	if got != "before the long line" {
		t.Fatalf("%q", got)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 4*size {
		t.Fatalf("allocated %d MB for a %d MB line", alloc>>20, size>>20)
	}
}

// Review: only a command that runs a sous binary is ours; echo, compound
// commands and prefixes are someone else's.
func TestIsOursRejectsLookalikes(t *testing.T) {
	for _, c := range []string{
		"echo /opt/sous hook session-start claude",
		"cd /x && /opt/sous hook session-start claude",
		"FOO=1 /opt/sous hook session-start claude",
	} {
		if IsOurs(c, RoleStart, "claude") {
			t.Errorf("%q is not ours", c)
		}
	}
	if !IsOurs("/Users/Sam Smith/bin/sous hook session-start claude", RoleStart, "claude") {
		t.Error("an old unquoted path with a space is ours")
	}
}

// Every harness is complete: a name that is unique, where its skills and
// hooks go, how to read its hooks, and at least a session start hook.
func TestEveryHarnessIsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, h := range All {
		if h.Name == "" || seen[h.Name] || h.Display == "" || h.Bin == "" || h.SkillDir == nil || h.HookFile == nil || h.Format == nil || h.Parse == nil || h.Last == nil {
			t.Errorf("incomplete: %+v", h)
		}
		seen[h.Name] = true
		if !strings.HasPrefix(h.SkillDir("/h"), "/h/") || !strings.HasPrefix(h.HookFile("/h"), "/h/") {
			t.Errorf("%s: skills and hooks live under home", h.Name)
		}
		var start bool
		for _, k := range h.Hooks {
			start = start || k.Role == RoleStart && k.Flag == ""
		}
		if !start {
			t.Errorf("%s: no session start hook", h.Name)
		}
		if got, ok := Find(h.Name); !ok || got.Name != h.Name {
			t.Errorf("Find(%s)", h.Name)
		}
	}
}

// Claude Code and Codex send the same hook input. Only a new session is
// fresh: resume, clear, compact and fork are not. Nothing, or something
// unreadable, is a fresh session in the hook's own folder.
func TestParseHookJSON(t *testing.T) {
	for _, h := range All {
		in := h.Parse([]byte(`{"session_id":"s1","cwd":"/code/acme/api","transcript_path":"/t.jsonl","source":"startup"}`))
		if in != (Input{SessionID: "s1", CWD: "/code/acme/api", TranscriptPath: "/t.jsonl", Fresh: true}) {
			t.Errorf("%s: %+v", h.Name, in)
		}
		for _, src := range []string{"resume", "clear", "compact", "fork"} {
			if h.Parse([]byte(`{"source":"` + src + `"}`)).Fresh {
				t.Errorf("%s: %s is not fresh", h.Name, src)
			}
		}
		for _, b := range []string{"", "not json"} {
			if in := h.Parse([]byte(b)); in != (Input{Fresh: true}) {
				t.Errorf("%s: %q: %+v", h.Name, b, in)
			}
		}
	}
}

// A Codex rollout keeps the last thing Codex said as a response_item
// message from the assistant; reasoning, tool calls and events come after
// it and are skipped.
func TestCodexLast(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rollout.jsonl")
	os.WriteFile(p, []byte(`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"old"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done: the"},{"type":"output_text","text":"index is rebuilt."}]}}
{"type":"response_item","payload":{"type":"reasoning","summary":[]}}
{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"Done"}}
`), 0o644)
	if got := codex.Last(p, 300); got != "Done: the index is rebuilt." {
		t.Fatalf("%q", got)
	}
	if got := codex.Last(p, 4); got != "Done" {
		t.Fatalf("cap: %q", got)
	}
}
