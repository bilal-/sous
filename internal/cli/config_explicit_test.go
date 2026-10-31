package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestConfigReplacementPathRetry(t *testing.T) {
	for _, key := range []string{"roots", "plugins"} {
		t.Run(key, func(t *testing.T) {
			f := fixture(t)
			f.writeConfig(key + " = [\"~/code\"]\n")
			out, errs, code := f.run("config", key, "~/code", "--json")
			var got struct {
				Did   string `json:"did"`
				Value any    `json:"value"`
			}
			if code != 0 || json.Unmarshal([]byte(out), &got) != nil || got.Did != didUnchanged || !reflect.DeepEqual(got.Value, []any{"~/code"}) {
				t.Fatalf("same path reports %s: code %d stderr %s", out, code, errs)
			}
		})
	}
}

func TestConfigReplacementOutcomeUsesCurrentState(t *testing.T) {
	f := fixture(t)
	f.writeConfig("refresh_hours = 2\n")
	var out, errs bytes.Buffer
	e := newEnv(f.Home, strings.NewReader(""), &out, &errs)
	defer e.close()
	e.JSON = true
	f.writeConfig("refresh_hours = 3\n")
	code := dispatch(e, "config", []string{"refresh_hours", "2"})
	var got struct {
		Did   string `json:"did"`
		Value any    `json:"value"`
	}
	if code != 0 || json.Unmarshal(out.Bytes(), &got) != nil || got.Did != didSet {
		t.Fatalf("changed current setting reports %s: code %d stderr %s", out.String(), code, errs.String())
	}
}

// Each invocation loads the same initial snapshot. Exactly one successfully
// writes the new value; the rest repeat that already persisted change.
func TestConcurrentExplicitConfigOutcome(t *testing.T) {
	f := fixture(t)
	f.writeConfig("refresh_hours = 4\n")
	const writers = 8
	envs := make([]*Env, writers)
	outs := make([]bytes.Buffer, writers)
	errs := make([]bytes.Buffer, writers)
	codes := make([]int, writers)
	for i := range writers {
		envs[i] = newEnv(f.Home, strings.NewReader(""), &outs[i], &errs[i])
		envs[i].JSON = true
		defer envs[i].close()
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes[i] = dispatch(envs[i], "config", []string{"refresh_hours", "5"})
		}()
	}
	close(start)
	wg.Wait()
	set, unchanged := 0, 0
	for i := range writers {
		var got settingJSON
		if codes[i] != 0 || json.Unmarshal(outs[i].Bytes(), &got) != nil {
			t.Fatalf("writer %d: code=%d out=%s err=%s", i, codes[i], outs[i].String(), errs[i].String())
		}
		switch got.Did {
		case didSet:
			set++
		case didUnchanged:
			unchanged++
		default:
			t.Errorf("writer %d outcome %q", i, got.Did)
		}
	}
	if set != 1 || unchanged != writers-1 {
		t.Fatalf("set=%d unchanged=%d; want one persisted change and seven retries", set, unchanged)
	}
}
