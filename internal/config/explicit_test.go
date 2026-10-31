package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func explicitWrite(t *testing.T, home, raw string) {
	t.Helper()
	if err := os.WriteFile(Path(home), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func explicitRead(t *testing.T, home string) string {
	t.Helper()
	b, err := os.ReadFile(Path(home))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetExplicitDistinguishesDefaultsFromWrittenValues(t *testing.T) {
	home := t.TempDir()
	const userHome = "/home/sam"
	changed, err := SetExplicit(home, userHome, "", "refresh_hours", nil)
	if err != nil || changed {
		t.Fatalf("unset absent: changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(Path(home)); !os.IsNotExist(err) {
		t.Fatalf("unsetting an absent key created a file: %v", err)
	}
	for _, tc := range []struct {
		name    string
		value   any
		changed bool
		written bool
	}{
		{"write default explicitly", 4, true, true},
		{"repeat explicit default", 4, false, true},
		{"remove explicit default", nil, true, false},
		{"repeat removal", nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed, err := SetExplicit(home, userHome, "", "refresh_hours", tc.value)
			if err != nil || changed != tc.changed {
				t.Fatalf("changed=%v err=%v; want %v", changed, err, tc.changed)
			}
			cfg, err := Load(home, userHome)
			if err != nil || cfg.RefreshHours != 4 {
				t.Fatalf("effective default changed: cfg=%+v err=%v", cfg, err)
			}
			written, _, err := Written(home)
			if err != nil || written["refresh_hours"] != tc.written {
				t.Fatalf("explicit presence=%v err=%v; want %v", written, err, tc.written)
			}
		})
	}
}

func TestSetExplicitPathComparisonAndStorage(t *testing.T) {
	const userHome = "/home/sam"
	for _, tc := range []struct {
		name, key, before string
		input             []string
		wantRaw           string
		wantEffective     []string
		changed           bool
	}{
		{"roots absolute retry", "roots", "roots = [\"~/code\"] # projects\n", []string{"/home/sam/code"}, "roots = [\"~/code\"] # projects\n", []string{"/home/sam/code"}, false},
		{"roots tilde retry", "roots", "roots = [\"/home/sam/code\"]\n", []string{"~/code"}, "roots = [\"~/code\"]\n", []string{"/home/sam/code"}, false},
		{"roots changed", "roots", "roots = [\"~/code\"]\n", []string{"/home/sam/new", "/outside/api"}, "roots = [\"~/new\", \"/outside/api\"]\n", []string{"/home/sam/new", "/outside/api"}, true},
		{"plugins absolute retry", "plugins", "plugins = [\"~/bin/sous-signal-todo\"]\n", []string{"/home/sam/bin/sous-signal-todo"}, "plugins = [\"/home/sam/bin/sous-signal-todo\"]\n", []string{"/home/sam/bin/sous-signal-todo"}, false},
		{"plugins literal retry", "plugins", "plugins = [\"/home/sam/bin/sous-signal-todo\"]\n", []string{"~/bin/sous-signal-todo"}, "plugins = [\"~/bin/sous-signal-todo\"]\n", []string{"/home/sam/bin/sous-signal-todo"}, false},
		{"plugins preserve order and spelling", "plugins", "plugins = [\"~/bin/sous-signal-todo\"]\n", []string{"/outside/sous-signal-other", "~/bin/sous-signal-todo"}, "plugins = [\"/outside/sous-signal-other\", \"~/bin/sous-signal-todo\"]\n", []string{"/outside/sous-signal-other", "/home/sam/bin/sous-signal-todo"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			explicitWrite(t, home, tc.before)
			input := append([]string(nil), tc.input...)
			changed, err := SetExplicit(home, userHome, "", tc.key, input)
			if err != nil || changed != tc.changed {
				t.Fatalf("changed=%v err=%v; want %v", changed, err, tc.changed)
			}
			if !reflect.DeepEqual(input, tc.input) {
				t.Fatalf("caller input changed: %v; want %v", input, tc.input)
			}
			if got := explicitRead(t, home); got != tc.wantRaw {
				t.Fatalf("stored bytes: %q; want %q", got, tc.wantRaw)
			}
			cfg, err := Load(home, userHome)
			if err != nil {
				t.Fatal(err)
			}
			key, _ := KeyNamed(tc.key)
			if got := key.Value(cfg).([]string); !reflect.DeepEqual(got, tc.wantEffective) {
				t.Fatalf("effective paths: %v; want %v", got, tc.wantEffective)
			}
		})
	}
}

func TestSetExplicitEmptyListIsNotAnUnset(t *testing.T) {
	home := t.TempDir()
	var typedNil []string
	for _, tc := range []struct {
		name    string
		value   any
		changed bool
		written bool
	}{
		{"typed nil creates explicit empty list", typedNil, true, true},
		{"typed nil repeats empty list", typedNil, false, true},
		{"empty slice repeats empty list", []string{}, false, true},
		{"nil interface removes key", nil, true, false},
		{"remove twice", nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed, err := SetExplicit(home, "/home/sam", "", "ignore", tc.value)
			if err != nil || changed != tc.changed {
				t.Fatalf("changed=%v err=%v; want %v", changed, err, tc.changed)
			}
			written, _, err := Written(home)
			if err != nil || written["ignore"] != tc.written {
				t.Fatalf("presence=%v err=%v; want %v", written, err, tc.written)
			}
			cfg, err := Load(home, "/home/sam")
			if err != nil || len(cfg.Ignore) != 0 {
				t.Fatalf("empty list: %+v %v", cfg, err)
			}
			want := ""
			if tc.written {
				want = "ignore = []\n"
			}
			if got := explicitRead(t, home); got != want {
				t.Fatalf("raw=%q; want %q", got, want)
			}
		})
	}
}

func TestSetExplicitProjectPresenceIsSeparateFromInheritance(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"backend", "markdown"}, {"plugin mode", "~/shared"}} {
		t.Run(tc.key, func(t *testing.T) {
			home := t.TempDir()
			// The second case is an arbitrary plugin key with literal tilde text.
			before := "[projects.\"acme/*\"] # inherited settings\n\"" + tc.key + "\" = \"" + tc.value + "\"\n"
			explicitWrite(t, home, before)
			for _, step := range []struct {
				name    string
				value   any
				changed bool
				written bool
			}{
				{"missing exact unset", nil, false, false},
				{"write inherited value explicitly", tc.value, true, true},
				{"repeat exact override", tc.value, false, true},
				{"remove exact override", nil, true, false},
				{"repeat exact removal", nil, false, false},
			} {
				t.Run(step.name, func(t *testing.T) {
					changed, err := SetExplicit(home, "/home/sam", "acme/api", tc.key, step.value)
					if err != nil || changed != step.changed {
						t.Fatalf("changed=%v err=%v; want %v", changed, err, step.changed)
					}
					cfg, err := Load(home, "/home/sam")
					if err != nil {
						t.Fatal(err)
					}
					_, present := cfg.Projects["acme/api"][tc.key]
					if present != step.written || cfg.Projects["acme/*"][tc.key] != tc.value || cfg.Project("acme/api").Raw[tc.key] != tc.value {
						t.Fatalf("wrong explicit/inherited settings: %#v", cfg.Projects)
					}
				})
			}
			if got := explicitRead(t, home); got != before {
				t.Fatalf("wildcard or comments changed after removing exact override:\n%q\nwant %q", got, before)
			}
		})
	}
}

