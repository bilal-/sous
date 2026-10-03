package runner

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type memRunner struct {
	started map[string]string // uid → ref
	state   Status
}

func (m *memRunner) Start(req Request) (string, error) {
	if ref, ok := m.started[req.UID]; ok {
		return ref, nil
	}
	ref := "mem:" + req.UID
	m.started[req.UID] = ref
	return ref, nil
}
func (m *memRunner) Status(string, string) (Status, error) { return m.state, nil }
func (m *memRunner) Reply(string, string, string) error    { return ErrUnsupported }
func (m *memRunner) Stop(string, string) error             { return nil }
func (m *memRunner) Clean(string, string) error            { return errors.New("worktree busy") }

func TestOpsSpeakTheContract(t *testing.T) {
	m := &memRunner{started: map[string]string{}, state: Status{State: Done, Text: "fixed"}}
	ops := ops(m)
	do := func(op string, in string, args ...string) (string, string, int) {
		var out, errb bytes.Buffer
		code := ops[op](args, strings.NewReader(in), &out, &errb)
		return out.String(), errb.String(), code
	}
	req := `{"v":0,"id":7,"uid":"u1","project":"/p","brief":"b"}`
	if out, _, code := do("start", req, "/p"); code != 0 || out != "mem:u1\n" {
		t.Fatalf("start: %q %d", out, code)
	}
	if out, _, _ := do("start", req, "/p"); out != "mem:u1\n" {
		t.Fatalf("start twice: %q", out)
	}
	if _, _, code := do("start", `{"v":1,"id":7,"uid":"u1","project":"/p","brief":"b"}`, "/p"); code != 2 {
		t.Fatal("newer v is exit 2")
	}
	if _, _, code := do("start", `{"v":0}`, "/p"); code != 2 {
		t.Fatal("missing fields is exit 2")
	}
	if out, _, code := do("status", "", "/p", "mem:u1"); code != 0 || out != `{"v":0,"state":"done","text":"fixed"}`+"\n" {
		t.Fatalf("status: %q", out)
	}
	if _, _, code := do("reply", "answer", "/p", "mem:u1"); code != 2 {
		t.Fatal("unsupported reply is exit 2")
	}
	if _, errs, code := do("clean", "", "/p", "mem:u1"); code != 1 || !strings.Contains(errs, "busy") {
		t.Fatalf("clean failure: %q %d", errs, code)
	}
	if _, _, code := do("status", "", "/p"); code != 2 {
		t.Fatal("usage is exit 2")
	}
}
