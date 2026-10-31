package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestUpdateListOrderAndPresence(t *testing.T) {
	for _, key := range []string{"roots", "plugins", "ignore", "gitlab_hosts"} {
		t.Run(key, func(t *testing.T) {
			home := t.TempDir()
			for _, tc := range []struct {
				items   []string
				add     bool
				want    []string
				changed bool
			}{
				{[]string{"absent"}, false, []string{}, true},
				{[]string{"absent"}, false, []string{}, false},
				{[]string{"one", "two"}, true, []string{"one", "two"}, true},
				{[]string{"two", "three"}, true, []string{"one", "two", "three"}, true},
				{[]string{"two"}, false, []string{"one", "three"}, true},
				{[]string{"two"}, false, []string{"one", "three"}, false},
			} {
				got, changed, err := UpdateList(home, filepath.Join(home, "person"), key, tc.items, tc.add)
				if err != nil || changed != tc.changed || !slices.Equal(got, tc.want) {
					t.Fatalf("%v add %v: %v, changed %v, error %v", tc.items, tc.add, got, changed, err)
				}
				current, err := Load(home, filepath.Join(home, "person"))
				if err != nil {
					t.Fatal(err)
				}
				k, _ := KeyNamed(key)
				if !slices.Equal(k.Value(current).([]string), tc.want) {
					t.Fatal("result differs from persisted value")
				}
			}
		})
	}
}

func TestUpdateListPreservesManualConfig(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		items           []string
		wantError       bool
	}{
		{"layout", "# keep\r\nignore = [\"base/*\"] # keep\r\n\r\n[future]\r\nvalue = 42\r\n", "# keep\r\nignore = [\"base/*\", \"new/*\"] # keep\r\n\r\n[future]\r\nvalue = 42\r\n", []string{"new/*"}, false},
		{"comments inside", "ignore = [\n# keep\n\"base/*\"\n]\n", "", []string{"base/*"}, true},
		{"malformed", "ignore = [", "", []string{"new/*"}, true},
		{"manual duplicates", "ignore = [\"base/*\", \"base/*\"]\n", "ignore = [\"base/*\", \"base/*\"]\n", []string{"base/*"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(Path(home), []byte(tc.raw), 0o644); err != nil {
				t.Fatal(err)
			}
			_, changed, err := UpdateList(home, home, "ignore", tc.items, true)
			if (err != nil) != tc.wantError || tc.wantError && changed {
				t.Fatalf("changed %v, error %v", changed, err)
			}
			want := tc.want
			if tc.wantError {
				want = tc.raw
			}
			raw, err := os.ReadFile(Path(home))
			if err != nil || string(raw) != want {
				t.Fatalf("unexpected edit: %q, %v", raw, err)
			}
		})
	}
}

func TestUpdateListKeepsConcurrentChanges(t *testing.T) {
	home := t.TempDir()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, changed, err := UpdateList(home, home, "ignore", []string{fmt.Sprintf("project%d/*", i)}, true); err != nil || !changed {
				t.Errorf("changed %v, error %v", changed, err)
			}
		}()
	}
	wg.Wait()
	c, err := Load(home, home)
	if err != nil || len(c.Ignore) != 8 {
		t.Fatalf("lost changes: %+v, %v", c, err)
	}
	for i := range 8 {
		if !slices.Contains(c.Ignore, fmt.Sprintf("project%d/*", i)) {
			t.Fatalf("missing change %d: %v", i, c.Ignore)
		}
	}
}

func TestUpdateListRequiresListKey(t *testing.T) {
	for _, key := range []string{"unknown", "agent"} {
		home := t.TempDir()
		if _, _, err := UpdateList(home, home, key, []string{"value"}, true); err == nil || !strings.Contains(err.Error(), "list") {
			t.Fatalf("key %s: %v", key, err)
		}
		if _, err := os.Stat(Path(home)); !os.IsNotExist(err) {
			t.Fatalf("invalid key created config: %v", err)
		}
	}
}
