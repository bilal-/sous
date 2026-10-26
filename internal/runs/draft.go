package runs

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/tracker"
)

// DraftOpener opens draft pull requests on the project's tracker with the
// tool sous already uses for it: gh for GitHub, glab for GitLab. It looks
// for one already open from the branch first, so approving again never
// opens a second.
func DraftOpener(cfg *config.Config) func(repo, branch, title, body string) (string, error) {
	return func(repo, branch, title, body string) (string, error) {
		remote := project.Remote(repo)
		host, path := tracker.ParseRemote(remote)
		switch {
		case host == tracker.GitHubHost:
			account := tracker.GitHubAccount(cfg, project.OrgName(repo))
			out, err := tracker.GHRun(account, "pr", "list", "--repo", path, "--head", branch, "--state", "all", "--json", "url", "--jq", ".[0].url // empty")
			if url := strings.TrimSpace(string(out)); err == nil && url != "" {
				return url, nil
			}
			out, err = tracker.GHRun(account, "pr", "create", "--repo", path, "--draft", "--head", branch, "--title", title, "--body", body)
			if err != nil {
				return "", err
			}
			return lastLine(string(out)), nil
		case host != "" && (strings.Contains(host, "gitlab") || slices.Contains(cfg.GitLabHosts, host)):
			out, err := tracker.GLabRun(host, "mr", "list", "-R", path, "--source-branch", branch, "--all", "-F", "json")
			var mrs []struct {
				WebURL string `json:"web_url"`
			}
			if err == nil && json.Unmarshal(out, &mrs) == nil && len(mrs) > 0 && mrs[0].WebURL != "" {
				return mrs[0].WebURL, nil
			}
			out, err = tracker.GLabRun(host, "mr", "create", "-R", path, "--draft", "--source-branch", branch, "--title", title, "--description", body, "--yes")
			if err != nil {
				return "", err
			}
			return lastLine(string(out)), nil
		}
		return "", fmt.Errorf("no tracker sous knows for %q; the branch is pushed, open a pull request by hand", remote)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
