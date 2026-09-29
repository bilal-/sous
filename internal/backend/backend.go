// Package backend files threads into a project's own tracker. Backends are
// executables (built-ins re-exec'd via `sous backend <name> <op>`). The
// contract is argv in, stdin/stdout out — the project is always an argument.
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/plugin"
)

const Timeout = 15 * time.Second

// ErrUnsupported: an optional op (url) this backend does not implement.
var ErrUnsupported = errors.New("unsupported")

// Implementation is what every built-in backend provides. Third-party
// backends implement the same five ops as an executable; Ops turns an
// Implementation into that executable's behaviour so built-ins take the
// identical door.
type Implementation interface {
	Detect(project string, warn io.Writer) bool
	File(req Request) (string, error)
	Status(project, ref string) (string, error)
	Close(project, ref string) error
	URL(project, ref string) (string, error) // ErrUnsupported if none
}

// Op is one contract operation as seen from argv/stdin/stdout.
type Op func(args []string, stdin io.Reader, stdout, stderr io.Writer) int

// Ops adapts an Implementation to the contract's op table. Usage errors are
// exit 2, failures exit 1 with the reason on stderr, unsupported url exit 2.
func Ops(b Implementation) map[string]Op {
	return map[string]Op{
		"detect": func(args []string, _ io.Reader, _, stderr io.Writer) int {
			if len(args) != 1 {
				fmt.Fprintln(stderr, "usage: detect <project>")
				return 2
			}
			if b.Detect(args[0], stderr) {
				return 0
			}
			return 1
		},
		"file": func(_ []string, stdin io.Reader, stdout, stderr io.Writer) int {
			var req Request
			if err := json.NewDecoder(stdin).Decode(&req); err != nil || req.Project == "" || req.ID <= 0 {
				fmt.Fprintln(stderr, "file: need JSON with id, project, text, kind on stdin")
				return 2
			}
			if req.V != 0 {
				fmt.Fprintf(stderr, "file: contract v%d is newer than this backend speaks (v0)\n", req.V)
				return 2
			}
			ref, err := b.File(req)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			fmt.Fprintln(stdout, ref)
			return 0
		},
		"status": func(args []string, _ io.Reader, stdout, stderr io.Writer) int {
			if len(args) != 2 {
				fmt.Fprintln(stderr, "usage: status <project> <ref>")
				return 2
			}
			st, err := b.Status(args[0], args[1])
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			fmt.Fprintln(stdout, st)
			return 0
		},
		"close": func(args []string, _ io.Reader, _, stderr io.Writer) int {
			if len(args) != 2 {
				fmt.Fprintln(stderr, "usage: close <project> <ref>")
				return 2
			}
			if err := b.Close(args[0], args[1]); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return 0
		},
		"url": func(args []string, _ io.Reader, stdout, stderr io.Writer) int {
			if len(args) != 2 {
				fmt.Fprintln(stderr, "usage: url <project> <ref>")
				return 2
			}
			u, err := b.URL(args[0], args[1])
			if errors.Is(err, ErrUnsupported) {
				return 2
			}
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			fmt.Fprintln(stdout, u)
			return 0
		},
	}
}

// BuiltinOrder is the detection order for built-ins: a project's own
// follow-ups file before any remote host.
var BuiltinOrder = []string{"markdown", "github", "gitlab"}

// Builtins constructs every built-in for one invocation.
func Builtins(home string, cfg *config.Config) map[string]Implementation {
	return map[string]Implementation{
		"markdown": Markdown{},
		"github":   GitHub(home, cfg),
		"gitlab":   GitLab(home, cfg),
	}
}

// BuiltinNames: the built-ins that exist, in detection order.
func BuiltinNames(home string, cfg *config.Config) []string {
	have := Builtins(home, cfg)
	var names []string
	for _, n := range BuiltinOrder {
		if _, ok := have[n]; ok {
			names = append(names, n)
		}
	}
	return names
}

var timeout = Timeout // overridable in tests

type Backend struct {
	Name string
	Argv []string
}

// Offline: the backend's items live in the project itself (FOLLOWUPS.md),
// so asking about them never leaves the machine.
func (b Backend) Offline() bool { return offline[b.Name] }

var offline = map[string]bool{"markdown": true}

// Local is the sentinel for "no upstream": the note stays in threads.json.
var Local = Backend{Name: "local"}

var ErrUnknownBackend = errors.New("backend not available")

// errNotApplicable: exit 1 with nothing on stderr — a silent "no".
var errNotApplicable = errors.New("not applicable")

func Backends(exe string, builtins, thirdParty []string) []Backend {
	var out []Backend
	for _, p := range plugin.Discover(exe, "backend", builtins, thirdParty) {
		out = append(out, Backend(p))
	}
	return out
}

func Find(bs []Backend, name string) (Backend, bool) {
	for _, b := range bs {
		if b.Name == name {
			return b, true
		}
	}
	return Backend{}, false
}

