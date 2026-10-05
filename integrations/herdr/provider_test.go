package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/integration"
)

func TestMain(m *testing.M) {
	if os.Getenv("SOUS_HERDR_TEST_NATIVE") == "1" {
		if len(os.Args) > 1 && (os.Args[1] == "integration" || os.Args[1] == "here") {
			fakeSousProcess()
			os.Exit(0)
		}
		main()
		os.Exit(0)
	}
	if os.Getenv("SOUS_HERDR_TEST_COMMAND") == "1" {
		cwd, _ := os.Getwd()
		b, _ := json.Marshal(map[string]any{"argv": os.Args[1:], "cwd": cwd})
		_ = os.WriteFile(os.Getenv("SOUS_HERDR_TEST_RESULT"), b, 0o600)
		_, _ = os.Stdout.WriteString(`{"did":"noted","id":"1"}` + "\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeSousProcess() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if os.Args[1] == "here" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"project": os.Args[len(os.Args)-1]})
		return
	}
	if len(os.Args) < 3 || os.Args[2] == "--json" {
		_ = json.NewEncoder(os.Stdout).Encode(integration.Describe("test"))
		return
	}
	read := func(context.Context) (integration.Snapshot, error) {
		b, err := os.ReadFile(os.Getenv("SOUS_HERDR_TEST_SNAPSHOT"))
		var s integration.Snapshot
		if err == nil {
			err = json.Unmarshal(b, &s)
		}
		return s, err
	}
	if os.Args[2] == "watch" {
		_ = integration.Watch(ctx, read, 20*time.Millisecond, func(e integration.Event) error { return json.NewEncoder(os.Stdout).Encode(e) })
		return
	}
	s, err := read(ctx)
	if err != nil {
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(s)
}

func TestAdvertisedActionsUseLiteralArgvAndWorkingDirectory(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	project := filepath.Join(home, "acme", "a pi")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(home, "result.json")
	p := &CLIProvider{Binary: binary, Env: []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "SOUS_HOME=" + filepath.Join(home, "sous"), "SOUS_HERDR_TEST_COMMAND=1", "SOUS_HERDR_TEST_RESULT=" + result}}
	text := "--help; $(touch should-not-exist)\ncheck parser"
	var note integration.Action
	for _, action := range integration.Describe("test").Actions {
		if action.ID == "note" {
			note = action
		}
	}
	if _, err := p.Invoke(context.Background(), note, map[string]string{"project": project, "text": text}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Argv []string `json:"argv"`
		Cwd  string   `json:"cwd"`
	}
	b, _ := os.ReadFile(result)
	canonical, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil || !reflect.DeepEqual(got.Argv, []string{"note", "--json", "--", text}) || got.Cwd != canonical {
		t.Fatalf("text or path was interpreted instead of passed literally: %s, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(project, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatal("action text ran as shell code")
	}
	if _, _, err := bindAction(binary, note, map[string]string{"text": text}); err == nil {
		t.Fatal("missing project parameter was silently accepted")
	}
}

func TestTaskViewHidesImplementationKeysAndSanitizesControlSequences(t *testing.T) {
	state := bridgeState{Version: 1, Available: true, Snapshot: ptr(snapshot("first", "running"))}
	state.Snapshot.Items[0].Text = "check parser\x1b[2J\nthen retry"
	view := View{Scope: "all"}
	output := view.Render(state, 86, 22)
	if strings.Contains(output, "\x1b") || strings.Contains(output, "n:retry") || !strings.Contains(output, "check parser") || !strings.Contains(output, "Tasks") {
		t.Fatalf("unsafe or implementation-facing view: %q", output)
	}
	state.Available, state.Error = false, "source unreadable"
	output = view.Render(state, 86, 22)
	if !strings.Contains(output, "source unreadable") || !strings.Contains(output, "check parser") {
		t.Fatal("unavailable source hid known work")
	}
}

func ptr[T any](v T) *T { return &v }

func TestPromptKeepsTaskIdentityWhenTheListChanges(t *testing.T) {
	s := snapshot("first", "running")
	v := View{Scope: "all", Project: "/code/acme/other"}
	v.beginPrompt("Close this note? Type yes", s.Items[0])
	s.Items[0].Key, s.Items[0].Project = "n:another", "/code/acme/other"
	_ = v.Render(bridgeState{Available: true, Snapshot: &s}, 86, 22)
	if v.PromptKey != "n:retry" || v.PromptProject != "/code/acme/api" {
		t.Fatal("a changing list redirected an in-progress action")
	}
}
