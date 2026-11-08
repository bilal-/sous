package tracker

import (
	"net/url"
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

// defaultHost is the host refs leave out for tracker, or "".
func defaultHost(tracker string) string {
	k, _ := KindOf(tracker)
	return k.Host
}

func (r Ref) String() string {
	sep := "#"
	if r.MR {
		sep = "!"
	}
	where := r.Host + "/" + r.Repo
	if h := defaultHost(r.Tracker); h != "" && r.Host == h {
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
	if h := defaultHost(tr); h != "" {
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
	if k, ok := KindOf(r.Tracker); ok {
		return k.URL(r)
	}
	return ""
}

// RefFromURL reads an item's web page address as a ref: a GitHub issue or
// pull request, or a GitLab issue or merge request on any host. A pull or
// merge request comes back with MR set.
func RefFromURL(s string) (Ref, bool) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return Ref{}, false
	}
	path := strings.Trim(u.Path, "/")
	number := func(n string) bool { _, err := strconv.Atoi(n); return err == nil && !strings.HasPrefix(n, "-") }
	if repo, kind, ok := strings.Cut(path, "/-/"); ok {
		k, n, _ := strings.Cut(kind, "/")
		if (k == "issues" || k == "merge_requests") && number(n) && strings.Contains(repo, "/") {
			return Ref{Tracker: GitLab, Host: u.Host, Repo: repo, Number: n, MR: k == "merge_requests"}, true
		}
		return Ref{}, false
	}
	parts := strings.Split(path, "/")
	if u.Host == GitHubHost && len(parts) == 4 && (parts[2] == "issues" || parts[2] == "pull") && number(parts[3]) {
		return Ref{Tracker: GitHub, Host: GitHubHost, Repo: parts[0] + "/" + parts[1], Number: parts[3], MR: parts[2] == "pull"}, true
	}
	return Ref{}, false
}
