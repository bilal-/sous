// Package doctor checks that sous is set up and working, and says what to
// do about anything that is not. It changes nothing.
package doctor

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/install"
	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/text"
	"github.com/bilal-/sous/internal/thread"
	"github.com/bilal-/sous/internal/tracker"
)

// Status of one check. Warn is worth a look but not broken.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Bad  Status = "bad"
)

// Check is one thing doctor looked at.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// Inputs are what the checks need, all read by the caller.
type Inputs struct {
	UserHome, SousHome, Exe string
	Shell, GOOS, Zdotdir    string
	Cfg                     *config.Config
	CfgErr                  error
	Now                     time.Time
}

// Run makes every check, in the order a person would fix them: config and
// projects, what setup installs, the trackers, plugins, data files, and the
// saved board.
func Run(in Inputs) []Check {
	out := configChecks(in)
	for _, r := range install.Check(in.UserHome, in.SousHome, in.Exe, in.Shell, in.GOOS, in.Zdotdir) {
		out = append(out, fromResult(r.Name, r.OK, r.Detail, r.Fix, severity(r.Optional)))
	}
	if in.Cfg != nil {
		out = append(out, trackerChecks(in.Cfg)...)
		out = append(out, pluginChecks(in.Cfg.Plugins)...)
		out = append(out, runnerChecks(in.UserHome)...)
	}
	out = append(out, dataChecks(in.SousHome, in.Now)...)
	return append(out, boardChecks(in)...)
}

// trackerTimeout bounds each tracker's checks: a gh or glab that hangs is
// reported, not waited on.
var trackerTimeout = 30 * time.Second

// trackerChecks asks gh and glab at once, within trackerTimeout. A failure
// the board would show as a failed source is a problem; a tracker that is
// simply not set up here is a note.
func trackerChecks(cfg *config.Config) []Check {
	runs := tracker.Kinds
	answers := make([]chan []tracker.Result, len(runs))
	for i, k := range runs {
		answers[i] = make(chan []tracker.Result, 1)
		go func() { answers[i] <- k.Check(cfg) }()
	}
	ctx, cancel := context.WithTimeout(context.Background(), trackerTimeout)
	defer cancel()
	var out []Check
	for i, r := range runs {
		var results []tracker.Result
		select {
		case results = <-answers[i]:
		case <-ctx.Done():
			results = []tracker.Result{{Name: r.Tool, Detail: fmt.Sprintf("did not answer within %s", trackerTimeout), Fix: "try " + r.Tool + " auth status"}}
		}
		for _, t := range results {
			out = append(out, fromResult(t.Name, t.OK, t.Detail, t.Fix, severity(t.Optional)))
		}
	}
	return out
}

// Count is how many checks have status st.
func Count(cs []Check, st Status) int {
	n := 0
	for _, c := range cs {
		if c.Status == st {
			n++
		}
	}
	return n
}

// severity of a failed check: a part a person may do without is worth a
// look; anything else is broken.
func severity(optional bool) Status {
	if optional {
		return Warn
	}
	return Bad
}

func fromResult(name string, ok bool, detail, fix string, notOK Status) Check {
	if ok {
		return Check{Name: name, Status: OK, Detail: detail}
	}
	return Check{Name: name, Status: notOK, Detail: detail, Fix: fix}
}

func configChecks(in Inputs) []Check {
	path := config.Tilde(in.UserHome, config.Path(in.SousHome))
	if in.CfgErr != nil {
		return []Check{{Name: "config", Status: Bad, Detail: path + " cannot be read: " + in.CfgErr.Error(), Fix: "fix " + path + " by hand"}}
	}
	out := []Check{{Name: "config", Status: OK, Detail: path + " reads"}}
	if _, err := os.Stat(config.Path(in.SousHome)); err != nil {
		out[0].Detail = path + " is not written yet, so sous uses its defaults"
	}
	if len(in.Cfg.Roots) == 0 {
		return append(out, Check{Name: "project folders", Status: Bad, Detail: "none set", Fix: "sous setup, or sous setup ~/path/to/your/projects"})
	}
	ps, unavailable := project.Discover(in.Cfg.Roots, in.Cfg.Ignore, io.Discard)
	c := Check{Name: "project folders", Status: OK, Detail: text.Plural(len(ps), "project") + " in " + shown(in, in.Cfg.Roots)}
	switch {
	case unavailable > 0:
		var unreadable []string
		for _, r := range in.Cfg.Roots {
			if _, err := os.ReadDir(r); err != nil {
				unreadable = append(unreadable, config.Tilde(in.UserHome, r))
			}
		}
		c.Status, c.Detail = Bad, "cannot read "+strings.Join(unreadable, ", ")
		c.Fix = "sous config roots <folders that exist>, or sous setup <folder>"
	case len(ps) == 0:
		c.Status, c.Fix = Warn, "sous setup ~/path/to/your/projects"
	}
	return append(out, c)
}

