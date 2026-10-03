package runner

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/harness"
	"github.com/bilal-/sous/internal/plugin"
)

// result is result.json: how the agent ended.
type result struct {
	Exit    int       `json:"exit"`
	Message string    `json:"message,omitempty"`
	Session string    `json:"session,omitempty"`
	Reason  string    `json:"reason,omitempty"` // why it ended without the agent saying: out of time
	Stopped bool      `json:"stopped,omitempty"`
	Ended   time.Time `json:"ended"`
}

// Watch runs the agent for one run (or, with resume, carries on after a
// reply) and records how it ended. It is the only process that waits on
// the agent, and it exits when the agent does.
func (a *Agent) Watch(dir string, resume bool) error {
	var m runMeta
	if err := readJSON(dir, "run.json", &m); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "watcher.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(dir, "log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	start, _ := logf.Seek(0, io.SeekEnd)
	args := a.h.Headless.Start(m.run())
	if resume {
		args = a.h.Headless.Resume(m.run())
	}
	limited, cancel := context.WithTimeout(context.Background(), m.Limit)
	defer cancel()
	ctx, unhook := signal.NotifyContext(limited, syscall.SIGTERM, syscall.SIGINT) // sous done: stop
	defer unhook()
	cmd := plugin.GroupCommand(ctx, a.h.Bin, args...)
	cmd.Dir = m.Worktree
	cmd.Env = agentEnv(m)
	cmd.Stdout, cmd.Stderr = logf, logf
	runErr := cmd.Start()
	if runErr == nil {
		// Recorded so stop can reach the agent even if this watcher dies.
		if err := os.WriteFile(filepath.Join(dir, "agent.pgid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "stop reaches the agent only through this watcher: %v\n", err) // into watcher.log
		}
		runErr = cmd.Wait()
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // anything it left running ends with it
	}
	r := result{Ended: time.Now().UTC()}
	var ee *exec.ExitError
	switch {
	case errors.Is(limited.Err(), context.DeadlineExceeded):
		r.Exit, r.Reason = -1, fmt.Sprintf("ran out of time (%s)", m.Limit)
	case ctx.Err() != nil:
		r.Exit, r.Stopped = -1, true
	case errors.As(runErr, &ee):
		r.Exit = ee.ExitCode()
	case runErr != nil:
		r.Exit, r.Reason = -1, runErr.Error()
	}
	out, _ := os.ReadFile(filepath.Join(dir, "log"))
	out = out[min(int(start), len(out)):] // this attempt's output only
	r.Session = a.h.Headless.Session(out)
	r.Message = a.h.Headless.Last(m.run(), out)
	if r.Session != "" {
		m.Session, m.Answer = r.Session, ""
		if err := writeJSON(dir, "run.json", m); err != nil {
			return err
		}
	}
	return writeJSON(dir, "result.json", r)
}

// watcherLog is the watcher's own output, in the run's folder.
const watcherLog = "watcher.log"

// Status reads the run folder; it never asks the agent anything.
func (a *Agent) Status(_, ref string) (Status, error) {
	dir, m, err := a.meta(ref)
	if err != nil {
		return Status{}, err
	}
	st := Status{Branch: m.Branch, Worktree: m.Worktree, Log: filepath.Join(dir, "log")}
	var r result
	if readJSON(dir, "result.json", &r) != nil {
		if watcherAlive(dir) {
			st.State = Running
		} else {
			st.State, st.Text = Failed, "stopped without a result"
			if why := tail(filepath.Join(dir, watcherLog)); why != "" {
				st.Text += ": " + finalLine(why)
			}
		}
		return st, nil
	}
	state, text := parseMarker(r.Message)
	switch {
	case r.Stopped:
		st.State, st.Text = Failed, "stopped"
	case r.Reason != "":
		st.State, st.Text = Failed, r.Reason
	case state != "":
		st.State, st.Text = state, text
	case r.Exit == 0:
		st.State, st.Text = Done, finalLine(r.Message)
	default: // the agent's own last words, else the log's
		st.State, st.Text = Failed, fmt.Sprintf("exit %d: %s", r.Exit, finalLine(cmp.Or(r.Message, tail(filepath.Join(dir, "log")))))
	}
	if st.State != Failed && uncommitted(m.Worktree) {
		st.Text += " (changes not committed, in the worktree)"
	}
	return st, nil
}

// parseMarker reads how the agent's last message says the run ended.
func parseMarker(msg string) (State, string) {
	needsYou, said, ok := harness.ReadMarker(msg)
	switch {
	case !ok:
		return "", ""
	case needsYou:
		return NeedsYou, said
	}
	return Done, said
}

// finalLine is the last line of what an agent or its watcher said.
func finalLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// tail is the end of a file, for a failure's last words.
func tail(path string) string {
	b, _ := os.ReadFile(path)
	if len(b) > 4096 {
		b = b[len(b)-4096:]
	}
	return string(b)
}

// agentEnv is the agent's environment: the watcher's, with pushes blocked
// and PWD its worktree, said once. Some agents (opencode) trust PWD over
// the folder they are started in, and the watcher's own PWD is wherever
// sous was run from.
func agentEnv(m runMeta) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PWD=") {
			env = append(env, kv)
		}
	}
	return append(append(env, pushBlock(m.Project)...), "PWD="+m.Worktree)
}
