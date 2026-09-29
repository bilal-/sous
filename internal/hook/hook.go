// Package hook implements the agent-session integration. Hooks record what
// happened and inject context at startup; they never infer and never fail.
package hook

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilal-/sous/internal/project"
)

// Input is the subset of hook JSON both Claude Code and Codex send.
type Input struct {
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	Source         string `json:"source"` // startup | resume | clear | compact
	TranscriptPath string `json:"transcript_path"`
	Reason         string `json:"reason"`
}

func Parse(r io.Reader) (Input, bool) {
	var in Input
	b, err := io.ReadAll(r)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return in, false
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, false
	}
	return in, true
}

// StartRoot: the repo to inject for, only on a fresh session.
func StartRoot(in Input, fallback string) (string, bool) {
	if in.Source != "" && in.Source != "startup" {
		return "", false
	}
	return Root(in, fallback)
}

// Root is the repo the hook's cwd is in — the input's cwd, or fallback
// (the hook process's own directory) when the agent sent none.
func Root(in Input, fallback string) (string, bool) {
	cwd := in.CWD
	if cwd == "" {
		cwd = fallback
	}
	if cwd == "" {
		return "", false
	}
	return project.ForPath(cwd)
}

// LastAssistantText reads a Claude-style JSONL transcript and returns the last
// assistant text, capped at max runes. Any oddity yields "".
func LastAssistantText(path string, max int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	last := ""
	for sc.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "assistant" {
			continue
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(line.Message.Content, &parts) == nil {
			var texts []string
			for _, p := range parts {
				if p.Type == "text" && p.Text != "" {
					texts = append(texts, p.Text)
				}
			}
			if len(texts) > 0 {
				last = strings.Join(texts, " ")
			}
			continue
		}
		var s string
		if json.Unmarshal(line.Message.Content, &s) == nil && s != "" {
			last = s
		}
	}
	last = strings.Join(strings.Fields(last), " ")
	r := []rune(last)
	if len(r) > max {
		return string(r[:max])
	}
	return last
}

// Install adds {type:command, command, timeout:10} under hooks.<event> in a
// Claude Code / Codex settings file, unless that exact command is present.
func Install(settingsPath, event, command string) (bool, error) {
	doc := map[string]any{}
	b, err := os.ReadFile(settingsPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return false, err
	default:
		if err := json.Unmarshal(b, &doc); err != nil {
			return false, err
		}
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		doc["hooks"] = hooks
	}
	groups, _ := hooks[event].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		inner, _ := gm["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if hm["command"] == command {
				return false, nil
			}
		}
	}
	groups = append(groups, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 10}}})
	hooks[event] = groups
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return false, err
	}
	tmp := settingsPath + ".sous.tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, settingsPath)
}
