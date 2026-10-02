// Package plugin is the one subprocess door every axis (signals, backends,
// launchers, runners) goes through: discovery by name convention, and execution with
// a process group, a deadline, and a bounded wait. Built-ins are re-exec'd
// through `sous <axis> <name>` so they take exactly this path.
package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Plugin struct {
	Name string
	Argv []string
}

// Axes are the kinds of plugin, each named sous-<axis>-<name>.
var Axes = []string{"signal", "backend", "launcher", "runner"}

// Named reports whether path's file name is sous-<axis>-<name> for a known
// axis; Discover skips any other.
func Named(path string) bool {
	for _, a := range Axes {
		if name, ok := strings.CutPrefix(filepath.Base(path), "sous-"+a+"-"); ok && name != "" {
			return true
		}
	}
	return false
}

// Discover lists built-ins (as [exe, axis, name]) then third-party
// executables named sous-<axis>-<name>.
func Discover(exe, axis string, builtins, thirdParty []string) []Plugin {
	var out []Plugin
	for _, n := range builtins {
		out = append(out, Plugin{Name: n, Argv: []string{exe, axis, n}})
	}
	prefix := "sous-" + axis + "-"
	for _, p := range thirdParty {
		base := filepath.Base(p)
		if strings.HasPrefix(base, prefix) {
			out = append(out, Plugin{Name: strings.TrimPrefix(base, prefix), Argv: []string{p}})
		}
	}
	return out
}

type Result struct {
	Stdout   string
	Stderr   string
	Code     int  // exit code when the process ran
	TimedOut bool // the deadline killed it
	Err      error
}

// Exec runs argv with stdin under a deadline. A non-zero exit is a Code, not
// an Err; Err is for "could not run" (missing binary) or the deadline. The
// whole process group is killed on timeout so a shell wrapper's children
// cannot keep the pipe open.
func Exec(ctx context.Context, argv []string, stdin []byte, timeout time.Duration) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	r := Result{Stdout: out.String(), Stderr: errb.String()}
	if err == nil {
		return r
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		r.TimedOut = true
		r.Err = errors.New("timed out after " + timeout.String())
		return r
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		r.Code = ee.ExitCode()
		return r
	}
	r.Err = err
	return r
}

// Op is one contract call as a program sees it: arguments, standard input,
// standard output and error, and an exit code. Built ins implement their
// calls as Ops so they go through the same door as a plugin program.
type Op func(args []string, stdin io.Reader, stdout, stderr io.Writer) int

// ErrNo: exit 1 with nothing on standard error, a plain "no".
var ErrNo = errors.New("not applicable")

// Answer is what a contract call said: its standard output and exit code,
// and for an exit 2, the reason it gave on standard error.
type Answer struct {
	Out    string
	Code   int
	Reason string
}

// Call runs one contract call and reads its exit code the way every kind of
// plugin does: 0 and 2 are answers; 1 is a failure, its reason on standard
// error (ErrNo when silent); anything else, a timeout, or a program that
// cannot run is an error.
func Call(ctx context.Context, name, op string, argv []string, stdin []byte, timeout time.Duration) (Answer, error) {
	res := Exec(ctx, argv, stdin, timeout)
	switch {
	case res.TimedOut:
		return Answer{}, fmt.Errorf("%s %s: timed out after %s", name, op, timeout)
	case res.Err != nil:
		return Answer{}, fmt.Errorf("%s %s: %w", name, op, res.Err)
	}
	msg := strings.TrimSpace(res.Stderr)
	switch res.Code {
	case 0, 2:
		return Answer{Out: strings.TrimSpace(res.Stdout), Code: res.Code, Reason: msg}, nil
	case 1:
		if msg == "" {
			return Answer{Code: 1}, ErrNo
		}
		return Answer{Code: 1}, errors.New(msg)
	}
	if msg == "" {
		msg = fmt.Sprintf("exit %d", res.Code)
	}
	return Answer{Code: res.Code}, errors.New(msg)
}

// Refused is the error for an exit 2 that the caller cannot accept, with
// the plugin's reason when it gave one.
func Refused(name, op string, a Answer) error {
	if a.Reason == "" {
		return fmt.Errorf("%s %s: refused (exit 2)", name, op)
	}
	return fmt.Errorf("%s %s: refused: %s", name, op, a.Reason)
}
