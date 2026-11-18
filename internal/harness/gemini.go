package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Gemini CLI: hooks in ~/.gemini/settings.json (timeouts in milliseconds),
// skills in ~/.gemini/skills, JSON hook replies with additionalContext, and
// headless via -p= with --output-format json --approval-mode auto_edit.
var gemini = Harness{
	Name:    "gemini",
	Display: "Gemini CLI",
	Bin:     "gemini",
	Present: func(home string) bool {
		if _, err := exec.LookPath("gemini"); err == nil {
			return true
		}
		// Never check ~/.gemini alone: Antigravity also writes under ~/.gemini.
		return fileExists(home, ".gemini/settings.json", ".gemini/oauth_creds.json", ".gemini/google_accounts.json")
	},
	SkillDir: func(home string) string { return filepath.Join(home, ".gemini", "skills") },
	HookFile: func(home string) string { return filepath.Join(home, ".gemini", "settings.json") },
	Format:   HooksJSON{Millis: true, Name: "sous"},
	Hooks: []Hook{
		{Role: RoleStart, Event: "SessionStart"},
		{Role: RoleEnd, Event: "AfterAgent"},
		{Role: RoleEnd, Event: "PreCompress"},
		{Role: RoleEnd, Event: "SessionEnd"},
	},
	Parse: parseHookJSON,
	Reply: func(role, say string) string {
		if role != RoleStart || strings.TrimSpace(say) == "" {
			return "{}"
		}
		b, _ := json.Marshal(map[string]any{
			"hookSpecificOutput": map[string]string{
				"hookEventName":     "SessionStart",
				"additionalContext": say,
			},
		})
		return string(b)
	},
	Last:     func(transcript string) string { return geminiTranscript(transcript, "gemini") },
	LastUser: func(transcript string) string { return geminiTranscript(transcript, "user") },
	Headless: &Headless{
		Start:   func(r Run) []string { return append(geminiArgs(), "-p="+r.Prompt) },
		Resume:  func(r Run) []string { return append(geminiArgs(), "--resume", r.Session, "-p="+r.Answer) },
		Session: func(log []byte) string { return geminiResult(log, false).SessionID },
		Last:    func(_ Run, log []byte) string { return geminiResult(log, true).Response },
	},
}

func geminiArgs() []string {
	return []string{"--approval-mode", "auto_edit", "--output-format", "json"}
}

type geminiRun struct {
	SessionID string `json:"session_id"`
	Response  string `json:"response"`
}

func geminiResult(log []byte, nonEmpty bool) geminiRun {
	return lastJSON(log, func(r geminiRun) bool {
		return r.SessionID != "" && (!nonEmpty || r.Response != "")
	})
}

func geminiTranscript(path, wantType string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var chat struct {
		Messages []struct {
			Type    string          `json:"type"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(b, &chat) == nil && len(chat.Messages) > 0 {
		for i := len(chat.Messages) - 1; i >= 0; i-- {
			m := chat.Messages[i]
			if m.Type != wantType {
				continue
			}
			if s := partsText(m.Content); s != "" {
				return s
			}
		}
		return ""
	}
	return lastLine(path, `"`+wantType+`"`, func(line []byte) string {
		var m struct {
			Type    string          `json:"type"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(line, &m) != nil || m.Type != wantType {
			return ""
		}
		return partsText(m.Content)
	})
}
