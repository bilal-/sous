// Package backend files threads into a project's own tracker. Backends are
// executables (built-ins re-exec'd via `sous backend <name> <op>`). The
// contract is argv in, stdin/stdout out — the project is always an argument.
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/plugin"
)

// ErrUnsupported: an optional call (url) this backend does not have.
var ErrUnsupported = plugin.ErrUnsupported

// Implementation is what every built-in backend provides. Third-party
// backends implement the same five calls as a program; Ops turns an
// Implementation into that program's behaviour so built-ins take the
// identical door.
type Implementation interface {
	Detect(project string, warn io.Writer) bool
	File(req Request) (string, error)
	Status(project, ref string) (string, error)
	Close(project, ref string) error
	URL(project, ref string) (string, error) // ErrUnsupported if none
}

// Ops adapts an Implementation to the contract's calls.
func Ops(b Implementation) map[string]plugin.Op {
	return map[string]plugin.Op{
		"detect": func(args []string, _ io.Reader, _, stderr io.Writer) int {
			if len(args) != 1 {
				return plugin.Usage(stderr, "detect <project>")
			}
			if b.Detect(args[0], stderr) {
				return plugin.ExitOK
			}
			return plugin.ExitFailed
		},
		"file": func(_ []string, stdin io.Reader, stdout, stderr io.Writer) int {
			var req Request
			if err := json.NewDecoder(stdin).Decode(&req); err != nil || req.Project == "" || req.ID <= 0 {
				fmt.Fprintln(stderr, "file: need JSON with id, project, text, kind on stdin")
				return plugin.ExitRefused
			}
			if req.V != 0 {
				fmt.Fprintf(stderr, "file: contract v%d is newer than this backend speaks (v0)\n", req.V)
				return plugin.ExitRefused
			}
			ref, err := b.File(req)
			if err == nil {
				fmt.Fprintln(stdout, ref)
			}
			return plugin.Exit(err, stderr)
		},
		"status": plugin.RefCall("status", func(project, ref string, _ io.Reader, stdout io.Writer) error {
			st, err := b.Status(project, ref)
			if err == nil {
				fmt.Fprintln(stdout, st)
			}
			return err
		}),
		"close": plugin.RefCall("close", func(project, ref string, _ io.Reader, _ io.Writer) error {
			return b.Close(project, ref)
		}),
		"url": plugin.RefCall("url", func(project, ref string, _ io.Reader, stdout io.Writer) error {
			u, err := b.URL(project, ref)
			if err == nil {
				fmt.Fprintln(stdout, u)
			}
			return err
		}),
	}
}

// Deps is what every built in backend is made from.
type Deps struct {
	Home string
	Cfg  *config.Config
}

func builtin(make func(Deps) Implementation) func(Deps) map[string]plugin.Op {
	return func(d Deps) map[string]plugin.Op { return Ops(make(d)) }
}

// Registry is the built in backends, in detection order: a project's own
// follow-ups file before any remote host. markdown is offline: its items
// live in the project, so asking about them never leaves the machine.
var Registry = plugin.Registry[Deps]{Axis: "backend", Builtins: []plugin.Builtin[Deps]{
	{Name: "markdown", Offline: true, Ops: builtin(func(Deps) Implementation { return Markdown{} })},
	{Name: "github", Ops: builtin(func(d Deps) Implementation { return GitHub(d.Home, d.Cfg) })},
	{Name: "gitlab", Ops: builtin(func(d Deps) Implementation { return GitLab(d.Home, d.Cfg) })},
}}

var timeout = plugin.Timeout // overridable in tests

// Backend is a backend as sous calls it.
type Backend = plugin.Plugin

// Local is the sentinel for "no upstream": the note stays in threads.json.
var Local = Backend{Name: "local"}

var ErrUnknownBackend = errors.New("backend not available")

// Backends lists the named built ins, then the backend programs among
// thirdParty.
func Backends(exe string, builtins, thirdParty []string) []Backend {
	return Registry.Discover(exe, builtins, thirdParty)
}

