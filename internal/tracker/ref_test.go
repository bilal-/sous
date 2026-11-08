package tracker

import (
	"strings"
	"testing"
)

func TestRefRoundTripAndWebURL(t *testing.T) {
	for _, c := range []struct {
		s   string
		ref Ref
		url string
	}{
		{"github:acme/chime#42", Ref{Tracker: "github", Host: "github.com", Repo: "acme/chime", Number: "42"}, "https://github.com/acme/chime/issues/42"},
		{"gitlab:git.example.org/frontend/app-next#12", Ref{Tracker: "gitlab", Host: "git.example.org", Repo: "frontend/app-next", Number: "12"}, "https://git.example.org/frontend/app-next/-/issues/12"},
		{"gitlab:git.example.org/group/sub/api!3", Ref{Tracker: "gitlab", Host: "git.example.org", Repo: "group/sub/api", Number: "3", MR: true}, "https://git.example.org/group/sub/api/-/merge_requests/3"},
		{"jira:jira.example.org/OPS#7", Ref{Tracker: "jira", Host: "jira.example.org", Repo: "OPS", Number: "7"}, ""},
	} {
		got, ok := ParseRef(c.s)
		if !ok || got != c.ref {
			t.Errorf("ParseRef(%q) = %+v, %v", c.s, got, ok)
		}
		if s := c.ref.String(); s != c.s {
			t.Errorf("String() = %q, want %q", s, c.s)
		}
		if u := c.ref.WebURL(); u != c.url {
			t.Errorf("WebURL(%q) = %q, want %q", c.s, u, c.url)
		}
	}
	for _, bad := range []string{"", "github", "github:acme/chime", "github:#4", "md:FOLLOWUPS.md:3:ab", "gitlab:hostonly#3", "github:acme/chime#x"} {
		if r, ok := ParseRef(bad); ok {
			t.Errorf("ParseRef(%q) accepted: %+v", bad, r)
		}
	}
}

func TestGitHubRefNeedsOwner(t *testing.T) {
	if r, ok := ParseRef("github:repo#1"); ok {
		t.Fatalf("accepted %+v", r)
	}
}

func TestRefFromURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/api/issues/12":                     "github:acme/api#12",
		"https://github.com/acme/api/pull/9":                        "github:acme/api!9",
		"https://gitlab.com/acme/api/-/issues/40":                   "gitlab:gitlab.com/acme/api#40",
		"https://git.example.org/group/sub/api/-/merge_requests/3/": "gitlab:git.example.org/group/sub/api!3",
		"https://github.com/acme/api/issues/12?x=1#issuecomment-5":  "github:acme/api#12",
	} {
		r, ok := RefFromURL(in)
		got := r.Tracker + ":" + r.Host + "/" + r.Repo
		if r.MR {
			got += "!" + r.Number
		} else {
			got += "#" + r.Number
		}
		if r.Tracker == GitHub {
			got = strings.Replace(got, "github.com/", "", 1)
		}
		if !ok || got != want {
			t.Errorf("RefFromURL(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"github:acme/api#1", "https://github.com/acme/api", "https://github.com/acme/api/issues/x", "https://example.org/acme/api/issues/1", "ftp://github.com/acme/api/issues/1", "https://gitlab.com/acme/-/issues/1"} {
		if r, ok := RefFromURL(bad); ok {
			t.Errorf("RefFromURL(%q) accepted: %+v", bad, r)
		}
	}
}
