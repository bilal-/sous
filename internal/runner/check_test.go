package runner

import (
	"context"
	"strings"
	"testing"
)

// goodRunner keeps its one run's state in files beside it.
const goodRunner = `describe) echo '{"v":0,"calls":["start","status","stop"],"fields":["phase"]}';;
start) cat > /dev/null; echo fake:1;;
status) if [ -f "$(dirname "$0")/stopped" ]; then echo '{"v":0,"state":"failed","phase":"stopped"}'; else echo '{"v":0,"state":"running","phase":"working"}'; fi;;
stop) touch "$(dirname "$0")/stopped";;`

func TestCheckPassesAGoodRunner(t *testing.T) {
	r := fakeRunner(t, goodRunner)
	if got := Check(context.Background(), r, t.TempDir(), CheckOptions{}); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestCheckFindsBrokenRunners(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"new ref on retry": {`start) cat > /dev/null; date +fake:%N;;
status) echo '{"v":0,"state":"running"}';;
stop) ;;`, "same uid"},
		"ref without its name": {`start) cat > /dev/null; echo other:1;;
status) echo '{"v":0,"state":"running"}';;
stop) ;;`, "must start with"},
		"no status": {`start) cat > /dev/null; echo fake:1;;
status) echo nope;;
stop) ;;`, "status"},
		"stop refused": {`start) cat > /dev/null; echo fake:1;;
status) echo '{"v":0,"state":"running"}';;
stop) exit 2;;`, "stop"},
		"working after stop": {`describe) echo '{"v":0,"calls":["start","status","stop"],"fields":["phase"]}';;
start) cat > /dev/null; echo fake:1;;
status) echo '{"v":0,"state":"running","phase":"working"}';;
stop) ;;`, "after stop"},
	} {
		got := strings.Join(Check(context.Background(), fakeRunner(t, tc.body), t.TempDir(), CheckOptions{}), "\n")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q in %q", name, tc.want, got)
		}
	}
}
