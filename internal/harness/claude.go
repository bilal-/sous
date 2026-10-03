package harness

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Claude Code: hooks in ~/.claude/settings.json, a JSONL transcript, and
// headless with -p.
var claude = Harness{
	Name:     "claude",
	Display:  "Claude Code",
	Bin:      "claude",
	SkillDir: func(home string) string { return filepath.Join(home, ".claude", "skills") },
	HookFile: func(home string) string { return filepath.Join(home, ".claude", "settings.json") },
	Format:   HooksJSON{},
	Hooks: []Hook{
		{Role: RoleStart, Event: "SessionStart"},
		{Role: RoleEnd, Event: "SessionEnd"},
	},
	Parse: parseHookJSON,
	Last:  func(transcript string, max int) string { return lastLine(transcript, max, claudeText) },
	// Every prompt follows "--", so text that starts with a dash is never
	// read as a flag. The permissions are the person's own: file edits
	// accepted and the tools its settings allow; no permission check is
	// skipped.
	Headless: &Headless{
		Start:   func(r Run) []string { return append(claudeArgs(), "--", r.Prompt) },
		Resume:  func(r Run) []string { return append(claudeArgs(), "--resume", r.Session, "--", r.Answer) },
		Session: func(log []byte) string { return claudeResult(log).SessionID },
		Last:    func(_ Run, log []byte) string { return claudeResult(log).Result },
	},
}

// claudeText is the text of one assistant transcript line, or "".
func claudeText(line []byte) string {
	var l struct {
		Type    string `json:"type"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "assistant" {
		return ""
	}
	return contentText(l.Message.Content, "text")
}

// claudeArgs: headless, file edits accepted, and the git commands that
// commit on the run's branch allowed (a reply cannot grant a permission, so
// without them a run could never commit). Everything else follows the
// person's own Claude settings.
func claudeArgs() []string {
	return []string{"-p", "--output-format", "json", "--permission-mode", "acceptEdits",
		"--allowedTools", "Bash(git add:*)", "Bash(git commit:*)", "Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)"}
}

// claudeResult: the last JSON object Claude printed (--output-format json).
func claudeResult(log []byte) (r struct {
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
}) {
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if json.Unmarshal([]byte(lines[i]), &r) == nil && r.SessionID != "" {
			return r
		}
	}
	return r
}
