package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/bilal-/sous/internal/report"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/thread"
)

// cmdReport: sous report [--week] [--open]. Default window is since the
// last report (24 h on the first run); --week is the last 7 days and does
// not move the marker.
func cmdReport(e *Env, a argv) int {
	week, open := a.has("week"), a.has("open")
	now := time.Now()
	since := now.Add(-24 * time.Hour)
	last, ok, err := report.LastTaken(e.store())
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	if week {
		since = now.Add(-7 * 24 * time.Hour)
	} else if ok {
		since = last
	}
	d, code := buildBoard(e, nil)
	if code != 0 {
		return code
	}
	closed, err := thread.ClosedSince(e.store(), since, now)
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	sessions, err := session.All(e.store())
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	r := report.Build(d, closed, sessions, since, now)
	code = 0
	switch {
	case e.JSON:
		code = e.writeJSON(r)
	case open:
		code = openReport(e, r)
	default:
		report.Render(e.Stdout, r)
	}
	// Only a report that was shown has been taken; --week never moves it.
	if code == 0 && !week {
		if err := report.MarkTaken(e.store(), now); err != nil {
			return fail(e, 1, "%v", err)
		}
	}
	return code
}

// openReport writes the page to $SOUS_HOME/report.html and opens it with
// the platform opener. The page is a static file: no server is started.
func openReport(e *Env, r report.Report) int {
	page := filepath.Join(e.Home, "report.html")
	f, err := os.Create(page)
	if err != nil {
		return fail(e, 1, "%v", err)
	}
	if err := report.RenderHTML(f, r); err != nil {
		f.Close()
		return fail(e, 1, "%v", err)
	}
	f.Close()
	opener := "open" // macOS
	if _, err := exec.LookPath(opener); err != nil {
		opener = "xdg-open"
	}
	if err := exec.Command(opener, page).Run(); err != nil {
		fmt.Fprintf(e.Stdout, "report written to %s\n", page)
		return 0
	}
	fmt.Fprintf(e.Stderr, "→ %s\n", page)
	return 0
}
