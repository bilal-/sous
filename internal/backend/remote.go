package backend

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/bilal-/sous/internal/tracker"
)

// A remote tracker backed by a CLI (gh, glab, jira…) has one shape: find
// the repo this project maps to, check the tool and identity, create an
// issue with a recovery marker in its body, read its state, close it.
// Remote implements that shape once; each tracker supplies an IssueCLI —
// the argv tables and error classification that differ between tools.

// Target names one repository on one host as seen by one identity.
type Target struct {
	Host     string // "github.com", "git.example.org"
	Repo     string // "owner/repo" or "group/subgroup/project"
	Identity string // gh account or "" (active); glab host
}

// Issue is what a tracker reports about one item.
type Issue struct {
	Number string
	Body   string
	State  string // open | closed
	URL    string
}

// ErrMissing: the item does not exist (a deleted issue). ErrInvisible: the
// repo cannot be seen by this identity — the wrong account, never treated
// as "missing".
var (
	ErrMissing   = errors.New("not found")
	ErrInvisible = errors.New("repository not visible")
)

// IssueCLI is the per-tracker table Remote drives.
type IssueCLI interface {
	Name() string                                         // ref prefix and error prefix: "github"
	Locate(project string, warn io.Writer) (Target, bool) // from the project's remote; false = not this tracker's; a near-miss (right shape, unknown host) is explained on warn
	Available(t Target, warn io.Writer) bool              // tool installed and authenticated for t.Identity; reason on warn
	TargetFromRef(ref tracker.Ref) Target                 // where a ref points; Identity is a best guess, Remote prefers the project's
	ListMine(t Target) ([]Issue, error)                   // open+closed issues I created, with bodies (recovery)
	Create(t Target, title, body string) (Issue, error)   // returns at least Number
	View(t Target, number string) (Issue, error)          // State and URL; ErrMissing / ErrInvisible classified
	Close(t Target, number string) error
}

// Remote is the Implementation over any IssueCLI.
type Remote struct {
	CLI  IssueCLI
	Home string // for the install id in markers
}

func (r Remote) Detect(project string, warn io.Writer) bool {
	t, ok := r.CLI.Locate(project, warn)
	if !ok {
		return false
	}
	return r.CLI.Available(t, warn)
}

// title: an issue title from note text, capped the way trackers render it.
func title(text string) string {
	if utf8.RuneCountInString(text) <= 120 {
		return text
	}
	return string([]rune(text)[:117]) + "…"
}

// markers are what a retry looks for in issues already filed: the note's
// uid, and for a note older than uids the <install>:<id> marker a sous
// before uids wrote. The first is the one new issues carry.
func markers(home string, req Request) ([]string, error) {
	if req.UID == "" {
		return nil, errors.New("the request has no uid")
	}
	if !req.Legacy {
		return []string{markerComment(req.UID)}, nil
	}
	inst, err := tracker.InstallID(home)
	if err != nil {
		return []string{markerComment(req.UID)}, nil // no old marker to look for
	}
	return []string{markerComment(req.UID), markerComment(fmt.Sprintf("%s:%d", inst, req.ID))}, nil
}

// recoveryLimit is how many of your issues a retry looks through for an
// earlier try; a list that long may be cut short, so filing refuses.
const recoveryLimit = 1000

func (r Remote) File(req Request) (string, error) {
	name := r.CLI.Name()
	t, ok := r.CLI.Locate(req.Project, io.Discard)
	if !ok {
		return "", fmt.Errorf("not a %s remote", name)
	}
	ms, err := markers(r.Home, req)
	if err != nil {
		return "", err
	}
	// Crash recovery: an issue with our marker may already exist. If the
	// lookup itself fails, fail closed — a retry later beats a duplicate on
	// a shared tracker.
	existing, err := r.CLI.ListMine(t)
	if err == nil && len(existing) >= recoveryLimit {
		err = fmt.Errorf("you have %d or more issues there; the list may be cut short", recoveryLimit)
	}
	if err != nil {
		return "", fmt.Errorf("could not check for an existing issue: %w", err)
	}
	for _, e := range existing {
		for _, m := range ms {
			if strings.Contains(e.Body, m) {
				return r.ref(t, e.Number), nil
			}
		}
	}
	body := fmt.Sprintf("%s\n\n%s\n_Filed from sous._\n", req.Text, ms[0])
	created, err := r.CLI.Create(t, title(req.Text), body)
	if err != nil {
		return "", err
	}
	return r.ref(t, created.Number), nil
}

func (r Remote) ref(t Target, number string) string {
	return tracker.Ref{Tracker: r.CLI.Name(), Host: t.Host, Repo: t.Repo, Number: number}.String()
}

// parseRef resolves a ref to a target and item number. The identity is the
// project's (the same one Detect and File used — an exact config key or an
// org glob); the ref's own repo string only supplies the identity when the
// project can no longer be located.
func (r Remote) parseRef(project, ref string) (Target, string, bool) {
	parsed, ok := tracker.ParseRef(ref)
	if !ok || parsed.Tracker != r.CLI.Name() || parsed.MR {
		return Target{}, "", false
	}
	t := r.CLI.TargetFromRef(parsed)
	n := parsed.Number
	if pt, ok := r.CLI.Locate(project, io.Discard); ok {
		t.Identity = pt.Identity
	}
	return t, n, true
}

func (r Remote) Status(project, ref string) (string, error) {
	t, n, ok := r.parseRef(project, ref)
	if !ok {
		return "unknown", nil
	}
	is, err := r.CLI.View(t, n)
	switch {
	case errors.Is(err, ErrMissing):
		return "unknown", nil
	case errors.Is(err, ErrInvisible):
		who := t.Identity
		if who == "" {
			who = "the active account"
		}
		return "unknown", fmt.Errorf("%s is not visible to %s (check the project's account/host in config.toml): %v", t.Repo, who, err)
	case err != nil:
		return "unknown", err
	}
	switch is.State {
	case "open", "closed":
		return is.State, nil
	}
	return "unknown", nil
}

func (r Remote) Close(project, ref string) error {
	t, n, ok := r.parseRef(project, ref)
	if !ok {
		return fmt.Errorf("not a %s ref", r.CLI.Name())
	}
	return r.CLI.Close(t, n)
}

func (r Remote) URL(project, ref string) (string, error) {
	t, n, ok := r.parseRef(project, ref)
	if !ok {
		return "", fmt.Errorf("not a %s ref", r.CLI.Name())
	}
	is, err := r.CLI.View(t, n)
	if err != nil {
		return "", err
	}
	if is.URL == "" {
		return "", ErrUnsupported
	}
	return is.URL, nil
}
