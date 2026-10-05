package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestPluginConfigurationUsesHerdrContextAndPrivateSessionState(t *testing.T) {
	home := t.TempDir()
	binary, _ := os.Executable()
	config := filepath.Join(home, "config")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	text := "sous_path = " + string(mustJSON(t, binary)) + "\nnotifications = false\n"
	if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"HOME": home, "SOUS_HOME": filepath.Join(home, "sous"), "HERDR_ENV": "1", "HERDR_SOCKET_PATH": filepath.Join(home, "server.sock"),
		"HERDR_PLUGIN_CONFIG_DIR": config, "HERDR_PLUGIN_STATE_DIR": filepath.Join(home, "state"),
		"HERDR_PLUGIN_CONTEXT_JSON": `{"workspace_cwd":"/code/acme/api","focused_pane_cwd":"/code/acme/api/sub"}`,
	} {
		t.Setenv(name, value)
	}
	o, err := loadOptions()
	if err != nil || o.Notifications || o.CallerCwd != "/code/acme/api" || filepath.Dir(o.StateDir) != filepath.Join(home, "state") {
		t.Fatalf("wrong host configuration: notifications=%v, caller cwd=%q, %v", o.Notifications, o.CallerCwd, err)
	}
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(home, "other.sock"))
	other, err := loadOptions()
	if err != nil || o.StateDir == other.StateDir {
		t.Fatal("different herdr sessions shared task links and alerts")
	}
}

func TestPluginManifestUsesOnlyBuiltEntrypoints(t *testing.T) {
	var manifest struct {
		ID                             string `toml:"id"`
		Build, Startup, Actions, Panes []struct {
			ID      string   `toml:"id"`
			Command []string `toml:"command"`
		}
	}
	if _, err := toml.DecodeFile("herdr-plugin.toml", &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ID != pluginID || len(manifest.Build) != 1 || len(manifest.Startup) != 1 || len(manifest.Panes) != 2 {
		t.Fatalf("incomplete install manifest: %+v", manifest)
	}
	for _, entries := range [][]struct {
		ID      string   `toml:"id"`
		Command []string `toml:"command"`
	}{manifest.Startup, manifest.Actions, manifest.Panes} {
		for _, entry := range entries {
			if len(entry.Command) != 2 || entry.Command[0] != "./sous-herdr" {
				t.Fatalf("entrypoint is not an argv command: %+v", entry)
			}
			if entry.ID == "tasks" && len(entries) == len(manifest.Actions) && entry.Command[1] == "board" {
				t.Fatal("an action cannot run the interactive board without a PTY")
			}
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
