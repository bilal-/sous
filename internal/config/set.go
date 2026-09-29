package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/bilal-/sous/internal/store"
)

// Set writes one setting into config.toml, creating the file if needed:
// key in table (nil for the top level, or {"projects", "acme/*"}), set to a
// string, an int or a list of strings; nil removes it. Only that setting's
// text changes; comments, layout and every other setting stay as written.
// The result is checked to mean exactly the old file plus this one change,
// or nothing is written.
func Set(sousHome string, table []string, key string, value any) error {
	return store.EditFile(Path(sousHome), 0o644, func(b []byte) ([]byte, error) {
		old := string(b)
		out := edit(old, table, key, value)
		// Checked even when the text did not change: an unset that found no
		// line while the setting is there (written some other way) must
		// fail, not report it removed.
		if err := sameButFor(old, out, table, key, value); err != nil {
			return nil, fmt.Errorf("%v; edit %s by hand", err, Path(sousHome))
		}
		if out == old {
			return nil, nil
		}
		return []byte(out), nil
	})
}

// edit is the text change: replace the key's value, add the key where its
// table is (the top level goes above the first table; a table that does not
// exist is added at the end), or remove the key's line.
func edit(s string, table []string, key string, value any) string {
	start, end, found := findKey(s, table, key)
	if value == nil {
		if !found {
			return s
		}
		if end < len(s) && s[end] == '\n' {
			end++
		}
		for start > 0 && s[start-1] != '\n' {
			start--
		}
		return s[:start] + s[end:]
	}
	name := key
	if !isBareKey(key) {
		name = tomlString(key)
	}
	line := name + " = " + tomlValue(value)
	if found {
		return s[:start] + line + s[end:]
	}
	at, ok := tableEnd(s, table)
	if !ok { // a new table, at the end of the file
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		if s != "" {
			s += "\n"
		}
		return s + tableHeader(table) + "\n" + line + "\n"
	}
	if at > 0 && s[at-1] != '\n' { // the last line had no newline
		line = "\n" + line
	}
	return s[:at] + line + "\n" + s[at:]
}

// sameButFor checks that out means exactly old with the one change made.
func sameButFor(old, out string, table []string, key string, value any) error {
	var want, got map[string]any
	if _, err := toml.Decode(old, &want); err != nil {
		return fmt.Errorf("config.toml does not parse (%v)", err)
	}
	if _, err := toml.Decode(out, &got); err != nil {
		return fmt.Errorf("the change would not parse (%v)", err)
	}
	if want == nil {
		want = map[string]any{}
	}
	m := want
	for _, t := range table {
		next, ok := m[t].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[t] = next
		}
		m = next
	}
	if value == nil {
		delete(m, key)
	} else {
		m[key] = decoded(value)
	}
	if !reflect.DeepEqual(want, got) {
		return errors.New("that setting is written in a way sous cannot change safely")
	}
	return nil
}

// decoded is value as the TOML decoder would give it back.
func decoded(v any) any {
	switch v := v.(type) {
	case int:
		return int64(v)
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out
	}
	return v
}

// tomlValue writes a string, an int or a list of strings as TOML.
func tomlValue(v any) string {
	switch v := v.(type) {
	case int:
		return strconv.Itoa(v)
	case []string:
		q := make([]string, len(v))
		for i, s := range v {
			q[i] = tomlString(s)
		}
		return "[" + strings.Join(q, ", ") + "]"
	}
	return tomlString(fmt.Sprint(v))
}

// tomlString is s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tableHeader is how sous writes a table's header: [projects."acme/*"].
func tableHeader(table []string) string {
	parts := make([]string, len(table))
	for i, t := range table {
		if isBareKey(t) {
			parts[i] = t
		} else {
			parts[i] = tomlString(t)
		}
	}
	return "[" + strings.Join(parts, ".") + "]"
}

func isBareKey(s string) bool {
	for _, r := range s {
		if !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return s != ""
}

// findKey finds `key = value` directly in table (nil: the top level) and
// returns the span from the key to the end of its value.
func findKey(s string, table []string, key string) (start, end int, ok bool) {
	sc := tomlScanner{s: s}
	var current []string
	for sc.i < len(s) {
		sc.skipBlank()
		if sc.i >= len(s) {
			break
		}
		if s[sc.i] == '[' {
			current = sc.header()
			continue
		}
		lineStart := sc.i
		name := sc.keyName()
		sc.skipSpaces()
		if sc.i >= len(s) || s[sc.i] != '=' {
			sc.skipLine()
			continue
		}
		sc.i++
		sc.value()
		if name == key && slices.Equal(current, table) {
			return lineStart, sc.i, true
		}
		sc.skipLine()
	}
	return 0, 0, false
}

// tableEnd is where a new key for table goes: after its last line that is
// not blank (the top level: before the first table). false when table
// does not exist.
func tableEnd(s string, table []string) (int, bool) {
	sc := tomlScanner{s: s}
	var current []string
	in, last := len(table) == 0, 0
	for sc.i < len(s) {
		sc.skipBlank()
		if sc.i >= len(s) {
			break
		}
		if s[sc.i] == '[' {
			if in {
				return last, true
			}
			current = sc.header()
			in = slices.Equal(current, table)
			last = sc.i
			continue
		}
		sc.keyName()
		sc.skipSpaces()
		if sc.i < len(s) && s[sc.i] == '=' {
			sc.i++
			sc.value()
		}
		sc.skipLine()
		if in {
			last = sc.i
		}
	}
	if !in {
		return 0, false
	}
	if last == 0 && len(table) == 0 {
		return 0, true
	}
	return last, true
}
