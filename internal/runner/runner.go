// Package runner hands work to runners: programs that run an agent on a
// task somewhere else and report how it is going. Runners are plugins named
// sous-runner-<name>; the built ins are reached as `sous runner <name>`.
// sous never waits on a run and never checks a process: that is the
// runner's job.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bilal-/sous/internal/plugin"
)

// Timeout bounds every call; start must detach its work to answer in time.
const Timeout = plugin.Timeout

var timeout = Timeout // overridable in tests

// Request is start's standard input.
type Request struct {
	V        int    `json:"v"`
	ID       int    `json:"id"`  // the note's number: sous/run-<id>
	UID      string `json:"uid"` // stable: a repeated start with it returns the same ref
	Project  string `json:"project"`
	Brief    string `json:"brief"`
	HereFile string `json:"here_file,omitempty"`
}

// State is how a run is going.
type State string

const (
	Running  State = "running"
	NeedsYou State = "needs_you"
	Done     State = "done"
	Failed   State = "failed"
)

func (s State) Valid() bool { return s == Running || s == NeedsYou || s == Done || s == Failed }

// Status is status's one line of JSON.
type Status struct {
	V        int    `json:"v"`
	State    State  `json:"state"`
	Text     string `json:"text,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Log      string `json:"log,omitempty"`
}

var (
	// ErrUnsupported: an optional call (reply, clean) this runner does not have.
	ErrUnsupported = plugin.ErrUnsupported
	// ErrNotSetUp: start exited 3; the reason says what is missing.
	ErrNotSetUp = plugin.ErrNotSetUp
)

// Runner is a runner as sous calls it. A built in runner is Offline: it
// reads files on this machine, so sous may ask it while a session starts.
type Runner = plugin.Plugin

// Runners lists the named built ins, then the runner programs among
// thirdParty.
func Runners(exe string, builtins, thirdParty []string) []Runner {
	return Registry.Discover(exe, builtins, thirdParty)
}

// Find is the runner called name.
func Find(rs []Runner, name string) (Runner, bool) { return plugin.Find(rs, name) }

// ByRef: refs name their runner, "claude:…".
func ByRef(rs []Runner, ref string) (Runner, error) {
	if r, ok := plugin.ByRef(rs, ref); ok {
		return r, nil
	}
	name, _, _ := strings.Cut(ref, ":")
	return Runner{}, fmt.Errorf("no runner for %q (is sous-runner-%s listed in plugins?)", ref, name)
}

func call(ctx context.Context, r Runner, stdin []byte, op string, args ...string) (plugin.Answer, error) {
	argv := append(append(append([]string{}, r.Argv...), op), args...)
	return plugin.Call(ctx, r.Name, op, argv, stdin, timeout)
}

// Start hands a task to r and returns the run's ref.
func Start(ctx context.Context, r Runner, req Request) (string, error) {
	a, err := call(ctx, r, plugin.Request(req), "start", req.Project)
	switch {
	case a.Code == plugin.ExitNotSetUp:
		return "", fmt.Errorf("%s: %w: %v", r.Name, ErrNotSetUp, err)
	case err != nil:
		return "", fmt.Errorf("%s start: %w", r.Name, err)
	case a.Code == plugin.ExitRefused:
		return "", plugin.Refused(r.Name, "start", a)
	case a.Out == "":
		return "", fmt.Errorf("%s start: printed no ref", r.Name)
	}
	return a.Out, nil
}

// GetStatus never guesses: anything but a readable v0 line with a known
// state is an error, and the caller keeps what it knew.
func GetStatus(ctx context.Context, r Runner, project, ref string) (Status, error) {
	a, err := call(ctx, r, nil, "status", project, ref)
	if err != nil {
		return Status{}, err
	}
	var st Status
	switch {
	case a.Code != 0:
		return Status{}, plugin.Refused(r.Name, "status", a)
	case json.Unmarshal([]byte(a.Out), &st) != nil:
		return Status{}, fmt.Errorf("%s status: not a JSON line: %.60q", r.Name, a.Out)
	case st.V != 0:
		return Status{}, fmt.Errorf("%s status: contract v%d is newer than this sous speaks (v0)", r.Name, st.V)
	case !st.State.Valid():
		return Status{}, fmt.Errorf("%s status: unknown state %q", r.Name, st.State)
	}
	return st, nil
}

// Reply passes the person's answer to a run that needs them.
func Reply(ctx context.Context, r Runner, project, ref, answer string) error {
	return optional(r, "reply")(call(ctx, r, []byte(answer), "reply", project, ref))
}

// Stop ends a run; stopping twice is fine.
func Stop(ctx context.Context, r Runner, project, ref string) error {
	a, err := call(ctx, r, nil, "stop", project, ref)
	if err == nil && a.Code != 0 {
		return plugin.Refused(r.Name, "stop", a)
	}
	return err
}

// Clean removes what a finished run left behind, keeping its work.
func Clean(ctx context.Context, r Runner, project, ref string) error {
	return optional(r, "clean")(call(ctx, r, nil, "clean", project, ref))
}

// optional reads an optional call's answer: exit 2 is "not supported".
func optional(r Runner, op string) func(plugin.Answer, error) error {
	return func(a plugin.Answer, err error) error {
		switch {
		case err != nil:
			return err
		case a.Code == plugin.ExitRefused:
			return fmt.Errorf("%s %s: %w", r.Name, op, ErrUnsupported)
		}
		return nil
	}
}
