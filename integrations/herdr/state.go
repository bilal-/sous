package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"

	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

type bridgeState struct {
	Version   int                   `json:"version"`
	Available bool                  `json:"available"`
	Snapshot  *integration.Snapshot `json:"snapshot"`
	Error     string                `json:"error"`
	Warnings  map[string]string     `json:"warnings"`
}

var stateFile = bridgeFormat{}

func readState(st *store.Store) (bridgeState, error) {
	s, err := store.Load[bridgeState](st, "bridge", stateFile)
	if err != nil {
		return bridgeState{}, err
	}
	return *s, nil
}

type notice struct{ Title, Body, Sound string }

func notices(before, after *integration.Snapshot) []notice {
	if before == nil || after == nil || before.Revision == after.Revision {
		return nil
	}
	old := map[string]integration.Item{}
	for _, item := range before.Items {
		old[item.Key] = item
	}
	var out []notice
	for _, item := range after.Items {
		if item.Stale || (item.Run != nil && item.Run.Error != "") {
			continue
		}
		prior, existed := old[item.Key]
		title, sound := "", "request"
		body := item.Text
		if item.Run != nil {
			previousState := thread.RunState("")
			if prior.Run != nil {
				previousState = prior.Run.State
			}
			if previousState == item.Run.State {
				continue
			}
			if item.Run.Text != "" {
				body = item.Run.Text
			}
			switch item.Run.State {
			case thread.RunNeedsYou:
				title = "Task needs you"
			case thread.RunDone:
				title, sound = "Task finished", "done"
			case thread.RunFailed:
				title = "Task failed"
			}
		} else if item.Section == "on_you" && (!existed || prior.Section != "on_you") && item.Source != "human" {
			title = "New task on you"
		}
		if title != "" {
			out = append(out, notice{"sous · " + title, clip(item.Name+" · "+body, 240), sound})
		}
	}
	return out
}

func (b *Bridge) Accept(ctx context.Context, e integration.Event) error {
	if e.Kind != "event" || e.Provider != "sous" || e.V != integration.Version {
		return fmt.Errorf("unsupported sous subscription protocol")
	}
	if e.Type == "snapshot" || e.Type == "changes" {
		if e.Snapshot == nil || e.Snapshot.Kind != "snapshot" || e.Snapshot.V != integration.Version || e.Snapshot.Provider != "sous" {
			return fmt.Errorf("invalid sous snapshot")
		}
		snapshot := b.projectSnapshot(ctx, *e.Snapshot)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		e.Snapshot = &snapshot
	}
	_, err := store.Modify[bridgeState](b.State, "bridge", stateFile, func(state *bridgeState) error {
		switch e.Type {
		case "unavailable":
			state.Available, state.Error = false, e.Error
		case "snapshot", "changes":
			if b.Config.Notifications && e.Type == "changes" && state.Available && state.Snapshot != nil && e.PreviousRevision == state.Snapshot.Revision {
				alerts := notices(state.Snapshot, e.Snapshot)
				if len(alerts) > 5 {
					alerts = []notice{{"sous · Tasks need attention", fmt.Sprintf("%d task changes; open Tasks to review them", len(alerts)), "request"}}
				}
				var notificationError error
				for _, alert := range alerts {
					if err := b.Host.Call(ctx, "notification.show", map[string]any{"title": alert.Title, "body": alert.Body, "sound": alert.Sound}, nil); err != nil {
						notificationError = err
					}
				}
				if len(alerts) > 0 {
					setWarning(state, "notification", notificationError)
				}
			}
			state.Available, state.Error, state.Snapshot = true, "", e.Snapshot
		default:
			return fmt.Errorf("unknown sous event %q", e.Type)
		}
		return nil
	})
	if err != nil {
		return err
	}
	_ = b.Publish(ctx) // sidebar failures are recorded without dropping the subscription
	return nil
}

func (b *Bridge) failure(message string) {
	_, _ = store.Modify[bridgeState](b.State, "bridge", stateFile, func(s *bridgeState) error { s.Available, s.Error = false, message; return nil })
}