func TestSetExplicitRejectsLatestMalformedContents(t *testing.T) {
	for _, malformed := range []string{"roots = [\n", "refresh_hours = \"later\"\n"} {
		for _, value := range []any{2, nil} {
			t.Run(malformed+"/"+map[bool]string{false: "replace", true: "unset"}[value == nil], func(t *testing.T) {
				home := t.TempDir()
				explicitWrite(t, home, "refresh_hours = 4\n")
				if _, err := Load(home, "/home/sam"); err != nil {
					t.Fatal(err)
				}
				explicitWrite(t, home, malformed)
				changed, err := SetExplicit(home, "/home/sam", "", "refresh_hours", value)
				if err == nil || changed {
					t.Fatalf("malformed current state: changed=%v err=%v", changed, err)
				}
				if got := explicitRead(t, home); got != malformed {
					t.Fatalf("malformed state overwritten: %q", got)
				}
			})
		}
	}
}

func TestSetExplicitStillRefusesCommentedArrayOnNoOp(t *testing.T) {
	for _, key := range []string{"roots", "plugins"} {
		t.Run(key, func(t *testing.T) {
			home := t.TempDir()
			before := key + " = [\n  \"~/code\", # why this folder matters\n]\n"
			explicitWrite(t, home, before)
			changed, err := SetExplicit(home, "/home/sam", "", key, []string{"~/code"})
			if err == nil || changed || !strings.Contains(err.Error(), "comments") {
				t.Fatalf("unsafe no-op: changed=%v err=%v", changed, err)
			}
			if got := explicitRead(t, home); got != before {
				t.Fatalf("comment lost: %q", got)
			}
		})
	}
}

func TestSetExplicitKeepsUnknownFieldsAndLayout(t *testing.T) {
	home := t.TempDir()
	before := "# personal layout\r\nrefresh_hours = 4 # hours\r\ncustom = { labels = [\"one\", \"two\"] }\r\n\r\n[projects.'acme/*'] # project defaults\r\n\"plugin mode\" = \"kept\"\r\n"
	explicitWrite(t, home, before)
	changed, err := SetExplicit(home, "/home/sam", "", "refresh_hours", 8)
	if err != nil || !changed {
		t.Fatalf("replace: changed=%v err=%v", changed, err)
	}
	// Set already normalizes this spacing on the changed scalar line.
	want := strings.Replace(before, "refresh_hours = 4 ", "refresh_hours = 8", 1)
	if got := explicitRead(t, home); got != want {
		t.Fatalf("unrelated bytes changed: %q; want %q", got, want)
	}
	changed, err = SetExplicit(home, "/home/sam", "acme/*", "plugin mode", "~/literal")
	if err != nil || !changed {
		t.Fatalf("project replacement: changed=%v err=%v", changed, err)
	}
	want = strings.Replace(want, `"kept"`, `"~/literal"`, 1)
	if got := explicitRead(t, home); got != want {
		t.Fatalf("unknown field or layout changed: %q; want %q", got, want)
	}
	cfg, err := Load(home, "/home/sam")
	if err != nil || cfg.Projects["acme/*"]["plugin mode"] != "~/literal" {
		t.Fatalf("project string was expanded: %#v err=%v", cfg, err)
	}
}
