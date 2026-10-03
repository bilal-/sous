// Package signal defines the plugin contract (v0) and the built-in plugins.
package signal

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/plugin"
)

type Kind string

const (
	Me         Kind = "me"
	Them       Kind = "them"
	Unfinished Kind = "unfinished"
	Info       Kind = "info"
)

// Signal is one finding. This is the wire format; keep it stable.
type Signal struct {
	V        int       `json:"v"`
	ID       string    `json:"id"`
	Project  string    `json:"project"`
	Kind     Kind      `json:"kind"`
	Text     string    `json:"text"`
	Observed time.Time `json:"observed"`
	Ref      *string   `json:"ref"`
	// State optionally fingerprints what the text summarizes, so a snooze
	// ends when the thing changes even if its summary line does not.
	State string `json:"state,omitempty"`
}

// ID is stable across runs and machines: "s:" + 12 hex of sha256(project\tkey).
func ID(project, key string) string {
	sum := sha256.Sum256([]byte(project + "\t" + key))
	return "s:" + hex.EncodeToString(sum[:])[:12]
}

func WriteLine(w io.Writer, s Signal) error {
	s.V = 0
	s.Observed = s.Observed.UTC()
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// ReadLinesLenient parses what it can and counts what it cannot. Used by the
// runner: one stray debug print must not discard a plugin's real findings.
// Lines with an unknown contract version or no id are counted as bad.
func ReadLinesLenient(r io.Reader) ([]Signal, int) {
	var out []Signal
	bad := 0
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s Signal
		if err := json.Unmarshal([]byte(line), &s); err != nil || s.V != 0 || s.ID == "" {
			bad++
			continue
		}
		out = append(out, s)
	}
	if sc.Err() != nil {
		bad++
	}
	return out, bad
}

// Scanner is the shape of every built-in signal plugin: paths in, JSON
// Lines out, warnings to warn, an error only when the plugin as a whole
// failed (partial output is still kept by the runner).
type Scanner func(paths []string, w, warn io.Writer, now time.Time) error

// Registry is the built-in scanners, in order. A scanner is only built
// when it runs: building the gitlab scanner asks glab which hosts it knows,
// and a local-only caller must never pay for that. git is offline: it never
// touches the network, so the resume view (and so the session hook and sous
// go) may run it; the board runs them all, and here reads what the rest
// last found from observed.json.
var Registry = plugin.Registry[*config.Config]{Axis: "signal", Builtins: []plugin.Builtin[*config.Config]{
	{Name: "git", Offline: true, Ops: scanOps(func(*config.Config) Scanner { return ScanGit })},
	{Name: "github", Ops: scanOps(ScanGitHub)},
	{Name: "gitlab", Ops: scanOps(ScanGitLab)},
}}

// scanOps is a built-in scanner's one call, `scan`: paths on stdin, JSON
// Lines out. The scanner says what went wrong on stderr itself.
func scanOps(make func(*config.Config) Scanner) func(*config.Config) map[string]plugin.Op {
	return func(cfg *config.Config) map[string]plugin.Op {
		return map[string]plugin.Op{"scan": func(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
			if len(args) != 0 {
				return plugin.Usage(stderr, "scan  (paths on stdin)")
			}
			// A scanner writes its own reasons to stderr as it goes.
			return plugin.Exit(make(cfg)(ReadPaths(stdin), stdout, stderr, time.Now().UTC()), io.Discard)
		}}
	}
}

// ReadPaths reads one path per line from stdin, skipping blanks.
func ReadPaths(r io.Reader) []string {
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if p := strings.TrimSpace(sc.Text()); p != "" {
			out = append(out, p)
		}
	}
	return out
}
