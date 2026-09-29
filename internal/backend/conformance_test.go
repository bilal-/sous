package backend_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bilal-/sous/internal/backend"
	"github.com/bilal-/sous/internal/backend/backendtest"
	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/testutil"
	"github.com/bilal-/sous/internal/tracker"
)

func TestMarkdownConforms(t *testing.T) {
	p := t.TempDir()
	os.WriteFile(filepath.Join(p, backend.MarkdownFile), []byte("# Follow-ups\n"), 0o644)
	backendtest.Run(t, backend.Markdown{}, p)
}

// stateDir holds a fake tracker's issues: <n>.marker and <n>.state.
const ghFake = `echo "A new release of gh is available" >&2
s=$STATE
case "$1 $2" in
  "auth status") exit 0;;
  "issue list")
    printf '['; sep=
    for f in "$s"/*.marker; do [ -e "$f" ] || continue
      printf '%s{"number":%s,"body":"%s"}' "$sep" "$(basename "$f" .marker)" "$(cat "$f")"; sep=,
    done; printf ']';;
  "issue create")
    while [ $# -gt 0 ]; do [ "$1" = -b ] && body=$2; shift; done
    n=$(( $(ls "$s" | grep -c marker) + 1 ))
    printf '%s' "$body" | grep -o '<!-- sous:[^ ]* -->' > "$s/$n.marker"; echo OPEN > "$s/$n.state"
    echo "https://github.com/acme/chime/issues/$n";;
  "issue view")
    [ -e "$s/$3.state" ] || { echo "GraphQL: Could not resolve to an issue or pull request with the number of $3. (repository.issue)" >&2; exit 1; }
    printf '{"state":"%s","url":"https://github.com/acme/chime/issues/%s"}' "$(cat "$s/$3.state")" "$3";;
  "issue close") echo CLOSED > "$s/$3.state";;
  *) echo "unexpected: $*" >&2; exit 1;;
esac`

const glabFake = `echo "DEPRECATION WARNING: x" >&2
s=$STATE
[ "$1" = --hostname ] && shift 2
[ "$1 $2" = "auth status" ] && { echo "git.example.org"; exit 0; }
[ "$1" = api ] || { echo "unexpected: $*" >&2; exit 1; }
shift
method=GET; [ "$1" = -X ] && { method=$2; shift 2; }
path=$1; shift
while [ $# -gt 0 ]; do case "$2" in description=*) body=${2#description=};; esac; shift; done
case "$method $path" in
  "GET "*"issues?scope=created_by_me"*)
    printf '['; sep=
    for f in "$s"/*.marker; do [ -e "$f" ] || continue
      printf '%s{"iid":%s,"description":"%s"}' "$sep" "$(basename "$f" .marker)" "$(cat "$f")"; sep=,
    done; printf ']';;
  "POST "*/issues)
    n=$(( $(ls "$s" | grep -c marker) + 1 ))
    printf '%s' "$body" | grep -o '<!-- sous:[^ ]* -->' > "$s/$n.marker"; echo opened > "$s/$n.state"
    printf '{"iid":%s,"web_url":"https://git.example.org/acme/chime/-/issues/%s"}' "$n" "$n";;
  "PUT "*"state_event=close") n=${path%%\?*}; n=${n##*/}; echo closed > "$s/$n.state";;
  "GET "*/issues/*)
    n=${path##*/}
    [ -e "$s/$n.state" ] || { echo '{"message":"404 Not found"}' >&2; exit 1; }
    printf '{"iid":%s,"state":"%s","web_url":"https://git.example.org/acme/chime/-/issues/%s"}' "$n" "$(cat "$s/$n.state")" "$n";;
  *) echo "unexpected: $method $path" >&2; exit 1;;
esac`

// remoteFixture: a project on remote, and a stateful fake of tool in PATH.
func remoteFixture(t *testing.T, tool, script, remote string) (home, project string) {
	tracker.ResetCache()
	t.Cleanup(tracker.ResetCache)
	t.Setenv("STATE", t.TempDir())
	testutil.FakeBin(t, tool, script)
	home = t.TempDir()
	os.WriteFile(filepath.Join(home, "install-id"), []byte("abcd1234\n"), 0o644)
	project = testutil.Repo(t, filepath.Join(t.TempDir(), "acme", "chime"), true, remote)
	return home, project
}

// The real GitHub and GitLab tables, against stateful fakes of their CLIs,
// then with the CLI unreachable.
func TestRemoteTrackersConform(t *testing.T) {
	for _, c := range []struct {
		tool, fake, remote, down, ref string
		make                          func(home string) backend.Implementation
	}{
		{"gh", ghFake, "git@github.com:acme/chime.git",
			`echo "error connecting to api.github.com" >&2; exit 1`, "github:acme/chime#1",
			func(home string) backend.Implementation { return backend.GitHub(home, &config.Config{}) }},
		{"glab", glabFake, "https://git.example.org/acme/chime.git",
			`[ "$2 $3" = "auth status" ] && { echo git.example.org; exit 0; }; echo "dial tcp: lookup git.example.org: no such host" >&2; exit 1`, "gitlab:git.example.org/acme/chime#1",
			func(home string) backend.Implementation { return backend.GitLab(home, &config.Config{}) }},
	} {
		t.Run(c.tool, func(t *testing.T) {
			home, p := remoteFixture(t, c.tool, c.fake, c.remote)
			b := c.make(home)
			backendtest.Run(t, b, p)
			testutil.FakeBin(t, c.tool, c.down)
			backendtest.RunUnreachable(t, b, p, c.ref)
		})
	}
}
