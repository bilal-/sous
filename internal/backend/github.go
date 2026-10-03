package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/tracker"
)

// githubCLI is the gh table for Remote. It knows gh's argv and gh's error
// text; everything about filing lives in Remote.
type githubCLI struct{ cfg *config.Config }

// GitHub is the built-in backend for github.com remotes via gh.
func GitHub(home string, cfg *config.Config) Remote {
	return Remote{CLI: githubCLI{cfg: cfg}, Home: home}
}

func (githubCLI) Name() string { return tracker.GitHub }

func (g githubCLI) Locate(projectPath string) (Target, error) {
	host, path := tracker.ParseRemote(project.Remote(projectPath))
	if host != tracker.GitHubHost {
		return Target{}, plugin.ErrNo
	}
	return Target{Host: host, Repo: path, Identity: tracker.GitHubAccount(g.cfg, project.OrgName(projectPath))}, nil
}

// TargetFromRef: the repo from a ref. Identity is filled in by Remote from
// the project; when the project cannot be located, the ref's owner is the
// best guess for the org (local org dirs usually match the GitHub owner).
func (g githubCLI) TargetFromRef(ref tracker.Ref) Target {
	owner, _, _ := strings.Cut(ref.Repo, "/")
	return Target{Host: ref.Host, Repo: ref.Repo, Identity: g.cfg.Project(strings.ToLower(owner) + "/*").GitHubAccount}
}

// run executes gh for the target's account: stdout on success, stderr as
// the error.
func (g githubCLI) run(t Target, args ...string) (string, error) {
	out, err := tracker.GHRun(t.Identity, args...)
	return strings.TrimSpace(string(out)), err
}

func (g githubCLI) ListMine(t Target) ([]Issue, error) {
	// The repository connection, not the search index: search lags after a
	// create, which is exactly the window a crash retry hits.
	out, err := g.run(t, "issue", "list", "-R", t.Repo, "--state", "all", "--author", "@me", "--limit", strconv.Itoa(recoveryLimit), "--json", "number,body")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number int
		Body   string
	}
	if json.Unmarshal([]byte(out), &raw) != nil {
		return nil, errors.New("unexpected list output")
	}
	var out2 []Issue
	for _, r := range raw {
		out2 = append(out2, Issue{Number: fmt.Sprint(r.Number), Body: r.Body})
	}
	return out2, nil
}

func (g githubCLI) Create(t Target, title, body string) (Issue, error) {
	url, err := g.run(t, "issue", "create", "-R", t.Repo, "-t", title, "-b", body)
	if err != nil {
		return Issue{}, err
	}
	n := url[strings.LastIndex(url, "/")+1:] // gh prints the issue URL
	if n == "" || strings.Trim(n, "0123456789") != "" {
		return Issue{}, fmt.Errorf("unexpected create output %q", url)
	}
	return Issue{Number: n, URL: url}, nil
}

func (g githubCLI) View(t Target, n string) (Issue, error) {
	out, err := g.run(t, "issue", "view", n, "-R", t.Repo, "--json", "state,url")
	if err != nil {
		// gh distinguishes a missing issue "(repository.issue)" from a repo
		// this account cannot see "(repository)".
		msg := err.Error()
		switch {
		case strings.Contains(msg, "(repository.issue)"):
			return Issue{}, ErrMissing
		case strings.Contains(msg, "(repository)"), strings.Contains(msg, "Could not resolve to a Repository"):
			return Issue{}, fmt.Errorf("%w: %s", ErrInvisible, msg)
		}
		return Issue{}, err
	}
	var v struct{ State, URL string }
	if json.Unmarshal([]byte(out), &v) != nil {
		return Issue{}, errors.New("unexpected view output")
	}
	is := Issue{Number: n, URL: v.URL}
	switch strings.ToUpper(v.State) {
	case "OPEN":
		is.State = "open"
	case "CLOSED":
		is.State = "closed"
	}
	return is, nil
}

func (g githubCLI) Close(t Target, n string) error {
	_, err := g.run(t, "issue", "close", n, "-R", t.Repo)
	return err
}
