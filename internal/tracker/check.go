package tracker

import (
	"errors"
	"strings"

	"github.com/bilal-/sous/internal/config"
)

// Result is one tracker check: whether it works, what was found, and the
// command that puts it right. Optional: the tracker is simply not set up
// here, which the board passes over quietly; any other failure shows on
// the board as a source that failed.
type Result struct {
	Name     string
	OK       bool
	Detail   string
	Fix      string
	Optional bool
}

// CheckGitHub says whether gh can do what the board asks of it, with the
// same calls: installed, logged in, each configured account usable, and
// each login's notifications readable. It asks GitHub, so it is for sous
// doctor, never for the board's hot path.
func CheckGitHub(accounts []string) []Result {
	if err := GHInstalled(); err != nil {
		if len(accounts) == 0 {
			return []Result{{Name: "gh", OK: true, Detail: "not installed; GitHub is not set up here (optional)"}}
		}
		return []Result{{Name: "gh", Detail: "not installed, but config.toml names GitHub accounts", Fix: "install gh: https://cli.github.com"}}
	}
	err := GHReady("")
	gh := readyResult("gh", err, "gh auth login")
	gh.Optional = len(accounts) == 0 && notSetUp(err)
	out := []Result{gh}
	for _, a := range accounts {
		out = append(out, readyResult("GitHub account "+a, GHReady(a), "gh auth login --hostname github.com  (then log in as "+a+")"))
	}
	// Notifications, with each working login's own token.
	for i, a := range append([]string{""}, accounts...) {
		if !out[i].OK {
			continue
		}
		name := "GitHub notifications"
		if a != "" {
			name += " (" + a + ")"
		}
		_, err := GHNotifications(a)
		r := readyResult(name, err, NotificationsFix)
		if err != nil && strings.Contains(err.Error(), NotificationsFix) {
			r.Fix = NotificationsFix
		}
		out = append(out, r)
	}
	return out
}

// CheckGitLab says whether glab can reach each GitLab host the board asks:
// the ones config.toml names and the ones glab is logged in to.
func CheckGitLab(configured []string) []Result {
	if err := GLabInstalled(); err != nil {
		if len(configured) == 0 {
			return []Result{{Name: "glab", OK: true, Detail: "not installed; GitLab is not set up here (optional)"}}
		}
		return []Result{{Name: "glab", Detail: "not installed, but config.toml names GitLab hosts", Fix: "install glab: https://gitlab.com/gitlab-org/cli"}}
	}
	hosts, err := GitLabHosts(&config.Config{GitLabHosts: configured})
	if err != nil {
		return []Result{{Name: "glab", Detail: "could not ask glab which hosts it knows: " + err.Error(), Fix: "glab auth status"}}
	}
	if len(hosts) == 0 {
		return []Result{{Name: "glab", OK: true, Detail: "installed, not logged in to any host; GitLab is not set up here (optional)"}}
	}
	out := []Result{{Name: "glab", OK: true, Detail: "installed"}}
	for _, h := range hosts {
		out = append(out, readyResult("GitLab host "+h, GLabReady(h), "glab auth login --hostname "+h))
	}
	return out
}

// notSetUp: the tool or its login is missing, rather than broken.
func notSetUp(err error) bool {
	return errors.Is(err, ErrNotInstalled) || errors.Is(err, ErrNoLogin)
}

// readyResult turns a readiness error into a result, naming the fix only
// when the problem is one the fix solves (no login).
func readyResult(name string, err error, fix string) Result {
	if err == nil {
		return Result{Name: name, OK: true, Detail: "works"}
	}
	r := Result{Name: name, Detail: err.Error()}
	if notSetUp(err) || strings.Contains(err.Error(), "token") {
		r.Fix = fix
	}
	return r
}
