package signaltest

import (
	"github.com/bilal-/sous/internal/testutil"
	"testing"
)

// check runs Run in a throwaway test and reports whether it failed.
func check(argv, projects []string) (failed bool) {
	ft := &testing.T{}
	done := make(chan struct{})
	go func() { defer close(done); defer func() { recover() }(); Run(ft, argv, projects) }()
	<-done
	return ft.Failed()
}

// A line without v is not a finding in the contract; the order of
// lines is not part of the contract.
func TestConformanceChecks(t *testing.T) {
	noV := testutil.Script(t, t.TempDir(), "sous-signal-x", `read p; echo "{\"id\":\"s:1\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"x\"}"`)
	if !check([]string{noV}, []string{"/p"}) {
		t.Error("a line without v passed")
	}
	shuffled := testutil.Script(t, t.TempDir(), "sous-signal-x", `read p; if [ -f "$0.ran" ]; then echo "{\"v\":0,\"id\":\"s:2\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"b\"}"; echo "{\"v\":0,\"id\":\"s:1\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"a\"}"; else touch "$0.ran"; echo "{\"v\":0,\"id\":\"s:1\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"a\"}"; echo "{\"v\":0,\"id\":\"s:2\",\"project\":\"$p\",\"kind\":\"me\",\"text\":\"b\"}"; fi`)
	if check([]string{shuffled}, []string{"/p"}) {
		t.Error("the same findings in another order failed")
	}
}
