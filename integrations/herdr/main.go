// sous-herdr is a standalone host plugin. Only this entry point reads the
// host's environment; the adapter takes its configuration and services as args.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/bilal-/sous/internal/integration"
	"github.com/bilal-/sous/internal/store"
)

type Config struct {
	SousPath               string `toml:"sous_path"`
	Notifications          bool   `toml:"notifications"`
	RefreshIntervalSeconds int    `toml:"refresh_interval_seconds"`
}

func defaultConfig() Config { return Config{Notifications: true} }

type options struct {
	Config
	Socket, StateDir, Executable, Pane, TaskKey, CallerCwd string
	Env                                                    []string
	Provider                                               *CLIProvider
}

func loadOptions() (options, error) {
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_SOCKET_PATH") == "" || os.Getenv("HERDR_PLUGIN_STATE_DIR") == "" {
		return options{}, errors.New("run this through the installed sous.tasks herdr plugin; see integrations/herdr/README.md")
	}
	cfg := defaultConfig()
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		path := filepath.Join(dir, "config.toml")
		if _, err := os.Stat(path); err == nil {
			meta, err := toml.DecodeFile(path, &cfg)
			if err != nil {
				return options{}, err
			}
			if len(meta.Undecoded()) > 0 {
				return options{}, fmt.Errorf("unknown plugin setting %s", meta.Undecoded()[0])
			}
		} else if !os.IsNotExist(err) {
			return options{}, err
		}
	}
	if cfg.RefreshIntervalSeconds != 0 && (cfg.RefreshIntervalSeconds < 30 || cfg.RefreshIntervalSeconds > 86400) {
		return options{}, errors.New("refresh_interval_seconds must be 0 (manual), or 30–86400")
	}
	program := cfg.SousPath
	if program == "" {
		program = "sous"
	}
	binary, err := exec.LookPath(program)
	if err != nil {
		return options{}, errors.New("sous is not on herdr's PATH; install it or set sous_path in the plugin config")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return options{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return options{}, err
	}
	socket := os.Getenv("HERDR_SOCKET_PATH")
	instance := os.Getenv("SOUS_HOME")
	if instance == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return options{}, err
		}
		instance = filepath.Join(home, ".sous")
	}
	digest := sha256.Sum256([]byte(socket + "\x00" + instance + "\x00" + binary))
	stateDir := filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), hex.EncodeToString(digest[:8]))
	var invocation struct {
		WorkspaceCwd string `json:"workspace_cwd"`
		PaneCwd      string `json:"focused_pane_cwd"`
	}
	_ = json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &invocation)
	callerCwd := invocation.WorkspaceCwd
	if callerCwd == "" {
		callerCwd = invocation.PaneCwd
	}
	env := os.Environ()
	return options{Config: cfg, Socket: socket, StateDir: stateDir, Executable: executable,
		Pane: os.Getenv("HERDR_PANE_ID"), TaskKey: os.Getenv("SOUS_HERDR_TASK_KEY"), CallerCwd: callerCwd, Env: env,
		Provider: &CLIProvider{Binary: binary, Env: env}}, nil
}

func (o options) bridge() *Bridge {
	return &Bridge{State: &store.Store{Home: o.StateDir}, Host: SocketHost{Path: o.Socket}, Provider: o.Provider, Config: o.Config}
}

func start(ctx context.Context, o options) error {
	if _, err := readState(o.bridge().State); err != nil {
		return err
	}
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(o.StateDir, "stop")); err == nil {
		owner, err := lease(ctx, o.StateDir)
		if err != nil {
			if errors.Is(err, syscall.EWOULDBLOCK) {
				if _, stopErr := os.Stat(filepath.Join(o.StateDir, "stop")); os.IsNotExist(stopErr) {
					return nil // another start handed over to a healthy observer
				}
			}
			return fmt.Errorf("previous observer is still stopping: %w", err)
		}
		defer owner.Close()
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(filepath.Join(o.StateDir, "stop")); err != nil && !os.IsNotExist(err) {
		return err
	}
	log, err := os.OpenFile(filepath.Join(o.StateDir, "observer.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(o.Executable, "watch")
	cmd.Env, cmd.Stdout, cmd.Stderr = o.Env, log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func selected(ctx context.Context, p Provider, key string) (integration.Item, error) {
	snapshot, err := p.Read(ctx)
	if err != nil {
		return integration.Item{}, err
	}
	for _, item := range snapshot.Items {
		if item.Key == key {
			return item, nil
		}
	}
	return integration.Item{}, errors.New("this task is no longer in the list; open Tasks again")
}

func projectPane(ctx context.Context, o options) error {
	item, err := selected(ctx, o.Provider, o.TaskKey)
	if err != nil {
		return err
	}
	d, err := o.Provider.Describe(ctx)
	if err != nil {
		return err
	}
	if err := o.bridge().Host.Call(ctx, "pane.report_metadata", map[string]any{
		"pane_id": o.Pane, "source": "plugin:" + pluginID,
		"tokens": map[string]string{"sous_task": item.Key, "sous_instance": o.bridge().instance(), "sous_task_title": clip(item.Text, 120)},
	}, nil); err != nil {
		return err
	}
	for _, action := range d.Actions {
		if action.ID != "open" {
			continue
		}
		args, _, err := bindAction(o.Provider.Binary, action, map[string]string{"project": item.Project})
		if err != nil {
			return err
		}
		if err := os.Chdir(item.Project); err != nil {
			return err
		}
		return syscall.Exec(o.Provider.Binary, args, o.Env)
	}
	return errors.New("sous does not advertise an open action")
}

func run(ctx context.Context, args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		fmt.Println("sous-herdr: the sous.tasks herdr plugin\nCommands: board, start, stop, sync, refresh, project\nInstall and configuration: integrations/herdr/README.md")
		return nil
	}
	if len(args) != 1 || !slices.Contains([]string{"tasks", "board", "start", "watch", "stop", "sync", "refresh", "project"}, args[0]) {
		return errors.New("usage: sous-herdr board|start|stop|sync|refresh|project")
	}
	o, err := loadOptions()
	if err != nil {
		return err
	}
	b := o.bridge()
	switch args[0] {
	case "tasks":
		params := map[string]any{"plugin_id": pluginID, "entrypoint": "tasks", "placement": "overlay", "focus": true}
		return b.Host.Call(ctx, "plugin.pane.open", params, nil)
	case "start":
		return start(ctx, o)
	case "watch":
		return b.Run(ctx)
	case "stop":
		return store.WriteFile(filepath.Join(o.StateDir, "stop"), []byte("stop\n"), 0o600)
	case "sync":
		s, err := o.Provider.Read(ctx)
		if err != nil {
			b.failure(err.Error())
			_ = b.Publish(ctx)
			return err
		}
		return b.Accept(ctx, integration.Event{Kind: "event", V: 0, Provider: "sous", Type: "snapshot", Snapshot: &s, Revision: s.Revision})
	case "refresh":
		return b.Refresh(ctx)
	case "project":
		return projectPane(ctx, o)
	case "board":
		if err := start(ctx, o); err != nil {
			return err
		}
		return boardPane(ctx, o)
	}
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "sous-herdr: "+err.Error())
		os.Exit(1)
	}
}
