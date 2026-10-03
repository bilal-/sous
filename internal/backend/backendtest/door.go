package backendtest

import (
	"context"
	"io"
	"testing"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/plugin"
)

// Door drives a backend through the executable contract — argv, stdin,
// stdout, exit codes — exactly as sous does, so Run can check a built-in
// re-exec or a third-party sous-backend-<name> end to end.
func Door(b backend.Backend) backend.Implementation { return door{b} }

type door struct{ b backend.Backend }

func (d door) Detect(project string) error {
	got, err := backend.Detect(context.Background(), []backend.Backend{d.b}, project, "", io.Discard)
	if err != nil {
		return err
	}
	if got.Name != d.b.Name {
		return plugin.ErrNo
	}
	return nil
}

func (d door) File(req backend.Request) (string, error) {
	return backend.File(context.Background(), d.b, req)
}

func (d door) Status(project, ref string) (string, error) {
	return backend.Status(context.Background(), d.b, project, ref)
}

func (d door) Close(project, ref string) error {
	return backend.Close(context.Background(), d.b, project, ref)
}

func (d door) URL(project, ref string) (string, error) {
	u, ok, err := backend.URL(context.Background(), d.b, project, ref)
	if err == nil && !ok {
		return "", backend.ErrUnsupported
	}
	return u, err
}

// RunDoor is Run through the executable door, plus the door's own rule: a
// request in a newer contract version is refused, not guessed at.
func RunDoor(t *testing.T, b backend.Backend, project string) {
	t.Helper()
	Run(t, Door(b), project)
	if ref, err := backend.File(context.Background(), b, backend.Request{V: 1, ID: 43, UID: "000000000043", Project: project, Text: "from the future", Kind: "idea"}); err == nil {
		t.Fatalf("a v1 request was accepted: %q", ref)
	}
}
