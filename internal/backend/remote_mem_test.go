package backend_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/backend/backendtest"
	"github.com/bilal-/sous/internal/plugin"
	"github.com/bilal-/sous/internal/tracker"
)

type memCLI struct {
	located  map[string]backend.Target // project → target
	issues   map[string][]backend.Issue
	listErr  error
	created  int
	viewErrs map[string]error
	seen     []backend.Target
}

func (m *memCLI) Name() string { return "mem" }
func (m *memCLI) Locate(p string) (backend.Target, error) {
	if t, ok := m.located[p]; ok {
		return t, nil
	}
	return backend.Target{}, plugin.ErrNo
}
func (m *memCLI) TargetFromRef(ref tracker.Ref) backend.Target {
	return backend.Target{Host: ref.Host, Repo: ref.Repo, Identity: "from-ref"}
}
func (m *memCLI) ListMine(t backend.Target) ([]backend.Issue, error) {
	m.seen = append(m.seen, t)
	return m.issues[t.Repo], m.listErr
}
func (m *memCLI) Create(t backend.Target, title, body string) (backend.Issue, error) {
	m.created++
	is := backend.Issue{Number: fmt.Sprint(100 + m.created), Body: body}
	m.issues[t.Repo] = append(m.issues[t.Repo], is)
	return is, nil
}
func (m *memCLI) View(t backend.Target, n string) (backend.Issue, error) {
	m.seen = append(m.seen, t)
	if err, ok := m.viewErrs[n]; ok {
		return backend.Issue{}, err
	}
	for _, is := range m.issues[t.Repo] {
		if is.Number == n {
			if is.State == "" {
				is.State = "open"
			}
			return is, nil
		}
	}
	return backend.Issue{}, backend.ErrMissing
}
func (m *memCLI) Close(t backend.Target, n string) error {
	m.seen = append(m.seen, t)
	for i, is := range m.issues[t.Repo] {
		if is.Number == n {
			m.issues[t.Repo][i].State = "closed"
		}
	}
	return nil
}

func TestRemoteLogicAgainstMemoryCLI(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "install-id"), []byte("abcd1234\n"), 0o644)
	cli := &memCLI{located: map[string]backend.Target{"/ws/acme/billing": {Host: "mem", Repo: "studio/billing", Identity: "acct-exact"}}, issues: map[string][]backend.Issue{}, viewErrs: map[string]error{}}
	r := backend.Remote{CLI: cli, Home: home}

	ref, err := r.File(backend.Request{ID: 7, UID: "000000000007", Project: "/ws/acme/billing", Text: "x", Kind: "me"})
	if err != nil || ref != "mem:mem/studio/billing#101" || cli.created != 1 {
		t.Fatal(ref, err, cli.created)
	}
	// Crash retry: marker found in a body → same ref, nothing created.
	if ref2, _ := r.File(backend.Request{ID: 7, UID: "000000000007", Project: "/ws/acme/billing", Text: "x", Kind: "me"}); ref2 != ref || cli.created != 1 {
		t.Fatal("recovery must reuse the issue")
	}
	// Lookup failure fails closed.
	cli.listErr = errors.New("502")
	if _, err := r.File(backend.Request{ID: 8, UID: "000000000008", Project: "/ws/acme/billing", Text: "y", Kind: "me"}); err == nil || cli.created != 1 {
		t.Fatal("must not create when the lookup failed")
	}
	cli.listErr = nil
	// Status/close/url use the *project's* identity, not one
	// guessed from the ref's owner.
	cli.seen = nil
	if st, err := r.Status("/ws/acme/billing", ref); err != nil || st != "open" {
		t.Fatal(st, err)
	}
	if err := r.Close("/ws/acme/billing", ref); err != nil {
		t.Fatal(err)
	}
	for _, s := range cli.seen {
		if s.Identity != "acct-exact" {
			t.Fatalf("identity must come from the project, got %+v", s)
		}
	}
	// Unknown project (moved/deleted): fall back to the ref.
	cli.seen = nil
	r.Status("/nowhere", ref)
	if len(cli.seen) != 1 || cli.seen[0].Identity != "from-ref" {
		t.Fatalf("fallback to ref-derived target: %+v", cli.seen)
	}
	cli.viewErrs["101"] = fmt.Errorf("%w: 404", backend.ErrInvisible)
	if _, err := r.Status("/ws/acme/billing", ref); err == nil || !strings.Contains(err.Error(), "acct-exact") {
		t.Fatalf("invisible names the identity: %v", err)
	}
	cli.viewErrs["101"] = backend.ErrMissing
	if st, err := r.Status("/ws/acme/billing", ref); err != nil || st != "unknown" {
		t.Fatal(st, err)
	}
}

func TestRemoteConforms(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "install-id"), []byte("abcd1234\n"), 0o644)
	cli := &memCLI{located: map[string]backend.Target{"/ws/acme/chime": {Host: "mem", Repo: "acme/chime", Identity: "acct"}}, issues: map[string][]backend.Issue{}, viewErrs: map[string]error{}}
	backendtest.Run(t, backend.Remote{CLI: cli, Home: home}, "/ws/acme/chime")
}

// Remote markers carry the note's uid when there is one.
func TestRemoteMarkerUsesUID(t *testing.T) {
	cli := &memCLI{located: map[string]backend.Target{"/ws/acme/api": {Host: "mem", Repo: "acme/api"}}, issues: map[string][]backend.Issue{}, viewErrs: map[string]error{}}
	r := backend.Remote{CLI: cli, Home: t.TempDir()}
	if _, err := r.File(backend.Request{ID: 3, UID: "0123456789ab", Project: "/ws/acme/api", Text: "x", Kind: "me"}); err != nil {
		t.Fatal(err)
	}
	if body := cli.issues["acme/api"][0].Body; !strings.Contains(body, "<!-- sous:0123456789ab -->") {
		t.Fatalf("%q", body)
	}
}

// An issue created before upgrading carries the old
// <install>:<id> marker; the retry after upgrading must find it.
func TestRemoteRecoveryFindsAPreUpgradeMarker(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "install-id"), []byte("abcd1234\n"), 0o644)
	cli := &memCLI{located: map[string]backend.Target{"/ws/acme/api": {Host: "mem", Repo: "acme/api"}},
		issues:   map[string][]backend.Issue{"acme/api": {{Number: "40", Body: "x\n\n<!-- sous:abcd1234:3 -->"}}},
		viewErrs: map[string]error{}}
	r := backend.Remote{CLI: cli, Home: home}
	ref, err := r.File(backend.Request{ID: 3, UID: "0123456789ab", Legacy: true, Project: "/ws/acme/api", Text: "x", Kind: "me"})
	if err != nil || !strings.HasSuffix(ref, "#40") || cli.created != 0 {
		t.Fatalf("%q %v created=%d", ref, err, cli.created)
	}
}
