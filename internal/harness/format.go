package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/bilal-/sous/internal/store"
)

// Format is how a harness's hook settings file is laid out.
type Format interface {
	// Place puts cmd into file for event. Any sous hook for the same role
	// and agent, from this binary or one that moved, is replaced: the first
	// in place, the rest removed. Every other hook is left as it was. It
	// reports whether the file changed.
	Place(file, event string, cmd Cmd) (bool, error)
	// Commands lists the hook commands under event, or an error when file
	// cannot be read as this format (fs.ErrNotExist when it is missing).
	Commands(file, event string) ([]string, error)
}

// HooksJSON is the layout Claude Code started and Codex, Qwen Code, Gemini CLI
// and Factory Droid share:
// {"hooks": {"<Event>": [{"matcher": "...", "hooks": [{"type": "command",
// "command": "..."}]}]}} (or at the root when RootMap is true).
type HooksJSON struct {
	Millis        bool   // timeout in milliseconds (Gemini CLI) rather than seconds
	Name          string // optional friendly hook name (Gemini CLI)
	StatusMessage bool   // set statusMessage on each handler (Codex hook UI label & spinner)
	RootMap       bool   // events sit at top level without a "hooks" key (Factory Droid)
}

func (f HooksJSON) Place(file, event string, cmd Cmd) (bool, error) {
	return editJSON(file, func(doc map[string]any) bool {
		hooks := doc
		if !f.RootMap {
			hooks = child(doc, "hooks")
		}
		kept, changed := f.placeHook(event, hooks[event], cmd)
		if changed {
			hooks[event] = kept
		}
		return changed
	})
}

func (f HooksJSON) Commands(file, event string) ([]string, error) {
	var groups []struct {
		Hooks []handlerJSON `json:"hooks"`
	}
	if f.RootMap {
		var doc map[string][]struct {
			Hooks []handlerJSON `json:"hooks"`
		}
		if err := readJSON(file, &doc); err != nil {
			return nil, err
		}
		groups = doc[event]
	} else {
		var doc struct {
			Hooks map[string][]struct {
				Hooks []handlerJSON `json:"hooks"`
			} `json:"hooks"`
		}
		if err := readJSON(file, &doc); err != nil {
			return nil, err
		}
		groups = doc.Hooks[event]
	}
	var out []string
	for _, g := range groups {
		for _, h := range g.Hooks {
			out = append(out, h.Command)
		}
	}
	return out, nil
}

// placeHook returns event's hook groups with cmd in them once per matcher:
// under each matcher (none counts as one), the first sous hook for its
// role and agent is updated in place and any others are removed.
// Entries sous does not understand, and every other hook, are kept as they
// are. changed says whether anything moved.
func (f HooksJSON) placeHook(eventName string, event any, cmd Cmd) (kept []any, changed bool) {
	groups, _ := event.([]any)
	placed := map[string]bool{} // matcher → our hook is there
	extra := f.extra(eventName, cmd)
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			kept = append(kept, g)
			continue
		}
		matcher, _ := gm["matcher"].(string)
		inner, _ := gm["hooks"].([]any)
		keep, here, moved := placeIn(inner, cmd, placed[matcher], "command", extra)
		if here {
			placed[matcher] = true
		}
		changed = changed || moved
		if len(keep) > 0 || len(inner) == 0 {
			gm["hooks"] = keep
			kept = append(kept, gm)
		}
	}
	if len(placed) == 0 {
		kept = append(kept, map[string]any{"hooks": []any{f.handlerFor(eventName, cmd)}})
		changed = true
	}
	return kept, changed
}

// placeIn is handlers with cmd in them once: the first sous hook for its
// role and agent is updated in place, any others removed, and every other
// handler kept as it is. With already, cmd is placed elsewhere and every
// sous hook here goes. placed says cmd is among the handlers returned.
func placeIn(handlers []any, cmd Cmd, already bool, key string, extra map[string]any) (kept []any, placed, changed bool) {
	want := cmd.String()
	kept = []any{}
	for _, h := range handlers {
		hm, _ := h.(map[string]any)
		c, _ := hm[key].(string)
		switch {
		case hm == nil || !cmd.Ours(c):
			kept = append(kept, h)
		case !already && !placed:
			placed = true
			if c != want {
				hm[key], changed = want, true
			}
			for k, v := range extra {
				if cur, _ := hm[k].(string); cur != v {
					hm[k], changed = v, true
				}
			}
			kept = append(kept, h)
		default:
			changed = true // a second sous hook for the same moment
		}
	}
	return kept, placed, changed
}

