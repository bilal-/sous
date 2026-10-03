package signal

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
	args := append([]string{"search", "prs", "--state=open", "--limit", strconv.Itoa(searchLimit), "--json", "repository,number,title,updatedAt"}, extra...)
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
	if len(prs) >= searchLimit {
		return hits, incomplete(searchLimit)
	}
	return hits, nil
}

// ghFailingChecks: your open pull requests whose latest checks failed. One
// GraphQL search; pending, passing or absent checks are not failing.
func ghFailingChecks(account string) ([]Hit, error) {
	out, runErr := tracker.GHRun(account, "api", "--hostname", tracker.GitHubHost, "graphql", "-f", "query="+checksQuery)
	var resp struct {
		Data *struct {
			Search struct {
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
				Nodes []struct {
					Number     int       `json:"number"`
					Title      string    `json:"title"`
					UpdatedAt  time.Time `json:"updatedAt"`
					Repository struct {
						NameWithOwner string `json:"nameWithOwner"`
					} `json:"repository"`
					Commits struct {
						Nodes []struct {
							Commit struct {
								OID    string `json:"oid"`
								Rollup *struct {
									State string `json:"state"`
								} `json:"statusCheckRollup"`
							} `json:"commit"`
						} `json:"nodes"`
					} `json:"commits"`
				} `json:"nodes"`
			} `json:"search"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || resp.Data == nil {
		if runErr != nil {
			return nil, runErr
		}
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("checks: %s", resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("checks: no data (%v)", err)
	}
	var hits []Hit
	for _, n := range resp.Data.Search.Nodes {
		if len(n.Commits.Nodes) == 0 {
			continue
		}
		c := n.Commits.Nodes[0].Commit
		if c.Rollup == nil || (c.Rollup.State != "FAILURE" && c.Rollup.State != "ERROR") {
			continue
		}
		hits = append(hits, Hit{Repo: n.Repository.NameWithOwner, Number: strconv.Itoa(n.Number), Title: n.Title, Updated: n.UpdatedAt, State: c.OID})
	}
	switch {
	case len(resp.Errors) > 0: // partial data (an org behind single sign-on, say)
		return hits, fmt.Errorf("checks: %s; %w", resp.Errors[0].Message, ErrIncomplete)
	case resp.Data.Search.PageInfo.HasNextPage: // more pull requests than one page
		return hits, incomplete(checksPage)
	}
	return hits, nil
}

// ghNotifications: unread notifications addressed to you, one per thread:
// mentions, team mentions and assignments on issues and pull requests.
// Requests are left to the review search. The state is the thread's
// last update, so new activity ends a snooze.
func ghNotifications(account string) ([]Hit, error) {
	out, err := tracker.GHNotifications(account)
	if err != nil {
		return nil, err
	}
	var pages [][]struct {
		Reason    string    `json:"reason"`
		UpdatedAt time.Time `json:"updated_at"`
		Repo      struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Subject struct {
			Type  string  `json:"type"`
			URL   *string `json:"url"`
			Title string  `json:"title"`
		} `json:"subject"`
	}
	if err := json.Unmarshal(out, &pages); err != nil {
		return nil, fmt.Errorf("notifications: %w", err)
	}
	var hits []Hit
	for _, page := range pages {
		for _, n := range page {
			label, ok := noticeLabels[n.Reason]
			item, isItem := noticeItems[n.Subject.Type]
			if !ok || !isItem || n.Subject.URL == nil {
				continue
			}
			num := (*n.Subject.URL)[strings.LastIndex(*n.Subject.URL, "/")+1:]
			hits = append(hits, Hit{Repo: n.Repo.FullName, Number: num, Title: n.Subject.Title, Updated: n.UpdatedAt,
				State: n.UpdatedAt.UTC().Format(time.RFC3339), Label: label, Item: item})
		}
	}
	return hits, nil
}

// noticeLabels: the notification reasons that are addressed to you.
var noticeLabels = map[string]string{"mention": "mentioned", "team_mention": "team mentioned", "assign": "assigned"}

// noticeItems: the notification subjects sous shows, and how it names them.
var noticeItems = map[string]string{"Issue": "issue #", "PullRequest": "PR #"}

// checksPage is how many of your open pull requests one checks search reads.
const checksPage = 100

var checksQuery = `query{search(query:"is:pr is:open author:@me archived:false",type:ISSUE,first:` + strconv.Itoa(checksPage) + `){pageInfo{hasNextPage} nodes{... on PullRequest{number title updatedAt repository{nameWithOwner} commits(last:1){nodes{commit{oid statusCheckRollup{state}}}}}}}}`

// ScanGitHub: obligations with a person on the other end, searched once per
// configured account (the active one plus every github_account in config).
func ScanGitHub(cfg *config.Config) Scanner {
	accounts := cfg.Identities(config.KeyGitHubAccount)
	return RemoteScanner{
		Name:       tracker.GitHub,
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
			{Key: "checks", Label: "checks failing", Item: "PR #", Fetch: ghFailingChecks},
			{Key: "notice", Fetch: ghNotifications},
		},
	}.Scan
}
