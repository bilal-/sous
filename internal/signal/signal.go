// Package signal defines the plugin contract (v0) and the built-in plugins.
package signal

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
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

// builtins maps each built-in to its constructor. Construction is deferred
// until a scanner is actually run: building the gitlab scanner asks glab
// which hosts it knows, and a local-only caller must never pay for that.
var builtins = map[string]func(*config.Config) Scanner{
	"git":    func(*config.Config) Scanner { return ScanGit },
	"github": ScanGitHub,
	"gitlab": ScanGitLab,
}

// Builtin constructs one built-in scanner.
func Builtin(name string, cfg *config.Config) (Scanner, bool) {
	newScanner, ok := builtins[name]
	if !ok {
		return nil, false
	}
	return newScanner(cfg), true
}

// LocalBuiltins are the built-ins that never touch the network. The resume
// view (and so the session hook and `sous go`) runs only these; the board
// runs everything and `here` reads the board's observations for the rest.
func LocalBuiltins() []string { return []string{"git"} }

// BuiltinNames, sorted, for the runner's plugin list.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
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
