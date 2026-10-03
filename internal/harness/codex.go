package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Codex: hooks in ~/.codex/hooks.json in Claude Code's layout, a JSONL
// rollout for a transcript, and headless with exec.
var codex = Harness{
	Name:     "codex",
	Display:  "Codex",
	Bin:      "codex",
	SkillDir: func(home string) string { return filepath.Join(home, ".codex", "skills") },
	HookFile: func(home string) string { return filepath.Join(home, ".codex", "hooks.json") },
	Format:   HooksJSON{},
	Hooks: []Hook{
		{Role: RoleStart, Event: "SessionStart"},
		{Role: RoleEnd, Event: "SessionEnd", Flag: "codex-session-end"},
	},
	Parse: parseHookJSON,
	Last:  func(transcript string, max int) string { return lastLine(transcript, max, `"assistant"`, codexText) },
	// Codex runs in its workspace-write sandbox; no permission check is
	// skipped.
	Headless: &Headless{
		Start: func(r Run) []string {
			return append([]string{"exec"}, append(codexArgs(r), "--", r.Prompt)...)
		},
		Resume: func(r Run) []string {
			return append([]string{"exec", "resume"}, append(codexArgs(r), "--", r.Session, r.Answer)...)
		},
		Session: func(log []byte) string {
			if m := codexThread.FindSubmatch(log); m != nil {
				return string(m[1])
			}
			return ""
		},
		Last: func(r Run, _ []byte) string {
			b, _ := os.ReadFile(codexLastFile(r))
			return strings.TrimSpace(string(b))
		},
	},
}

// codexText is the text of one assistant message in a rollout, or "".
func codexText(line []byte) string {
	var l struct {
		Type    string `json:"type"`
		Payload struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "response_item" || l.Payload.Type != "message" || l.Payload.Role != "assistant" {
		return ""
	}
	return contentText(l.Payload.Content, "output_text")
}

// codexLastFile is where Codex writes its last message: the run folder,
// beside the worktree, so the agent never sees or commits it.
func codexLastFile(r Run) string { return filepath.Join(r.Dir, "last.txt") }

// codexArgs: the workspace-write sandbox, plus git's objects, refs and logs
// and this worktree's own git folder, which committing writes to and which
// live in the project, outside the worktree. Never the repo's hooks or
// config, which would reach the person's own commits.
func codexArgs(r Run) []string {
	var roots []string
	for _, d := range r.GitDirs {
		roots = append(roots, strconv.Quote(d))
	}
	return []string{"--json", "-c", `sandbox_mode="workspace-write"`,
		"-c", "sandbox_workspace_write.writable_roots=[" + strings.Join(roots, ",") + "]",
		"-o", codexLastFile(r)}
}

var codexThread = regexp.MustCompile(`"(?:thread_id|session_id)"\s*:\s*"([^"]+)"`)
