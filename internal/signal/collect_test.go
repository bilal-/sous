package signal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func script(t *testing.T, dir, name, body string) string {
	p := filepath.Join(dir, name)
	os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755)
	return p
}

func TestCollectStatuses(t *testing.T) {
	dir := t.TempDir()
	ok := script(t, dir, "sous-signal-ok", `while read p; do echo "{\"v\":0,\"id\":\"s:aaaaaaaaaaaa\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"review\",\"observed\":\"2026-01-01T00:00:00Z\",\"ref\":null}"; done`)
	broken := script(t, dir, "sous-signal-broken", `echo boom >&2; exit 1`)
	slow := script(t, dir, "sous-signal-slow", `sleep 3`)
	plugins := Plugins("", nil, []string{ok, broken, slow})
	c := Collect(context.Background(), plugins, []string{"/p one", "/p two"}, 500*time.Millisecond)
	st := map[string]PluginStatus{}
	for _, p := range c.Plugins {
		st[p.Name] = p
	}
	if st["ok"].Status != "ok" || st["broken"].Status != "failed" || st["slow"].Status != "timeout" {
		t.Fatalf("statuses: %+v", st)
	}
	if st["broken"].Error == nil || *st["broken"].Error != "boom" {
		t.Fatalf("error captured: %+v", st["broken"])
	}
	if len(c.Signals) != 2 || c.Signals[0].Plugin != "ok" || c.Signals[1].Project != "/p two" {
		t.Fatalf("signals: %+v", c.Signals)
	}
}

func TestPluginsNaming(t *testing.T) {
	ps := Plugins("/bin/sous", []string{"git", "github"}, []string{"/x/sous-signal-jira", "/x/not-a-plugin"})
	if len(ps) != 3 || ps[0].Name != "git" || ps[0].Argv[0] != "/bin/sous" || ps[0].Argv[2] != "git" || ps[2].Name != "jira" {
		t.Fatalf("%+v", ps)
	}
}

// A plugin that emits findings and then fails (one of two queries errored)
// must have its findings kept alongside the failed status.
func TestCollectKeepsPartialOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	partial := script(t, dir, "sous-signal-partial", `echo '{"v":0,"id":"s:aaaaaaaaaaaa","project":"/p","kind":"me","text":"review","observed":"2026-01-01T00:00:00Z","ref":null}'; echo "second query failed" >&2; exit 1`)
	c := Collect(context.Background(), Plugins("", nil, []string{partial}), []string{"/p"}, time.Second)
	if len(c.Plugins) != 1 || c.Plugins[0].Status != "failed" || *c.Plugins[0].Error != "second query failed" {
		t.Fatalf("status: %+v", c.Plugins)
	}
	if len(c.Signals) != 1 || c.Signals[0].Text != "review" {
		t.Fatalf("partial output must be kept: %+v", c.Signals)
	}
}

// Review 2 C1: a shell-wrapper plugin forks a child that inherits stdout;
// killing only the direct child leaves Wait blocked on the pipe. The runner
// must kill the process group and bound the wait.
func TestCollectKillsProcessGroupOnTimeout(t *testing.T) {
	dir := t.TempDir()
	forker := script(t, dir, "sous-signal-forker", `sleep 30`) // sh forks sleep; sh dies, sleep keeps the pipe
	start := time.Now()
	c := Collect(context.Background(), Plugins("", nil, []string{forker}), []string{"/p"}, 500*time.Millisecond)
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("Collect hung %v past a 500ms timeout", el)
	}
	if c.Plugins[0].Status != "timeout" {
		t.Fatalf("%+v", c.Plugins)
	}
}

// Review 2 M1-adjacent, promoted: an ok plugin with one stray non-JSON line
// must not lose all its findings.
func TestCollectSkipsBadLinesKeepsGood(t *testing.T) {
	dir := t.TempDir()
	mixed := script(t, dir, "sous-signal-mixed", `echo '{"v":0,"id":"s:aaaaaaaaaaaa","project":"/p","kind":"me","text":"review","observed":"2026-01-01T00:00:00Z","ref":null}'; echo 'debug: hello'; echo '{"v":0,"id":"s:bbbbbbbbbbbb","project":"/p","kind":"me","text":"two","observed":"2026-01-01T00:00:00Z","ref":null}'`)
	c := Collect(context.Background(), Plugins("", nil, []string{mixed}), []string{"/p"}, time.Second)
	if len(c.Signals) != 2 {
		t.Fatalf("good lines must survive a bad one: %+v", c.Signals)
	}
	if c.Plugins[0].Status != StatusFailed || c.Plugins[0].Error == nil || !strings.Contains(*c.Plugins[0].Error, "1 unreadable line") {
		t.Fatalf("bad line must be counted: %+v", c.Plugins[0])
	}
}

// Exit 3 means "not set up on this machine": status off, reason kept, and
// multi-line tool output squashed to one readable line.
func TestCollectNotSetUp(t *testing.T) {
	dir := t.TempDir()
	off := script(t, dir, "sous-signal-off", `printf 'gh: not logged in\n\n   run gh auth login\n' >&2; exit 3`)
	c := Collect(context.Background(), Plugins("", nil, []string{off}), []string{"/p"}, time.Second)
	if st := c.Plugins[0]; st.Status != "off" || st.Error == nil || *st.Error != "gh: not logged in run gh auth login" {
		t.Fatalf("%+v %q", st, *st.Error)
	}
}

// Review: a plugin whose output was partly unreadable did not report
// everything, so what it found before is kept (stale), not dropped.
func TestUnreadableLinesMakeThePluginIncomplete(t *testing.T) {
	dir := t.TempDir()
	p := script(t, dir, "sous-signal-half", `echo "not json"`)
	c := Collect(context.Background(), Plugins("", nil, []string{p}), []string{"/p"}, time.Second)
	if c.Plugins[0].Status == StatusOK {
		t.Fatalf("%+v", c.Plugins[0])
	}
}
