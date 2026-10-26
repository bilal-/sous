package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/runner"
	"github.com/bilal-/sous/internal/runs"
	"github.com/bilal-/sous/internal/thread"
)

// Version 1's actions a host runs with an op it made. Each is done once per
// op (runs.Ops), however often the host asks, and answers with one outcome.
var integrationActs = map[string]func(e *Env, target string, in []byte, a argv) (string, runs.Outcome){
	"run":   actRun,
	"stop":  actStop,
	"reply": actReply,
}

// The most a host may send on standard input, by action.
var actInputMax = map[string]int{"run": 16 << 10, "reply": 4 << 10}

// outcomeExit is each outcome's exit code.
var outcomeExit = map[string]int{
	runs.Done: 0, runs.AlreadyDone: 0, runs.Refused: exitUsage, runs.Failed: exitFailed,
	runs.NotReady: exitNotReady, runs.Conflict: exitConflict, runs.InProgress: exitInProgress,
}

const (
	exitConflict   = 4 // what the action names moved on
	exitInProgress = 5 // the same op is being done right now
)

// cmdIntegrationAct is `sous integration --protocol 1 <action> --op <op>
// [--question <id>:<token>] -- <target>`, the target a project for run and a
// note's number otherwise; run's brief and reply's answer come on standard
// input.
func cmdIntegrationAct(e *Env, a argv, v int, action string) int {
	act := integrationActs[action]
	usage := fmt.Sprintf("usage: sous integration --protocol 1 %s --op <op> -- <target>", action)
	switch {
	case v != integration.Version1:
		return fail(e, exitUsage, "%s is a version 1 action; ask with --protocol 1\n%s", action, usage)
	case a.value("op") == "" || len(a.pos) != 2:
		return fail(e, exitUsage, "%s", usage)
	case a.has("question") && action != "reply":
		return fail(e, exitUsage, "--question goes with reply\n%s", usage)
	}
	op, target := a.value("op"), a.pos[1]
	var in []byte
	if max, ok := actInputMax[action]; ok {
		b, err := io.ReadAll(io.LimitReader(e.Stdin, int64(max)+1))
		if err != nil {
			return fail(e, exitFailed, "reading standard input: %v", err)
		}
		in = b
	}
	var id string
	o := runs.Ops{Store: e.store(), Now: time.Now}.Do(op, action, opHash(action, target, a.value("question"), in), func() runs.Outcome {
		if max, ok := actInputMax[action]; ok && (len(in) > max || strings.TrimSpace(string(in)) == "") {
			return runs.Outcome{Outcome: runs.Refused, Reason: "bad_input"}
		}
		var out runs.Outcome
		id, out = act(e, target, in, a)
		return out
	})
	if o.ID == "" {
		o.ID = id
	}
	if e.JSON {
		e.writeJSON(struct {
			Kind   string `json:"kind"`
			V      int    `json:"v"`
			Op     string `json:"op"`
			Action string `json:"action"`
			runs.Outcome
		}{"outcome", integration.Version1, op, action, o})
	} else {
		line := action + " " + strings.ReplaceAll(o.Outcome, "_", " ")
		if o.ID != "" {
			line += " · " + o.ID
		}
		if o.Reason != "" {
			line += " · " + o.Reason
		}
		fmt.Fprintln(e.Stdout, line)
	}
	return outcomeExit[o.Outcome]
}

// opHash is what an op asked for: the action, its target, and its input,
// byte for byte.
func opHash(parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(fmt.Sprint(p)))
		switch p := p.(type) {
		case []byte:
			h.Write(p)
		default:
			fmt.Fprint(h, p)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// failed is an outcome for an error the action could not get past.
func failed(step string, err error) runs.Outcome {
	return runs.Outcome{Outcome: runs.Failed, Reason: step + ": " + err.Error()}
}

func actRun(e *Env, target string, in []byte, a argv) (string, runs.Outcome) {
	cfg, code := e.config()
	if code != 0 {
		return "", runs.Outcome{Outcome: runs.NotReady, Reason: "config"}
	}
	p, code := resolveProject(e, target)
	if code != 0 {
		return "", runs.Outcome{Outcome: runs.Refused, Reason: "bad_input"}
	}
	if cfg.Project(project.OrgName(p.Path)).RemoteRuns != "true" {
		return "", runs.Outcome{Outcome: runs.Refused, Reason: "not_allowed_project"}
	}
	_, hereFile := hereContext(e, p, io.Discard)
	id, _, err := e.dispatcher().Start(e.ctx(), p, string(in), a.value("op"), cfg.RunAgent(), e.Source, hereFile)
	n := ""
	if id > 0 {
		n = strconv.Itoa(id)
	}
	switch {
	case err == nil:
		return n, runs.Outcome{Outcome: runs.Done, ID: n}
	case errors.Is(err, runs.ErrNoRunner), errors.Is(err, runner.ErrNotSetUp):
		return n, runs.Outcome{Outcome: runs.NotReady, ID: n, Reason: "runner: " + err.Error()}
	case errors.As(err, new(thread.ValidationError)):
		return n, runs.Outcome{Outcome: runs.Refused, Reason: "bad_input"}
	}
	o := failed("start", err)
	o.ID = n
	return n, o
}

// noteOf is the open note an action names, or the outcome refusing it.
func noteOf(e *Env, target string) (int, *runs.Outcome) {
	id, err := strconv.Atoi(target)
	if err != nil || id <= 0 {
		return 0, &runs.Outcome{Outcome: runs.Refused, Reason: "bad_input"}
	}
	if _, code := e.config(); code != 0 {
		return 0, &runs.Outcome{Outcome: runs.NotReady, Reason: "config"}
	}
	return id, nil
}

// runErr turns an error about a note's run into an outcome.
func runErr(id int, step string, err error) runs.Outcome {
	n := strconv.Itoa(id)
	switch {
	case errors.Is(err, thread.ErrNotFound), errors.Is(err, runs.ErrNotARun):
		return runs.Outcome{Outcome: runs.Refused, ID: n, Reason: "not_a_run"}
	case errors.Is(err, runs.ErrStale):
		return runs.Outcome{Outcome: runs.Conflict, ID: n, Reason: "stale_question"}
	case errors.Is(err, runs.ErrNotWaiting):
		return runs.Outcome{Outcome: runs.Conflict, ID: n, Reason: "wrong_phase"}
	case errors.Is(err, runner.ErrUnsupported):
		return runs.Outcome{Outcome: runs.Refused, ID: n, Reason: "unsupported"}
	}
	o := failed(step, err)
	o.ID = n
	return o
}

func actStop(e *Env, target string, _ []byte, _ argv) (string, runs.Outcome) {
	id, refused := noteOf(e, target)
	if refused != nil {
		return "", *refused
	}
	if err := e.dispatcher().Stop(e.ctx(), id); err != nil {
		return target, runErr(id, "stop", err)
	}
	return target, runs.Outcome{Outcome: runs.Done, ID: target}
}

func actReply(e *Env, target string, in []byte, a argv) (string, runs.Outcome) {
	id, refused := noteOf(e, target)
	if refused != nil {
		return "", *refused
	}
	q := a.value("question")
	if !strings.Contains(q, ":") {
		return target, runs.Outcome{Outcome: runs.Refused, ID: target, Reason: "bad_input"}
	}
	if err := e.dispatcher().ReplyTo(e.ctx(), id, q, string(in), a.value("op")); err != nil {
		return target, runErr(id, "reply", err)
	}
	return target, runs.Outcome{Outcome: runs.Done, ID: target}
}
