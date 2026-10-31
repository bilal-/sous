package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
)

type ownedStage struct {
	Version    int       `json:"v"`
	Generation string    `json:"generation"`
	Token      string    `json:"token"`
	Dir        string    `json:"dir"`
	Deadline   time.Time `json:"deadline"`
	Argv       []string  `json:"argv"`
}

type stageOutcome struct {
	Version    int    `json:"v"`
	Final      bool   `json:"final"`
	Generation string `json:"generation"`
	Token      string `json:"token"`
	Exit       int    `json:"exit"`
	Error      string `json:"error,omitempty"`
	Stopped    bool   `json:"stopped,omitempty"`
	TimedOut   bool   `json:"timed_out,omitempty"`
}

type stopRequest struct {
	Generation string `json:"generation"`
}

type ownershipUncertainty struct {
	Generation string `json:"generation"`
	Token      string `json:"token"`
	Reason     string `json:"reason"`
}

func outcomeName(s ownedStage) string { return "outcome-" + s.Token + ".json" }

func decodeOutcome(in io.Reader, s ownedStage, final bool) (stageOutcome, error) {
	var out stageOutcome
	b, err := io.ReadAll(io.LimitReader(in, 1<<20))
	if err != nil {
		return out, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return out, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	exit, present := fields["exit"]
	if !present || bytes.Equal(bytes.TrimSpace(exit), []byte("null")) || out.Version != 1 || out.Final != final || out.Generation != s.Generation || out.Token != s.Token || out.Exit < -1 || out.Stopped && out.TimedOut || (out.Stopped || out.TimedOut || out.Error != "") && out.Exit == 0 {
		return out, errors.New("invalid or mismatched stage outcome")
	}
	return out, nil
}

func readOutcome(s ownedStage) (stageOutcome, error) {
	f, err := os.Open(filepath.Join(s.Dir, outcomeName(s)))
	if err != nil {
		return stageOutcome{}, err
	}
	defer f.Close()
	return decodeOutcome(f, s, true)
}

// Intent is durable before any owner/worker starts. Thus a missing outcome
// remains detectable even if both watcher and owner die and the worker closes
// fd 3. A free flock alone never authenticates completion.
func saveIntent(s ownedStage) error {
	if err := knownUncertainty(s.Dir); err != nil {
		return err
	}
	return writeJSON(s.Dir, "stage.json", s)
}

func knownUncertainty(dir string) error {
	var u ownershipUncertainty
	err := readJSON(dir, "uncertain.json", &u)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read ownership uncertainty: %w", err)
	}
	return fmt.Errorf("ownership uncertain: %s", u.Reason)
}

func uncertain(s ownedStage, cause error) error {
	err := fmt.Errorf("owner ended without a trustworthy outcome: %w", cause)
	if writeErr := writeJSON(s.Dir, "uncertain.json", ownershipUncertainty{s.Generation, s.Token, err.Error()}); writeErr != nil {
		return fmt.Errorf("%w; recording ownership uncertainty: %v", err, writeErr)
	}
	return err
}

func receivedStop(s ownedStage) (bool, error) {
	var req stopRequest
	err := readJSON(s.Dir, "stop.json", &req)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !validOwnerID(req.Generation) {
		return false, errors.New("invalid stop request generation")
	}
	return req.Generation == s.Generation, nil
}

func ownedLock(dir string) (*os.File, error) {
	f := os.NewFile(3, "inherited-run-lock")
	if f == nil {
		return nil, errors.New("no inherited run lock")
	}
	a, err := f.Stat()
	if err != nil {
		return nil, err
	}
	b, err := os.Stat(filepath.Join(dir, "watcher.lock"))
	if err != nil || !os.SameFile(a, b) || !a.Mode().IsRegular() {
		return nil, errors.New("fd 3 is not this run's lock")
	}
	syscall.CloseOnExec(3) // Child inheritance is explicit through ExtraFiles.
	return f, nil
}

func decodeStage(in io.Reader) (ownedStage, error) {
	var s ownedStage
	err := json.NewDecoder(io.LimitReader(in, 1<<20)).Decode(&s)
	if err != nil {
		return s, err
	}
	if s.Version != 1 || !validOwnerID(s.Generation) || !validOwnerID(s.Token) || strings.ContainsAny(s.Token, "/.\\") || !filepath.IsAbs(s.Dir) || s.Deadline.IsZero() || len(s.Argv) == 0 {
		return s, errors.New("incomplete stage specification")
	}
	return s, nil
}

