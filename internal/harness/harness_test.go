package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/harness/harnesstest"
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
func LastAssistantText(path string) string { return claude.Last(path) }

func TestLastAssistantText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(`{"type":"user","message":{"content":"do it"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Done. Tests pass; next is the widget template."}]}}
{"type":"system","subtype":"x"}
`), 0o644)
	if got := LastAssistantText(p); got != "Done. Tests pass; next is the widget template." {
		t.Fatalf("%q", got)
	}
	if LastAssistantText("/nope") != "" {
		t.Fatal("missing file → empty")
	}
	os.WriteFile(p, []byte("garbage\n"), 0o644)
	if LastAssistantText(p) != "" {
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
	if got := LastAssistantText(p); got != "the real last words" {
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

// Only a sous hook is replaced: the program must be sous and the
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

// The last message is found by reading the transcript's end, so a
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
	got := LastAssistantText(p)
	if got != "done for today" {
		t.Fatalf("%q", got)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("took %v", el)
	}
}

// Odd settings (a non-object entry, an empty group) must neither
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

// One very long line (a big tool result) must not be copied again
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
	got := LastAssistantText(p)
	runtime.ReadMemStats(&after)
	if got != "before the long line" {
		t.Fatalf("%q", got)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 4*size {
		t.Fatalf("allocated %d MB for a %d MB line", alloc>>20, size>>20)
	}
}

// Only a command that runs a sous binary is ours; echo, compound
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
		if h.Name == "" || seen[h.Name] || h.Display == "" || h.Bin == "" || h.HookFile == nil || h.Format == nil || h.Parse == nil {
			t.Errorf("incomplete: %+v", h)
		}
		seen[h.Name] = true
		if h.SkillDir != nil && !strings.HasPrefix(h.SkillDir("/h"), "/h/") || !strings.HasPrefix(h.HookFile("/h"), "/h/") {
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
	for _, h := range []Harness{claude, codex} {
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
	if got := codex.Last(p); got != "Done: the index is rebuilt." {
		t.Fatalf("%q", got)
	}
}

// Antigravity's hooks send camelCase JSON, the project first among the
// workspace paths. Its first model call in a new conversation is the
// session start; later calls, and the first call of a resumed one, are not.
func TestParseAgy(t *testing.T) {
	in := parseAgy([]byte(`{"conversationId":"c1","workspacePaths":["/code/acme/api","/x"],"transcriptPath":"/t.jsonl","invocationNum":0,"initialNumSteps":1}`))
	if in != (Input{SessionID: "c1", CWD: "/code/acme/api", TranscriptPath: "/t.jsonl", Fresh: true}) {
		t.Fatalf("%+v", in)
	}
	for _, b := range []string{`{"invocationNum":1,"initialNumSteps":1}`, `{"invocationNum":0,"initialNumSteps":4}`, `{"executionNum":0,"terminationReason":"NO_TOOL_CALL"}`} {
		if parseAgy([]byte(b)).Fresh {
			t.Errorf("%s is not a new session", b)
		}
	}
	if in := parseAgy([]byte("not json")); !in.Fresh {
		t.Fatal("unreadable input is a new session in the hook's folder")
	}
}

// Antigravity reads every hook's answer as JSON: context to add before the
// model runs, or nothing.
func TestAgyReply(t *testing.T) {
	if got := antigravity.ReplyTo(RoleStart, "[sous] hello"); got != `{"injectSteps":[{"ephemeralMessage":"[sous] hello"}]}` {
		t.Fatal(got)
	}
	for _, c := range [][2]string{{RoleStart, ""}, {RoleEnd, ""}} {
		if got := antigravity.ReplyTo(c[0], c[1]); got != "{}" {
			t.Errorf("%v: %q", c, got)
		}
	}
	if claude.ReplyTo(RoleStart, "") != "" || claude.ReplyTo(RoleStart, "x") != "x" {
		t.Fatal("Claude Code takes plain text")
	}
}

// The last thing Antigravity said is the model's last planner response; a
// run that was refused a command says so, as needing the person.
func TestAgyLastAndRunAnswer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "transcript_full.jsonl")
	os.WriteFile(p, []byte(`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","content":"fix it"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","content":"Done: the index is rebuilt."}
{"step_index":2,"source":"SYSTEM_SDK","type":"EPHEMERAL_MESSAGE","content":"[sous] ..."}
`), 0o644)
	if got := antigravity.Last(p); got != "Done: the index is rebuilt." {
		t.Fatalf("%q", got)
	}
	log := []byte(`{"conversation_id":"c9","status":"SUCCESS","response":"","denied_actions":[{"action":"command","display_name":"RunCommand"}]}`)
	if s := antigravity.Headless.Session(log); s != "c9" {
		t.Fatal(s)
	}
	if got := antigravity.Headless.Last(Run{}, log); !strings.Contains(got, "SOUS: needs you Antigravity refused RunCommand (command)") {
		t.Fatalf("%q", got)
	}
	args := antigravity.Headless.Start(Run{Prompt: "-x starts with a dash"})
	if args[len(args)-1] != "-p=-x starts with a dash" {
		t.Fatalf("a prompt is never read as a flag: %q", args)
	}
}

