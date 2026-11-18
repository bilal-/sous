package harness

import (
	_ "embed"
	"path/filepath"
	"regexp"
)

// opencode: no shell hooks, but plugins: sous writes one (OpencodePlugin)
// that runs its hooks at a session's first model call and when the
// session goes idle. Skills come from ~/.agents/skills, which it reads.
var opencode = Harness{
	Name:     "opencode",
	Display:  "opencode",
	Bin:      "opencode",
	Present:  func(home string) bool { return installed(home, "opencode", ".config/opencode") },
	HookFile: func(home string) string { return filepath.Join(home, ".config", "opencode", "plugins", "sous.js") },
	Format:   OpencodePlugin{},
	Hooks: []Hook{
		{Role: RoleStart, Event: RoleStart},
		{Role: RoleEnd, Event: RoleEnd},
	},
	Parse: parseHookJSON,
	// No Last: the plugin sends what the agent said last; there is no
	// transcript.
	// opencode run asks nobody: a run has the permissions the person's
	// opencode config gives, as an opencode session of theirs would.
	Headless: &Headless{
		Start:   func(r Run) []string { return opencodeArgs(r, "--", r.Prompt) },
		Resume:  func(r Run) []string { return opencodeArgs(r, "--session", r.Session, "--", r.Answer) },
		Session: firstMatch(opencodeSession),
		Last:    func(_ Run, log []byte) string { return opencodeLast(log) },
	},
}

// opencodeArgs: one run, its events as JSON, in the worktree. --dir because
// opencode works where it is told, not where PWD says.
func opencodeArgs(r Run, then ...string) []string {
	return append([]string{"run", "--format", "json", "--dir", r.Worktree}, then...)
}

var opencodeSession = regexp.MustCompile(`"sessionID"\s*:\s*"([^"]+)"`)

// opencodeLast is the last text the agent wrote, from opencode run's JSON
// events.
func opencodeLast(log []byte) string {
	type event struct {
		Type string `json:"type"`
		Part struct {
			Text string `json:"text"`
		} `json:"part"`
	}
	return lastJSON(log, func(e event) bool { return e.Type == "text" && e.Part.Text != "" }).Part.Text
}

//go:embed assets/opencode-plugin.js
var opencodePlugin string

// OpencodePlugin is a plugin file sous owns whole: each hook's command is
// written into it, on a "// sous hook <event>: <command>" line it can be
// read back from.
type OpencodePlugin struct{}

func (OpencodePlugin) Place(file, event string, cmd Cmd) (bool, error) {
	return (ScriptPlugin{Template: opencodePlugin}).Place(file, event, cmd)
}

func (OpencodePlugin) Commands(file, event string) ([]string, error) {
	return (ScriptPlugin{Template: opencodePlugin}).Commands(file, event)
}