// handler is cmd as a hook handler, the agent told to wait HookTimeout.
func (f HooksJSON) handler(cmd Cmd) map[string]any {
	return f.handlerFor("", cmd)
}

func (f HooksJSON) handlerFor(event string, cmd Cmd) map[string]any {
	timeout := HookTimeout.Seconds()
	if f.Millis {
		timeout = float64(HookTimeout.Milliseconds())
	}
	m := map[string]any{"type": "command", "command": cmd.String(), "timeout": timeout}
	for k, v := range f.extra(event, cmd) {
		m[k] = v
	}
	return m
}

func (f HooksJSON) extra(event string, cmd Cmd) map[string]any {
	if f.Name == "" && !f.StatusMessage {
		return nil
	}
	m := map[string]any{}
	if f.Name != "" {
		m["name"] = f.Name
	}
	if f.StatusMessage {
		m["statusMessage"] = statusMessageFor(event, cmd)
	}
	return m
}

func statusMessageFor(event string, cmd Cmd) string {
	switch event {
	case "SessionStart":
		return "sous: load project context"
	case "Stop":
		return "sous: record turn context"
	case "PreCompact", "PreCompress":
		return "sous: record context before compact"
	case "SessionEnd":
		return "sous: record session end"
	default:
		if cmd.Role == RoleStart {
			return "sous: load project context"
		}
		return "sous: record session context"
	}
}

// FlatHooksJSON is {"version": 1, "hooks": {"<Event>": [...]}} used by Cursor
// ({"command": "..."}) and Copilot CLI ({"type":"command","bash":"...","timeoutSec":10}).
type FlatHooksJSON struct {
	Copilot bool
}

func (f FlatHooksJSON) field() string {
	if f.Copilot {
		return "bash"
	}
	return "command"
}

func (f FlatHooksJSON) entry(cmd Cmd) map[string]any {
	if f.Copilot {
		return map[string]any{"type": "command", "bash": cmd.String(), "timeoutSec": int(HookTimeout.Seconds())}
	}
	return map[string]any{"command": cmd.String()}
}

func (f FlatHooksJSON) Place(file, event string, cmd Cmd) (bool, error) {
	return editJSON(file, func(doc map[string]any) bool {
		changed := false
		if _, ok := doc["version"]; !ok {
			doc["version"] = 1
			changed = true
		}
		hooks := child(doc, "hooks")
		existing, _ := hooks[event].([]any)
		kept, placed, moved := placeIn(existing, cmd, false, f.field(), nil)
		changed = changed || moved
		if !placed {
			kept = append(kept, f.entry(cmd))
			changed = true
		}
		if changed {
			hooks[event] = kept
		}
		return changed
	})
}

