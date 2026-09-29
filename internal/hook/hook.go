// Package hook implements the agent-session integration. Hooks record what
// happened and inject context at startup; they never infer and never fail.
package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
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
func StartRoot(in Input, fallback, home string) (string, bool) {
	if in.Source != "" && in.Source != "startup" {
		return "", false
	}
	return Root(in, fallback, home)
}

// Root is the repo the hook's cwd is in — the input's cwd, or fallback
// (the hook process's own directory) when the agent sent none.
func Root(in Input, fallback, home string) (string, bool) {
	cwd := in.CWD
	if cwd == "" {
		cwd = fallback
	}
	if cwd == "" {
		return "", false
	}
	return project.ForPath(cwd, home)
}

// LastAssistantText is the last assistant text in a Claude style JSONL
// transcript, capped at max runes; "" when there is none or the file is
// odd. It reads backwards from the end and stops at that message, so a
// transcript of any size costs only what comes after it.
func LastAssistantText(path string, max int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	var last string
	eachLineFromEnd(f, st.Size(), func(line []byte) bool {
		if !bytes.Contains(line, []byte(`"assistant"`)) {
			return true
		}
		last = assistantText(line)
		return last == ""
	})
	last = strings.Join(strings.Fields(last), " ")
	if r := []rune(last); len(r) > max {
		return string(r[:max])
	}
	return last
}

// assistantText is the text of one assistant transcript line, or "".
func assistantText(line []byte) string {
	var l struct {
		Type    string `json:"type"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "assistant" {
		return ""
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(l.Message.Content, &parts) == nil {
		var texts []string
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		return strings.Join(texts, " ")
	}
	var s string
	json.Unmarshal(l.Message.Content, &s)
	return s
}

// eachLineFromEnd calls fn for each line of r, last line first, until fn
// returns false. It reads in chunks from the end.
func eachLineFromEnd(r io.ReaderAt, size int64, fn func([]byte) bool) {
	const chunk = 1 << 20
	var tail []byte // the start of a line whose beginning is not read yet
	for end := size; end > 0; {
		start := max(end-chunk, 0)
		buf := make([]byte, end-start)
		if _, err := r.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
			return
		}
		buf = append(buf, tail...)
		for {
			nl := bytes.LastIndexByte(buf, '\n')
			if nl < 0 {
				break
			}
			if line := buf[nl+1:]; len(line) > 0 && !fn(line) {
				return
			}
			buf = buf[:nl]
		}
		tail, end = buf, start
	}
	if len(tail) > 0 {
		fn(tail)
	}
}

// RecordEnd remembers the session that just ended in its project: which
// agent, when, and its last assistant message. fallback is the hook's own
// folder when the agent sent none.
func RecordEnd(s *store.Store, in Input, agent, fallback, home string, now time.Time) error {
	root, ok := Root(in, fallback, home)
	if !ok {
		return nil
	}
	sess := session.Session{Agent: agent, SessionID: in.SessionID, Ended: now}
	if sess.SessionID == "" {
		sess.SessionID = "unknown"
	}
	if in.TranscriptPath != "" {
		if m := LastAssistantText(in.TranscriptPath, 300); m != "" {
			sess.LastMessage = &m
		}
	}
	return session.Record(s, root, sess)
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
	changed := false
	err := store.EditFile(settingsPath, 0o644, func(b []byte) ([]byte, error) {
		doc := map[string]any{}
		if len(b) > 0 {
			if err := json.Unmarshal(b, &doc); err != nil {
				return nil, err
			}
		}
		hooks, _ := doc["hooks"].(map[string]any)
		if hooks == nil {
			hooks = map[string]any{}
			doc["hooks"] = hooks
		}
		var kept []any
		kept, changed = placeHook(hooks[event], command, role, agent, args[0])
		if !changed {
			return nil, nil
		}
		hooks[event] = kept
		return json.MarshalIndent(doc, "", "  ")
	})
	return changed, err
}

// placeHook returns event's hook groups with command in them exactly once:
// the first sous hook for role and agent is updated in place, any others
// are removed, and every other hook is kept. changed says whether anything
// moved.
func placeHook(event any, command, role, agent, exe string) (kept []any, changed bool) {
	groups, _ := event.([]any)
	placed := false
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		inner, _ := gm["hooks"].([]any)
		var keep []any
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			c, _ := hm["command"].(string)
			switch {
			case !IsOurs(c, role, agent, exe):
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
	return kept, changed
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
