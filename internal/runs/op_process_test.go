package runs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

// This helper is a request process, never an actual runner or agent. Holding
// stdin open holds its callback after the durable reservation has been made.
func TestOpsRequestProcessHelper(t *testing.T) {
	if os.Getenv("SOUS_OP_REQUEST_TEST") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) != 5 {
		fmt.Fprintln(os.Stderr, "bad request helper arguments")
		os.Exit(2)
	}
	home, op, stamp, mode := args[1], args[2], args[3], args[4]
	now, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	o := Ops{Store: &store.Store{Home: home}, Now: func() time.Time { return now }}
	out := o.Do(op, "snooze", "same", func() Outcome {
		if mode == "hold" {
			fmt.Fprintln(os.Stdout, "reserved")
			var b [1]byte
			io.ReadFull(os.Stdin, b[:])
		}
		return Outcome{Outcome: Done, ID: "7"}
	})
	if err = json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func opRequestCmd(t *testing.T, ctx context.Context, home, op string, now time.Time, mode string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestOpsRequestProcessHelper$", "-test.timeout=12s", "--", home, op, now.Format(time.RFC3339), mode)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "HOME=") && !strings.HasPrefix(kv, "SOUS_HOME=") && !strings.HasPrefix(kv, "SOUS_OP_REQUEST_TEST=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "SOUS_HOME="+home, "SOUS_OP_REQUEST_TEST=1")
	return cmd
}

func holdOpRequest(t *testing.T, home, op string, now time.Time) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := opRequestCmd(t, ctx, home, op, now, "hold")
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		cancel()
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		in.Close()
		cancel()
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { cmd.Process.Kill(); in.Close(); cmd.Wait(); cancel() }) }
	t.Cleanup(stop)
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "reserved\n" {
			stop()
			t.Fatalf("request never reserved: %q, stderr %q", line, stderr.String())
		}
	case <-time.After(3 * time.Second):
		stop()
		t.Fatalf("request never reserved: %s", stderr.String())
	}
	return stop
}

func runOpRequest(t *testing.T, home, op string, now time.Time) Outcome {
	t.Helper()
	// The live owner stays held while this deadline bounds a contending caller.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := opRequestCmd(t, ctx, home, op, now, "done").CombinedOutput()
	if err != nil {
		t.Fatalf("request did not return promptly: %v, %s", err, raw)
	}
	var out Outcome
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("request outcome: %v: %s", err, raw)
	}
	return out
}

func TestOpsCrossProcessOwnerIgnoresAgeAndAllowsOtherOps(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	const op = "op-process-owner01"
	holdOpRequest(t, home, op, now)
	if out := runOpRequest(t, home, op, now.AddDate(100, 0, 0)); out.Outcome != InProgress {
		t.Fatalf("live old owner was replaced: %+v", out)
	}
	if out := runOpRequest(t, home, "op-other-process01", now); out.Outcome != Done {
		t.Fatalf("unrelated op blocked by live owner: %+v", out)
	}
}

func TestOpsCrossProcessRecoversImmediatelyAfterOwnerDeath(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	const op = "op-process-crash01"
	stop := holdOpRequest(t, home, op, now)
	s := &store.Store{Home: home}
	before, err := store.Load[opsDoc](s, opsFile, opsMigrator)
	if err != nil {
		t.Fatal(err)
	}
	old := before.Ops[op]
	if old.Generation == "" || old.Final != nil {
		t.Fatalf("expected pending owned reservation: %+v", old)
	}
	stop() // wait for this exact child; never signal a saved/reused PID.
	if out := runOpRequest(t, home, op, now); out.Outcome != Done {
		t.Fatalf("dead owner required a lease delay: %+v", out)
	}
	after, err := store.Load[opsDoc](s, opsFile, opsMigrator)
	if err != nil {
		t.Fatal(err)
	}
	current := after.Ops[op]
	if current.Generation == "" || current.Generation == old.Generation || current.Final == nil || !current.At.Equal(now) {
		t.Fatalf("recovery: before=%+v after=%+v", old, current)
	}
}

