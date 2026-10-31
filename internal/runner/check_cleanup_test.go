package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckStopsAfterStatusFailure(t *testing.T) {
	for _, phase := range []string{"initial", "poll"} {
		t.Run(phase, func(t *testing.T) {
			status := `status) echo malformed;;`
			if phase == "poll" {
				status = `status)
if [ -f "$(dirname "$0")/polled" ]; then
    echo malformed
else
    touch "$(dirname "$0")/polled"
    echo '{"v":0,"state":"running"}'
fi;;`
			}
			r := fakeRunner(t, `describe) echo '{"v":0,"calls":["start","status","stop"]}';;
start) cat >/dev/null; echo fake:1;;
stop) touch "$(dirname "$0")/stopped";;
`+status)
			problems := Check(context.Background(), r, t.TempDir(), CheckOptions{Wait: time.Second})
			if !strings.Contains(strings.Join(problems, "\n"), "status") {
				t.Fatalf("check lost status diagnostic: %v", problems)
			}
			marker := filepath.Join(filepath.Dir(r.Argv[0]), "stopped")
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("check left its started run active after %s status failure; problems=%v; stop marker: %v", phase, problems, err)
			}
		})
	}
}

func TestCheckCleanupSurvivesCancellation(t *testing.T) {
	r := fakeRunner(t, goodRunner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	problems := Check(ctx, r, t.TempDir(), CheckOptions{Finish: func(string) { cancel() }})
	if !strings.Contains(strings.Join(problems, "\n"), "status") {
		t.Fatalf("lost status diagnostic: %v", problems)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.Argv[0]), "stopped")); err != nil {
		t.Fatalf("cancelled Check did not stop its started run: %v", problems)
	}
}

func TestCheckRetainsPrimaryAndCleanupFailures(t *testing.T) {
	r := fakeRunner(t, `
describe) echo '{"v":0,"calls":["start","status","stop"]}';;
start) cat >/dev/null; echo fake:1;;
status) echo broken-status >&2; exit 1;;
stop) echo broken-stop >&2; exit 1;;
clean) touch "$(dirname "$0")/cleaned";;
`)
	problems := strings.Join(Check(context.Background(), r, t.TempDir(), CheckOptions{}), "\n")
	if !strings.Contains(problems, "broken-status") || !strings.Contains(problems, "broken-stop") {
		t.Fatalf("primary or cleanup failure was lost: %s", problems)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.Argv[0]), "cleaned")); !os.IsNotExist(err) {
		t.Fatal("Check cleaned a run whose work must be preserved")
	}
}

func TestCheckStillStopsExactlyTwiceNormally(t *testing.T) {
	r := fakeRunner(t, strings.Replace(goodRunner, `stop) touch`, `stop) echo stop >> "$(dirname "$0")/stops"; touch`, 1))
	if problems := Check(context.Background(), r, t.TempDir(), CheckOptions{}); len(problems) != 0 {
		t.Fatal(problems)
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(r.Argv[0]), "stops"))
	if err != nil || strings.Count(string(b), "stop\n") != 2 {
		t.Fatalf("normal stop calls: %q %v", b, err)
	}
}
