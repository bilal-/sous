package harness

import (
	"encoding/json"
	"fmt"
	"os"

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

// HooksJSON is the layout Claude Code started and Codex and others share:
// {"hooks": {"<Event>": [{"matcher": "...", "hooks": [{"type": "command",
// "command": "..."}]}]}}.
type HooksJSON struct{}

func (HooksJSON) Place(file, event string, cmd Cmd) (bool, error) {
	return editJSON(file, func(doc map[string]any) bool {
		hooks := child(doc, "hooks")
		kept, changed := placeHook(hooks[event], cmd)
		if changed {
			hooks[event] = kept
		}
		return changed
	})
}

func (HooksJSON) Commands(file, event string) ([]string, error) {
	var doc struct {
		Hooks map[string][]struct {
			Hooks []handlerJSON `json:"hooks"`
		} `json:"hooks"`
	}
	if err := readJSON(file, &doc); err != nil {
		return nil, err
	}
	var out []string
	for _, g := range doc.Hooks[event] {
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
func placeHook(event any, cmd Cmd) (kept []any, changed bool) {
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
		keep, here, moved := placeIn(inner, cmd, placed[matcher])
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
		kept = append(kept, map[string]any{"hooks": []any{handler(cmd)}})
		changed = true
	}
	return kept, changed
}

// placeIn is handlers with cmd in them once: the first sous hook for its
// role and agent is updated in place, any others removed, and every other
// handler kept as it is. With already, cmd is placed elsewhere and every
// sous hook here goes. placed says cmd is among the handlers returned.
func placeIn(handlers []any, cmd Cmd, already bool) (kept []any, placed, changed bool) {
	want := cmd.String()
	kept = []any{}
	for _, h := range handlers {
		hm, _ := h.(map[string]any)
		c, _ := hm["command"].(string)
		switch {
		case hm == nil || !cmd.Ours(c):
			kept = append(kept, h)
		case !already && !placed:
			placed = true
			if c != want {
				hm["command"], changed = want, true
			}
			kept = append(kept, h)
		default:
			changed = true // a second sous hook for the same moment
		}
	}
	return kept, placed, changed
}

// handler is cmd as a hook handler, the agent told to wait HookTimeout.
func handler(cmd Cmd) map[string]any {
	return map[string]any{"type": "command", "command": cmd.String(), "timeout": HookTimeout.Seconds()}
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
