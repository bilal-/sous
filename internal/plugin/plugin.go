// Package plugin is the one subprocess door every axis (signals, backends,
// launchers) goes through: discovery by name convention, and execution with
// a process group, a deadline, and a bounded wait. Built-ins are re-exec'd
// through `sous <axis> <name>` so they take exactly this path.
package plugin

import (
	"bytes"
	"context"
	"errors"
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
