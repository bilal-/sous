package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServeShutdownStopsActiveWatcher(t *testing.T) {
	f := fixture(t)
	pidPath := filepath.Join(f.Home, "watcher.pid")
	// Register cleanup before the fake process can start, including early failures.
	t.Cleanup(func() {
		b, _ := os.ReadFile(pidPath)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			syscall.Kill(-pid, syscall.SIGKILL)
		}
	})
	exe := filepath.Join(f.Home, "fake-sous")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$$\" > %q\nprintf '{\"kind\":\"event\",\"type\":\"snapshot\"}\\n'\nexec sleep 60\n", pidPath)
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	read, write := io.Pipe()
	defer read.Close()
	defer write.Close()
	env := &Env{Home: f.SousHome, Exe: exe, Stdout: write, Stderr: io.Discard, vctx: ctx}
	done := make(chan int, 1)
	go func() { done <- cmdServe(env, argv{set: map[string][]string{"port": {"0"}}}) }()
	type addressResult struct {
		line string
		err  error
	}
	addressReady := make(chan addressResult, 1)
	go func() { line, err := bufio.NewReader(read).ReadString('\n'); addressReady <- addressResult{line, err} }()
	var line string
	select {
	case answer := <-addressReady:
		if answer.err != nil {
			t.Fatal(answer.err)
		}
		line = answer.line
	case <-time.After(4 * time.Second):
		t.Fatal("serve did not print its address")
	}
	address := strings.TrimSpace(strings.TrimPrefix(line, "sous serve · "))
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	res, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	res, err = client.Get(strings.Split(address, "?")[0] + "api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if _, err := bufio.NewReader(res.Body).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(-pid, syscall.SIGKILL)
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serve returned %d", code)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("serve did not return")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("serve returned but its active watch subprocess %d remains alive", pid)
}
