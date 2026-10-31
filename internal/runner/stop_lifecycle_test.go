package runner

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/testutil"
)

// A FIFO gates Stop's result read after it observes a finished run. The
// hard link keeps that same inode addressable after Reply removes result.json.
// Closing the writer injects EOF at that read, equivalent to the error branch
// reached if Reply unlinked an ordinary result before Stop opened it. Only fake
// agents and this test's private run directory are involved.
func TestStopDoesNotPublishStoppedOverConcurrentReply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, ref, dir := startedRun(t, "claude", time.Minute)
	t.Setenv("SOUS_HOME", a.Home)
	var err error
	a.Exe, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, m, err := a.meta(ref)
	if err != nil {
		t.Fatal(err)
	}
	m.Session = "s-1"
	if err := writeJSON(dir, "run.json", m); err != nil {
		t.Fatal(err)
	}
	testutil.FakeBin(t, "claude", "sleep 10")
	t.Cleanup(func() { a.Stop("", ref) })
	fifo := filepath.Join(dir, "result.json")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "result-read-gate")
	if err := os.Link(fifo, alias); err != nil {
		t.Fatal(err)
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- a.Stop("", ref) }()
	fd := -1
	waitFor(t, func() bool {
		fd, err = syscall.Open(alias, syscall.O_WRONLY|syscall.O_NONBLOCK, 0600)
		return err == nil
	})
	syscall.CloseOnExec(fd) // The gate belongs to the test, never to the watcher.
	w := os.NewFile(uintptr(fd), "result-read-gate")
	defer w.Close()
	replyDone := make(chan error, 1)
	go func() { replyDone <- a.Reply("", ref, "use the small fixture") }()
	replied := false
	select {
	case err := <-replyDone:
		if err != nil {
			t.Fatal(err)
		}
		replied = true
	case <-time.After(200 * time.Millisecond):
	}
	w.Close()
	stopErr := <-stopDone
	if stopErr != nil {
		t.Fatal(stopErr)
	}
	if !replied {
		if err := <-replyDone; err != nil {
			t.Fatal(err)
		}
	}
	if watcherAlive(dir) {
		st, err := a.Status("", ref)
		if err != nil {
			t.Fatal(err)
		}
		if st.State != Running {
			t.Fatalf("Stop published a finished result over a live resumed run: state=%s text=%q stopErr=%v", st.State, st.Text, stopErr)
		}
	}
}