// anchor leads the worker group and remains live after the worker finishes.
// Its private fd 4 sends only a provisional child outcome; it is never inherited
// by the actual worker. Only own publishes a final outcome after group cleanup.
func anchor(in io.Reader) error {
	s, err := decodeStage(in)
	if err != nil {
		return err
	}
	if syscall.Getpgrp() != os.Getpid() {
		return errors.New("anchor must lead its own group")
	}
	lock, err := ownedLock(s.Dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	pipe := os.NewFile(4, "private-child-outcome")
	if pipe == nil {
		return errors.New("no private outcome pipe")
	}
	st, err := pipe.Stat()
	if err != nil || st.Mode()&os.ModeNamedPipe == 0 {
		return errors.New("fd 4 is not the private outcome pipe")
	}
	syscall.CloseOnExec(4)
	defer pipe.Close()
	// own enforces the exact deadline. This grace is only a last resort
	// after owner loss; it prevents a lost anchor from living indefinitely.
	timer := time.NewTimer(time.Until(s.Deadline.Add(2 * time.Second)))
	defer timer.Stop()
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{lock}
	// No Setpgid and no stdin: inherit anchor group and lock only, never fd 4.
	out := stageOutcome{Version: 1, Generation: s.Generation, Token: s.Token}
	if err := cmd.Start(); err != nil {
		out.Exit, out.Error = -1, err.Error()
	} else {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			var ee *exec.ExitError
			switch {
			case errors.As(err, &ee):
				out.Exit = ee.ExitCode()
			case err != nil:
				out.Exit, out.Error = -1, err.Error()
			}
		case <-timer.C:
			return syscall.Kill(0, syscall.SIGKILL)
		}
	}
	if err := json.NewEncoder(pipe).Encode(out); err != nil {
		// own is gone or cannot receive. Kill only this live anchor's own
		// group; no final outcome is published, so ownershipUncertainty remains.
		_ = syscall.Kill(0, syscall.SIGKILL)
		return err
	}
	pipe.Close()
	// Remain a live, signalable member until own kills this group. This
	// avoids Darwin's EPERM for a process group containing only a zombie.
	<-timer.C
	return syscall.Kill(0, syscall.SIGKILL)
}

// own never waits/reaps its direct anchor child before signaling its group.
// Its live child or unreaped zombie pins the PID, even if the anchor crashes.
// Saved PIDs and aggregate flock liveness confer no signaling authority.
func (a *Agent) own(in io.Reader) error {
	s, err := decodeStage(in)
	if err != nil {
		return err
	}
	lock, err := ownedLock(s.Dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	finish := func(out stageOutcome) error {
		out.Version, out.Final = 1, true
		out.Generation, out.Token = s.Generation, s.Token
		return writeJSON(s.Dir, outcomeName(s), out)
	}
	if stopped, err := receivedStop(s); err != nil || stopped {
		out := stageOutcome{Exit: -1, Stopped: stopped}
		if err != nil {
			out.Error = err.Error()
		}
		return finish(out) // No anchor or worker was started.
	}
	if time.Now().After(s.Deadline) {
		return finish(stageOutcome{Exit: -1, TimedOut: true})
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "owner.pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		return finish(stageOutcome{Exit: -1, Error: err.Error()})
	}
	// Explicit SIGCHLD handling prevents inherited SIG_IGN from auto-reaping
	// the anchor. Notifications do not wait and may safely be dropped.
	childSignals := make(chan os.Signal, 1)
	signal.Notify(childSignals, syscall.SIGCHLD)
	defer signal.Stop(childSignals)
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return finish(stageOutcome{Exit: -1, Error: err.Error()})
	}
	defer readEnd.Close()
	data, _ := json.Marshal(s)
	cmd := exec.Command(a.Exe, "runner", a.Name, "anchor")
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{lock, writeEnd}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		writeEnd.Close()
		return finish(stageOutcome{Exit: -1, Error: err.Error()})
	}
	writeEnd.Close() // EOF must depend only on the anchor's copy, never ours.
	cleanup := func(out stageOutcome, trusted bool) error {
		// cmd.Wait has NEVER been called; this exact direct child still pins
		// its PID even if it exited. No later numeric signal is sent after Wait.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("cannot stop retained anchor group: %w", err)
		}
		_ = cmd.Wait()
		if !trusted {
			return errors.New("anchor ended without a trustworthy child outcome")
		}
		return finish(out) // Publication follows actual group signal and reaping.
	}
	// Diagnostic only; tests substitute an unrelated fixture victim's ID.
	if err := os.WriteFile(filepath.Join(s.Dir, "agent.pgid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		return cleanup(stageOutcome{Exit: -1, Error: err.Error()}, true)
	}
	type received struct {
		out stageOutcome
		err error
	}
	childResult := make(chan received, 1)
	go func() {
		out, err := decodeOutcome(readEnd, s, false)
		childResult <- received{out, err}
	}()
	timer := time.NewTimer(time.Until(s.Deadline))
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case got := <-childResult:
			if got.err != nil {
				return cleanup(stageOutcome{Exit: -1, Error: got.err.Error()}, false)
			}
			return cleanup(got.out, true)
		case <-timer.C:
			return cleanup(stageOutcome{Exit: -1, TimedOut: true}, true)
		case <-ticker.C:
			stopped, err := receivedStop(s)
			if err != nil {
				return cleanup(stageOutcome{Exit: -1, Error: err.Error()}, true)
			}
			if stopped {
				return cleanup(stageOutcome{Exit: -1, Stopped: true}, true)
			}
		}
	}
}

