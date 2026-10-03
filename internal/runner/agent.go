package runner

import (
	"bytes"
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

	"github.com/bilal-/sous/internal/harness"
)

// Agent is a built in runner: it starts one agent CLI in a worktree, and
// that is all: retrying, reviewing and scheduling belong to a runner
// plugin. The watcher (watch.go) records how the
// agent ended.
type Agent struct {
	Name  string
	Home  string // SOUS_HOME: runs live in Home/runs/<uid>
	Exe   string // this sous, which runs the watcher
	Limit time.Duration
	h     harness.Harness // its Headless is set
}

// Builtin is the built in runner name, if there is one.
func Builtin(name, home, exe string, limit time.Duration) (*Agent, bool) {
	h, ok := harness.Find(name)
	if !ok || h.Headless == nil {
		return nil, false
	}
	return &Agent{Name: name, Home: home, Exe: exe, Limit: limit, h: h}, true
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
	// GitDirs: the folders outside the worktree that a commit writes to.
	GitDirs []string `json:"git_dirs,omitempty"`
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
Commit your work on this branch if you can; if you cannot, leave it in
the worktree and say so. Never push.
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
	if _, err := exec.LookPath(a.h.Bin); err != nil {
		return "", fmt.Errorf("%w: %s is not installed", ErrNotSetUp, a.h.Bin)
	}
	if out, err := exec.Command("git", "-C", req.Project, "rev-parse", "--show-toplevel").CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s is not a git repository, so there is nowhere safe to work: %s", req.Project, strings.TrimSpace(string(out)))
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o700); errors.Is(err, os.ErrExist) {
		return ref, nil // another start of this run got here first
	} else if err != nil {
		return "", err
	}
	m := runMeta{Name: a.Name, ID: req.ID, Project: req.Project, Worktree: filepath.Join(dir, "worktree"), Branch: fmt.Sprintf("sous/run-%d", req.ID), Limit: a.Limit}
	m.Prompt = fmt.Sprintf(preamble, m.Branch)
	if here, err := os.ReadFile(req.HereFile); req.HereFile != "" && err == nil && len(bytes.TrimSpace(here)) > 0 {
		// Where the person left off, inline: the file may be outside what
		// the agent is allowed to read.
		m.Prompt = strings.Replace(m.Prompt, "The task:", "Where the person left off in this project:\n\n"+strings.TrimSpace(string(here))+"\n\nThe task:", 1)
	}
	m.Prompt += req.Brief
	if out, err := exec.Command("git", "-C", req.Project, "worktree", "add", "-q", "-b", m.Branch, m.Worktree, "HEAD").CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("making the worktree: %s", strings.TrimSpace(string(out)))
	}
	m.GitDirs = commitDirs(m.Worktree)
	if err := writeJSON(dir, "run.json", m); err != nil {
		return "", err
	}
	lock, err := lockRun(dir)
	if err != nil {
		return "", err
	}
	return ref, a.launch(dir, false, lock)
}

// commitDirs: where a commit in worktree writes, outside it: the shared
// objects, refs and logs, and the worktree's own git folder.
func commitDirs(worktree string) []string {
	common, err1 := exec.Command("git", "-C", worktree, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	own, err2 := exec.Command("git", "-C", worktree, "rev-parse", "--absolute-git-dir").Output()
	if err1 != nil || err2 != nil {
		return nil
	}
	c := strings.TrimSpace(string(common))
	return []string{filepath.Join(c, "objects"), filepath.Join(c, "refs"), filepath.Join(c, "logs"), strings.TrimSpace(string(own))}
}

// uncommitted: the worktree has changes git has not recorded, the agent's
// own files included.
func uncommitted(worktree string) bool {
	out, err := exec.Command("git", "-C", worktree, "status", "--porcelain").Output()
	return err != nil || len(strings.TrimSpace(string(out))) > 0
}

// lockRun takes the run's lock, which only a live watcher holds: the
// kernel lets go of it when the watcher dies, however it dies. An error
// says the run is still working.
func lockRun(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "watcher.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errWorking
	}
	return f, nil
}

var errWorking = errors.New("still working")

// watcherAlive: someone holds the run's lock.
func watcherAlive(dir string) bool {
	f, err := lockRun(dir)
	if err != nil {
		return errors.Is(err, errWorking)
	}
	f.Close()
	return false
}

// launch starts the watcher, detached in its own session, and hands it the
// run's lock (taken by the caller), so the run is working from this moment
// until the watcher exits. It outlives this call, and is the only process
// that waits on the agent.
func (a *Agent) launch(dir string, resume bool, lock *os.File) error {
	defer lock.Close()
	os.Remove(filepath.Join(dir, "watcher.pid"))
	os.Remove(filepath.Join(dir, "agent.pgid"))
	args := []string{"runner", a.Name, "watch", dir, "--lock-fd", "3"}
	if resume {
		args = append(args, "--resume")
	}
	// The watcher's own words (not the agent's) are kept beside the run,
	// so a watcher that fails says why.
	out, err := os.OpenFile(filepath.Join(dir, watcherLog), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	cmd := exec.Command(a.Exe, args...)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the watcher: %w", err)
	}
	return cmd.Process.Release()
}

