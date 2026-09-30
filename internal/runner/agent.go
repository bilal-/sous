package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Agent is a built in runner: it starts one agent CLI in a worktree, and
// that is all (spec: the hard line). The watcher (watch.go) records how the
// agent ended.
type Agent struct {
	Name  string
	Home  string // SOUS_HOME: runs live in Home/runs/<uid>
	Exe   string // this sous, which runs the watcher
	Limit time.Duration
	cli   cli
}

// Builtin is the built in runner name, if there is one.
func Builtin(name, home, exe string, limit time.Duration) (*Agent, bool) {
	c, ok := clis[name]
	if !ok {
		return nil, false
	}
	return &Agent{Name: name, Home: home, Exe: exe, Limit: limit, cli: c}, true
}

// runMeta is run.json: what the watcher needs to start or resume the agent.
type runMeta struct {
	Name     string        `json:"name"`
	ID       int           `json:"id"`
	Project  string        `json:"project"`
	Worktree string        `json:"worktree"`
	Branch   string        `json:"branch"`
	Prompt   string        `json:"prompt"`
	Session  string        `json:"session,omitempty"`
	Answer   string        `json:"answer,omitempty"` // the reply the next watcher passes on
	Limit    time.Duration `json:"limit"`
}

func (a *Agent) dir(uid string) string { return filepath.Join(a.Home, "runs", uid) }

// uidOf reads the uid out of a ref this runner made ("claude:<uid>").
func (a *Agent) uidOf(ref string) (string, error) {
	uid, ok := strings.CutPrefix(ref, a.Name+":")
	if !ok || uid == "" || strings.ContainsAny(uid, "/.") {
		return "", fmt.Errorf("%q is not a %s run", ref, a.Name)
	}
	return uid, nil
}

const preamble = `You are working alone, in a git worktree made for this task, on branch %s.
Commit your work on this branch. Never push.
If you need to do something you are not allowed to do, stop and ask for it.
End your last message with one line:
SOUS: done <one line summary of what you did>
or
SOUS: needs you <one question for the person>

The task:

`

func (a *Agent) Start(req Request) (string, error) {
	ref := a.Name + ":" + req.UID
	if _, err := a.uidOf(ref); err != nil {
		return "", err
	}
	dir := a.dir(req.UID)
	if _, err := os.Stat(filepath.Join(dir, "run.json")); err == nil {
		return ref, nil // started before: safe to repeat
	}
	if _, err := exec.LookPath(a.cli.bin); err != nil {
		return "", fmt.Errorf("%w: %s is not installed", ErrNotSetUp, a.cli.bin)
	}
	if out, err := exec.Command("git", "-C", req.Project, "rev-parse", "--show-toplevel").CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s is not a git repository, so there is nowhere safe to work: %s", req.Project, strings.TrimSpace(string(out)))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	m := runMeta{Name: a.Name, ID: req.ID, Project: req.Project, Worktree: filepath.Join(dir, "worktree"), Branch: fmt.Sprintf("sous/run-%d", req.ID), Limit: a.Limit}
	m.Prompt = fmt.Sprintf(preamble, m.Branch) + req.Brief
	if out, err := exec.Command("git", "-C", req.Project, "worktree", "add", "-q", "-b", m.Branch, m.Worktree, "HEAD").CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("making the worktree: %s", strings.TrimSpace(string(out)))
	}
	if err := writeJSON(dir, "run.json", m); err != nil {
		return "", err
	}
	return ref, a.launch(dir, false)
}

// launch starts the watcher, detached in its own session: it outlives this
// call, and is the only process that waits on the agent.
func (a *Agent) launch(dir string, resume bool) error {
	args := []string{"runner", a.Name, "watch", dir}
	if resume {
		args = append(args, "--resume")
	}
	cmd := exec.Command(a.Exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the watcher: %w", err)
	}
	return cmd.Process.Release()
}

// pushBlock is the git environment for the agent: every push URL, whatever
// its form, is rewritten to a scheme no git can reach, so a push fails even
// if tried. The repository's own config is never changed.
func pushBlock() []string {
	prefixes := []string{"https://", "http://", "ssh://", "git://", "git@", "file://", "/"}
	env := []string{fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(prefixes))}
	for i, p := range prefixes {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=url.sous-no-push::.pushInsteadOf", i), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, p))
	}
	return env
}

func writeJSON(dir, name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "."+name)
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

func readJSON(dir, name string, v any) error {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// meta reads a run this runner made, by its ref.
func (a *Agent) meta(ref string) (string, runMeta, error) {
	var m runMeta
	uid, err := a.uidOf(ref)
	if err != nil {
		return "", m, err
	}
	dir := a.dir(uid)
	if err := readJSON(dir, "run.json", &m); err != nil {
		return "", m, fmt.Errorf("no run %s here", ref)
	}
	return dir, m, nil
}

func readString(dir, name string) string {
	b, _ := os.ReadFile(filepath.Join(dir, name))
	return strings.TrimSpace(string(b))
}

// Reply passes the answer to the agent's session, with a new watcher.
func (a *Agent) Reply(_, ref, answer string) error {
	dir, m, err := a.meta(ref)
	if err != nil {
		return err
	}
	var r result
	switch {
	case strings.TrimSpace(answer) == "":
		return errors.New("the answer is empty")
	case readJSON(dir, "result.json", &r) != nil && watcherAlive(dir):
		return fmt.Errorf("run %d is still working; wait for it to ask", m.ID)
	case m.Session == "":
		return errors.New("the agent never started a session, so it cannot carry on; start a new run with the answer in the brief")
	}
	m.Answer = answer
	if err := writeJSON(dir, "run.json", m); err != nil {
		return err
	}
	os.Remove(filepath.Join(dir, "result.json"))
	return a.launch(dir, true)
}

// Stop asks the watcher to stop the agent (it records "stopped"), and
// makes sure of it. Stopping a finished run changes nothing.
func (a *Agent) Stop(_, ref string) error {
	dir, _, err := a.meta(ref)
	if err != nil {
		return err
	}
	var r result
	if readJSON(dir, "result.json", &r) == nil {
		return nil
	}
	if pid, err := strconv.Atoi(readString(dir, "watcher.pid")); err == nil && pid > 0 {
		syscall.Kill(pid, syscall.SIGTERM)
		for i := 0; i < 40 && syscall.Kill(pid, 0) == nil; i++ {
			time.Sleep(50 * time.Millisecond)
		}
		syscall.Kill(-pid, syscall.SIGKILL)
		syscall.Kill(pid, syscall.SIGKILL)
	}
	if readJSON(dir, "result.json", &r) != nil {
		return writeJSON(dir, "result.json", result{Exit: -1, Stopped: true, Ended: time.Now().UTC()})
	}
	return nil
}

// Clean removes a finished run's worktree. Its branch goes only when it
// holds no work (git refuses to delete a branch it has not merged).
func (a *Agent) Clean(_, ref string) error {
	dir, m, err := a.meta(ref)
	if err != nil {
		return err
	}
	var r result
	if readJSON(dir, "result.json", &r) != nil && watcherAlive(dir) {
		return fmt.Errorf("run %d is still working; stop it first", m.ID)
	}
	if _, err := os.Stat(m.Worktree); err == nil {
		if out, err := exec.Command("git", "-C", m.Project, "worktree", "remove", "--force", m.Worktree).CombinedOutput(); err != nil {
			return fmt.Errorf("removing the worktree: %s", strings.TrimSpace(string(out)))
		}
	}
	exec.Command("git", "-C", m.Project, "branch", "-d", m.Branch).Run()
	return nil
}
