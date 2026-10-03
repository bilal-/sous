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

// Ops adapts an Implementation to the contract: usage errors exit 2,
// failures exit 1 with the reason, unsupported optional calls exit 2, not
// set up exits 3.
func Ops(impl Implementation) map[string]plugin.Op {
	return map[string]plugin.Op{
		"start": func(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
			var req Request
			if len(args) != 1 || json.NewDecoder(stdin).Decode(&req) != nil || req.ID <= 0 || req.UID == "" || req.Project == "" || req.Brief == "" {
				return plugin.Usage(stderr, "start <project>, with JSON on stdin: v, id, uid, project, brief")
			}
			if req.V != 0 {
				fmt.Fprintf(stderr, "start: contract v%d is newer than this runner speaks (v0)\n", req.V)
				return plugin.ExitRefused
			}
			ref, err := impl.Start(req)
			if err == nil {
				fmt.Fprintln(stdout, ref)
			}
			return plugin.Exit(err, stderr)
		},
		"status": plugin.RefCall("status", func(project, ref string, _ io.Reader, stdout io.Writer) error {
			st, err := impl.Status(project, ref)
			if err != nil {
				return err
			}
			st.V = 0
			b, err := json.Marshal(st)
			if err == nil {
				fmt.Fprintf(stdout, "%s\n", b)
			}
			return err
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
