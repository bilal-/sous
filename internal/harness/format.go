package harness

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bilal-/sous/internal/store"
)

// Format is how a harness's hook settings file is laid out.
type Format interface {
	// Place puts command (from Command) into file for event. Any sous hook
	// for the same role and agent, from this binary or one that moved, is
	// replaced: the first in place, the rest removed. Every other hook is
	// left as it was. It reports whether the file changed.
	Place(file, event, command string) (bool, error)
	// Commands lists the hook commands under event, or an error when file
	// cannot be read as this format (fs.ErrNotExist when it is missing).
	Commands(file, event string) ([]string, error)
}

// HooksJSON is the layout Claude Code started and Codex and others share:
// {"hooks": {"<Event>": [{"matcher": "...", "hooks": [{"type": "command",
// "command": "..."}]}]}}.
type HooksJSON struct{}

func (HooksJSON) Place(file, event, command string) (bool, error) {
	args := shellSplit(command)
	if len(args) != 4 {
		return false, fmt.Errorf("not a hook command: %q", command)
	}
	role, agent := args[2], args[3]
	changed := false
	err := store.EditFile(file, 0o644, func(b []byte) ([]byte, error) {
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

func (HooksJSON) Commands(file, event string) ([]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON", file)
	}
	var out []string
	for _, g := range doc.Hooks[event] {
		for _, c := range g.Hooks {
			out = append(out, c.Command)
		}
	}
	return out, nil
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
