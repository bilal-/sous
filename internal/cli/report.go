package cli

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/bilal-/sous/internal/report"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/thread"
)

// cmdReport: sous report [--week] [--open]. A report counts as seen, and
// the next starts from it, only once it was actually shown.
func cmdReport(e *Env, a argv) int {
	week, now := a.has("week"), time.Now()
	since, err := report.Window(e.store(), now, week)
	if err != nil {
		return fail(e, exitFailed, "%v", err)
	}
	d, code := buildBoard(e, nil)
	if code != 0 {
		return code
	}
	closed, err := thread.ClosedSince(e.store(), since, now)
	if err != nil {
		return fail(e, exitFailed, "%v", err)
	}
	sessions, err := session.All(e.store())
	if err != nil {
		return fail(e, exitFailed, "%v", err)
	}
	r := report.Build(d, closed, sessions, since, now)
	// The next report starts after everything this one gathered, including
	// closes found while building the board, so nothing shows twice.
	upTo := time.Now()
	seen := true
	switch {
	case e.JSON:
		// A program reads; only the person seeing it moves the mark.
		code, seen = e.writeJSON(r.JSON()), false
	case a.has("open"):
		code, seen = openReport(e, r)
	default:
		if err := report.Render(e.Stdout, r); err != nil {
			code = fail(e, exitFailed, "%v", err)
		}
	}
	if code == 0 && seen {
		if err := report.Take(e.store(), upTo, week); err != nil {
			return fail(e, exitFailed, "%v", err)
		}
	}
	return code
}

// openReport writes the page and opens it with the platform opener. When
// no opener works the path is printed, and the report does not count as
// seen yet.
func openReport(e *Env, r report.Report) (code int, seen bool) {
	page, err := report.WritePage(e.Home, r)
	if err != nil {
		return fail(e, exitFailed, "%v", err), false
	}
	opener := "open" // macOS
	if _, err := exec.LookPath(opener); err != nil {
		opener = "xdg-open"
	}
	if err := exec.Command(opener, page).Run(); err != nil {
		fmt.Fprintf(e.Stdout, "report written to %s\n", page)
		return 0, false
	}
	fmt.Fprintf(e.Stderr, "→ %s\n", page)
	return 0, true
}
