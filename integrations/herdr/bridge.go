package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/store"
)

type Bridge struct {
	State     *store.Store
	Host      Host
	Provider  Provider
	Config    Config
	Interval  time.Duration
	publishMu sync.Mutex
}

func lease(ctx context.Context, dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "observer.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (b *Bridge) Run(ctx context.Context) error {
	owner, err := lease(ctx, b.State.Home)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil // an observer for this session already owns the feed
	}
	if err != nil {
		return err
	}
	defer owner.Close()
	if _, err := readState(b.State); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		check := time.NewTicker(100 * time.Millisecond)
		defer check.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-check.C:
				if _, err := os.Stat(filepath.Join(b.State.Home, "stop")); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	monitor := make(chan error, 1)
	go func() { monitor <- b.Host.Monitor(ctx); cancel() }()
	finished := make(chan error, 1)
	go func() {
		description, err := b.Provider.Describe(ctx)
		if err != nil {
			b.failure("Task provider could not be discovered: " + err.Error())
			finished <- err
			return
		}
		if description.V != integration.Version || description.Protocol != integration.Protocol {
			finished <- fmt.Errorf("unsupported task provider protocol")
			return
		}
		backoff := time.Second
		for ctx.Err() == nil {
			err := b.Provider.Watch(ctx, func(e integration.Event) error {
				backoff = time.Second
				return b.Accept(ctx, e)
			})
			if ctx.Err() != nil {
				break
			}
			message := "Task subscription ended; reconnecting"
			if err != nil {
				message += ": " + err.Error()
			}
			b.failure(message)
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			backoff = min(30*time.Second, backoff*2)
		}
		finished <- nil
	}()
	interval := b.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var refresh <-chan time.Time
	if b.Config.RefreshIntervalSeconds > 0 {
		timer := time.NewTicker(time.Duration(b.Config.RefreshIntervalSeconds) * time.Second)
		defer timer.Stop()
		refresh = timer.C
	}
	refreshDone := make(chan error, 1)
	refreshing := false
	defer func() {
		cancel()
		if refreshing {
			<-refreshDone
		}
		_, _ = store.Modify[bridgeState](b.State, "bridge", stateFile, func(s *bridgeState) error {
			if s.Available {
				s.Available, s.Error = false, "Task observer stopped; open Tasks or start it again"
			}
			return nil
		})
	}()
	for {
		select {
		case err := <-finished:
			cancel()
			return err
		case <-ctx.Done():
			cancel()
			<-finished // wait for the provider child to stop before dropping the lease
			return nil
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(b.State.Home, "stop")); err == nil {
				cancel()
				continue
			}
			var plugins struct {
				Plugins []struct {
					ID      string `json:"plugin_id"`
					Enabled bool   `json:"enabled"`
				} `json:"plugins"`
			}
			if err := b.Host.Call(ctx, "plugin.list", map[string]any{}, &plugins); err != nil {
				cancel()
				continue
			}
			enabled := false
			for _, plugin := range plugins.Plugins {
				enabled = enabled || (plugin.ID == pluginID && plugin.Enabled)
			}
			if !enabled {
				cancel()
				continue
			}
			_ = b.Publish(ctx)
		case <-refresh:
			if _, ok := b.Provider.(interface{ Refresh(context.Context) error }); ok && !refreshing {
				refreshing = true
				go func() { refreshDone <- b.Refresh(ctx) }()
			}
		case <-refreshDone:
			refreshing = false
		}
	}
}