// ByRef routes an existing ref to the backend that owns it. Refs are
// self-describing: "md:" is markdown, "<name>:" is that backend.
func ByRef(bs []Backend, ref string) (Backend, error) {
	prefix, _, ok := strings.Cut(ref, ":")
	if !ok {
		return Backend{}, fmt.Errorf("%w: malformed ref %q", ErrUnknownBackend, ref)
	}
	if prefix == "md" {
		prefix = "markdown"
	}
	if b, ok := Find(bs, prefix); ok {
		return b, nil
	}
	return Backend{}, fmt.Errorf("%w: no backend for ref %q", ErrUnknownBackend, ref)
}

type result struct {
	out  string
	code int
	err  error
}

func run(ctx context.Context, b Backend, stdin []byte, op string, args ...string) result {
	if b.Name == "local" {
		return result{err: errors.New("local threads have no upstream backend")}
	}
	argv := append(append([]string{}, b.Argv...), op)
	argv = append(argv, args...)
	res := plugin.Exec(ctx, argv, stdin, timeout)
	if res.TimedOut {
		return result{err: fmt.Errorf("%s %s: timed out after %s", b.Name, op, timeout)}
	}
	if res.Err != nil {
		return result{err: fmt.Errorf("%s %s: %w", b.Name, op, res.Err)}
	}
	msg := strings.TrimSpace(res.Stderr)
	code := res.Code
	if code != 0 && code != 1 && code != 2 {
		if msg == "" {
			msg = fmt.Sprintf("exit %d", code)
		}
		return result{code: code, err: errors.New(msg)}
	}
	if code == 1 {
		if msg == "" {
			return result{code: 1, err: errNotApplicable}
		}
		return result{code: 1, err: errors.New(msg)}
	}
	return result{out: strings.TrimSpace(res.Stdout), code: code}
}

// Detect picks the backend for filing into project. An explicit override
// must name an available backend (a typo must not silently mean "local").
// Otherwise the first backend whose detect exits 0 wins; a probe that
// crashes is warned and skipped.
func Detect(ctx context.Context, bs []Backend, project, override string, warn io.Writer) (Backend, error) {
	if override != "" {
		if b, ok := Find(bs, override); ok {
			return b, nil
		}
		return Backend{}, fmt.Errorf("%w: %q is declared but no sous-backend-%s is listed in config plugins", ErrUnknownBackend, override, override)
	}
	for _, b := range bs {
		r := run(ctx, b, nil, "detect", project)
		switch {
		case r.err == nil && r.code == 0:
			return b, nil
		case r.code == 1:
			// Not applicable — but a stated reason (auth, missing tool) must
			// reach the user, or "no tracker" is a mystery.
			if warn != nil && r.err != nil && !errors.Is(r.err, errNotApplicable) {
				fmt.Fprintf(warn, "sous: backend %s: %v\n", b.Name, r.err)
			}
			continue
		default:
			if warn != nil {
				fmt.Fprintf(warn, "sous: backend %s detect failed: %v\n", b.Name, r.err)
			}
		}
	}
	return Local, nil
}

// Request is the file op's stdin. V is the contract version (0 until the
// contract is declared public); a backend refuses one it does not speak.
type Request struct {
	V   int    `json:"v"`
	ID  int    `json:"id"`
	UID string `json:"uid,omitempty"` // the note's stable id: mark filed items with it
	// Legacy: the note predates uids, so a retry may also look for the
	// marker an older sous wrote (by note number). Never set for new notes.
	Legacy  bool   `json:"legacy,omitempty"`
	Project string `json:"project"`
	Text    string `json:"text"`
	Kind    string `json:"kind"`
}

func File(ctx context.Context, b Backend, req Request) (string, error) {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	enc.Encode(req)
	r := run(ctx, b, body.Bytes(), "file")
	if r.err != nil {
		return "", r.err
	}
	if r.code == 2 {
		return "", fmt.Errorf("%s file: rejected the request", b.Name)
	}
	if r.out == "" {
		return "", fmt.Errorf("%s file: printed no ref", b.Name)
	}
	return r.out, nil
}

func Status(ctx context.Context, b Backend, project, ref string) (string, error) {
	r := run(ctx, b, nil, "status", project, ref)
	if r.err != nil {
		return "unknown", r.err
	}
	switch r.out {
	case "open", "closed":
		return r.out, nil
	}
	return "unknown", nil
}

func Close(ctx context.Context, b Backend, project, ref string) error {
	r := run(ctx, b, nil, "close", project, ref)
	if r.err != nil {
		return r.err
	}
	if r.code == 2 {
		return fmt.Errorf("%s close: rejected %s", b.Name, ref)
	}
	return nil
}

// URL is optional: ok=false when the backend exits 2.
func URL(ctx context.Context, b Backend, project, ref string) (string, bool, error) {
	r := run(ctx, b, nil, "url", project, ref)
	if r.err != nil {
		return "", false, r.err
	}
	if r.code == 2 {
		return "", false, nil
	}
	return r.out, true, nil
}
