package harness

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Copilot CLI: hooks in ~/.copilot/hooks/sous.json, skills in ~/.copilot/skills.
var copilot = Harness{
	Name:     "copilot",
	Display:  "Copilot CLI",
	Bin:      "copilot",
	Present:  func(home string) bool { return installed(home, "copilot", ".copilot") },
	SkillDir: func(home string) string { return filepath.Join(home, ".copilot", "skills") },
	HookFile: func(home string) string { return filepath.Join(home, ".copilot", "hooks", "sous.json") },
	Format:   FlatHooksJSON{Copilot: true},
	Hooks: []Hook{
		{Role: RoleStart, Event: "sessionStart"},
		{Role: RoleEnd, Event: "sessionEnd"},
	},
	Parse: parseHookJSON,
	Reply: func(role, say string) string {
		if role != RoleStart || strings.TrimSpace(say) == "" {
			return "{}"
		}
		b, _ := json.Marshal(map[string]string{"additionalContext": say})
		return string(b)
	},
}
