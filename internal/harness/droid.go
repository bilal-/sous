package harness

import "path/filepath"

// Factory Droid: hooks in ~/.factory/hooks.json (root event map, matcher groups).
var droid = Harness{
	Name:     "droid",
	Display:  "Factory Droid",
	Bin:      "droid",
	Present:  func(home string) bool { return installed(home, "droid", ".factory") },
	HookFile: func(home string) string { return filepath.Join(home, ".factory", "hooks.json") },
	Format:   HooksJSON{RootMap: true},
	Hooks: []Hook{
		{Role: RoleStart, Event: "SessionStart"},
		{Role: RoleEnd, Event: "Stop"},
		{Role: RoleEnd, Event: "PreCompact"},
		{Role: RoleEnd, Event: "SessionEnd"},
	},
	Parse: parseHookJSON,
}
