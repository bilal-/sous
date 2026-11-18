package harness

import "path/filepath"

// Kimi Code: [[hooks]] in ~/.kimi-code/config.toml; UserPromptSubmit injects
// context (once per session), while Stop and SessionEnd record progress.
var kimi = Harness{
	Name:     "kimi",
	Display:  "Kimi Code",
	Bin:      "kimi",
	Present:  func(home string) bool { return installed(home, "kimi", ".kimi-code") },
	HookFile: func(home string) string { return filepath.Join(home, ".kimi-code", "config.toml") },
	Format:   TOMLHooks{},
	Hooks: []Hook{
		{Role: RoleStart, Event: "UserPromptSubmit"},
		{Role: RoleEnd, Event: "Stop"},
		{Role: RoleEnd, Event: "SessionEnd"},
	},
	Parse: parseHookJSON,
}