func newOwnerID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func validOwnerID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16
}

func (m runMeta) ownershipVersion() error {
	if m.OwnerVersion == 0 && m.Generation == "" {
		return nil
	}
	if m.OwnerVersion != 1 || !validOwnerID(m.Generation) {
		return errors.New("unsupported or invalid run ownership version; preserve this run and use its original sous version")
	}
	return nil
}

func runLive(dir string) (bool, error) {
	lock, err := lockRun(dir)
	if errors.Is(err, errWorking) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	lock.Close()
	return false, nil
}

// finishedStage is called only after liveness has ended (or while the caller
// owns the run lock). A lost result stays uncertain even if every inherited
// descriptor has been closed; subsequent result files cannot clear that fact.
func finishedStage(dir string, m runMeta) (*stageOutcome, error) {
	if err := knownUncertainty(dir); err != nil {
		return nil, err
	}
	var s ownedStage
	err := readJSON(dir, "stage.json", &s)
	if errors.Is(err, os.ErrNotExist) {
		if m.OwnerVersion == 0 {
			var legacy struct {
				Exit *int `json:"exit"`
			}
			if err := readJSON(dir, "result.json", &legacy); err != nil || legacy.Exit == nil {
				return nil, uncertain(ownedStage{Dir: dir}, errors.New("legacy run has no trustworthy completion record; preserve its work and inspect the original runner"))
			}
		}
		return nil, nil
	}
	if err != nil {
		return nil, uncertain(ownedStage{Dir: dir, Generation: m.Generation}, err)
	}
	if m.OwnerVersion != 1 || s.Generation != m.Generation || s.Dir != dir || s.Version != 1 || !validOwnerID(s.Token) {
		return nil, uncertain(ownedStage{Dir: dir, Generation: m.Generation}, errors.New("invalid or mismatched stage intent"))
	}
	out, err := readOutcome(s)
	if err != nil {
		return nil, uncertain(s, err)
	}
	return &out, nil
}

func ownershipState(dir string, m runMeta) (bool, error) {
	if err := m.ownershipVersion(); err != nil {
		return false, err
	}
	if err := knownUncertainty(dir); err != nil {
		return false, err
	}
	live, err := runLive(dir)
	if err != nil || live {
		return live, err
	}
	_, err = finishedStage(dir, m)
	return false, err
}

// prepareAttempt runs with both lifecycle and run locks held. It never adopts
// unresolved work, and never uses an earlier attempt's stop request or outcome.
func prepareAttempt(dir string) error {
	var m runMeta
	if err := readJSON(dir, "run.json", &m); err != nil {
		return err
	}
	if err := m.ownershipVersion(); err != nil {
		return err
	}
	if _, err := finishedStage(dir, m); err != nil {
		return err
	}
	generation, err := newOwnerID()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, "stage.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	m.OwnerVersion, m.Generation = 1, generation
	if err := writeJSON(dir, "run.json", m); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, "result.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func requestStop(dir string, m runMeta) error {
	if m.OwnerVersion != 1 || !validOwnerID(m.Generation) {
		return errors.New("this live legacy run has no authenticated owner; let its original runner finish and inspect the run before cleanup")
	}
	return writeJSON(dir, "stop.json", stopRequest{Generation: m.Generation})
}

func (a *Agent) runStage(ctx context.Context, dir string, m runMeta, argv []string, lock *os.File, stdout, stderr io.Writer) (stageOutcome, error) {
	if _, err := finishedStage(dir, m); err != nil {
		return stageOutcome{}, err
	}
	token, err := newOwnerID()
	if err != nil {
		return stageOutcome{}, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(m.Limit)
	}
	s := ownedStage{Version: 1, Generation: m.Generation, Token: token, Dir: dir, Deadline: deadline, Argv: argv}
	if err := saveIntent(s); err != nil {
		return stageOutcome{}, err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return stageOutcome{}, err
	}
	cmd := exec.Command(a.Exe, "runner", a.Name, "own")
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = bytes.NewReader(data)
	cmd.Dir, cmd.Env = m.Worktree, agentEnv(m)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		out := stageOutcome{Version: 1, Final: true, Generation: s.Generation, Token: s.Token, Exit: -1, Error: err.Error()}
		if err := writeJSON(dir, outcomeName(s), out); err != nil {
			return out, uncertain(s, err)
		}
		return out, nil
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-ctx.Done():
		// The owner also has the absolute deadline; it survives watcher loss.
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			_ = requestStop(dir, m)
		}
		<-done
	}
	out, err := readOutcome(s)
	if err != nil {
		return out, uncertain(s, err)
	}
	return out, nil
}
