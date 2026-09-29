package signal

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/tracker"
)

// A remote tracker's obligations (review requests, changes requested) are
// found the same way whatever the tool: map local projects to the tracker's
// repos, run each query once per identity, emit one signal per hit that
// maps back to a local project, dedupe across identities. RemoteScanner
// does that once; each tracker supplies the queries.

// Hit is one item a query returned.
type Hit struct {
	Host    string // "" = the scanner's Host; set when a tracker spans hosts (GitLab)
	Repo    string // as the tracker names it, e.g. "acme/chime"
	Number  string
	Title   string
	Updated time.Time
}

// Query is one search a tracker runs, per identity.
type Query struct {
	Key   string // signal key part: "review"
	Label string // row text prefix: "review requested"
	Item  string // "PR #" or "MR !" — noun and sigil as the tracker writes them
	Kind  Kind   // Me (waiting on me) or Them (mine, waiting on others); "" = Me
	Fetch func(identity string) ([]Hit, error)
}

// ErrNotSetUp: the tool this plugin needs is missing or logged out and
// nothing in config asks for it. A plugin exits ExitNotSetUp; the board
// treats that as quiet unless the plugin found things before.
var ErrNotSetUp = errors.New("not set up")

// ExitNotSetUp is the exit code for ErrNotSetUp in the signal contract.
const ExitNotSetUp = 3

type RemoteScanner struct {
	Name       string       // "github" / "gitlab": ref prefix and messages
	Host       string       // remote host to match, "" = any
	Identities []string     // "" = the tool's own default, plus configured ones
	Available  func() error // tool installed and usable; the error says why not
	// Configured: the person asked for this tracker in config.toml. Then an
	// unavailable tool is a failure; otherwise it is just not set up here.
	Configured bool
	Queries    []Query
	MR         bool // hits are merge requests: refs use "!" (see tracker.Ref)
}

// Scan is the Scanner for this tracker.
func (rs RemoteScanner) Scan(paths []string, w, warn io.Writer, now time.Time) error {
	if err := rs.Available(); err != nil {
		fmt.Fprintf(warn, "%s: %v\n", rs.Name, err)
		if !rs.Configured {
			return fmt.Errorf("%w: %v", ErrNotSetUp, err)
		}
		return err
	}
	byRepo := map[string]string{} // lower "host/repo" → local path
	for _, p := range paths {
		host, path := tracker.ParseRemote(project.Remote(p))
		if host == "" || (rs.Host != "" && host != rs.Host) {
			continue
		}
		byRepo[strings.ToLower(host+"/"+path)] = p
	}
	if len(byRepo) == 0 {
		return nil
	}
	// A failed query is reported but must not discard the other queries'
	// findings; the runner marks the plugin failed while stdout is kept.
	// The same item seen from two identities gets the same id; Observe dedupes.
	var firstErr error
	emitted := map[string]bool{}
	for _, identity := range rs.Identities {
		for _, q := range rs.Queries {
			hits, err := q.Fetch(identity)
			if err != nil {
				fmt.Fprintf(warn, "%s %s%s: %v\n", rs.Name, q.Key, as(identity), err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			for _, h := range hits {
				host := h.Host
				if host == "" {
					host = rs.Host
				}
				p, ok := byRepo[strings.ToLower(host+"/"+h.Repo)]
				if !ok {
					continue
				}
				id := ID(p, fmt.Sprintf("%s:%s:%s", rs.Name, q.Key, h.Number))
				if emitted[id] {
					continue
				}
				emitted[id] = true
				ref := tracker.Ref{Tracker: rs.Name, Host: host, Repo: h.Repo, Number: h.Number, MR: rs.MR}.String()
				kind := q.Kind
				if kind == "" {
					kind = Me
				}
				WriteLine(w, Signal{ID: id, Project: p, Kind: kind,
					Text: fmt.Sprintf("%s · %s%s %s", q.Label, q.Item, h.Number, h.Title), Observed: h.Updated, Ref: &ref})
			}
		}
	}
	return firstErr
}

func as(identity string) string {
	if identity == "" {
		return ""
	}
	return " as " + identity
}
