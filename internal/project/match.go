package project

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrNoMatch = errors.New("no project matches")

type AmbiguousError struct {
	Term string
	Hits []Project
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s matches %d projects:", e.Term, len(e.Hits))
	for _, h := range e.Hits {
		fmt.Fprintf(&b, "\n  %s/%s", h.Org, h.Name)
	}
	return b.String()
}

// rung: 1 exact basename, 2 exact org/basename, 3 basename prefix,
// 4 org/basename prefix, 5 substring, 6 subsequence, 0 no match.
func rung(p Project, term string) int {
	t := strings.ToLower(term)
	n := strings.ToLower(p.Name)
	on := strings.ToLower(p.Org + "/" + p.Name)
	switch {
	case n == t:
		return 1
	case on == t:
		return 2
	case strings.HasPrefix(n, t):
		return 3
	case strings.HasPrefix(on, t):
		return 4
	case strings.Contains(on, t):
		return 5
	case isSubseq(t, on):
		return 6
	}
	return 0
}

func isSubseq(needle, hay string) bool {
	i := 0
	for _, c := range hay {
		if i < len(needle) && rune(needle[i]) == c {
			i++
		}
	}
	return i == len(needle)
}

// Candidates: every project on the best rung for term.
func Candidates(ps []Project, term string) []Project {
	best := 0
	var hits []Project
	for _, p := range ps {
		r := rung(p, term)
		if r == 0 {
			continue
		}
		if best == 0 || r < best {
			best, hits = r, nil
		}
		if r == best {
			hits = append(hits, p)
		}
	}
	return hits
}

// Match: exactly one project, or ErrNoMatch / *AmbiguousError. Sous never guesses.
func Match(ps []Project, term string) (Project, error) {
	hits := Candidates(ps, term)
	switch len(hits) {
	case 0:
		return Project{}, fmt.Errorf("%w '%s' (try: sous projects)", ErrNoMatch, term)
	case 1:
		return hits[0], nil
	}
	return Project{}, &AmbiguousError{Term: term, Hits: hits}
}

// ErrNotInProject: no term was given and cwd is not inside a repo.
var ErrNotInProject = errors.New("not inside a project")

// Resolve implements the -p rule: a term goes through the ladder over the
// discovered projects; no term means the repo containing cwd, which need
// not be under a root (it is still a fine place to take a note).
func Resolve(roots, ignore []string, term, cwd string, warn io.Writer) (Project, error) {
	if term != "" {
		if warn == nil {
			warn = io.Discard
		}
		ps, _ := Discover(roots, ignore, warn)
		return Match(ps, term)
	}
	root, ok := ForPath(cwd)
	if !ok {
		return Project{}, ErrNotInProject
	}
	return Describe(root), nil
}
