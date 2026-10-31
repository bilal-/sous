package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/bilal-/sous/internal/update"
)

func cmdUpdate(e *Env, a argv) int {
	u := e.Updater
	if u == nil {
		u = update.New(e.Exe, Version)
		u.OS, u.Arch = runtime.GOOS, runtime.GOARCH
		u.Env = append(os.Environ(), "NONINTERACTIVE=1", "HOMEBREW_NO_AUTO_UPDATE=1", "GIT_TERMINAL_PROMPT=0")
	}
	ctx, cancel := context.WithTimeout(e.ctx(), 5*time.Minute)
	defer cancel()
	if a.has("check") {
		s, err := u.Check(ctx)
		if err != nil {
			return fail(e, exitFailed, "update: %v", err)
		}
		next := []string{"sous version"}
		said := fmt.Sprintf("sous %s is current (latest release %s)", s.Current, s.Latest)
		if s.Available != nil && *s.Available {
			said = fmt.Sprintf("sous %s available · installed %s · %s", s.Latest, s.Current, s.Method)
			next = []string{"sous update"}
		}
		if s.Method == "source" {
			said = fmt.Sprintf("sous %s is a source build · latest release %s · update your checkout and run make install", s.Current, s.Latest)
			next = []string{"sous update --help"}
		}
		if e.JSON {
			return e.writeJSON(struct {
				update.Status
				Next []string `json:"next"`
			}{s, next})
		}
		fmt.Fprintln(e.Stdout, said+" · "+next[0])
		return 0
	}
	s, err := u.Apply(ctx)
	if err != nil {
		return fail(e, exitFailed, "update: %v", err)
	}
	did, said := didAlreadyCurrent, "sous "+s.Installed+" is already current"
	if s.Changed {
		did, said = didUpdated, "updated sous from "+s.Current+" to "+s.Installed
	}
	return e.changed(changedJSON{ID: "sous", Did: did, Update: &s, Next: []string{"sous version", "sous doctor"}}, said)
}
