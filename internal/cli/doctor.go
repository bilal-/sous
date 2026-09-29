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
	problems := doctor.Problems(checks)
	code := 0
	if problems > 0 {
		code = 1
	}
	if e.JSON {
		if c := e.writeJSON(map[string]any{"problems": problems, "checks": checks}); c != 0 {
			return c
		}
		return code
	}
	warnings := 0
	for _, c := range checks {
		if c.Status == doctor.Warn {
			warnings++
		}
	}
	fmt.Fprintf(e.Stdout, "sous doctor · %d checks · %d problems · %d to look at\n\n", len(checks), problems, warnings)
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