// sous's hooks live in a group of their own in Antigravity's hooks.json;
// another group (herdr's) is left as it is; placing twice changes nothing.
func TestNamedHooksJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hooks.json")
	os.WriteFile(p, []byte(`{"herdr":{"PreInvocation":[{"command":"bash herdr.sh","type":"command"}]}}`), 0o644)
	cmd := Cmd{Exe: "/bin/sous", Role: RoleStart, Agent: "agy"}
	for i, want := range []bool{true, false} {
		if changed, err := (NamedHooksJSON{}).Place(p, "PreInvocation", cmd); err != nil || changed != want {
			t.Fatalf("place %d: %v %v", i, changed, err)
		}
	}
	got, err := (NamedHooksJSON{}).Commands(p, "PreInvocation")
	if err != nil || len(got) != 2 || got[0] != "bash herdr.sh" || got[1] != "/bin/sous hook session-start agy" {
		t.Fatalf("%q %v", got, err)
	}
	moved := Cmd{Exe: "/opt/sous", Role: RoleStart, Agent: "agy"}
	if changed, _ := (NamedHooksJSON{}).Place(p, "PreInvocation", moved); !changed {
		t.Fatal("a sous that moved replaces its old hook")
	}
	if got, _ := (NamedHooksJSON{}).Commands(p, "PreInvocation"); len(got) != 2 || got[1] != "/opt/sous hook session-start agy" {
		t.Fatalf("%q", got)
	}
}

// sous owns its opencode plugin whole: placing a hook writes the file with
// every command sous gave it, and reads them back; a second place of the
// same changes nothing; a file sous did not write is not taken for one.
func TestOpencodePlugin(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plugins", "sous.js")
	start := Cmd{Exe: "/opt/my sous/sous", Role: RoleStart, Agent: "opencode"}
	end := Cmd{Exe: "/opt/my sous/sous", Role: RoleEnd, Agent: "opencode"}
	for _, c := range []Cmd{start, end} {
		if changed, err := (OpencodePlugin{}).Place(p, c.Role, c); err != nil || !changed {
			t.Fatal(changed, err)
		}
	}
	if changed, _ := (OpencodePlugin{}).Place(p, RoleStart, start); changed {
		t.Fatal("placing again changes nothing")
	}
	for _, c := range []Cmd{start, end} {
		got, err := (OpencodePlugin{}).Commands(p, c.Role)
		if err != nil || len(got) != 1 || got[0] != c.String() {
			t.Fatalf("%s: %q %v", c.Role, got, err)
		}
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `const start = "'/opt/my sous/sous' hook session-start opencode"`) {
		t.Fatalf("the command is a JS string:\n%s", b)
	}
	other := filepath.Join(t.TempDir(), "mine.js")
	os.WriteFile(other, []byte("export const Mine = async () => ({})\n"), 0o644)
	if _, err := (OpencodePlugin{}).Commands(other, RoleStart); err == nil {
		t.Fatal("someone else's plugin is not sous's")
	}
	if _, err := (OpencodePlugin{}).Place(other, RoleStart, start); err == nil {
		t.Fatal("someone else's file is never written over")
	}
	if _, err := (OpencodePlugin{}).Place(p, RoleStart, Cmd{Exe: "/x\nevil", Role: RoleStart, Agent: "opencode"}); err == nil {
		t.Fatal("a command with a line break would break out of its comment")
	}
}

