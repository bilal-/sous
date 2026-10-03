package runner

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/bilal-/sous/internal/plugin"
)

// Implementation is what a built in runner provides; Ops turns it into the
// contract's calls, so built ins take the same door as plugin programs.
// Reply and Clean may return ErrUnsupported.
type Implementation interface {
	Start(req Request) (string, error)
	Status(project, ref string) (Status, error)
	Reply(project, ref, answer string) error
	Stop(project, ref string) error
	Clean(project, ref string) error
}

// ops adapts an Implementation to the contract: usage errors exit 2,
// failures exit 1 with the reason, unsupported optional calls exit 2, not
// set up exits 3.
func ops(impl Implementation) map[string]plugin.Op {
	return map[string]plugin.Op{
		"start": func(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
			var req Request
			if code, ok := plugin.DecodeRequest(stdin, &req, func() int { return req.V },
				func() bool {
					return len(args) == 1 && req.ID > 0 && req.UID != "" && req.Project != "" && req.Brief != ""
				},
				"start", "start <project>, with JSON on stdin: v, id, uid, project, brief", stderr); !ok {
				return code
			}
			ref, err := impl.Start(req)
			if err == nil {
				fmt.Fprintln(stdout, ref)
			}
			return plugin.Exit(err, stderr)
		},
		"status": plugin.PrintCall("status", func(project, ref string) (string, error) {
			st, err := impl.Status(project, ref)
			if err != nil {
				return "", err
			}
			st.V = plugin.Version
			b, err := json.Marshal(st)
			return string(b), err
		}),
		"reply": plugin.RefCall("reply", func(project, ref string, stdin io.Reader, _ io.Writer) error {
			answer, err := io.ReadAll(stdin)
			if err != nil {
				return err
			}
			return impl.Reply(project, ref, string(answer))
		}),
		"stop":  plugin.RefCall("stop", func(project, ref string, _ io.Reader, _ io.Writer) error { return impl.Stop(project, ref) }),
		"clean": plugin.RefCall("clean", func(project, ref string, _ io.Reader, _ io.Writer) error { return impl.Clean(project, ref) }),
	}
}
