package tracker

import (
	"os"
	"path/filepath"
	"testing"
)

// Gitlab.com-work is an alias like github.com-work; nested
// includes are read; an Include under Match is not; renaming a dotted host
// only lands on a tracker host, never a port trick like altssh.gitlab.com.
func TestSSHAliasesRound4(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	os.MkdirAll(ssh, 0o700)
	os.WriteFile(filepath.Join(ssh, "config"), []byte(`Include a.conf
Match host secret
  Include m.conf
Host gitlab.com-work
  HostName gitlab.com
Host gitlab.com
  HostName altssh.gitlab.com
Host git.example.org
  HostName 10.0.0.5
`), 0o600)
	os.WriteFile(filepath.Join(ssh, "a.conf"), []byte("Include b.conf\n"), 0o600)
	os.WriteFile(filepath.Join(ssh, "b.conf"), []byte("Host work\n  HostName github.com\n"), 0o600)
	os.WriteFile(filepath.Join(ssh, "m.conf"), []byte("Host sneaky\n  HostName github.com\n"), 0o600)
	Init(home, t.TempDir(), os.Environ())
	t.Cleanup(func() { Init("", "", nil) })
	for in, want := range map[string]string{
		"gitlab.com-work/a/b": "gitlab.com",
		"work/a/b":            "github.com",
		"sneaky/a/b":          "",
		"gitlab.com/a/b":      "gitlab.com",
		"git.example.org/a/b": "git.example.org",
	} {
		if host, _ := ParseRemote(in); host != want {
			t.Errorf("%q → %q, want %q", in, host, want)
		}
	}
}
