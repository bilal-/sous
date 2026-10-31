package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestApproveRejectsNewerResponseVersion(t *testing.T) {
	r := fakeRunner(t, `approve) cat >/dev/null; echo '{"v":1,"branch":"sous/run-1","commit":"abc","base":"base"}';;`)
	ap, err := Approve(context.Background(), r, "/code/acme/api", "fake:1", "abc", "")
	if err == nil || !strings.Contains(err.Error(), "v1") {
		t.Fatalf("unsupported approval accepted: %+v %v", ap, err)
	}
}

func TestApproveAcceptsCurrentAndLegacyResponses(t *testing.T) {
	for _, version := range []string{`"v":0,`, ""} {
		t.Run(version, func(t *testing.T) {
			r := fakeRunner(t, `approve) cat >/dev/null; echo '{`+version+`"branch":"sous/run-1","commit":"abc","base":"base"}';;`)
			ap, err := Approve(context.Background(), r, "/code/acme/api", "fake:1", "abc", "")
			if err != nil || ap.Commit != "abc" || ap.Branch != "sous/run-1" {
				t.Fatalf("current approval rejected: %+v %v", ap, err)
			}
		})
	}
}

type approvalRecorder struct {
	*memRunner
	approvals int
}

func (m *approvalRecorder) Approve(project, ref, commit string) (Approval, error) {
	m.approvals++
	return m.memRunner.Approve(project, ref, commit)
}

func TestApproveDoorRejectsInvalidRequestsBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		args        []string
		diagnostic  string
	}{
		{"newer version", `{"v":1,"commit":"abc"}`, []string{"/code/acme/api", "mem:u1"}, "v1"},
		{"malformed JSON", `{`, []string{"/code/acme/api", "mem:u1"}, "usage:"},
		{"missing commit", `{"v":0}`, []string{"/code/acme/api", "mem:u1"}, "usage:"},
		{"wrong arguments", `{"v":0,"commit":"abc"}`, []string{"/code/acme/api"}, "usage: approve <project> <ref>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &approvalRecorder{memRunner: &memRunner{}}
			var out, errb bytes.Buffer
			code := ops(m)["approve"](tc.args, strings.NewReader(tc.input), &out, &errb)
			if code != 2 || m.approvals != 0 || out.Len() != 0 || !strings.Contains(errb.String(), tc.diagnostic) {
				t.Fatalf("invalid approval must fail before mutation: code=%d calls=%d out=%q err=%q", code, m.approvals, out.String(), errb.String())
			}
		})
	}
}

func TestApproveDoorAcceptsCurrentAndLegacyRequests(t *testing.T) {
	for _, input := range []string{`{"v":0,"commit":"abc"}`, `{"commit":"abc"}`} {
		t.Run(input, func(t *testing.T) {
			m := &approvalRecorder{memRunner: &memRunner{}}
			var out, errb bytes.Buffer
			code := ops(m)["approve"]([]string{"/code/acme/api", "mem:u1"}, strings.NewReader(input), &out, &errb)
			var ap Approval
			if code != 0 || m.approvals != 1 || json.Unmarshal(out.Bytes(), &ap) != nil || ap.V != 0 || ap.Commit != "abc" || ap.Branch != "sous/run-1" || errb.Len() != 0 {
				t.Fatalf("current approval refused: code=%d calls=%d out=%q err=%q", code, m.approvals, out.String(), errb.String())
			}
		})
	}
}
