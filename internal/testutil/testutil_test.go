package testutil

import (
	"os"
	"testing"
)

// The developer's environment never reaches a test: git config, a zsh
// folder of theirs, how they mark their notes.
func TestTheEnvironmentIsIsolated(t *testing.T) {
	for _, k := range []string{"ZDOTDIR", "SOUS_SOURCE"} {
		if _, set := os.LookupEnv(k); set {
			t.Errorf("%s is set", k)
		}
	}
	if os.Getenv("GIT_CONFIG_GLOBAL") != os.DevNull || os.Getenv("GIT_CONFIG_NOSYSTEM") != "1" {
		t.Error("git reads the developer's config")
	}
}
