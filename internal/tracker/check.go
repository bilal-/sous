package tracker

import (
	"errors"
	"strings"

	"github.com/bilal-/sous/internal/config"
)

// Result is one tracker check: whether it works, what was found, and the
// command that puts it right.
type Result struct {
	Name   string
	OK     bool
	Detail string
	Fix    string
}

// CheckGitHub says whether gh can do what sous asks of it: installed,
// logged in, each configured account usable, and notifications readable.
// It asks GitHub, so it is for sous doctor, never for the board's hot path.
// With gh missing and no accounts configured, GitHub is simply not set up.
func CheckGitHub(accounts []string) []Result {
	if err := GHInstalled(); err != nil {
		if len(accounts) == 0 {
			return []Result{{Name: "gh", OK: true, Detail: "not installed; GitHub is not set up here (optional)"}}
		}
		return []Result{{Name: "gh", Detail: "not installed, but config.toml names GitHub accounts", Fix: "install gh: https://cli.github.com"}}
	}
	out := []Result{readyResult("gh", GHReady(""), "gh auth login")}
	for _, a := range accounts {
		out = append(out, readyResult("GitHub account "+a, GHReady(a), "gh auth login --hostname github.com  (then log in as "+a+")"))
	}
	if out[0].OK {
		_, err := GHRun("", "api", "--hostname", GitHubHost, "notifications", "-F", "per_page=1", "--method", "GET")
		r := readyResult("GitHub notifications", err, "gh auth refresh -s notifications")
		if err != nil && !strings.Contains(err.Error(), "403") && !strings.Contains(err.Error(), "404") {
			r.Fix = "" // not a scope problem: the reason says what it is
		}
		out = append(out, r)
	}
	return out
}

// CheckGitLab says whether glab can reach each GitLab host sous would use.
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

// readyResult turns a readiness error into a result, naming the fix only
// when the problem is one the fix solves (no login).
func readyResult(name string, err error, fix string) Result {
	if err == nil {
		return Result{Name: name, OK: true, Detail: "works"}
	}
	r := Result{Name: name, Detail: err.Error()}
	if errors.Is(err, ErrNoLogin) || errors.Is(err, ErrNotInstalled) || strings.Contains(err.Error(), "token") {
		r.Fix = fix
	}
	return r
}