// opencode run's JSON events give the session and the last thing said.
func TestOpencodeRunAnswer(t *testing.T) {
	log := []byte(`{"type":"step_start","sessionID":"ses_1","part":{}}
{"type":"text","sessionID":"ses_1","part":{"type":"text","text":"working on it"}}
{"type":"tool_use","sessionID":"ses_1","part":{"tool":"bash"}}
{"type":"text","sessionID":"ses_1","part":{"type":"text","text":"SOUS: done added hello.txt"}}
`)
	if s := opencode.Headless.Session(log); s != "ses_1" {
		t.Fatal(s)
	}
	if got := opencode.Headless.Last(Run{}, log); got != "SOUS: done added hello.txt" {
		t.Fatal(got)
	}
	for _, args := range [][]string{opencode.Headless.Start(Run{Prompt: "-x", Worktree: "/runs/u1/worktree"}), opencode.Headless.Resume(Run{Answer: "-y", Session: "ses_1", Worktree: "/runs/u1/worktree"})} {
		if args[len(args)-2] != "--" || !slices.Contains(args, "--dir") || args[slices.Index(args, "--dir")+1] != "/runs/u1/worktree" {
			t.Fatalf("a prompt is never a flag, and opencode is told where to work: %q", args)
		}
	}
}

// The opencode plugin speaks the hook input sous reads: run under node
// (when there is one), what it sends at a session's start and when it goes
// idle parses as a fresh start in its folder, and an end with the last
// thing the agent said.
func TestOpencodePluginSpeaksTheHookInput(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	dir := t.TempDir()
	got := filepath.Join(dir, "got")
	fake := filepath.Join(dir, "sous")
	os.WriteFile(fake, []byte("#!/bin/sh\nin=$(cat)\nprintf '%s\\n' \"$in\" >> "+got+"\necho context\n"), 0o755)
	plugin := filepath.Join(dir, "sous.mjs")
	for _, role := range []string{RoleStart, RoleEnd} {
		(OpencodePlugin{}).Place(plugin, role, Cmd{Exe: fake, Role: role, Agent: "opencode"})
	}
	drive := filepath.Join(dir, "drive.mjs")
	os.WriteFile(drive, []byte(`import { Sous } from "./sous.mjs"
const h = await Sous({ directory: "/code/acme/api" })
const out = { system: [] }
await h["experimental.chat.system.transform"]({ sessionID: "ses_1" }, out)
await h["experimental.chat.system.transform"]({ sessionID: "ses_1" }, out)
if (out.system.join() !== "context,context") throw new Error("system: " + out.system)
await h.event({ event: { type: "message.updated", properties: { info: { role: "assistant", id: "m1" } } } })
await h.event({ event: { type: "message.part.updated", properties: { part: { type: "text", messageID: "m1", sessionID: "ses_1", text: "all green" } } } })
await h.event({ event: { type: "session.idle", properties: { sessionID: "ses_1" } } })
const after = await Sous({ directory: "/code/acme/api" })
await after.event({ event: { type: "message.updated", properties: { info: { role: "assistant", id: "m2" } } } })
await after.event({ event: { type: "message.part.updated", properties: { part: { type: "text", messageID: "m2", sessionID: "ses_2", text: "earlier" } } } })
await after["experimental.chat.system.transform"]({ sessionID: "ses_2" }, { system: [] })
await after["experimental.chat.system.transform"]({}, { system: [] })
`), 0o644)
	if out, err := exec.Command(node, drive).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var lines []string
	for deadline := time.Now().Add(5 * time.Second); len(lines) < 3 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		b, _ := os.ReadFile(got) // the end is told without waiting
		lines = strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	slices.Sort(lines)
	byKind := map[string]Input{}
	for _, l := range lines {
		in := opencode.Parse([]byte(l))
		switch {
		case in.LastMessage != "":
			byKind["end"] = in
		case in.Fresh:
			byKind["start"] = in
		default:
			byKind["resumed"] = in
		}
	}
	if len(lines) != 3 || len(byKind) != 3 {
		t.Fatalf("one start (said once), one end, one resumed start, none without a session: %q", lines)
	}
	if in := byKind["start"]; in != (Input{SessionID: "ses_1", CWD: "/code/acme/api", Fresh: true}) {
		t.Errorf("start: %+v", in)
	}
	if in := byKind["end"]; in.SessionID != "ses_1" || in.CWD != "/code/acme/api" || in.LastMessage != "all green" {
		t.Errorf("end: %+v", in)
	}
	if in := byKind["resumed"]; in.SessionID != "ses_2" || in.Fresh {
		t.Errorf("a session already under way is not a new one: %+v", in)
	}
}

