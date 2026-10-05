package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	ossignal "os/signal"
	"reflect"
	"syscall"
	"time"

	"github.com/bilal-/sous/internal/config"
	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/thread"
)

func cmdIntegration(e *Env, a argv) int {
	call := "describe"
	if len(a.pos) > 0 {
		call = a.pos[0]
	}
	switch call {
	case "describe":
		d := integration.Describe(Version)
		if e.JSON {
			return e.writeJSON(d)
		}
		integration.RenderDescription(e.Stdout, d)
		return 0
	case "snapshot":
		s, err := integrationSnapshot(e, e.ctx())
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
		err := integration.Watch(ctx, func(ctx context.Context) (integration.Snapshot, error) { return integrationSnapshot(e, ctx) }, time.Second, func(event integration.Event) error {
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
		return fail(e, exitUsage, "integration calls are describe, snapshot and watch\nusage: sous integration [describe|snapshot|watch] [--json]")
	}
}

// A watch outlives a config edit. Read settings again for each snapshot,
// while retaining the environment and store selected for this invocation.
func integrationSnapshot(e *Env, ctx context.Context) (integration.Snapshot, error) {
	cfg, err := config.Load(e.Home, e.UserHome)
	if err != nil {
		return integration.Snapshot{}, err
	}
	current := *e
	current.Cfg, current.cfgErr = cfg, nil
	now := time.Now().UTC()
	return (integration.Reader{Store: e.store(), Configured: len(cfg.Roots) > 0, Now: now,
		Reconcile: func(ctx context.Context, views []thread.View) []thread.View {
			return reconcile(&current, ctx, views, now, true)
		},
	}).Read(ctx)
}

func integrationErr(e *Env, err error) int {
	if errors.Is(err, integration.ErrNotReady) {
		return failWith(e, errorJSON{Error: err.Error(), Exit: exitNotReady, Next: []string{"sous --refresh"}})
	}
	return fail(e, exitFailed, "%v", err)
}
