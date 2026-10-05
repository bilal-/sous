// Package integration supplies a native host with sous's task state. The host
// owns presentation, notifications and the lifetime of its subscription.
package integration

const (
	Version  = 0
	Provider = "sous"
	Protocol = "sous.integration"
)

type Operation struct {
	Argv    []string `json:"argv"`
	Format  string   `json:"format"`
	Offline bool     `json:"offline"`
}

// Action arguments are whole argv slots. A host substitutes parameters as
// literal arguments, never as a shell command, and invokes writes only on the
// person's request. Items list which actions apply to them.
type Action struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Argv       []string `json:"argv"`
	Cwd        string   `json:"cwd,omitempty"`
	Parameters []string `json:"parameters"`
	Format     string   `json:"format"`
	Effect     string   `json:"effect"`
	Offline    bool     `json:"offline"`
	Terminal   bool     `json:"terminal"`
}

type Description struct {
	Kind           string               `json:"kind"`
	V              int                  `json:"v"`
	Provider       string               `json:"provider"`
	Protocol       string               `json:"protocol"`
	Version        string               `json:"version"`
	Repository     string               `json:"repository"`
	Documentation  string               `json:"documentation"`
	Schema         string               `json:"schema"`
	PollIntervalMS int                  `json:"poll_interval_ms"`
	Operations     map[string]Operation `json:"operations"`
	Actions        []Action             `json:"actions"`
}

func Describe(version string) Description {
	return Description{
		Kind: "description", V: Version, Provider: Provider, Protocol: Protocol, Version: version,
		Repository:     "https://github.com/bilal-/sous",
		Documentation:  "https://github.com/bilal-/sous/blob/main/docs/integrations.md",
		Schema:         "https://raw.githubusercontent.com/bilal-/sous/main/integrations/schema.json",
		PollIntervalMS: 1000,
		Operations: map[string]Operation{
			"describe": {Argv: []string{"sous", "integration", "--json"}, Format: "json", Offline: true},
			"snapshot": {Argv: []string{"sous", "integration", "snapshot", "--json"}, Format: "json", Offline: true},
			"watch":    {Argv: []string{"sous", "integration", "watch", "--json"}, Format: "ndjson", Offline: true},
			"context":  {Argv: []string{"sous", "here", "--json", "--", "{path}"}, Format: "json", Offline: true},
			"refresh":  {Argv: []string{"sous", "--refresh"}, Format: "none", Offline: false},
		},
		Actions: []Action{
			{ID: "show", Title: "Show details", Argv: []string{"sous", "show", "--json", "--", "{id}"}, Parameters: []string{"id"}, Format: "json", Effect: "read"},
			{ID: "open", Title: "Open project", Argv: []string{"sous", "go", "--in", "{project}", "--", "{project}"}, Parameters: []string{"project"}, Format: "terminal", Effect: "launch", Offline: true, Terminal: true},
			{ID: "snooze", Title: "Snooze", Argv: []string{"sous", "snooze", "--json", "--", "{id}"}, Parameters: []string{"id"}, Format: "json", Effect: "write_private", Offline: true},
			{ID: "done", Title: "Close note", Argv: []string{"sous", "done", "--json", "--", "{id}"}, Parameters: []string{"id"}, Format: "json", Effect: "write_private"},
			{ID: "reply", Title: "Answer run", Argv: []string{"sous", "reply", "--json", "--", "{id}", "{answer}"}, Parameters: []string{"id", "answer"}, Format: "json", Effect: "write_private"},
			{ID: "note", Title: "Remember a note", Argv: []string{"sous", "note", "--json", "--", "{text}"}, Cwd: "{project}", Parameters: []string{"project", "text"}, Format: "json", Effect: "write_private", Offline: true},
		},
	}
}
