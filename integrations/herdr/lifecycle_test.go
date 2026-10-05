package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

func TestRestartWaitsForThePreviousObserverToStop(t *testing.T) {
	dir := t.TempDir()
	owner, err := lease(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	stop := filepath.Join(dir, "stop")
	if err := store.WriteFile(stop, []byte("stop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(t.TempDir(), "observer")
	started := filepath.Join(dir, "started")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf started > \"$SOUS_TEST_STARTED\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	joined := false
	defer func() {
		_ = owner.Close()
		if !joined {
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Error("restart did not stop during cleanup")
			}
		}
	}()
	go func() {
		done <- start(context.Background(), options{StateDir: dir, Executable: program, Env: []string{"SOUS_TEST_STARTED=" + started}})
	}()
	select {
	case err := <-done:
		joined = true
		t.Fatalf("restart returned while the previous observer still held its lease: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err := os.Stat(stop); err != nil {
		t.Fatalf("restart removed the stop request before the old observer stopped: %v", err)
	}
	if _, err := os.Stat(started); !os.IsNotExist(err) {
		t.Fatalf("new observer started before the old observer stopped: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restart did not continue after the old observer stopped")
	}
	if _, err := os.Stat(stop); !os.IsNotExist(err) {
		t.Fatalf("stop request was not cleared for the replacement observer: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if data, err := os.ReadFile(started); err == nil && string(data) == "started" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("replacement observer was not started")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
