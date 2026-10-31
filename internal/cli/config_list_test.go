package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/config"
)

func TestConfigUsesCurrentList(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "add", true: "remove"}[remove], func(t *testing.T) {
			f := fixture(t)
			if _, msg, code := f.run("config", "ignore", "base/*", "one/*", "two/*"); code != 0 {
				t.Fatal(code, msg)
			}
			if !remove {
				if _, msg, code := f.run("config", "ignore", "base/*"); code != 0 {
					t.Fatal(code, msg)
				}
			}
			var out1, out2, err1, err2 bytes.Buffer
			first := newEnv(f.Home, strings.NewReader(""), &out1, &err1)
			defer first.close()
			second := newEnv(f.Home, strings.NewReader(""), &out2, &err2)
			defer second.close()
			flag := "--add"
			if remove {
				flag = "--remove"
			}
			if c := dispatch(first, "config", []string{"ignore", flag, "one/*"}); c != 0 {
				t.Fatal(c, err1.String())
			}
			if c := dispatch(second, "config", []string{"ignore", flag, "two/*"}); c != 0 {
				t.Fatal(c, err2.String())
			}
			cfg, err := config.Load(f.SousHome, f.Home)
			if err != nil {
				t.Fatal(err)
			}
			want := "base/*,one/*,two/*"
			if remove {
				want = "base/*"
			}
			if got := strings.Join(cfg.Ignore, ","); got != want {
				t.Fatalf("lost update: got %q want %q", got, want)
			}
		})
	}
}

func TestConfigListChangeUsesCurrentOutcome(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "add", true: "remove"}[remove], func(t *testing.T) {
			f := fixture(t)
			initial := []string{"config", "ignore", "base/*"}
			if remove {
				initial = append(initial, "other/*")
			}
			if _, msg, code := f.run(initial...); code != 0 {
				t.Fatal(code, msg)
			}
			var out, errs bytes.Buffer
			stale := newEnv(f.Home, strings.NewReader(""), &out, &errs)
			defer stale.close()
			stale.JSON = true
			flag := "--add"
			if remove {
				flag = "--remove"
			}
			if _, msg, code := f.run("config", "ignore", flag, "other/*"); code != 0 {
				t.Fatal(code, msg)
			}
			if code := dispatch(stale, "config", []string{"ignore", flag, "other/*"}); code != 0 {
				t.Fatal(code, errs.String())
			}
			var got settingJSON
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err, out.String())
			}
			if got.Did != didUnchanged {
				t.Fatalf("reported %q for list already changed by another invocation: %s", got.Did, out.String())
			}
		})
	}
}

func TestConfigListPathsMatchExpandedValues(t *testing.T) {
	for _, key := range []string{"roots", "plugins"} {
		for _, remove := range []bool{false, true} {
			t.Run(key+map[bool]string{false: "/add", true: "/remove"}[remove], func(t *testing.T) {
				f := fixture(t)
				target, keep := filepath.Join(f.Home, "code"), filepath.Join(f.Home, "keep")
				if _, msg, code := f.run("config", key, target, keep); code != 0 {
					t.Fatal(code, msg)
				}
				flag := "--add"
				if remove {
					flag = "--remove"
				}
				out, msg, code := f.run("config", key, flag, "~/code", "--json")
				if code != 0 {
					t.Fatal(code, msg)
				}
				var got struct {
					Did   string   `json:"did"`
					Value []string `json:"value"`
				}
				if err := json.Unmarshal([]byte(out), &got); err != nil {
					t.Fatal(err)
				}
				want := []string{target, keep}
				did := didUnchanged
				if remove {
					want = []string{keep}
					did = didSet
				}
				if strings.Join(got.Value, ",") != strings.Join(want, ",") || got.Did != did {
					t.Fatalf("got %s; want %s %v", out, did, want)
				}
				cfg, err := config.Load(f.SousHome, f.Home)
				if err != nil {
					t.Fatal(err)
				}
				k, _ := config.KeyNamed(key)
				if stored := k.Value(cfg).([]string); strings.Join(stored, ",") != strings.Join(want, ",") {
					t.Fatalf("stored %v want %v", stored, want)
				}
			})
		}
	}
}