func shown(in Inputs, roots []string) string {
	s := ""
	for i, r := range roots {
		if i > 0 {
			s += ", "
		}
		s += config.Tilde(in.UserHome, r)
	}
	return s
}

func pluginChecks(plugins []string) []Check {
	var out []Check
	for _, p := range plugins {
		c := Check{Name: "plugin " + filepath.Base(p), Status: OK, Detail: "can run"}
		if _, err := exec.LookPath(p); err != nil {
			c.Status, c.Detail, c.Fix = Bad, p+" is missing or cannot run", "sous config plugins --remove "+p
		} else if !plugin.Named(p) {
			c.Status, c.Detail = Bad, "sous skips it: a plugin's name must be "+plugin.Names()+" and then its own name"
			c.Fix = "rename it, then sous config plugins --remove " + p + " and --add the new path"
		}
		out = append(out, c)
	}
	return out
}

// runnerChecks: the agent CLI each built in runner starts. Runs are
// optional, so a missing one is worth a look, not broken; sous go --run
// says the same, with exit 3, when it is tried.
func runnerChecks(home string) []Check {
	var out []Check
	for _, h := range harness.All {
		if h.Headless == nil || !h.Here(home) {
			continue
		}
		c := Check{Name: "runner " + h.Name, Status: OK, Detail: h.Bin + " installed"}
		if _, err := exec.LookPath(h.Bin); err != nil {
			c.Status, c.Detail = Warn, h.Bin+" is not installed, so sous go --run -a "+h.Name+" cannot start"
			c.Fix = "install " + h.Bin + ", or pick another: sous config runner <name>"
		}
		out = append(out, c)
	}
	return out
}

// dataChecks reads each data file the way sous does, so a damaged or newer
// file shows here rather than as a failed board.
func dataChecks(sousHome string, now time.Time) []Check {
	s := &store.Store{Home: sousHome, ReadOnly: true}
	reads := []struct {
		name string
		read func() error
	}{
		{"notes (threads.json)", func() error { _, err := thread.Open(s, now); return err }},
		{"sessions (sessions.json)", func() error { _, err := session.All(s); return err }},
		{"observations (observed.json)", func() error { _, err := signal.Known(s, ""); return err }},
	}
	var out []Check
	for _, r := range reads {
		c := Check{Name: r.name, Status: OK, Detail: "reads"}
		if err := r.read(); err != nil {
			c.Status, c.Detail, c.Fix = Bad, err.Error(), "restore it from a backup, or upgrade sous if it was written by a newer one"
		}
		out = append(out, c)
	}
	return out
}

// boardChecks: new shells print the saved board; an old one means the
// background refresh is not running. Each source its last refresh could not
// read is listed with the reason, which is what a ? on the board means.
func boardChecks(in Inputs) []Check {
	c, err := board.ReadCache(&store.Store{Home: in.SousHome, ReadOnly: true})
	switch {
	case err != nil:
		return []Check{{Name: "saved board", Status: Bad, Detail: err.Error(), Fix: "sous --refresh"}}
	case c.RenderedAt == nil:
		return []Check{{Name: "saved board", Status: Warn, Detail: "not built yet", Fix: "sous --refresh"}}
	}
	age := in.Now.Sub(*c.RenderedAt)
	window := config.Default().RefreshWindow()
	if in.Cfg != nil {
		window = in.Cfg.RefreshWindow()
	}
	detail := "built " + text.Ago(in.Now, *c.RenderedAt)
	board := Check{Name: "saved board", Status: OK, Detail: detail}
	if age > 2*window {
		board = Check{Name: "saved board", Status: Warn, Detail: detail + ", longer ago than it should be", Fix: "sous --refresh"}
	}
	out := []Check{board}
	// What the board marked ?, and why, from its last refresh.
	if c.Data != nil {
		for _, p := range c.Data.Plugins {
			if p.Gap() {
				out = append(out, Check{Name: "last refresh: " + p.Name, Status: Warn, Detail: string(p.Status) + ": " + cmp.Or(deref(p.Error), "no reason given"), Fix: "fix what the checks above name, then sous --refresh"})
			}
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
