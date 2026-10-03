package signal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bilal-/sous/internal/plugin"
)

type Plugin = plugin.Plugin

// Status is how a plugin's run went. The values are part of --json.
type Status string

const (
	StatusOK      Status = "ok"
	StatusFailed  Status = "failed"
	StatusTimeout Status = "timeout"
	StatusOff     Status = "off" // not set up on this machine
)

type PluginStatus struct {
	Name   string  `json:"name"`
	Status Status  `json:"status"`
	Error  *string `json:"error"`
}

// Gap: the run left the picture incomplete. Off is not a gap by itself;
// the board decides whether earlier findings make it one.
func (p PluginStatus) Gap() bool { return p.Status != StatusOK && p.Status != StatusOff }

type Tagged struct {
	Signal
	Plugin string `json:"plugin"`
}

type Collected struct {
	Signals []Tagged       `json:"signals"`
	Plugins []PluginStatus `json:"plugins"`
}

// Collect runs every plugin's scan concurrently with the same stdin, each under its
// own timeout. Results keep plugin order.
func Collect(ctx context.Context, plugins []Plugin, paths []string, timeout time.Duration) Collected {
	stdin := strings.Join(paths, "\n") + "\n"
	type result struct {
		st   PluginStatus
		sigs []Tagged
	}
	results := make([]result, len(plugins))
	var wg sync.WaitGroup
	for i, p := range plugins {
		wg.Add(1)
		go func(i int, p Plugin) {
			defer wg.Done()
			results[i] = runOne(ctx, p, stdin, timeout)
		}(i, p)
	}
	wg.Wait()
	var c Collected
	for _, r := range results {
		c.Plugins = append(c.Plugins, r.st)
		c.Signals = append(c.Signals, r.sigs...)
	}
	return c
}

func runOne(ctx context.Context, p Plugin, stdin string, timeout time.Duration) (r struct {
	st   PluginStatus
	sigs []Tagged
}) {
	argv := append(append([]string{}, p.Argv...), "scan")
	res := plugin.Exec(ctx, argv, []byte(stdin), timeout)
	r.st = PluginStatus{Name: p.Name, Status: StatusOK}
	switch {
	case res.TimedOut:
		r.st.Status = StatusTimeout
		msg := "exceeded " + timeout.String()
		r.st.Error = &msg
	case res.Err != nil || res.Code != 0:
		r.st.Status = StatusFailed
		if res.Code == plugin.ExitNotSetUp {
			r.st.Status = StatusOff
		}
		msg := strings.Join(strings.Fields(res.Stderr), " ")
		if msg == "" && res.Err != nil {
			msg = res.Err.Error()
		}
		if msg == "" {
			msg = fmt.Sprintf("exit %d", res.Code)
		}
		if r := []rune(msg); len(r) > 200 {
			msg = string(r[:200])
		}
		r.st.Error = &msg
	}
	// Whatever the plugin managed to emit is kept, even on failure: a
	// partially failing GitHub scan still knows about real review requests.
	if !res.TimedOut {
		sigs, bad := ReadLinesLenient(strings.NewReader(res.Stdout))
		if bad > 0 && r.st.Status == StatusOK {
			// Some findings could not be read: the scan is incomplete, so
			// what it reported before is kept, stale, rather than dropped.
			msg := fmt.Sprintf("%d unreadable line(s) skipped", bad)
			r.st.Status, r.st.Error = StatusFailed, &msg
		}
		for _, s := range sigs {
			r.sigs = append(r.sigs, Tagged{Signal: s, Plugin: p.Name})
		}
	}
	return r
}
