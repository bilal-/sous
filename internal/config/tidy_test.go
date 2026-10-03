package config

import (
	"strings"
	"testing"
)

// Deferred from the 0.2 review, now fixed: edits leave the file looking as
// if a person had made them.
func TestEditsLeaveTheFileTidy(t *testing.T) {
	checkSets(t, []setCase{
		{"unset takes its trailing comment", "agent = \"codex\" # mine\nroots = [\"~/code\"]\n", nil, "agent", nil, "roots = [\"~/code\"]\n"},
		{"CRLF stays CRLF when adding", "agent = \"codex\"\r\n", nil, "refresh_hours", 2, "agent = \"codex\"\r\nrefresh_hours = 2\r\n"},
		{"CRLF stays CRLF when unsetting", "agent = \"codex\"\r\nroots = [\"~/code\"]\r\n", nil, "agent", nil, "roots = [\"~/code\"]\r\n"},
		{"new table in a CRLF file", "agent = \"codex\"\r\n", projects("acme/*"), "backend", "markdown", "agent = \"codex\"\r\n\r\n[projects.\"acme/*\"]\r\nbackend = \"markdown\"\r\n"},
		{"last key takes its empty table", "agent = \"codex\"\n\n[projects.\"acme/*\"]\nbackend = \"markdown\"\n", projects("acme/*"), "backend", nil, "agent = \"codex\"\n"},
		{"a table with other keys stays", "[projects.\"acme/*\"]\nbackend = \"markdown\"\nx = \"1\"\n", projects("acme/*"), "backend", nil, "[projects.\"acme/*\"]\nx = \"1\"\n"},
		{"an empty table with a comment stays", "[projects.\"acme/*\"] # work\nbackend = \"markdown\"\n", projects("acme/*"), "backend", nil, "[projects.\"acme/*\"] # work\n"},
	})
}

// Replacing a list whose comments sous would lose is refused, so a person's
// notes inside it are never dropped silently.
func TestReplacingACommentedListIsRefused(t *testing.T) {
	in := "ignore = [\n  \"scratch/*\", # old experiments\n  \"tmp/*\",\n]\n"
	got, err := setIn(t, in, nil, "ignore", []string{"a/*"})
	if err == nil || got != in || !strings.Contains(err.Error(), "comment") {
		t.Fatalf("%v\n%s", err, got)
	}
}
