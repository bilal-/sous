package filing

import (
	"errors"
	"testing"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/thread"
)

func TestTicketRefTakesIssuesAndRefusesTheRest(t *testing.T) {
	f := &Filer{Backends: []backend.Backend{{Name: "jira", Argv: []string{"/bin/true"}}}}
	for in, want := range map[string]string{
		"https://github.com/acme/api/issues/12":     "github:acme/api#12",
		"github:acme/api#12":                        "github:acme/api#12",
		" https://gitlab.com/acme/api/-/issues/40 ": "gitlab:gitlab.com/acme/api#40",
		"gitlab:git.example.org/acme/api#4":         "gitlab:git.example.org/acme/api#4",
		"jira:jira.example.org/OPS#7":               "jira:jira.example.org/OPS#7",
	} {
		got, err := f.TicketRef(in)
		if err != nil || got != want {
			t.Errorf("TicketRef(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"https://github.com/acme/api/pull/9", "gitlab:git.example.org/acme/api!3", "https://example.org/acme/api/issues/1",
		"md:FOLLOWUPS.md:0123456789ab", "linear:ENG-4", "12", "", "local:x"} {
		if got, err := f.TicketRef(bad); !errors.As(err, new(thread.ValidationError)) {
			t.Errorf("TicketRef(%q) = %q, %v; want a refusal", bad, got, err)
		}
	}
}
