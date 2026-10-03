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
	if r, missing := missingTool("gh", "GitHub", "https://cli.github.com", "accounts", len(accounts) > 0); missing {
		return r
	}
	err := GHReady("")
	gh := readyResult("gh", err, "gh auth login", "gh auth status")
	gh.Optional = len(accounts) == 0 && notSetUp(err)
	out := []Result{gh}
	for _, a := range accounts {
		out = append(out, readyResult("GitHub account "+a, GHReady(a), "gh auth login --hostname github.com  (then log in as "+a+")", "gh auth status"))
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
		r := readyResult(name, err, NotificationsFix, "gh api notifications")
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
	if r, missing := missingTool("glab", "GitLab", "https://gitlab.com/gitlab-org/cli", "hosts", len(configured) > 0); missing {
		return r
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
		out = append(out, readyResult("GitLab host "+h, GLabReady(h), "glab auth login --hostname "+h, "glab auth status --hostname "+h))
	}
	return out
}

// missingTool is the check's answer when tool is not installed: a note
// when config asks nothing of it, a problem when config names its
// tracker's accounts or hosts (what).
func missingTool(tool, tracker, install, what string, configured bool) ([]Result, bool) {
	if installed(tool) == nil {
		return nil, false
	}
	if !configured {
		return []Result{{Name: tool, OK: true, Detail: "not installed; " + tracker + " is not set up here (optional)"}}, true
	}
	return []Result{{Name: tool, Detail: "not installed, but config.toml names " + tracker + " " + what, Fix: "install " + tool + ": " + install}}, true
}

// notSetUp: the tool or its login is missing, rather than broken.
func notSetUp(err error) bool {
	return errors.Is(err, ErrNotInstalled) || errors.Is(err, ErrNoLogin)
}

// readyResult turns a readiness error into a result, naming the fix only
// when the problem is one the fix solves (no login).
// Any other failure names the command that shows what is wrong (look), so
// there is always a next step.
func readyResult(name string, err error, fix, look string) Result {
	if err == nil {
		return Result{Name: name, OK: true, Detail: "works"}
	}
	r := Result{Name: name, Detail: err.Error(), Fix: look}
	if notSetUp(err) || strings.Contains(err.Error(), "token") {
		r.Fix = fix
	}
	return r
}
