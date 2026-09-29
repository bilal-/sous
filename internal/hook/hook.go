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
	var parts [][]byte // the unfinished line's pieces, last piece first
	flush := func(head []byte) bool {
		line := head
		if len(parts) > 0 { // join once, when the line's start is found
			line = append([]byte{}, head...)
			for i := len(parts) - 1; i >= 0; i-- {
				line = append(line, parts[i]...)
			}
			parts = parts[:0]
		}
		return len(line) == 0 || fn(line)
	}
	for end := size; end > 0; {
		start := max(end-chunk, 0)
		buf := make([]byte, end-start)
		if _, err := r.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
			return
		}
		for {
			nl := bytes.LastIndexByte(buf, '\n')
			if nl < 0 {
				break
			}
			if !flush(buf[nl+1:]) {
				return
			}
			buf = buf[:nl]
		}
		parts = append(parts, buf) // only newly read bytes are ever scanned
		end = start
	}
	flush(nil)
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
		if m := LastAssistantText(in.TranscriptPath, session.LastMessageRunes); m != "" {
			sess.LastMessage = &m
		}
	}
	return session.Record(s, root, sess)
}

// The two hook roles: what sous does when an agent session starts, and
// when it ends.
const (
	RoleStart = "session-start"
	RoleEnd   = "session-end"
)

// Command is the hook command line for exe: `<exe> hook <role> <agent>`,
// with exe quoted for the shell when it needs it.
func Command(exe, role, agent string) string {
	return shellQuote(exe) + " hook " + role + " " + agent
}

// IsOurs: is command a sous hook for role and agent, whichever sous binary
// it names? The program must be called sous (or be exe itself), and its
// arguments exactly ours.
func IsOurs(command, role, agent string, exe ...string) bool {
	tail := " hook " + role + " " + agent
	if prog, ok := strings.CutSuffix(command, tail); ok && oldUnquotedPath(prog) {
		return true // an older, unquoted line, whose path may hold spaces
	}
	args := shellSplit(command)
	if len(args) != 4 || args[1] != "hook" || args[2] != role || args[3] != agent {
		return false
	}
	return filepath.Base(args[0]) == "sous" || len(exe) > 0 && args[0] == exe[0]
}

// oldUnquotedPath: prog is nothing but an absolute path to a sous binary,
// as older versions wrote it without quotes (so spaces are allowed, and
// nothing that makes it a shell command: no prefix, no operators).
func oldUnquotedPath(prog string) bool {
	if !strings.HasPrefix(prog, "/") || !strings.HasSuffix(prog, "/sous") || strings.ContainsAny(prog, "'\";&|$`<>(){}=\\") {
		return false
	}
	// A space may sit inside one path ("/Users/Sam Smith/bin/sous"), where
	// the word after it continues that path. A word that starts a new path,
	// or is no path at all, means another program runs sous.
	for _, w := range strings.Split(prog, " ")[1:] {
		if strings.HasPrefix(w, "/") || !strings.Contains(w, "/") {
			return false
		}
	}
	return true
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

// placeHook returns event's hook groups with command in them once per
// matcher: under each matcher (none counts as one), the first sous hook
// for role and agent is updated in place and any others are removed.
// Entries sous does not understand, and every other hook, are kept as they
// are. changed says whether anything moved.
func placeHook(event any, command, role, agent, exe string) (kept []any, changed bool) {
	groups, _ := event.([]any)
	placed := map[string]bool{} // matcher → our hook is there
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			kept = append(kept, g)
			continue
		}
		matcher, _ := gm["matcher"].(string)
		inner, _ := gm["hooks"].([]any)
		keep := []any{}
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			c, _ := hm["command"].(string)
			switch {
			case hm == nil || !IsOurs(c, role, agent, exe):
				keep = append(keep, h)
			case !placed[matcher]:
				placed[matcher] = true
				if c != command {
					hm["command"], changed = command, true
				}
				keep = append(keep, h)
			default:
				changed = true // a second sous hook under the same matcher
			}
		}
		if len(keep) > 0 || len(inner) == 0 {
			gm["hooks"] = keep
			kept = append(kept, gm)
		}
	}
	if len(placed) == 0 {
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
