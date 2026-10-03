package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/tracker"
)

// gitlabCLI is the glab table for Remote. It uses `glab api` throughout —
// deterministic JSON, no editor, and identical on gitlab.com and
// self-hosted instances. The "identity" is the host, since glab holds one
// login per host.
type gitlabCLI struct{ cfg *config.Config }

// GitLab is the built-in backend for any host glab is logged into.
func GitLab(home string, cfg *config.Config) Remote {
	return Remote{CLI: gitlabCLI{cfg: cfg}, Home: home}
}

func (gitlabCLI) Name() string { return tracker.GitLab }

// knownHost: is host one glab is logged into (or declared in config)?
func (g gitlabCLI) knownHost(host string) (bool, error) {
	hosts, err := tracker.GitLabHosts(g.cfg)
	return slices.Contains(hosts, host), err
}

func (g gitlabCLI) Locate(projectPath string) (Target, error) {
	host, path := tracker.ParseRemote(project.Remote(projectPath))
	if host == "" || host == tracker.GitHubHost {
		return Target{}, plugin.ErrNo
	}
	known, err := g.knownHost(host)
	switch {
	case known:
		return Target{Host: host, Repo: path, Identity: host}, nil
	case !strings.Contains(host, "gitlab"):
		// Only a GitLab-looking host earns an explanation; Bitbucket or
		// Gitea is just not ours. Self-hosted GitLab under another name is
		// declared in gitlab_hosts.
		return Target{}, plugin.ErrNo
	case err != nil:
		return Target{}, err
	}
	return Target{}, fmt.Errorf("%s is not a host glab is logged into (glab auth login --hostname %s, or add it to gitlab_hosts)", host, host)
}

// TargetFromRef: on GitLab the host is the identity.
func (gitlabCLI) TargetFromRef(ref tracker.Ref) Target {
	return Target{Host: ref.Host, Repo: ref.Repo, Identity: ref.Host}
}

// api returns stdout on success and the stderr text as both the string and
// the error on failure (View classifies GitLab's 404 messages from it).
func (gitlabCLI) api(t Target, args ...string) (string, error) {
	out, err := tracker.GLabRun(t.Host, append([]string{"api"}, args...)...)
	if err != nil {
		return err.Error(), err
	}
	return strings.TrimSpace(string(out)), nil
}

func (gitlabCLI) enc(t Target) string { return url.PathEscape(t.Repo) }

func (g gitlabCLI) ListMine(t Target) ([]Issue, error) {
	out, err := g.api(t, "--paginate", fmt.Sprintf("projects/%s/issues?scope=created_by_me&state=all&per_page=100", g.enc(t)))
	if err != nil {
		return nil, err
	}
	var raw []struct {
		IID         int
		Description string
	}
	if json.Unmarshal([]byte(out), &raw) != nil {
		return nil, errors.New("unexpected list output")
	}
	var issues []Issue
	for _, r := range raw {
		issues = append(issues, Issue{Number: fmt.Sprint(r.IID), Body: r.Description})
	}
	return issues, nil
}

func (g gitlabCLI) Create(t Target, title, body string) (Issue, error) {
	out, err := g.api(t, "-X", "POST", "projects/"+g.enc(t)+"/issues", "-f", "title="+title, "-f", "description="+body)
	if err != nil {
		return Issue{}, err
	}
	var v struct {
		IID    int
		WebURL string `json:"web_url"`
	}
	if json.Unmarshal([]byte(out), &v) != nil || v.IID == 0 {
		return Issue{}, fmt.Errorf("unexpected create output %q", out)
	}
	return Issue{Number: fmt.Sprint(v.IID), URL: v.WebURL}, nil
}

func (g gitlabCLI) View(t Target, n string) (Issue, error) {
	out, err := g.api(t, fmt.Sprintf("projects/%s/issues/%s", g.enc(t), n))
	if err != nil {
		// GitLab says "404 Project Not Found" for a project this token
		// cannot see, and "404 Not found" for a missing issue.
		switch {
		case strings.Contains(out, "Project Not Found"):
			return Issue{}, fmt.Errorf("%w: %s", ErrInvisible, out)
		case strings.Contains(out, "404"):
			return Issue{}, ErrMissing
		}
		return Issue{}, err
	}
	var v struct {
		State  string
		WebURL string `json:"web_url"`
	}
	if json.Unmarshal([]byte(out), &v) != nil {
		return Issue{}, errors.New("unexpected view output")
	}
	is := Issue{Number: n, URL: v.WebURL}
	switch v.State {
	case "opened":
		is.State = "open"
	case "closed":
		is.State = "closed"
	}
	return is, nil
}

func (g gitlabCLI) Close(t Target, n string) error {
	_, err := g.api(t, "-X", "PUT", fmt.Sprintf("projects/%s/issues/%s?state_event=close", g.enc(t), n))
	return err
}