func (f FlatHooksJSON) Commands(file, event string) ([]string, error) {
	var doc struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
			Bash    string `json:"bash"`
		} `json:"hooks"`
	}
	if err := readJSON(file, &doc); err != nil {
		return nil, err
	}
	var out []string
	for _, h := range doc.Hooks[event] {
		c := h.Command
		if f.Copilot {
			c = h.Bash
		}
		if c != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

// ScriptPlugin owns a single generated JS/TS plugin or extension file whose
// commands are mirrored in "// sous hook <event>: <command>" comment lines.
type ScriptPlugin struct {
	Template string
}

var pluginHook = regexp.MustCompile(`(?m)^// sous hook (\S+): (.*)$`)

const pluginHeader = "// sous:"

func (p ScriptPlugin) Place(file, event string, cmd Cmd) (bool, error) {
	if strings.ContainsAny(cmd.String(), "\n\r") {
		return false, fmt.Errorf("a hook command cannot hold a line break: %q", cmd.String())
	}
	changed := false
	err := store.EditFile(file, 0o644, func(b []byte) ([]byte, error) {
		if len(b) > 0 && !strings.HasPrefix(string(b), pluginHeader) {
			return nil, fmt.Errorf("%s is not a plugin sous wrote; move it away, then sous setup", file)
		}
		cmds := map[string]string{}
		for _, m := range pluginHook.FindAllStringSubmatch(string(b), -1) {
			cmds[m[1]] = m[2]
		}
		cmds[event] = cmd.String()
		js := func(s string) string { q, _ := json.Marshal(s); return string(q) }
		out := strings.NewReplacer("{{start}}", cmds[RoleStart], "{{end}}", cmds[RoleEnd],
			"{{startJSON}}", js(cmds[RoleStart]), "{{endJSON}}", js(cmds[RoleEnd]),
			"{{timeoutMs}}", fmt.Sprint(HookTimeout.Milliseconds())).Replace(p.Template)
		if out == string(b) {
			return nil, nil
		}
		changed = true
		return []byte(out), nil
	})
	return changed, err
}

func (p ScriptPlugin) Commands(file, event string) ([]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range pluginHook.FindAllStringSubmatch(string(b), -1) {
		if m[1] == event && m[2] != "" {
			out = append(out, m[2])
		}
	}
	if len(out) == 0 && !strings.HasPrefix(string(b), pluginHeader) {
		return nil, fmt.Errorf("%s is not a plugin sous wrote", file)
	}
	return out, nil
}

// TOMLHooks manages [[hooks]] array-of-tables blocks in a TOML config file
// (such as ~/.kimi-code/config.toml), leaving unrelated settings intact.
type TOMLHooks struct{}

type tomlHookDoc struct {
	Hooks []struct {
		Event   string `toml:"event"`
		Command string `toml:"command"`
	} `toml:"hooks"`
}

func (TOMLHooks) Commands(file, event string) ([]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var doc tomlHookDoc
	if _, err := toml.Decode(string(b), &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid TOML: %w", file, err)
	}
	var out []string
	for _, h := range doc.Hooks {
		if h.Event == event && h.Command != "" {
			out = append(out, h.Command)
		}
	}
	return out, nil
}

func (TOMLHooks) Place(file, event string, cmd Cmd) (bool, error) {
	changed := false
	err := store.EditFile(file, 0o644, func(b []byte) ([]byte, error) {
		if len(b) > 0 {
			var check any
			if _, err := toml.Decode(string(b), &check); err != nil {
				return nil, fmt.Errorf("%s is not valid TOML: %w", file, err)
			}
		}
		lines := splitNonEmptyTail(string(b))
		kept, placed, modified := placeTOMLHooks(lines, event, cmd)
		if !placed {
			if len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) != "" {
				kept = append(kept, "")
			}
			kept = append(kept, "[[hooks]]",
				fmt.Sprintf("event = %q", event),
				fmt.Sprintf("command = %q", cmd.String()),
				fmt.Sprintf("timeout = %d", int(HookTimeout.Seconds())))
			modified = true
		}
		if !modified {
			return nil, nil
		}
		changed = true
		return []byte(strings.Join(kept, "\n") + "\n"), nil
	})
	return changed, err
}

func splitNonEmptyTail(s string) []string {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func placeTOMLHooks(lines []string, event string, cmd Cmd) (out []string, placed, changed bool) {
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) != "[[hooks]]" {
			out = append(out, lines[i])
			i++
			continue
		}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "[") {
			end++
		}
		block := lines[i:end]
		var parsed tomlHookDoc
		_, err := toml.Decode(strings.Join(block, "\n"), &parsed)
		if err != nil || len(parsed.Hooks) != 1 || parsed.Hooks[0].Event != event || !cmd.Ours(parsed.Hooks[0].Command) {
			out = append(out, block...)
			i = end
			continue
		}
		if placed {
			changed = true
			i = end
			continue
		}
		placed = true
		want := cmd.String()
		if parsed.Hooks[0].Command != want {
			for j := range block {
				if strings.HasPrefix(strings.TrimSpace(block[j]), "command") {
					block[j] = fmt.Sprintf("command = %q", want)
					changed = true
				}
			}
		}
		out = append(out, block...)
		i = end
	}
	return out, placed, changed
}

// handlerJSON is a hook handler as sous reads one back.
type handlerJSON struct {
	Command string `json:"command"`
}

// editJSON edits the JSON object in file (none, empty or null is an empty
// object) with fn, and writes it back when fn says it changed.
func editJSON(file string, fn func(doc map[string]any) bool) (bool, error) {
	changed := false
	err := store.EditFile(file, 0o644, func(b []byte) ([]byte, error) {
		var doc map[string]any
		if len(b) > 0 {
			if err := json.Unmarshal(b, &doc); err != nil {
				return nil, err
			}
		}
		if doc == nil {
			doc = map[string]any{}
		}
		if changed = fn(doc); !changed {
			return nil, nil
		}
		return json.MarshalIndent(doc, "", "  ")
	})
	return changed, err
}

// child is doc's object under key, made when it is missing or not one.
func child(doc map[string]any, key string) map[string]any {
	m, _ := doc[key].(map[string]any)
	if m == nil {
		m = map[string]any{}
		doc[key] = m
	}
	return m
}

// readJSON reads file into v, saying the file is not valid JSON when it
// cannot be.
func readJSON(file string, v any) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s is not valid JSON", file)
	}
	return nil
}