// Odd hook files: one holding null is an empty one, not a crash; values
// that are not hook groups (a "$schema") are passed over when reading.
func TestHookFilesThatAreOdd(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []Format{HooksJSON{}, NamedHooksJSON{}} {
		p := filepath.Join(dir, fmt.Sprintf("%T.json", f))
		os.WriteFile(p, []byte("null"), 0o644)
		cmd := Cmd{Exe: "/bin/sous", Role: RoleStart, Agent: "x"}
		if changed, err := f.Place(p, "Start", cmd); err != nil || !changed {
			t.Fatalf("%T on null: %v %v", f, changed, err)
		}
		if got, err := f.Commands(p, "Start"); err != nil || len(got) != 1 {
			t.Fatalf("%T: %q %v", f, got, err)
		}
	}
	p := filepath.Join(dir, "agy.json")
	os.WriteFile(p, []byte(`{"$schema":"https://example.org/s.json","version":1,"herdr":{"Stop":[{"command":"h"}]}}`), 0o644)
	if got, err := (NamedHooksJSON{}).Commands(p, "Stop"); err != nil || len(got) != 1 || got[0] != "h" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestLastJSONReadsAnyLayout(t *testing.T) {
	type out struct {
		ID string `json:"id"`
	}
	has := func(o out) bool { return o.ID != "" }
	for log, want := range map[string]string{
		"":                                     "",
		"warming up\n{\"id\":\"a\"}\n":         "a",
		"{\"id\":\"a\"}\n{\"id\":\"b\"}":       "b",
		"{\n  \"id\": \"a\"\n}\nbye\n":         "a",
		"{\"id\":\"a\"}{\"other\":1}\n[1,2]\n": "a",
		"{\"id\":\"a\"}\n{\"id\": \"cut sho":   "a",
		"note: {\"id\":\"x\"} in a line\n":     "",
	} {
		if got := lastJSON([]byte(log), has).ID; got != want {
			t.Errorf("%q: %q, want %q", log, got, want)
		}
	}
}

// The fake agent the runner tests use speaks for every harness that runs
// headless: each finds its session and its last words in what it printed.
func TestTheFakeAgentSpeaksForEveryHarness(t *testing.T) {
	dir := t.TempDir()
	r := Run{Dir: dir}
	script := filepath.Join(dir, "agent")
	os.WriteFile(script, []byte("#!/bin/sh\n"+harnesstest.Says("SOUS: done all green")), 0o755)
	log, err := exec.Command(script, "-o", codexLastFile(r)).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range All {
		if h.Headless == nil {
			continue
		}
		if got := h.Headless.Session(log); got != harnesstest.Session {
			t.Errorf("%s: session %q", h.Name, got)
		}
		if got := h.Headless.Last(r, log); got != "SOUS: done all green" {
			t.Errorf("%s: last %q", h.Name, got)
		}
	}
}
