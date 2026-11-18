package harness

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Cursor: hooks in ~/.cursor/hooks.json; skills come from ~/.agents/skills.
var cursor = Harness{
	Name:     "cursor",
	Display:  "Cursor",
	Bin:      "cursor",
	Present:  func(home string) bool { return installed(home, "cursor", ".cursor") },
	HookFile: func(home string) string { return filepath.Join(home, ".cursor", "hooks.json") },
	Format:   FlatHooksJSON{},
	Hooks: []Hook{
		{Role: RoleStart, Event: "sessionStart"},
		{Role: RoleEnd, Event: "stop"},
		{Role: RoleEnd, Event: "sessionEnd"},
	},
	Parse: parseHookJSON,
	Reply: func(role, say string) string {
		if role != RoleStart || strings.TrimSpace(say) == "" {
			return "{}"
		}
		b, _ := json.Marshal(map[string]string{"additional_context": say})
		return string(b)
	},
}
