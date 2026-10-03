package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bilal-/sous/docs"
)

// verbHelp is a command's usage line, then everything docs/commands.md
// says about it: its sections, as plain text.
func verbHelp(e *Env, v verb) int {
	if v.usage == "" {
		return doorHelp(e, v.name)
	}
	fmt.Fprintf(e.Stdout, "usage: %s\n", v.synopsis())
	if _, about, ok := strings.Cut(v.usage, "  "); ok {
		fmt.Fprintf(e.Stdout, "  %s\n", strings.TrimSpace(about))
	}
	for _, s := range docSections(docs.Commands, v.name) {
		fmt.Fprintf(e.Stdout, "\n%s\n", s)
	}
	return 0
}

// mdLink is a markdown link, whose text is all that reads in a terminal.
var mdLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)

// docSections are the sections of a guide about `sous <name>`: each from
// its "### `sous <name>…" heading to the next heading, the heading as its
// first line, links as their text and code fences dropped.
func docSections(guide, name string) []string {
	var out []string
	var cur []string
	open := false
	flush := func() {
		if open {
			out = append(out, strings.TrimRight(strings.Join(cur, "\n"), "\n"))
		}
		cur, open = nil, false
	}
	for _, line := range strings.Split(guide, "\n") {
		if strings.HasPrefix(line, "#") {
			flush()
			head, isCmd := strings.CutPrefix(line, "### `sous "+name)
			if isCmd && (head == "" || head[0] == ' ' || head[0] == '`') {
				open = true
				cur = append(cur, strings.ReplaceAll(strings.TrimPrefix(line, "### "), "`", ""))
			}
			continue
		}
		if open && !strings.HasPrefix(line, "```") {
			cur = append(cur, mdLink.ReplaceAllString(line, "$1"))
		}
	}
	flush()
	return out
}

// doorHelp is help for a command plugins and hooks call, which help does
// not list: its row of the table in docs/commands.md.
func doorHelp(e *Env, name string) int {
	for _, line := range strings.Split(docs.Commands, "\n") {
		cells := strings.Split(strings.ReplaceAll(line, `\|`, "\x00"), "|")
		if len(cells) < 4 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "`sous "+name+" ") {
			continue
		}
		clean := func(s string) string {
			return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "`", ""), "\x00", "|")
		}
		fmt.Fprintf(e.Stdout, "usage: %s\n  %s\n", clean(cells[1]), mdLink.ReplaceAllString(clean(cells[2]), "$1"))
		return 0
	}
	return fail(e, exitUsage, "%q is not a command; sous help lists them", name)
}
