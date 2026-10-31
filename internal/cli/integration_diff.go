package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/thread"
)

// diffPage is how much of a result's diff one page holds, so a page fits
// in one message to a phone.
const diffPage = 32 << 10

// cmdIntegrationDiff is `sous integration --protocol 1 diff [--page <n>]
// -- <n>`: a run's result as a patch, a page at a time, between the base
// and the commit the run reported, so what a host shows is what would be
// approved.
func cmdIntegrationDiff(e *Env, a argv, v int) int {
	if code := flagsOnly(e, a, "protocol", "page"); code != 0 {
		return code
	}
	usage := "usage: sous integration --protocol 1 diff [--page <n>] -- <n>"
	if v != integration.Version1 || len(a.pos) != 2 {
		return fail(e, exitUsage, "diff is a version 1 read; ask with --protocol 1\n%s", usage)
	}
	id, code := threadID(e, a.pos[1])
	if code != 0 {
		return code
	}
	page := 1
	if p := a.value("page"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 {
			return fail(e, exitUsage, "--page counts from 1, not %q\n%s", p, usage)
		}
		page = n
	}
	th, err := thread.Get(e.store(), id)
	if err != nil {
		return threadErr(e, err)
	}
	if th.Run == nil || th.Run.Result == nil {
		return fail(e, exitFailed, "run %d has no result to show; sous show %d", id, id)
	}
	res := th.Run.Result
	patch, err := project.Git(th.Project, "diff", "--no-color", res.Base, res.Commit)
	if err != nil {
		return fail(e, exitFailed, "reading run %d's result: %v", id, err)
	}
	pages := pageLines(patch, diffPage)
	if page > len(pages) {
		return fail(e, exitUsage, "run %d's result has %d pages", id, len(pages))
	}
	d := struct {
		Kind   string `json:"kind"`
		V      int    `json:"v"`
		ID     string `json:"id"`
		Base   string `json:"base"`
		Commit string `json:"commit"`
		Page   int    `json:"page"`
		Pages  int    `json:"pages"`
		Patch  string `json:"patch"`
	}{"diff", integration.Version1, fmt.Sprint(id), res.Base, res.Commit, page, len(pages), pages[page-1]}
	if e.JSON {
		return e.writeJSON(d)
	}
	fmt.Fprint(e.Stdout, d.Patch)
	return 0
}

// pageLines cuts s into pages of at most max bytes at line ends; a line
// longer than a page is cut where it must be. There is always one page.
func pageLines(s string, max int) []string {
	var pages []string
	var b strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		for len(line) > max {
			if b.Len() > 0 {
				pages, b = append(pages, b.String()), strings.Builder{}
			}
			pages, line = append(pages, line[:max]), line[max:]
		}
		if b.Len()+len(line) > max {
			pages, b = append(pages, b.String()), strings.Builder{}
		}
		b.WriteString(line)
	}
	if b.Len() > 0 || len(pages) == 0 {
		pages = append(pages, b.String())
	}
	return pages
}
