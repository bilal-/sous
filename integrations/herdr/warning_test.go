package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/store"
)

func warningState(t *testing.T, b *Bridge, ready func(string) bool) bridgeState {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		state, err := readState(b.State)
		if err != nil {
			t.Fatal(err)
		}
		if ready(state.Warning()) {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("warning did not reflect recovery: %q", state.Warning())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSidebarWarningClearsAfterSuccessfulPublication(t *testing.T) {
	b, h, _ := testBridge(t)
	h.Fail = map[string]error{"workspace.report_metadata": errors.New("sidebar offline")}
	if err := b.Accept(context.Background(), event("snapshot", snapshot("first", "running"))); err != nil {
		t.Fatal(err)
	}
	warningState(t, b, func(message string) bool { return strings.Contains(message, "sidebar offline") })
	h.Fail = nil
	if err := b.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	warningState(t, b, func(message string) bool { return message == "" })
}

func TestNotificationWarningClearsAfterSuccessfulDelivery(t *testing.T) {
	b, h, _ := testBridge(t)
	ctx := context.Background()
	if err := b.Accept(ctx, event("snapshot", snapshot("first", "running"))); err != nil {
		t.Fatal(err)
	}
	h.Fail = map[string]error{"notification.show": errors.New("notifications offline")}
	next := event("changes", snapshot("second", "needs_you"))
	next.PreviousRevision = "first"
	if err := b.Accept(ctx, next); err != nil {
		t.Fatal(err)
	}
	warningState(t, b, func(message string) bool { return strings.Contains(message, "notifications offline") })
	h.Fail = nil
	next = event("changes", snapshot("third", "done"))
	next.PreviousRevision = "second"
	if err := b.Accept(ctx, next); err != nil {
		t.Fatal(err)
	}
	warningState(t, b, func(message string) bool { return message == "" })
}

type refreshingProvider struct {
	*fakeProvider
	Calls chan error
	First error
}

func (p *refreshingProvider) Refresh(context.Context) error {
	err := p.First
	p.First = nil
	p.Calls <- err
	return err
}

func TestScheduledRefreshWarningClearsAfterRecovery(t *testing.T) {
	b, _, p := testBridge(t)
	p.Started, p.Stopped = make(chan struct{}), make(chan struct{})
	r := &refreshingProvider{fakeProvider: p, Calls: make(chan error, 8), First: errors.New("refresh offline")}
	b.Provider, b.Config.RefreshIntervalSeconds, b.Interval = r, 1, time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case <-r.Calls:
	case <-ctx.Done():
		t.Fatal("scheduled refresh did not run")
	}
	warningState(t, b, func(message string) bool { return strings.Contains(message, "refresh offline") })
	select {
	case <-r.Calls:
	case <-ctx.Done():
		t.Fatal("scheduled refresh did not recover")
	}
	warningState(t, b, func(message string) bool { return message == "" })
}

func TestRecoveringOneSourceKeepsOtherFailures(t *testing.T) {
	b, h, p := testBridge(t)
	b.Provider = &refreshingProvider{fakeProvider: p, Calls: make(chan error, 8), First: errors.New("refresh offline")}
	ctx := context.Background()
	if err := b.Refresh(ctx); err == nil {
		t.Fatal("failed refresh was reported as successful")
	}
	h.Fail = map[string]error{"workspace.report_metadata": errors.New("sidebar offline")}
	if err := b.Accept(ctx, event("snapshot", snapshot("first", "running"))); err != nil {
		t.Fatal(err)
	}
	warningState(t, b, func(message string) bool {
		return strings.Contains(message, "refresh offline") && strings.Contains(message, "sidebar offline")
	})
	h.Fail = nil
	if err := b.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	warningState(t, b, func(message string) bool {
		return strings.Contains(message, "refresh offline") && !strings.Contains(message, "sidebar offline")
	})
	if err := b.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	warningState(t, b, func(message string) bool { return message == "" })
}

func TestCancellingRefreshDoesNotReportAnOutage(t *testing.T) {
	b, _, p := testBridge(t)
	b.Provider = &refreshingProvider{fakeProvider: p, Calls: make(chan error, 8), First: context.Canceled}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh cancellation was lost: %v", err)
	}
	warningState(t, b, func(message string) bool { return message == "" })
}

func TestBridgeStateV1UpgradePreservesTasksAndWarnings(t *testing.T) {
	for _, warning := range []string{"", "Sidebar could not be updated: offline", "Remote refresh failed: offline",
		"Notification could not be delivered: offline", "Unrecognized older warning"} {
		t.Run(warning, func(t *testing.T) {
			st := &store.Store{Home: t.TempDir()}
			before := snapshot("first", "running")
			raw, err := json.Marshal(map[string]any{"version": 1, "available": true, "snapshot": before, "error": "", "warning": warning})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(st.Home, "bridge.json")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			after, err := readState(st)
			if err != nil {
				t.Fatal(err)
			}
			if after.Version != 2 || !after.Available || !reflect.DeepEqual(after.Snapshot, &before) || after.Warning() != warning {
				t.Fatal("upgrade changed known tasks, availability or the previous warning")
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var persisted bridgeState
			if err := json.Unmarshal(onDisk, &persisted); err != nil || persisted.Version != 2 {
				t.Fatalf("upgrade was not persisted: %v", err)
			}
		})
	}
}

func TestBridgeStateInvalidUpgradeLeavesTheOriginalFile(t *testing.T) {
	st := &store.Store{Home: t.TempDir()}
	path := filepath.Join(st.Home, "bridge.json")
	raw := []byte(`{"version":1,"available":true,"warning":42}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readState(st); err == nil {
		t.Fatal("invalid old warning silently lost")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("failed upgrade changed the original file: %v", err)
	}
}

func TestDisablingNotificationsClearsOnlyTheirDeliveryWarning(t *testing.T) {
	b, h, p := testBridge(t)
	ctx := context.Background()
	if err := b.Accept(ctx, event("snapshot", snapshot("first", "running"))); err != nil {
		t.Fatal(err)
	}
	h.Fail = map[string]error{"notification.show": errors.New("notifications offline")}
	next := event("changes", snapshot("second", "needs_you"))
	next.PreviousRevision = "first"
	if err := b.Accept(ctx, next); err != nil {
		t.Fatal(err)
	}
	b.Provider = &refreshingProvider{fakeProvider: p, Calls: make(chan error, 8), First: errors.New("refresh offline")}
	if err := b.Refresh(ctx); err == nil {
		t.Fatal("fixture refresh unexpectedly succeeded")
	}
	warningState(t, b, func(message string) bool {
		return strings.Contains(message, "notifications offline") && strings.Contains(message, "refresh offline")
	})
	b.Config.Notifications = false
	h.Fail = nil
	if err := b.Accept(ctx, event("snapshot", *next.Snapshot)); err != nil {
		t.Fatal(err)
	}
	state, err := readState(b.State)
	if err != nil || strings.Contains(state.Warning(), "notifications offline") || !strings.Contains(state.Warning(), "refresh offline") {
		t.Fatalf("disabled notification warning remained, or an active failure was hidden: %q, %v", state.Warning(), err)
	}
}
