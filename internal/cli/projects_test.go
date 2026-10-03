package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/project"
)

func TestRenderProjects(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	r := "gitlab.com/o/r"
	lc := now.Add(-2 * 24 * time.Hour)
	var b bytes.Buffer
	renderProjects(&b, []project.Project{{Org: "o", Name: "r", Remote: &r, LastCommit: &lc}, {Org: "personal", Name: "local-only"}}, now)
	out := b.String()
	if !strings.Contains(out, "gitlab") || !strings.Contains(out, "2d") || !strings.Contains(out, "local") || !strings.Contains(out, "no commits") {
		t.Fatalf("%s", out)
	}
}

func TestRenderTableHasHeaderAndNeverBlankHost(t *testing.T) {
	odd := "/srv/mirrors/api"
	var b bytes.Buffer
	renderProjects(&b, []project.Project{{Org: "acme", Name: "api", Remote: &odd}}, time.Now())
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "org") || !strings.Contains(lines[0], "host") || !strings.Contains(lines[1], "other") {
		t.Fatalf("%q", b.String())
	}
}
