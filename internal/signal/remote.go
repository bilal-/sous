package signal

import (
	"cmp"
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
	State   string // optional fingerprint (a PR's head commit): a change ends a snooze
}

// Query is one search a tracker runs, per identity.
type Query struct {
	Key   string // signal key part: "review"
	Label string // row text prefix: "review requested"
	Item  string // "PR #" or "MR !" — noun and sigil as the tracker writes them
	Kind  Kind   // Me (waiting on me) or Them (mine, waiting on others); "" = Me
	Fetch func(identity string) ([]Hit, error)
	// Limit is how many results Fetch asks for; reaching it means the answer
	// may be cut short. 0 means searchLimit.
	Limit int
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

// searchLimit is how many results one query asks for; reaching it means
// the answer may be cut short.
const searchLimit = 1000

// Scan is the Scanner for this tracker.
func (rs RemoteScanner) Scan(paths []string, w, warn io.Writer, now time.Time) error {
	if err := rs.Available(); err != nil {
		fmt.Fprintf(warn, "%s: %v\n", rs.Name, err)
		if !rs.Configured && (errors.Is(err, tracker.ErrNotInstalled) || errors.Is(err, tracker.ErrNoLogin)) {
			return fmt.Errorf("%w: %v", ErrNotSetUp, err)
		}
		return err
	}
	byRepo := rs.localRepos(paths)
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
			if limit := cmp.Or(q.Limit, searchLimit); err == nil && len(hits) >= limit {
				// A full page may have more behind it: show what came, and say
				// the scan is incomplete, so earlier findings stay (stale).
				err = fmt.Errorf("more than %d results; some may be missing", limit)
			}
			for _, h := range hits {
				if sig, ok := rs.signal(q, h, byRepo); ok && !emitted[sig.ID] {
					emitted[sig.ID] = true
					WriteLine(w, sig)
				}
			}
			if err != nil {
				fmt.Fprintf(warn, "%s %s%s: %v\n", rs.Name, q.Key, as(identity), err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

// localRepos maps lower "host/repo" to the local project folder, for the
// projects on this tracker.
func (rs RemoteScanner) localRepos(paths []string) map[string]string {
	byRepo := map[string]string{}
	for _, p := range paths {
		host, path := tracker.ParseRemote(project.Remote(p))
		if host != "" && (rs.Host == "" || host == rs.Host) {
			byRepo[strings.ToLower(host+"/"+path)] = p
		}
	}
	return byRepo
}

// signal turns one hit into a signal for its local project; false when the
// repo is not one of ours.
func (rs RemoteScanner) signal(q Query, h Hit, byRepo map[string]string) (Signal, bool) {
	host := h.Host
	if host == "" {
		host = rs.Host
	}
	p, ok := byRepo[strings.ToLower(host+"/"+h.Repo)]
	if !ok {
		return Signal{}, false
	}
	kind := q.Kind
	if kind == "" {
		kind = Me
	}
	ref := tracker.Ref{Tracker: rs.Name, Host: host, Repo: h.Repo, Number: h.Number, MR: rs.MR}.String()
	return Signal{ID: ID(p, fmt.Sprintf("%s:%s:%s", rs.Name, q.Key, h.Number)), Project: p, Kind: kind,
		Text: fmt.Sprintf("%s · %s%s %s", q.Label, q.Item, h.Number, h.Title), Observed: h.Updated, Ref: &ref, State: h.State}, true
}

func as(identity string) string {
	if identity == "" {
		return ""
	}
	return " as " + identity
}
