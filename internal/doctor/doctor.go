// Package doctor checks that sous is set up and working, and says what to
// do about anything that is not. It changes nothing.
package doctor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/install"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/session"
	"github.com/bilal-/sous/internal/signal"
	"github.com/bilal-/sous/internal/store"
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
		out = append(out, fromResult(r.Name, r.OK, r.Detail, r.Fix, Bad))
	}
	if in.Cfg != nil {
		for _, r := range tracker.CheckGitHub(in.Cfg.Identities(config.KeyGitHubAccount)) {
			out = append(out, fromResult(r.Name, r.OK, r.Detail, r.Fix, Warn))
		}
		for _, r := range tracker.CheckGitLab(in.Cfg.GitLabHosts) {
			out = append(out, fromResult(r.Name, r.OK, r.Detail, r.Fix, Warn))
		}
		out = append(out, pluginChecks(in.Cfg.Plugins)...)
	}
	out = append(out, dataChecks(in.SousHome, in.Now)...)
	return append(out, boardCheck(in))
}

// Problems counts the checks that are broken (not the warnings).
func Problems(cs []Check) int {
	n := 0
	for _, c := range cs {
		if c.Status == Bad {
			n++
		}
	}
	return n
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
		return []Check{{Name: "config", Status: Bad, Detail: path + " does not parse: " + in.CfgErr.Error(), Fix: "fix " + path + " by hand"}}
	}
	out := []Check{{Name: "config", Status: OK, Detail: path + " reads"}}
	if len(in.Cfg.Roots) == 0 {
		return append(out, Check{Name: "project folders", Status: Bad, Detail: "none set", Fix: "sous setup, or sous setup ~/path/to/your/projects"})
	}
	ps, unavailable := project.Discover(in.Cfg.Roots, in.Cfg.Ignore, io.Discard)
	noun := "projects"
	if len(ps) == 1 {
		noun = "project"
	}
	c := Check{Name: "project folders", Status: OK, Detail: fmt.Sprintf("%d %s in %s", len(ps), noun, shown(in, in.Cfg.Roots))}
	switch {
	case unavailable > 0:
		var missing []string
		for _, r := range in.Cfg.Roots {
			if _, err := os.Stat(r); err != nil {
				missing = append(missing, config.Tilde(in.UserHome, r))
			}
		}
		c.Status, c.Detail = Bad, fmt.Sprintf("%d folders cannot be read: %v", unavailable, missing)
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
		}
		out = append(out, c)
	}
	return out
}

// dataChecks reads each data file the way sous does, so a damaged or newer
// file shows here rather than as a failed board.
func dataChecks(sousHome string, now time.Time) []Check {
	s := &store.Store{Home: sousHome}
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

// boardCheck: new shells print the saved board; an old one means the
// background refresh is not running.
func boardCheck(in Inputs) Check {
	c, err := board.ReadCache(&store.Store{Home: in.SousHome})
	switch {
	case err != nil:
		return Check{Name: "saved board", Status: Bad, Detail: err.Error(), Fix: "sous --refresh"}
	case c.RenderedAt == nil:
		return Check{Name: "saved board", Status: Warn, Detail: "not built yet", Fix: "sous --refresh"}
	}
	age := in.Now.Sub(*c.RenderedAt)
	window := config.Default().RefreshWindow()
	if in.Cfg != nil {
		window = in.Cfg.RefreshWindow()
	}
	detail := "built " + project.Ago(in.Now, *c.RenderedAt)
	if age > 2*window {
		return Check{Name: "saved board", Status: Warn, Detail: detail + ", longer ago than it should be", Fix: "sous --refresh"}
	}
	return Check{Name: "saved board", Status: OK, Detail: detail}
}