// ByRef routes an existing ref to the backend that owns it. Refs are
// self-describing: "md:" is markdown, "<name>:" is that backend.
func ByRef(bs []Backend, ref string) (Backend, error) {
	if !strings.Contains(ref, ":") {
		return Backend{}, fmt.Errorf("%w: malformed ref %q", ErrUnknownBackend, ref)
	}
	if rest, ok := strings.CutPrefix(ref, "md:"); ok {
		ref = "markdown:" + rest
	}
	if b, ok := plugin.ByRef(bs, ref); ok {
		return b, nil
	}
	return Backend{}, fmt.Errorf("%w: no backend for ref %q", ErrUnknownBackend, ref)
}

type result struct {
	plugin.Answer
	err error
}

func run(ctx context.Context, b Backend, stdin []byte, op string, args ...string) result {
	if b.Name == "local" {
		return result{err: errors.New("local threads have no upstream backend")}
	}
	argv := append(append([]string{}, b.Argv...), op)
	a, err := plugin.Call(ctx, b.Name, op, append(argv, args...), stdin, timeout)
	return result{a, err}
}

// Detect picks the backend for filing into project. An explicit override
// must name an available backend (a typo must not silently mean "local").
// Otherwise the first backend whose detect exits 0 wins; a probe that
// crashes is warned and skipped.
func Detect(ctx context.Context, bs []Backend, project, override string, warn io.Writer) (Backend, error) {
	if override != "" {
		if b, ok := plugin.Find(bs, override); ok {
			return b, nil
		}
		return Backend{}, fmt.Errorf("%w: %q is declared but no sous-backend-%s is listed in config plugins", ErrUnknownBackend, override, override)
	}
	for _, b := range bs {
		r := run(ctx, b, nil, "detect", project)
		switch {
		case r.err == nil && r.Code == plugin.ExitOK:
			return b, nil
		case r.Code == plugin.ExitNotSetUp:
			if warn != nil {
				fmt.Fprintf(warn, "sous: backend %s not set up: %v\n", b.Name, r.err)
			}
		case r.Code == plugin.ExitFailed:
			// Not applicable — but a stated reason (auth, missing tool) must
			// reach the user, or "no tracker" is a mystery.
			if warn != nil && r.err != nil && !errors.Is(r.err, plugin.ErrNo) {
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
	r := run(ctx, b, plugin.Request(req), "file")
	if r.err != nil {
		return "", r.err
	}
	if r.Code == 2 {
		return "", plugin.Refused(b.Name, "file", r.Answer)
	}
	if r.Out == "" {
		return "", fmt.Errorf("%s file: printed no ref", b.Name)
	}
	return r.Out, nil
}

func Status(ctx context.Context, b Backend, project, ref string) (string, error) {
	r := run(ctx, b, nil, "status", project, ref)
	switch {
	case r.err != nil:
		return "", r.err
	case r.Code != 0:
		return "", plugin.Refused(b.Name, "status", r.Answer)
	}
	switch r.Out {
	case "open", "closed", "unknown":
		return r.Out, nil
	}
	return "", fmt.Errorf("%s status: not open, closed or unknown: %q", b.Name, r.Out)
}

func Close(ctx context.Context, b Backend, project, ref string) error {
	r := run(ctx, b, nil, "close", project, ref)
	if r.err != nil {
		return r.err
	}
	if r.Code == plugin.ExitRefused {
		return plugin.Refused(b.Name, "close "+ref, r.Answer)
	}
	return nil
}

// URL is optional: ok=false when the backend exits 2.
func URL(ctx context.Context, b Backend, project, ref string) (string, bool, error) {
	r := run(ctx, b, nil, "url", project, ref)
	if r.err != nil {
		return "", false, r.err
	}
	if r.Code == plugin.ExitRefused {
		return "", false, nil
	}
	return r.Out, true, nil
}