func (b *Bridge) project(ctx context.Context, session Session, workspace string) project.Project {
	for _, pane := range session.Panes {
		if pane.Workspace == workspace && pane.Cwd != "" {
			if path, err := b.Provider.Project(ctx, pane.Cwd); err == nil {
				return path
			}
		}
	}
	return project.Project{}
}

func (b *Bridge) Publish(ctx context.Context) (err error) {
	b.publishMu.Lock()
	defer b.publishMu.Unlock()
	defer func() {
		if ctx.Err() == nil {
			b.warning("sidebar", err)
		}
	}()
	state, err := readState(b.State)
	if err != nil {
		return err
	}
	runtime, err := session(ctx, b.Host)
	if err != nil {
		return err
	}
	for _, workspace := range runtime.Workspaces {
		project := b.project(ctx, runtime, workspace.ID)
		onYou, onOthers, unfinished := 0, 0, 0
		if state.Snapshot != nil {
			for _, item := range state.Snapshot.Items {
				if !projectMatches(state.Snapshot, project, item.Project) {
					continue
				}
				switch item.Section {
				case "on_you":
					onYou++
				case "on_others":
					onOthers++
				case "unfinished":
					unfinished++
				}
			}
		}
		label := fmt.Sprintf("%d on you · %d on others · %d unfinished", onYou, onOthers, unfinished)
		switch {
		case !state.Available:
			label = "sous unavailable"
		case project.Path == "":
			label = "sous project unavailable"
		case !projectTracked(state.Snapshot, project):
			label = "sous project not tracked"
		case state.Snapshot == nil || !state.Snapshot.Complete:
			label = "? " + label
		}
		freshness := ""
		if state.Snapshot != nil && state.Snapshot.AsOf != nil {
			freshness = "remote snapshot " + state.Snapshot.AsOf.Local().Format("Jan 2 15:04")
		}
		if err := b.Host.Call(ctx, "workspace.report_metadata", map[string]any{
			"workspace_id": workspace.ID, "source": "plugin:" + pluginID, "ttl_ms": 30000,
			"tokens": map[string]string{"sous_tasks": label, "sous_freshness": freshness},
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bridge) Open(ctx context.Context, item integration.Item) error {
	state, err := readState(b.State)
	if err != nil {
		return err
	}
	runtime, err := session(ctx, b.Host)
	if err != nil {
		return err
	}
	for _, pane := range runtime.Panes {
		if pane.Tokens["sous_task"] == item.Key && pane.Tokens["sous_instance"] == b.instance() {
			return b.Host.Call(ctx, "pane.focus", map[string]string{"pane_id": pane.ID}, nil)
		}
	}
	workspace := ""
	alias := ""
	for _, candidate := range runtime.Workspaces {
		identity := b.project(ctx, runtime, candidate.ID)
		if sameProjectPath(identity.Path, item.Project) {
			workspace = candidate.ID
			break
		}
		if alias == "" && projectMatches(state.Snapshot, identity, item.Project) {
			alias = candidate.ID
		}
	}
	if workspace == "" {
		workspace = alias
	}
	if workspace == "" {
		var created struct {
			Workspace Workspace `json:"workspace"`
		}
		if err := b.Host.Call(ctx, "workspace.create", map[string]any{"cwd": item.Project, "label": item.Name, "focus": false}, &created); err != nil {
			return err
		}
		workspace = created.Workspace.ID
		if workspace == "" {
			return fmt.Errorf("herdr did not return a workspace ID")
		}
	}
	return b.Host.Call(ctx, "plugin.pane.open", map[string]any{
		"plugin_id": pluginID, "entrypoint": "project", "placement": "tab", "workspace_id": workspace,
		"cwd": item.Project, "focus": true, "env": map[string]string{"SOUS_HERDR_TASK_KEY": item.Key},
	}, nil)
}

func (b *Bridge) instance() string {
	digest := sha256.Sum256([]byte(b.State.Home))
	return hex.EncodeToString(digest[:8])
}

func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

func clip(text string, width int) string {
	runes := []rune(clean(text))
	if width < 1 {
		return ""
	}
	if len(runes) > width {
		return string(runes[:width-1]) + "…"
	}
	return string(runes)
}
