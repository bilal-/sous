// Package backendtest checks a backend against the contract every backend
// shares, the way testing/fstest checks a file system. A built-in or a
// plugin's own tests call Run with a project the backend detects.
package backendtest

import (
	"errors"
	"io"
	"testing"

	"github.com/bilal-/sous/internal/backend"
)

// Run files two notes and walks the first through its life:
//
//   - detect claims the project
//   - file returns a ref, and filing the same id again returns the same ref
//     (crash recovery must never duplicate on a shared tracker)
//   - a different id gets a different ref
//   - a filed item is open; after close it is closed; closing twice is fine
//   - a ref this backend does not own is "unknown", never an error or closed
//   - url is a link or ErrUnsupported
func Run(t *testing.T, b backend.Implementation, project string) {
	t.Helper()
	if !b.Detect(project, io.Discard) {
		t.Fatal("detect: backend does not claim its own project")
	}
	req := backend.Request{ID: 41, Project: project, Text: "conformance note", Kind: "idea"}
	ref, err := b.File(req)
	if err != nil || ref == "" {
		t.Fatalf("file: %q, %v", ref, err)
	}
	if again, err := b.File(req); err != nil || again != ref {
		t.Fatalf("file is not idempotent on id: %q then %q (%v)", ref, again, err)
	}
	other, err := b.File(backend.Request{ID: 42, Project: project, Text: "another note", Kind: "me"})
	if err != nil || other == ref {
		t.Fatalf("a second id must get its own ref: %q vs %q (%v)", other, ref, err)
	}
	status := func(r, want string) {
		t.Helper()
		if got, err := b.Status(project, r); err != nil || got != want {
			t.Fatalf("status %s: %q, %v; want %q", r, got, err, want)
		}
	}
	status(ref, "open")
	if err := b.Close(project, ref); err != nil {
		t.Fatalf("close: %v", err)
	}
	status(ref, "closed")
	status(other, "open")
	if err := b.Close(project, ref); err != nil {
		t.Fatalf("closing a closed item: %v", err)
	}
	status("elsewhere:acme/other#1", "unknown")
	if u, err := b.URL(project, other); err != nil && !errors.Is(err, backend.ErrUnsupported) || err == nil && u == "" {
		t.Fatalf("url: %q, %v", u, err)
	}
}

// RunUnreachable checks the rule that matters most for trust: when the
// tracker cannot be reached, status is an error — never "unknown" (which
// reads as a missing item) and never "closed" (which would close the note).
// The caller files ref, then makes the tracker unreachable.
func RunUnreachable(t *testing.T, b backend.Implementation, project, ref string) {
	t.Helper()
	if st, err := b.Status(project, ref); err == nil {
		t.Fatalf("status with the tracker unreachable: %q and no error", st)
	}
}
