package signal

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/tracker"
)

type ghPR struct {
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func ghSearch(account string, extra ...string) ([]Hit, error) {
	args := append([]string{"search", "prs", "--state=open", "--limit", "100", "--json", "repository,number,title,updatedAt"}, extra...)
	out, err := tracker.GHRun(account, args...)
	if err != nil {
		return nil, err
	}
	var prs []ghPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(prs))
	for _, pr := range prs {
		hits = append(hits, Hit{Repo: pr.Repository.NameWithOwner, Number: fmt.Sprint(pr.Number), Title: pr.Title, Updated: pr.UpdatedAt})
	}
	return hits, nil
}

// ScanGitHub: obligations with a person on the other end, searched once per
// configured account (the active one plus every github_account in config).
func ScanGitHub(cfg *config.Config) Scanner {
	accounts := cfg.Identities("github_account")
	return RemoteScanner{
		Name:       "github",
		Host:       tracker.GitHubHost,
		Identities: append([]string{""}, accounts...),
		Configured: len(accounts) > 0,
		Available: func() error {
			// Configured accounts are each tried on their own, so a broken
			// default login alone does not hide what they can see.
			if len(accounts) > 0 {
				return tracker.GHInstalled()
			}
			return tracker.GHReady("")
		},
		Queries: []Query{
			{Key: "review", Label: "review requested", Item: "PR #", Fetch: func(a string) ([]Hit, error) { return ghSearch(a, "--review-requested=@me") }},
			{Key: "changes", Label: "changes requested", Item: "PR #", Fetch: func(a string) ([]Hit, error) { return ghSearch(a, "--author=@me", "--review=changes_requested") }},
		},
	}.Scan
}
