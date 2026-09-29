package tracker

import (
	"strconv"
	"strings"
)

// Ref is a pointer to one item in a remote tracker, written
// "<tracker>:<host>/<repo>#<n>" ("!<n>" for a GitLab merge request). A
// tracker with one public host (GitHub) leaves the host out. Backends,
// signals and the report all read and write refs through this type, so the
// grammar lives in one place.
type Ref struct {
	Tracker string // "github", "gitlab", or a plugin's name
	Host    string
	Repo    string // owner/repo, or a GitLab group path
	Number  string
	MR      bool // "!": a merge request rather than an issue
}

// defaultHost: trackers whose host goes unwritten.
var defaultHost = map[string]string{"github": GitHubHost}

func (r Ref) String() string {
	sep := "#"
	if r.MR {
		sep = "!"
	}
	where := r.Host + "/" + r.Repo
	if r.Host == defaultHost[r.Tracker] {
		where = r.Repo
	}
	return r.Tracker + ":" + where + sep + r.Number
}

// ParseRef reads a remote-tracker ref. Local refs (md:…) are not Refs.
func ParseRef(s string) (Ref, bool) {
	tr, rest, ok := strings.Cut(s, ":")
	if !ok || tr == "" {
		return Ref{}, false
	}
	r := Ref{Tracker: tr}
	i := strings.LastIndexAny(rest, "#!")
	if i < 0 {
		return Ref{}, false
	}
	r.MR, r.Number = rest[i] == '!', rest[i+1:]
	if _, err := strconv.Atoi(r.Number); err != nil {
		return Ref{}, false
	}
	where := rest[:i]
	if h, ok := defaultHost[tr]; ok {
		r.Host, r.Repo = h, where
		if !strings.Contains(where, "/") {
			return Ref{}, false // owner/repo
		}
	} else {
		r.Host, r.Repo, _ = strings.Cut(where, "/")
	}
	if r.Host == "" || r.Repo == "" || strings.HasPrefix(r.Repo, "/") {
		return Ref{}, false
	}
	return r, true
}

// WebURL is the item's page, for trackers whose URL shape is known; "" for
// the rest.
func (r Ref) WebURL() string {
	switch r.Tracker {
	case "github":
		return "https://github.com/" + r.Repo + "/issues/" + r.Number // GitHub redirects PR numbers
	case "gitlab":
		kind := "issues"
		if r.MR {
			kind = "merge_requests"
		}
		return "https://" + r.Host + "/" + r.Repo + "/-/" + kind + "/" + r.Number
	}
	return ""
}
