package harness

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// Antigravity (agy): skills and hooks in ~/.gemini/config, which both the
// app and the agy CLI read. Its hooks answer in JSON. There is no session
// start: PreInvocation runs before every model call, and the first call of
// a new conversation is its start. Stop runs as each turn ends, so the last
// one is how the session ended.
var antigravity = Harness{
	Name:    "agy",
	Display: "Antigravity",
	Bin:     "agy",
	Present: func(home string) bool {
		return Installed(home, "agy", ".gemini/config", ".gemini/antigravity", ".gemini/antigravity-cli")
	},
	SkillDir: func(home string) string { return filepath.Join(home, ".gemini", "config", "skills") },
	HookFile: func(home string) string { return filepath.Join(home, ".gemini", "config", "hooks.json") },
	Format:   NamedHooksJSON{},
	Hooks: []Hook{
		{Role: RoleStart, Event: "PreInvocation"},
		{Role: RoleEnd, Event: "Stop"},
	},
	Parse: parseAgy,
	// Every Antigravity hook prints a JSON object: what to add before the
	// model runs, or nothing ({}).
	Reply: func(role, say string) string {
		if role != RoleStart || strings.TrimSpace(say) == "" {
			return "{}"
		}
		b, _ := json.Marshal(map[string]any{"injectSteps": []any{map[string]string{"ephemeralMessage": say}}})
		return string(b)
	},
	Last: func(transcript string, max int) string {
		return lastLine(transcript, max, `"PLANNER_RESPONSE"`, agyText)
	},
	// agy -p runs one prompt with nobody there: file edits are accepted,
	// and a command the person's Antigravity settings do not allow is
	// refused (denied_actions), never approved.
	Headless: &Headless{
		Start: func(r Run) []string { return append(agyArgs(), "-p="+r.Prompt) },
		Resume: func(r Run) []string {
			return append(agyArgs(), "--conversation", r.Session, "-p="+r.Answer)
		},
		Session: func(log []byte) string { return agyResult(log).ConversationID },
		Last:    func(_ Run, log []byte) string { return agyResult(log).said() },
	},
}

// parseAgy reads Antigravity's hook input: camelCase JSON, the project
// first among workspacePaths. A new conversation's first model call is
// invocation 0 with the person's message as its only step.
func parseAgy(b []byte) Input {
	var in struct {
		ConversationID  string   `json:"conversationId"`
		WorkspacePaths  []string `json:"workspacePaths"`
		TranscriptPath  string   `json:"transcriptPath"`
		InvocationNum   *int     `json:"invocationNum"`
		InitialNumSteps *int     `json:"initialNumSteps"`
	}
	if json.Unmarshal(b, &in) != nil {
		return Input{Fresh: true}
	}
	out := Input{SessionID: in.ConversationID, TranscriptPath: in.TranscriptPath}
	if len(in.WorkspacePaths) > 0 {
		out.CWD = in.WorkspacePaths[0]
	}
	out.Fresh = in.InvocationNum != nil && *in.InvocationNum == 0 && in.InitialNumSteps != nil && *in.InitialNumSteps <= 1
	return out
}

// agyText is what the model said on one transcript line, or "".
func agyText(line []byte) string {
	var l struct {
		Source  string `json:"source"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if json.Unmarshal(line, &l) != nil || l.Source != "MODEL" || l.Type != "PLANNER_RESPONSE" {
		return ""
	}
	return l.Content
}

func agyArgs() []string { return []string{"--output-format", "json", "--mode", "accept-edits"} }

// agyRun is what agy -p --output-format json prints at the end.
type agyRun struct {
	ConversationID string `json:"conversation_id"`
	Response       string `json:"response"`
	Denied         []struct {
		Action  string `json:"action"`
		Display string `json:"display_name"`
	} `json:"denied_actions"`
}

// said is the run's last word. When the settings refused something and the
// agent did not say how it ended, that is what it needs from the person.
func (r agyRun) said() string {
	if len(r.Denied) == 0 || HasMarker(r.Response) {
		return r.Response
	}
	var refused []string
	for _, d := range r.Denied {
		refused = append(refused, fmt.Sprintf("%s (%s)", d.Display, d.Action))
	}
	return strings.TrimSpace(r.Response + "\n" + NeedsYou("Antigravity refused "+strings.Join(refused, ", ")+
		": allow it under permissions.allow in Antigravity's settings, then reply"))
}

// agyResult is the last JSON object agy printed.
func agyResult(log []byte) agyRun {
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var r agyRun
		if json.Unmarshal([]byte(lines[i]), &r) == nil && r.ConversationID != "" {
			return r
		}
	}
	return agyRun{}
}

// NamedHooksJSON is Antigravity's hooks.json: named groups, each mapping
// events to handlers, {"<name>": {"<Event>": [{"type": "command",
// "command": "..."}]}}. sous keeps its hooks in a group of its own, "sous";
// other groups are never touched.
type NamedHooksJSON struct{}

const sousGroup = "sous"

func (NamedHooksJSON) Place(file, event string, cmd Cmd) (bool, error) {
	return editJSON(file, func(doc map[string]any) bool {
		group := child(doc, sousGroup)
		handlers, _ := group[event].([]any)
		kept, placed, changed := placeIn(handlers, cmd, false)
		if !placed {
			kept, changed = append(kept, handler(cmd)), true
		}
		if changed {
			group[event] = kept
		}
		return changed
	})
}

// Commands are the commands any group runs on event. A value that is not a
// group (a "$schema", say) is not a hook, and is passed over.
func (NamedHooksJSON) Commands(file, event string) ([]string, error) {
	var doc map[string]json.RawMessage
	if err := readJSON(file, &doc); err != nil {
		return nil, err
	}
	names := slices.Sorted(maps.Keys(doc))
	var out []string
	for _, n := range names {
		var group map[string][]handlerJSON
		if json.Unmarshal(doc[n], &group) != nil {
			continue
		}
		for _, h := range group[event] {
			out = append(out, h.Command)
		}
	}
	return out, nil
}
