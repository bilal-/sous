package signaltest

import (
	"os"
	"path/filepath"
	"testing"
)

func script(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "sous-signal-x")
	os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755)
	return p
}

// check runs Run in a throwaway test and reports whether it failed.
func check(argv, projects []string) (failed bool) {
	ft := &testing.T{}
	done := make(chan struct{})
	go func() { defer close(done); defer func() { recover() }(); Run(ft, argv, projects) }()
	<-done
	return ft.Failed()
}

// Review: a line without v is not a finding in the contract; the order of
// lines is not part of the contract.
func TestConformanceChecks(t *testing.T) {
	noV := script(t, `read p; echo "{\"id\":\"s:1\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"x\"}"`)
	if !check([]string{noV}, []string{"/p"}) {
		t.Error("a line without v passed")
	}
	shuffled := script(t, `read p; if [ -f "$0.ran" ]; then echo "{\"v\":0,\"id\":\"s:2\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"b\"}"; echo "{\"v\":0,\"id\":\"s:1\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"a\"}"; else touch "$0.ran"; echo "{\"v\":0,\"id\":\"s:1\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"a\"}"; echo "{\"v\":0,\"id\":\"s:2\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"b\"}"; fi`)
	if check([]string{shuffled}, []string{"/p"}) {
		t.Error("the same findings in another order failed")
	}
}
