package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

type socketFixture struct {
	Path        string
	Listener    net.Listener
	Host        *fakeHost
	mu          sync.Mutex
	connections []net.Conn
	wg          sync.WaitGroup
}

func socketServer(t *testing.T) *socketFixture {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sous-herdr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	listener, err := net.Listen("unix", filepath.Join(dir, "herdr.sock"))
	if err != nil {
		t.Fatal(err)
	}
	f := &socketFixture{Path: listener.Addr().String(), Listener: listener, Host: &fakeHost{Gone: make(chan struct{}),
		Session: Session{Workspaces: []Workspace{{ID: "w1"}}, Panes: []Pane{{ID: "w1:p1", Workspace: "w1", Cwd: "/code/acme/api"}}}}}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.connections = append(f.connections, conn)
			f.mu.Unlock()
			f.wg.Add(1)
			go func() {
				defer f.wg.Done()
				defer conn.Close()
				var req struct {
					ID, Method string
					Params     json.RawMessage
				}
				if err := json.NewDecoder(conn).Decode(&req); err != nil {
					return
				}
				var params any
				_ = json.Unmarshal(req.Params, &params)
				if req.Method == "events.subscribe" {
					_ = json.NewEncoder(conn).Encode(map[string]any{"id": req.ID, "result": map[string]string{"type": "subscription_started"}})
					_, _ = bufio.NewReader(conn).ReadByte()
					return
				}
				var answer any
				err := f.Host.Call(context.Background(), req.Method, params, &answer)
				if err != nil {
					_ = json.NewEncoder(conn).Encode(map[string]any{"id": req.ID, "error": map[string]string{"code": "refused", "message": err.Error()}})
				} else {
					_ = json.NewEncoder(conn).Encode(map[string]any{"id": req.ID, "result": answer})
				}
			}()
		}
	}()
	t.Cleanup(f.close)
	return f
}

func (f *socketFixture) close() {
	_ = f.Listener.Close()
	f.mu.Lock()
	for _, conn := range f.connections {
		_ = conn.Close()
	}
	f.mu.Unlock()
	f.wg.Wait()
}

func TestSocketClientPropagatesHostErrorsAndStopsOnDisconnect(t *testing.T) {
	f := socketServer(t)
	f.Host.Fail = map[string]error{"pane.focus": os.ErrNotExist}
	client := SocketHost{Path: f.Path}
	if err := client.Call(context.Background(), "pane.focus", map[string]string{"pane_id": "w1:p1"}, nil); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("host refusal was ignored: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Monitor(ctx) }()
	f.close()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("observer did not notice host disconnect")
	}
}

func TestObserverProcessPublishesChangesAndDiesWithItsHost(t *testing.T) {
	f := socketServer(t)
	home := t.TempDir()
	binary, _ := os.Executable()
	config := filepath.Join(home, "config")
	state := filepath.Join(home, "state")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte("sous_path = "+string(mustJSON(t, binary))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "snapshot.json")
	if err := store.WriteFile(file, mustJSON(t, snapshot("first", "running")), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "SOUS_HOME=" + filepath.Join(home, "sous"),
		"HERDR_ENV=1", "HERDR_SOCKET_PATH=" + f.Path, "HERDR_PLUGIN_CONFIG_DIR=" + config, "HERDR_PLUGIN_STATE_DIR=" + state,
		"SOUS_HERDR_TEST_NATIVE=1", "SOUS_HERDR_TEST_SNAPSHOT=" + file, "GORACE=atexit_sleep_ms=0"}
	cmd := exec.Command(binary, "watch")
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	joined := false
	t.Cleanup(func() {
		if !joined {
			_ = cmd.Process.Kill()
			<-finished
		}
	})
	waitFor := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("plugin process did not produce the expected host update")
	}
	waitFor(func() bool { return len(f.Host.calls("workspace.report_metadata")) > 0 })
	if len(f.Host.calls("notification.show")) != 0 {
		t.Fatal("startup baseline sent notifications")
	}
	next := snapshot("second", "needs_you")
	if err := store.WriteFile(file, mustJSON(t, next), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(func() bool { return len(f.Host.calls("notification.show")) == 1 })
	if err := store.WriteFile(file, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(f.Path + "\x00" + filepath.Join(home, "sous") + "\x00" + binary))
	st := &store.Store{Home: filepath.Join(state, hex.EncodeToString(digest[:8]))}
	waitFor(func() bool {
		state, err := readState(st)
		return err == nil && !state.Available && state.Snapshot != nil
	})
	if err := store.WriteFile(file, mustJSON(t, next), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(func() bool { state, err := readState(st); return err == nil && state.Available })
	if len(f.Host.calls("notification.show")) != 1 {
		t.Fatal("recovery duplicated the alert")
	}
	f.close()
	select {
	case err := <-finished:
		joined = true
		if err != nil {
			t.Fatalf("plugin did not exit cleanly with herdr: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("plugin process outlived herdr")
	}
}
