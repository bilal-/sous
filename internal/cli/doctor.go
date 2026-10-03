package cli

import (
	"fmt"
	"runtime"
	"time"

	"github.com/bilal-/sous/internal/doctor"
)

// cmdDoctor checks that sous is set up and working, and says how to fix
// what is not. It is the one read command that asks GitHub and GitLab.
func cmdDoctor(e *Env, _ argv) int {
	checks := doctor.Run(doctor.Inputs{
		UserHome: e.UserHome, SousHome: e.Home, Exe: e.Exe,
		Shell: e.Shell, GOOS: runtime.GOOS, Zdotdir: e.Zdotdir,
		Cfg: e.Cfg, CfgErr: e.cfgErr, Now: time.Now(),
	})
	problems, warnings := doctor.Count(checks, doctor.Bad), doctor.Count(checks, doctor.Warn)
	code := 0
	if problems > 0 {
		code = exitFailed
	}
	if e.JSON {
		if c := e.writeJSON(map[string]any{"problems": problems, "warnings": warnings, "checks": checks}); c != 0 {
			return c
		}
		return code
	}
	fmt.Fprintf(e.Stdout, "sous doctor · %d checks · %s · %d to look at\n\n", len(checks), doctor.Plural(problems, "problem"), warnings)
	for _, c := range checks {
		mark := map[doctor.Status]string{doctor.OK: "✓", doctor.Warn: "!", doctor.Bad: "✗"}[c.Status]
		fmt.Fprintf(e.Stdout, "  %s %s: %s\n", mark, c.Name, c.Detail)
		if c.Fix != "" {
			fmt.Fprintf(e.Stdout, "      fix: %s\n", c.Fix)
		}
	}
	if problems == 0 && warnings == 0 {
		fmt.Fprintln(e.Stdout, "\n  everything works")
	}
	return code
}
