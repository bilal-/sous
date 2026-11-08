package project

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFollowupsReadsOpenBoxesSousDidNotFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, FollowupsFile, `# Follow-ups

- [ ] ask Sam about the cert
- [x] ship the pricing page
* [ ] retry the upload on 503
  - [ ] nested one counts too
- [ ] filed by sous already <!-- sous:0123456789ab -->
- [ ]
not a box
`)
	got, at, err := Followups(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ask Sam about the cert", "retry the upload on 503", "nested one counts too"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if at.IsZero() {
		t.Fatal("no change time")
	}
}

func TestFollowupsWithoutAFileIsNone(t *testing.T) {
	got, _, err := Followups(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestFollowupsUnreadableIsAnError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, FollowupsFile, "- [ ] x\n")
	os.Chmod(filepath.Join(dir, FollowupsFile), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, FollowupsFile), 0o644) })
	if os.Getuid() == 0 {
		t.Skip("root reads anything")
	}
	if _, _, err := Followups(dir); err == nil {
		t.Fatal("an unreadable file read as empty")
	}
}

func TestNextStep(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"heading", "# Status\n\nWorks.\n\n## Next step\n\n- wire the widget to the data service\n- then the layout\n", "wire the widget to the data service"},
		{"plural heading, numbered", "## Next steps\n1. cut the release\n", "cut the release"},
		{"short heading, plain line", "### Next\nreview Sam's copy\n", "review Sam's copy"},
		{"next up", "## Next up\n- [ ] migrate the cache\n", "migrate the cache"},
		{"inline", "Blockers: none\n**Next step:** ask Sam for the cert\n", "ask Sam for the cert"},
		{"inline short", "Next: deploy to staging\n", "deploy to staging"},
		{"another heading ends it", "## Next step\n\n## Done\n- old\n", ""},
		{"none", "# Status\nall good\n", ""},
		{"not a next heading", "## Nextjs upgrade\n- bump it\n", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, StatusFile, c.body)
			got, _, err := NextStep(dir)
			if err != nil || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
		})
	}
	if got, _, err := NextStep(t.TempDir()); got != "" || err != nil {
		t.Fatalf("no file: got %q, %v", got, err)
	}
}
