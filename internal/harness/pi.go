package harness

import (
	_ "embed"
	"encoding/json"
	"path/filepath"
)

// pi: TypeScript extension in ~/.pi/agent/extensions/sous.ts, skills in
// ~/.pi/agent/skills, JSONL sessions, and headless with -p --mode json.
var pi = Harness{
	Name:     "pi",
	Display:  "pi",
	Bin:      "pi",
	Present:  func(home string) bool { return installed(home, "pi", ".pi") },
	SkillDir: func(home string) string { return filepath.Join(home, ".pi", "agent", "skills") },
	HookFile: func(home string) string { return filepath.Join(home, ".pi", "agent", "extensions", "sous.ts") },
	Format:   PiExtension{},
	Hooks: []Hook{
		{Role: RoleStart, Event: RoleStart},
		{Role: RoleEnd, Event: RoleEnd},
	},
	Parse:    parseHookJSON,
	Last:     func(transcript string) string { return lastLine(transcript, `"assistant"`, piAssistantText) },
	LastUser: func(transcript string) string { return lastLine(transcript, `"user"`, piUserText) },
	Headless: &Headless{
		Start:   func(r Run) []string { return []string{"-p", "--mode", "json", "--", r.Prompt} },
		Resume:  func(r Run) []string { return []string{"-p", "--mode", "json", "--session", r.Session, "--", r.Answer} },
		Session: piSessionID,
		Last:    func(_ Run, log []byte) string { return piLastText(log) },
	},
}

func piAssistantText(line []byte) string { return piMessageLine(line, "assistant") }
func piUserText(line []byte) string      { return piMessageLine(line, "user") }

func piMessageLine(line []byte, wantRole string) string {
	var l struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "message" || l.Message.Role != wantRole {
		return ""
	}
	return contentText(l.Message.Content, "text")
}

func piSessionID(log []byte) string {
	type header struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	return lastJSON(log, func(h header) bool { return h.Type == "session" && h.ID != "" }).ID
}

func piLastText(log []byte) string {
	type msg struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	type event struct {
		Type     string `json:"type"`
		Message  msg    `json:"message"`
		Messages []msg  `json:"messages"`
	}
	ev := lastJSON(log, func(e event) bool {
		return e.Type == "agent_end" && len(e.Messages) > 0 || e.Type == "message_end" && e.Message.Role == "assistant"
	})
	if ev.Type == "message_end" {
		return contentText(ev.Message.Content, "text")
	}
	for i := len(ev.Messages) - 1; i >= 0; i-- {
		if ev.Messages[i].Role == "assistant" {
			if s := contentText(ev.Messages[i].Content, "text"); s != "" {
				return s
			}
		}
	}
	return ""
}

//go:embed assets/pi-extension.ts
var piExtension string

// PiExtension manages ~/.pi/agent/extensions/sous.ts using the shared ScriptPlugin layout.
type PiExtension struct{}

func (PiExtension) Place(file, event string, cmd Cmd) (bool, error) {
	return (ScriptPlugin{Template: piExtension}).Place(file, event, cmd)
}

func (PiExtension) Commands(file, event string) ([]string, error) {
	return (ScriptPlugin{Template: piExtension}).Commands(file, event)
}
