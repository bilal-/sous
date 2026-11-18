package harness

import (
	"encoding/json"
	"path/filepath"
)

// Qwen Code: hooks in ~/.qwen/settings.json (Claude Code layout, seconds timeout),
// skills in ~/.qwen/skills, and headless with --approval-mode auto-edit --output-format json.
var qwen = Harness{
	Name:     "qwen",
	Display:  "Qwen Code",
	Bin:      "qwen",
	Present:  func(home string) bool { return installed(home, "qwen", ".qwen") },
	SkillDir: func(home string) string { return filepath.Join(home, ".qwen", "skills") },
	HookFile: func(home string) string { return filepath.Join(home, ".qwen", "settings.json") },
	Format:   HooksJSON{},
	Hooks: []Hook{
		{Role: RoleStart, Event: "SessionStart"},
		{Role: RoleEnd, Event: "Stop"},
		{Role: RoleEnd, Event: "PreCompact"},
		{Role: RoleEnd, Event: "SessionEnd"},
	},
	Parse:    parseHookJSON,
	Last:     func(transcript string) string { return lastLine(transcript, `"assistant"`, qwenText) },
	LastUser: func(transcript string) string { return lastLine(transcript, `"user"`, qwenUserText) },
	Headless: &Headless{
		Start:   func(r Run) []string { return append(qwenArgs(), "--", r.Prompt) },
		Resume:  func(r Run) []string { return append(qwenArgs(), "--resume", r.Session, "--", r.Answer) },
		Session: func(log []byte) string { return claudeResult(log, false).SessionID },
		Last:    func(_ Run, log []byte) string { return claudeResult(log, true).Result },
	},
}

func qwenArgs() []string {
	return []string{"--approval-mode", "auto-edit", "--output-format", "json"}
}

func qwenText(line []byte) string     { return qwenLine(line, "assistant") }
func qwenUserText(line []byte) string { return qwenLine(line, "user") }

func qwenLine(line []byte, wantType string) string {
	var l struct {
		Type    string `json:"type"`
		Message struct {
			Parts   json.RawMessage `json:"parts"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != wantType {
		return ""
	}
	if len(l.Message.Parts) > 0 {
		if s := partsText(l.Message.Parts); s != "" {
			return s
		}
	}
	return contentText(l.Message.Content, "text")
}
