// Package runner hands work to runners: programs that run an agent on a
// task somewhere else and report how it is going. Runners are plugins named
// sous-runner-<name>; the built ins are reached as `sous runner <name>`.
// sous never waits on a run and never checks a process: that is the
// runner's job.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/thread"
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

// State is how a run is going: thread.RunState, which notes store.
type State = thread.RunState

// The states a runner reports. Starting is sous's own, until it answers.
const (
	Running  = thread.RunRunning
	NeedsYou = thread.RunNeedsYou
	Done     = thread.RunDone
	Failed   = thread.RunFailed
)

// Status is status's one line of JSON. Phase and what follows it come
// only from a runner whose describe names them; State is always there, so
// a reader that knows only states still reads the line right.
type Status struct {
	V        int    `json:"v"`
	State    State  `json:"state"`
	Text     string `json:"text,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Log      string `json:"log,omitempty"`

	Phase    thread.RunPhase     `json:"phase,omitempty"`
	Reason   string              `json:"reason,omitempty"` // why it is blocked or failed
	Progress *thread.RunProgress `json:"progress,omitempty"`
	Question *thread.RunQuestion `json:"question,omitempty"`
	Result   *thread.RunResult   `json:"result,omitempty"`
}

// check holds a status line to the contract beyond its state: a known
// phase agrees with the state, and a result names its commit.
func (st Status) check() error {
	if want, ok := st.Phase.State(); ok && want != st.State {
		return fmt.Errorf("phase %q beside state %q (want %q)", st.Phase, st.State, want)
	}
	if st.Result != nil && (st.Result.Commit == "" || st.Result.Base == "") {
		return errors.New("result without its base and commit")
	}
	if st.Question != nil && (st.Question.ID == "" || st.Question.Token == "") {
		return errors.New("question without its id and token")
	}
	return nil
}

var (
	// ErrUnsupported: an optional call (reply, clean) this runner does not have.
	ErrUnsupported = plugin.ErrUnsupported
	// ErrNotSetUp: start exited 3; the reason says what is missing.
	ErrNotSetUp = plugin.ErrNotSetUp
	// ErrConflict: reply or approve exited 4; what it named moved on.
	ErrConflict = plugin.ErrConflict
)

// Runner is a runner as sous calls it. A built in runner is Offline: it
// reads files on this machine, so sous may ask it while a session starts.
type Runner = plugin.Plugin

// ByRef: refs name their runner, "claude:…".
func ByRef(rs []Runner, ref string) (Runner, error) {
	if r, ok := plugin.ByRef(rs, ref); ok {
		return r, nil
	}
	name, _, _ := strings.Cut(ref, ":")
	return Runner{}, fmt.Errorf("no runner for %q (is %s%s listed in plugins?)", ref, plugin.Prefix(plugin.AxisRunner), name)
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
		return "", fmt.Errorf("%s: %w", r.Name, err)
	case err != nil:
		return "", fmt.Errorf("%s start: %w", r.Name, err)
	}
	return plugin.RefAnswer(r.Name, "start", a, nil)
}

// GetStatus never guesses: anything but a readable v0 line with a known
// state is an error, and the caller keeps what it knew.
func GetStatus(ctx context.Context, r Runner, project, ref string) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, plugin.StatusTimeout) // asked of many at once
	defer cancel()
	a, err := call(ctx, r, nil, "status", project, ref)
	if err != nil {
		return Status{}, err
	}
	var st Status
	switch {
	case a.Code != plugin.ExitOK:
		return Status{}, plugin.Refused(r.Name, "status", a)
	case json.Unmarshal([]byte(a.Out), &st) != nil:
		return Status{}, fmt.Errorf("%s status: not a JSON line: %.60q", r.Name, a.Out)
	case st.V != plugin.Version:
		return Status{}, plugin.Newer(r.Name+" status", st.V)
	case !st.State.Reported():
		return Status{}, fmt.Errorf("%s status: unknown state %q", r.Name, st.State)
	}
	if err := st.check(); err != nil {
		return Status{}, fmt.Errorf("%s status: %w", r.Name, err)
	}
	return st, nil
}

// Reply passes the person's answer to a run that needs them. A runner whose
// describe names question gets JSON naming the question answered (q, when
// known) and the op asking, and may refuse a stale one with exit 4
// (ErrConflict); any other gets the answer as it is.
func Reply(ctx context.Context, r Runner, project, ref, answer string, q *thread.RunQuestion, op string) error {
	in := []byte(answer)
	if Describe(ctx, r).Speaks("question") {
		type asked struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		}
		req := struct {
			V        int    `json:"v"`
			Op       string `json:"op,omitempty"`
			Question *asked `json:"question,omitempty"`
			Answer   string `json:"answer"`
		}{V: plugin.Version, Op: op, Answer: answer}
		if q != nil {
			req.Question = &asked{q.ID, q.Token}
		}
		in = plugin.Request(req)
	}
	return optional(r, "reply")(call(ctx, r, in, "reply", project, ref))
}

// Stop ends a run; stopping twice is fine.
func Stop(ctx context.Context, r Runner, project, ref string) error {
	a, err := call(ctx, r, nil, "stop", project, ref)
	if err == nil && a.Code != plugin.ExitOK {
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
		case a.Code == plugin.ExitConflict:
			return plugin.Conflicted(r.Name, op, a)
		}
		return nil
	}
}
