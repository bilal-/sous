package project

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/bilal-/sous/internal/testutil"
)

func fake(names ...string) []Project {
	var ps []Project
	for _, n := range names {
		org, name, _ := cut(n)
		ps = append(ps, Project{Path: "/ws/" + n, Org: org, Name: name})
	}
	return ps
}
func cut(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:], true
		}
	}
	return "", s, false
}

func TestLadder(t *testing.T) {
	ps := fake("oss/app-next", "oss/app-mobile", "oss/app-backoffice-next", "studio/billing", "personal/app")
	cases := map[string]string{
		"app": "app", "app-mob": "app-mobile", "studio/bi": "billing",
		"bill": "billing", "blg": "billing", "BILLING": "billing",
	}
	for term, want := range cases {
		p, err := Match(ps, term)
		if err != nil || p.Name != want {
			t.Errorf("%q → %v (%v), want %s", term, p.Name, err, want)
		}
	}
	_, err := Match(ps, "app-")
	var amb *AmbiguousError
	if !errors.As(err, &amb) || len(amb.Hits) != 3 {
		t.Fatalf("app- should be ambiguous with 3 hits: %v", err)
	}
	if _, err := Match(ps, "zzz"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("zzz: %v", err)
	}
	if c := Candidates(ps, "app-"); len(c) != 3 {
		t.Fatalf("candidates: %d", len(c))
	}
}

// An exact org/name must win over other projects it prefixes.
func TestExactOrgNameBeatsPrefix(t *testing.T) {
	ps := fake("org/r1", "org/r10", "org/r1-wt")
	p, err := Match(ps, "org/r1")
	if err != nil || p.Name != "r1" {
		t.Fatalf("%v %v", p, err)
	}
}

func TestResolvePath(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "ws", "acme", "api")
	testutil.Repo(t, repo, true, "")
	for _, term := range []string{repo, "~/ws/acme/api", "./acme/api"} {
		p, err := Resolve(nil, nil, term, filepath.Join(home, "ws"), home, nil)
		if err != nil || p.Name != "api" || p.Org != "acme" {
			t.Fatalf("Resolve(%q): %+v %v", term, p, err)
		}
	}
	if _, err := Resolve(nil, nil, filepath.Join(home, "missing"), home, home, nil); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("missing path: %v", err)
	}
}