// pushBlock is the git environment for the agent: every place a push
// could go is rewritten to a scheme no git can reach, so a push fails even
// if tried. It covers the usual address forms, and each of the project's
// remotes exactly as written: a url (pushInsteadOf) and a pushurl, which
// git only rewrites with insteadOf. Fetching still works. The repository's
// own config is never changed.
func pushBlock(project string) []string {
	type rule struct{ key, prefix string }
	var rules []rule
	for _, p := range []string{"https://", "http://", "ssh://", "git://", "git@", "file://", "/"} {
		rules = append(rules, rule{"pushInsteadOf", p})
	}
	out, _ := exec.Command("git", "-C", project, "config", "--get-regexp", `^remote\..*\.(url|pushurl)$`).Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		key, url, ok := strings.Cut(line, " ")
		switch {
		case !ok || url == "":
		case strings.HasSuffix(key, ".pushurl"):
			rules = append(rules, rule{"insteadOf", url})
		default:
			rules = append(rules, rule{"pushInsteadOf", url})
		}
	}
	env := []string{fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(rules))}
	for i, r := range rules {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=url.sous-no-push::.%s", i, r.key), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, r.prefix))
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
// Holding the run's lock from the check to the launch means two quick
// replies start one watcher.
func (a *Agent) Reply(_, ref, answer string) error {
	dir, m, err := a.meta(ref)
	if err != nil {
		return err
	}
	if strings.TrimSpace(answer) == "" {
		return errors.New("the answer is empty")
	}
	lock, err := lockRun(dir)
	if errors.Is(err, errWorking) {
		return fmt.Errorf("run %d is still working; wait for it to ask", m.ID)
	} else if err != nil {
		return err
	}
	if m.Session == "" {
		lock.Close()
		return errors.New("the agent never started a session, so it cannot carry on; start a new run with the answer in the brief")
	}
	m.Answer = answer
	if err := writeJSON(dir, "run.json", m); err != nil {
		lock.Close()
		return err
	}
	os.Remove(filepath.Join(dir, "result.json"))
	return a.launch(dir, true, lock)
}

// Stop asks the watcher to stop the agent (it records "stopped"), and makes
// sure of it, reaching the agent's own group when the watcher is gone. It
// signals only while the run's lock is held, which only the watcher and
// the agent do, so a process that later got the same pid is never touched.
// Stopping a finished run changes nothing.
func (a *Agent) Stop(_, ref string) error {
	dir, _, err := a.meta(ref)
	if err != nil {
		return err
	}
	signal := func(file string, sig syscall.Signal, group bool) bool {
		pid, err := strconv.Atoi(readString(dir, file))
		if err != nil || pid <= 0 {
			return false
		}
		if group {
			pid = -pid
		}
		return syscall.Kill(pid, sig) == nil
	}
	for i := 0; i < 60 && watcherAlive(dir); i++ {
		switch i {
		case 0:
			if !signal("watcher.pid", syscall.SIGTERM, false) {
				signal("agent.pgid", syscall.SIGTERM, true) // no watcher: the agent carries on alone
			}
		case 40:
			signal("agent.pgid", syscall.SIGKILL, true)
			signal("watcher.pid", syscall.SIGKILL, false)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if watcherAlive(dir) {
		return errors.New("the watcher did not stop")
	}
	var r result
	if readJSON(dir, "result.json", &r) != nil {
		return writeJSON(dir, "result.json", result{Exit: -1, Stopped: true, Ended: time.Now().UTC()})
	}
	return nil
}

// Clean removes what a finished run left: its worktree (refused while
// work there is not committed), its branch only when it holds no work
// (git refuses to delete a branch it has not merged), and its folder.
// Cleaning twice is fine.
func (a *Agent) Clean(_, ref string) error {
	uid, err := a.uidOf(ref)
	if err != nil {
		return err
	}
	if _, err := os.Stat(a.dir(uid)); os.IsNotExist(err) {
		return nil // cleaned before
	}
	dir, m, err := a.meta(ref)
	if err != nil {
		return err
	}
	if watcherAlive(dir) {
		return fmt.Errorf("run %d is still working; stop it first", m.ID)
	}
	if _, err := os.Stat(m.Worktree); err == nil {
		if uncommitted(m.Worktree) {
			return fmt.Errorf("run %d has work not committed in %s; commit or copy it first, then clean", m.ID, m.Worktree)
		}
		if out, err := exec.Command("git", "-C", m.Project, "worktree", "remove", m.Worktree).CombinedOutput(); err != nil {
			return fmt.Errorf("removing the worktree: %s", strings.TrimSpace(string(out)))
		}
	}
	exec.Command("git", "-C", m.Project, "branch", "-d", m.Branch).Run()
	return os.RemoveAll(dir)
}
