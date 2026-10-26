package integration

// Version 1 of the protocol. A host asks for it with --protocol 1 on every
// call; without the flag sous answers version 0 exactly as before. While
// Draft is true, version 1 may still change; integrations/schema.v1.json is
// the whole of it as it will be, and sous fills it in release by release.
const Version1 = 1

// Versions are the protocol versions this sous answers.
var Versions = []int{Version, Version1}

// Parameter is one argument an action takes. Type tells a host what to put
// there: an item's id, a project path, a line of text, longer text, a
// commit, or a question as <id>:<token>.
type Parameter struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // id | project | string | text | commit | question
	Required bool   `json:"required"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

// ActionV1 is an action as version 1 describes it. Scope says what it acts
// on: the whole board, a project, or one item, which lists it among its
// actions. Input "stdin" sends the one text parameter on standard input.
// Op: it takes --op <op> and answers a repeat with the same outcome.
type ActionV1 struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Argv       []string    `json:"argv"`
	Cwd        string      `json:"cwd,omitempty"`
	Parameters []Parameter `json:"parameters"`
	Scope      string      `json:"scope"` // global | project | item
	Input      string      `json:"input,omitempty"`
	Format     string      `json:"format"`
	Effect     string      `json:"effect"` // read | launch | write_private | write_public
	Confirm    bool        `json:"confirm"`
	Op         bool        `json:"op"`
	Offline    bool        `json:"offline"`
	Terminal   bool        `json:"terminal"`
}

// DescriptionV1 is version 1's description.
type DescriptionV1 struct {
	Kind           string               `json:"kind"`
	V              int                  `json:"v"`
	Versions       []int                `json:"versions"`
	Draft          bool                 `json:"draft"`
	Provider       string               `json:"provider"`
	Protocol       string               `json:"protocol"`
	Version        string               `json:"version"`
	Repository     string               `json:"repository"`
	Documentation  string               `json:"documentation"`
	Schema         string               `json:"schema"`
	PollIntervalMS int                  `json:"poll_interval_ms"`
	Operations     map[string]Operation `json:"operations"`
	Actions        []ActionV1           `json:"actions"`
}

// SchemaV1 is where version 1's schema lives.
const SchemaV1 = "https://raw.githubusercontent.com/bilal-/sous/main/integrations/schema.v1.json"

// DescribeV1 is version 1's description. Its commands carry --protocol 1,
// so a host runs them as given and stays on version 1.
func DescribeV1(version string) DescriptionV1 {
	v0 := Describe(version)
	ops := map[string]Operation{}
	for name, op := range v0.Operations {
		if name == "describe" || name == "snapshot" || name == "watch" {
			op.Argv = append([]string{"sous", "integration", "--protocol", "1"}, op.Argv[2:]...)
		}
		ops[name] = op
	}
	id := Parameter{Name: "id", Type: "id", Required: true}
	proj := Parameter{Name: "project", Type: "project", Required: true}
	return DescriptionV1{
		Kind: "description", V: Version1, Versions: Versions, Draft: true,
		Provider: Provider, Protocol: Protocol, Version: version,
		Repository: v0.Repository, Documentation: v0.Documentation, Schema: SchemaV1,
		PollIntervalMS: v0.PollIntervalMS, Operations: ops,
		Actions: []ActionV1{
			{ID: "show", Title: "Show details", Argv: []string{"sous", "show", "--json", "--", "{id}"}, Parameters: []Parameter{id}, Scope: "item", Format: "json", Effect: "read"},
			{ID: "open", Title: "Open project", Argv: []string{"sous", "go", "--in", "{project}", "--", "{project}"}, Parameters: []Parameter{proj}, Scope: "project", Format: "terminal", Effect: "launch", Offline: true, Terminal: true},
			{ID: "snooze", Title: "Snooze", Argv: []string{"sous", "snooze", "--json", "--", "{id}"}, Parameters: []Parameter{id}, Scope: "item", Format: "json", Effect: "write_private", Offline: true},
			{ID: "done", Title: "Close note", Argv: []string{"sous", "done", "--json", "--", "{id}"}, Parameters: []Parameter{id}, Scope: "item", Format: "json", Effect: "write_private"},
			{ID: "reply", Title: "Answer run", Argv: []string{"sous", "reply", "--json", "--", "{id}", "{answer}"}, Parameters: []Parameter{id, {Name: "answer", Type: "string", Required: true, MaxBytes: 4096}}, Scope: "item", Format: "json", Effect: "write_private"},
			{ID: "note", Title: "Remember a note", Argv: []string{"sous", "note", "--json", "--", "{text}"}, Cwd: "{project}", Parameters: []Parameter{proj, {Name: "text", Type: "string", Required: true, MaxBytes: 4096}}, Scope: "project", Format: "json", Effect: "write_private", Offline: true},
		},
	}
}