func TestOpsRetryableFailureKeepsInputBinding(t *testing.T) {
	for _, failure := range []Outcome{{Outcome: Failed, Reason: "temporary"}, {Outcome: NotReady, Reason: "config"}, {Outcome: Refused, Reason: "not_allowed_project"}} {
		t.Run(failure.Outcome, func(t *testing.T) {
			o := Ops{Store: &store.Store{Home: t.TempDir()}, Now: time.Now}
			const op = "op-failure-bound01"
			calls := 0
			if out := o.Do(op, "snooze", "same", func() Outcome { calls++; return failure }); out != failure {
				t.Fatal(out)
			}
			// Reopen the store: the binding must be on disk, not on this Ops value.
			o = Ops{Store: &store.Store{Home: o.Store.Home}, Now: time.Now}
			for _, input := range [][2]string{{"snooze", "changed"}, {"done", "same"}} {
				out := o.Do(op, input[0], input[1], func() Outcome { calls++; return Outcome{Outcome: Done} })
				if out.Outcome != Refused || out.Reason != "op_reused" || calls != 1 {
					t.Fatalf("failed op lost binding: %+v calls=%d", out, calls)
				}
			}
			out := o.Do(op, "snooze", "same", func() Outcome { calls++; return Outcome{Outcome: Done, ID: "7"} })
			if out.Outcome != Done || calls != 2 {
				t.Fatalf("same input could not retry: %+v calls=%d", out, calls)
			}
		})
	}
}

func TestOpsLegacyReceiptsAndFutureVersion(t *testing.T) {
	const op = "op-legacy-receipt1"
	for _, tc := range []struct {
		name, raw, want, reason string
		version                 int
	}{
		{"final", `{"version":1,"ops":{"op-legacy-receipt1":{"action":"snooze","hash":"same","at":"2026-10-01T00:00:00Z","final":{"outcome":"done","id":"7"}}}}`, AlreadyDone, "", 2},
		{"pending", `{"version":1,"ops":{"op-legacy-receipt1":{"action":"snooze","hash":"same","at":"2000-01-01T00:00:00Z"}}}`, NotReady, "legacy_operation", 2},
		{"future", `{"version":3,"ops":{}}`, Failed, "op_record:", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "ops.json")
			if err := os.WriteFile(path, []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			o := Ops{Store: &store.Store{Home: home}, Now: time.Now}
			called := false
			out := o.Do(op, "snooze", "same", func() Outcome { called = true; return Outcome{Outcome: Done} })
			if called || out.Outcome != tc.want || !strings.HasPrefix(out.Reason, tc.reason) {
				t.Fatalf("unsafe migration/replay: called=%v outcome=%+v", called, out)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "future" {
				if string(raw) != tc.raw {
					t.Fatalf("future state overwritten: %s", raw)
				}
				return
			}
			var d opsDoc
			if err = json.Unmarshal(raw, &d); err != nil {
				t.Fatal(err)
			}
			r := d.Ops[op]
			if d.Version != tc.version || r.Action != "snooze" || r.Hash != "same" || r.Generation != "" {
				t.Fatalf("legacy binding changed: %+v", d)
			}
			wantAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			if tc.name == "pending" {
				wantAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			if !r.At.Equal(wantAt) {
				t.Fatalf("legacy timestamp changed: %+v", r)
			}
			if tc.name == "final" && (r.Final == nil || r.Final.Outcome != Done || r.Final.ID != "7" || out.ID != "7") {
				t.Fatalf("legacy final changed: record=%+v outcome=%+v", r, out)
			}
			if tc.name == "pending" && r.Final != nil {
				t.Fatalf("legacy unknown ownership fabricated a final: %+v", r)
			}
		})
	}
}

func TestOpsStaleFinishCannotChangeNewPendingReservation(t *testing.T) {
	o := Ops{Store: &store.Store{Home: t.TempDir()}, Now: time.Now}
	const op = "op-stale-pending01"
	old, early, err := o.begin(op, "snooze", "same")
	if err != nil || early != nil {
		t.Fatalf("old reserve: %+v %v", early, err)
	}
	current, early, err := o.begin(op, "snooze", "same")
	if err != nil || early != nil || current == old {
		t.Fatalf("new reserve: %+v %v", early, err)
	}
	for _, out := range []Outcome{{Outcome: Failed}, {Outcome: Done, ID: "obsolete"}} {
		if err = o.finish(op, old, out); err == nil {
			t.Fatal("obsolete generation finalized current pending request")
		}
		d, err := store.Load[opsDoc](o.Store, opsFile, opsMigrator)
		if err != nil {
			t.Fatal(err)
		}
		if r := d.Ops[op]; r.Generation != current || r.Final != nil {
			t.Fatalf("obsolete completion changed pending owner: %+v", r)
		}
	}
}
