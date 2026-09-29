// Package hook implements the agent-session integration. Hooks record what
// happened and inject context at startup; they never infer and never fail.
package hook

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
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
	// A bufio.Reader has no line limit: tool results can be tens of MB, and
	// stopping at one would leave an older message as the last.
	rd := bufio.NewReader(f)
	last := ""
	for {
		raw, rerr := rd.ReadBytes('\n')
		if len(raw) == 0 && rerr != nil {
			break
		}
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(raw, &line) != nil || line.Type != "assistant" {
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

// Command is the hook command line for exe: `<exe> hook <role> <agent>`,
// with exe quoted for the shell when it needs it.
func Command(exe, role, agent string) string {
	return shellQuote(exe) + " hook " + role + " " + agent
}

// IsOurs: is command a sous hook for role and agent, whichever sous binary
// it names? The program must be called sous (or be exe itself), and its
// arguments exactly ours.
func IsOurs(command, role, agent string, exe ...string) bool {
	args := shellSplit(command)
	if len(args) != 4 || args[1] != "hook" || args[2] != role || args[3] != agent {
		return false
	}
	return filepath.Base(args[0]) == "sous" || len(exe) > 0 && args[0] == exe[0]
}

// Install puts command (from Command) into an agent's settings file for
// event. Any sous hook for the same role and agent, from this binary or one
// that moved, is replaced: the first in place, the rest removed. Every other
// hook is left as it was. It reports whether the file changed.
func Install(settingsPath, event, command string) (bool, error) {
	args := shellSplit(command)
	if len(args) != 4 {
		return false, fmt.Errorf("not a hook command: %q", command)
	}
	role, agent := args[2], args[3]
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
	var kept []any
	placed, changed := false, false
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		inner, _ := gm["hooks"].([]any)
		var keep []any
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			c, _ := hm["command"].(string)
			switch {
			case !IsOurs(c, role, agent, args[0]):
				keep = append(keep, h)
			case !placed:
				placed = true
				if c != command {
					hm["command"], changed = command, true
				}
				keep = append(keep, h)
			default:
				changed = true // a second sous hook for the same thing
			}
		}
		if len(keep) > 0 || len(inner) == 0 {
			gm["hooks"] = keep
			kept = append(kept, gm)
		}
	}
	if !placed {
		kept = append(kept, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 10}}})
		changed = true
	}
	if !changed {
		return false, nil
	}
	hooks[event] = kept
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	return true, store.WriteFile(settingsPath, out, 0o644)
}

// shellQuote quotes s for a POSIX shell when it holds anything but plain
// path characters.
func shellQuote(s string) string {
	for _, c := range s {
		if !(c == '/' || c == '.' || c == '-' || c == '_' || c == '~' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return s
}

// shellSplit splits a command line into words the way a POSIX shell would
// for the simple cases hook commands use: spaces, '...', "..." and \.
func shellSplit(s string) []string {
	var words []string
	var cur strings.Builder
	inWord, quote := false, rune(0)
	for i := 0; i < len(s); i++ {
		c := rune(s[i])
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteRune(c)
			}
		case quote == '"':
			if c == '"' {
				quote = 0
			} else if c == '\\' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}
