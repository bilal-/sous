package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	ossignal "os/signal"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/thread"
)

func cmdIntegration(e *Env, a argv) int {
	call := "describe"
	if len(a.pos) > 0 {
		call = a.pos[0]
	}
	v := integration.Version
	if p := a.value("protocol"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || !slices.Contains(integration.Versions, n) {
			return fail(e, exitUsage, "sous integration speaks protocol versions %s, not %q", joinInts(integration.Versions), p)
		}
		v = n
	}
	if _, ok := integrationActs[call]; ok {
		return cmdIntegrationAct(e, a, v, call)
	}
	if call == "diff" {
		return cmdIntegrationDiff(e, a, v)
	}
	if a.has("page") {
		return fail(e, exitUsage, "--page goes with diff")
	}
	if len(a.pos) > 1 || a.has("op") || a.has("question") || a.has("commit") || a.has("max-bytes") && call != "watch" {
		return fail(e, exitUsage, "usage: sous integration [describe|snapshot|watch] [--protocol <n>] [--json]")
	}
	switch call {
	case "describe":
		if v == integration.Version1 {
			d := integration.DescribeV1(Version)
			if e.JSON {
				return e.writeJSON(d)
			}
			integration.RenderDescription(e.Stdout, d.Protocol, d.V, d.Version)
			return 0
		}
		d := integration.Describe(Version)
		if e.JSON {
			return e.writeJSON(d)
		}
		integration.RenderDescription(e.Stdout, d.Protocol, d.V, d.Version)
		return 0
	case "snapshot":
		s, err := integrationSnapshot(e, e.ctx(), v)
		if err != nil {
			return integrationErr(e, err)
		}
		if e.JSON {
			return e.writeJSON(s)
		}
		integration.RenderSnapshot(e.Stdout, s)
		return 0
	case "watch":
		ctx, stop := ossignal.NotifyContext(e.ctx(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		enc := json.NewEncoder(e.Stdout)
		st := integration.Stream{V: v, Interval: time.Second}
		if mb := a.value("max-bytes"); mb != "" {
			n, err := strconv.Atoi(mb)
			if err != nil || n < minWatchBytes || v < integration.Version1 {
				return fail(e, exitUsage, "--max-bytes takes a number of bytes, at least %d, with --protocol 1", minWatchBytes)
			}
			st.MaxBytes = n
		}
		var asked time.Time
		e.jsonLines = true
		err := st.Run(ctx, func(ctx context.Context) (integration.Snapshot, error) {
			// Version 0 promises an offline watch; version 1 asks runner plugins.
			ask := v >= integration.Version1 && time.Since(asked) >= askRunnersEvery
			if ask {
				asked = time.Now()
			}
			return readSnapshot(e, ctx, v, ask)
		}, func(event integration.Event) error {
			if e.JSON {
				return enc.Encode(noNulls(reflect.ValueOf(event)).Interface())
			}
			return integration.RenderEvent(e.Stdout, event)
		})
		if err != nil {
			return integrationErr(e, err)
		}
		return 0
	default:
		return fail(e, exitUsage, "integration calls are describe, snapshot and watch, and with --protocol 1 run, stop and reply\nusage: sous integration [describe|snapshot|watch] [--protocol <n>] [--json]")
	}
}

// A watch outlives a config edit. Read settings again for each snapshot,
// while retaining the environment and store selected for this invocation.
func integrationSnapshot(e *Env, ctx context.Context, v int) (integration.Snapshot, error) {
	return readSnapshot(e, ctx, v, false)
}

// askRunnersEvery is how often a watch asks runner plugins how their runs
// are going; between, it reads what they said last, as a snapshot does.
const askRunnersEvery = 5 * time.Second

// readSnapshot reads the board; with askRunners, runner plugins are asked
// too, each within its status time. Trackers stay offline either way.
func readSnapshot(e *Env, ctx context.Context, v int, askRunners bool) (integration.Snapshot, error) {
	cfg, err := config.Load(e.Home, e.UserHome)
	if err != nil {
		return integration.Snapshot{}, err
	}
	current := *e
	current.Cfg, current.cfgErr = cfg, nil
	now := time.Now().UTC()
	return (integration.Reader{Store: e.store(), Configured: len(cfg.Roots) > 0, Now: now, V: v,
		RunsAllowed: func(path string) bool { return cfg.Project(project.OrgName(path)).RemoteRuns == "true" },
		Reconcile: func(ctx context.Context, views []thread.View) []thread.View {
			views = reconcile(&current, ctx, views, now, true)
			if askRunners {
				views = current.dispatcher().Refresh(ctx, views, false)
			}
			return views
		},
	}).Read(ctx)
}

// minWatchBytes is the smallest line limit a watch takes: enough for the
// board's own fields and a handful of items.
const minWatchBytes = 16 << 10

func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, " and ")
}

func integrationErr(e *Env, err error) int {
	if errors.Is(err, integration.ErrNotReady) {
		return failWith(e, errorJSON{Error: err.Error(), Exit: exitNotReady, Next: []string{"sous --refresh"}})
	}
	return fail(e, exitFailed, "%v", err)
}
