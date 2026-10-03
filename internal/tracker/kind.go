package tracker

import "github.com/bilal-/sous/internal/config"

// Kind is one tracker sous has built in support for: what refs, links,
// ssh remotes and sous doctor need to know about it. The backend that
// files into it and the signal that finds work in it are named after it.
type Kind struct {
	Name string // "github": the ref prefix, and the backend's and signal's name
	Tool string // the command line tool sous runs: "gh"
	// Host is its one public host, which refs leave out; "" when it has
	// many (GitLab), and refs carry it.
	Host string
	// PublicHosts are hosts an ssh alias may stand for (Host github.com-work,
	// HostName github.com).
	PublicHosts []string
	// URL is an item's web page.
	URL func(r Ref) string
	// Ready says why the tool cannot work for identity (a GitHub account,
	// a GitLab host) right now: missing, logged out, no token.
	Ready func(identity string) error
	// Check is what sous doctor asks it, with the settings it reads.
	Check func(cfg *config.Config) []Result
}

// Kinds are the built in trackers.
var Kinds = []Kind{
	{
		Name: "github", Tool: "gh", Host: GitHubHost, PublicHosts: []string{GitHubHost}, Ready: GHReady,
		URL: func(r Ref) string {
			return "https://github.com/" + r.Repo + "/issues/" + r.Number // GitHub redirects PR numbers
		},
		Check: func(cfg *config.Config) []Result { return CheckGitHub(cfg.Identities(config.KeyGitHubAccount)) },
	},
	{
		Name: "gitlab", Tool: "glab", PublicHosts: []string{"gitlab.com"}, Ready: GLabReady,
		URL: func(r Ref) string {
			kind := "issues"
			if r.MR {
				kind = "merge_requests"
			}
			return "https://" + r.Host + "/" + r.Repo + "/-/" + kind + "/" + r.Number
		},
		Check: func(cfg *config.Config) []Result { return CheckGitLab(cfg.GitLabHosts) },
	},
}

// KindOf is the built in tracker called name.
func KindOf(name string) (Kind, bool) {
	for _, k := range Kinds {
		if k.Name == name {
			return k, true
		}
	}
	return Kind{}, false
}
