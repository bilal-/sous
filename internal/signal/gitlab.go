package signal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/tracker"
)

type glMR struct {
	References struct {
		Full string `json:"full"` // "group/project!iid"
	} `json:"references"`
	IID       int       `json:"iid"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

// glMRs runs one merge_requests query on a host and returns hits keyed by
// host so RemoteScanner can map them to local repos.
func glMRs(host, query string) ([]Hit, error) {
	out, err := tracker.GLabRun(host, "api", "merge_requests?state=opened&per_page=100&"+query)
	if err != nil {
		return nil, err
	}
	var mrs []glMR
	if err := json.Unmarshal(out, &mrs); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(mrs))
	for _, m := range mrs {
		repo, _, _ := strings.Cut(m.References.Full, "!")
		hits = append(hits, Hit{Host: host, Repo: repo, Number: fmt.Sprint(m.IID), Title: m.Title, Updated: m.UpdatedAt})
	}
	return hits, nil
}

// glUsername: the login on a host, for reviewer_username= filters.
func glUsername(host string) (string, error) {
	out, err := tracker.GLabRun(host, "api", "user")
	if err != nil {
		return "", err
	}
	var u struct{ Username string }
	if json.Unmarshal(out, &u) != nil || u.Username == "" {
		return "", errors.New("could not read username")
	}
	return u.Username, nil
}

// ScanGitLab: merge requests waiting on me (reviewer) and mine waiting on
// others (authored, open), on every host glab is logged into. The identity
// loop is over hosts.
func ScanGitLab(cfg *config.Config) Scanner {
	hosts, hostsErr := tracker.GitLabHosts(cfg)
	return RemoteScanner{
		Name:       "gitlab",
		Identities: hosts,
		Configured: len(cfg.GitLabHosts) > 0,
		Available: func() error {
			if _, err := exec.LookPath("glab"); err != nil {
				return errors.New("glab not installed")
			}
			if hostsErr != nil {
				return hostsErr
			}
			if len(hosts) == 0 {
				return errors.New("not logged in to any GitLab host (run glab auth login)")
			}
			return nil
		},
		Queries: []Query{
			{Key: "review", Label: "review requested", Item: "MR !", Kind: Me, Fetch: func(host string) ([]Hit, error) {
				u, err := glUsername(host)
				if err != nil {
					return nil, err
				}
				return glMRs(host, "scope=all&reviewer_username="+u)
			}},
			{Key: "authored", Label: "awaiting review", Item: "MR !", Kind: Them, Fetch: func(host string) ([]Hit, error) {
				return glMRs(host, "scope=created_by_me")
			}},
		},
		MR: true,
	}.Scan
}
