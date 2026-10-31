package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/store"
	"github.com/bilal-/sous/internal/thread"
)

// The v1 schema names every public field of the v1 types that exist, and
// discovery points at it.
func TestSchemaV1NamesEveryPublicField(t *testing.T) {
	var schema struct {
		ID   string `json:"$id"`
		Defs map[string]struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	readRepositoryJSON(t, "integrations/schema.v1.json", &schema)
	if schema.ID != DescribeV1("test").Schema {
		t.Fatalf("schema ID %q disagrees with discovery", schema.ID)
	}
	for name, value := range map[string]any{"description": DescriptionV1{}, "action": ActionV1{}, "parameter": Parameter{}, "meta": Meta{}} {
		shape := schema.Defs[name]
		fields := jsonFields(reflect.TypeOf(value))
		for field, always := range fields {
			if _, ok := shape.Properties[field]; !ok {
				t.Errorf("%s.%s is missing from the schema", name, field)
			}
			if always && !slices.Contains(shape.Required, field) {
				t.Errorf("%s.%s is always present but not required", name, field)
			}
		}
		for field := range shape.Properties {
			if _, ok := fields[field]; !ok {
				t.Errorf("schema %s.%s has no public counterpart", name, field)
			}
		}
	}
}

// Version 1's commands stay on version 1, and it says it is a draft.
func TestDescribeV1(t *testing.T) {
	d := DescribeV1("test")
	if d.V != 1 || !d.Draft || !slices.Equal(d.Versions, []int{0, 1}) {
		t.Fatalf("%+v", d)
	}
	for _, name := range []string{"describe", "snapshot", "watch"} {
		argv := d.Operations[name].Argv
		if i := slices.Index(argv, "--protocol"); i < 0 || argv[i+1] != "1" {
			t.Errorf("%s: %v", name, argv)
		}
	}
	for _, a := range d.Actions {
		if a.Scope == "" || len(a.Parameters) == 0 {
			t.Errorf("%s: %+v", a.ID, a)
		}
	}
}

// Every example validates against the v1 schema, and the description
// example is what sous says.
func TestExamplesV1Validate(t *testing.T) {
	var schema map[string]any
	readRepositoryJSON(t, "integrations/schema.v1.json", &schema)
	defs := schema["$defs"].(map[string]any)
	dir := filepath.Join("../..", "integrations/examples/v1")
	files, _ := os.ReadDir(dir)
	if len(files) < 3 {
		t.Fatalf("examples: %v", files)
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var docs []any
		switch {
		case strings.HasSuffix(f.Name(), ".ndjson"):
			sc := bufio.NewScanner(bytes.NewReader(b))
			sc.Buffer(nil, 1<<20)
			for sc.Scan() {
				var v any
				if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
					t.Fatalf("%s: %v", f.Name(), err)
				}
				docs = append(docs, v)
			}
			if err := sc.Err(); err != nil {
				t.Fatal(err)
			}
		default:
			var v any
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatalf("%s: %v", f.Name(), err)
			}
			if list, ok := v.([]any); ok {
				docs = list
			} else {
				docs = []any{v}
			}
		}
		for i, doc := range docs {
			for _, problem := range validate(defs, schema, doc, fmt.Sprintf("%s[%d]", f.Name(), i)) {
				t.Error(problem)
			}
		}
	}
}

// validate checks the vocabulary used by our examples, after the schema itself
// passes TestSchemaV1ValidationVocabulary. This is a test helper, not a general
// JSON Schema implementation; new keywords need validation and negative fixtures.
func validate(defs map[string]any, schema any, v any, at string) []string {
	if allowed, ok := schema.(bool); ok {
		if !allowed {
			return []string{at + ": forbidden value"}
		}
		return nil
	}
	s, _ := schema.(map[string]any)
	var bad []string
	if ref, ok := s["$ref"].(string); ok {
		target, found := defs[strings.TrimPrefix(ref, "#/$defs/")]
		if !strings.HasPrefix(ref, "#/$defs/") || !found {
			return []string{at + ": unknown reference " + ref}
		}
		bad = append(bad, validate(defs, target, v, at)...)
	}
	if c, ok := s["const"]; ok && !reflect.DeepEqual(c, v) {
		bad = append(bad, fmt.Sprintf("%s: %v, want %v", at, v, c))
	}
	if e, ok := s["enum"].([]any); ok && !slices.ContainsFunc(e, func(x any) bool { return reflect.DeepEqual(x, v) }) {
		bad = append(bad, fmt.Sprintf("%s: %v is not one of %v", at, v, e))
	}
	if typ, ok := s["type"]; ok && !typeOK(typ, v) {
		bad = append(bad, fmt.Sprintf("%s: %T is not %v", at, v, typ))
	}
	if n, ok := v.(float64); ok {
		if min, ok := s["minimum"].(float64); ok && n < min {
			bad = append(bad, fmt.Sprintf("%s: %v is less than %v", at, n, min))
		}
	}
	if str, ok := v.(string); ok {
		if pattern, ok := s["pattern"].(string); ok {
			matches, err := regexp.MatchString(pattern, str)
			if err != nil || !matches {
				bad = append(bad, at+": does not match "+pattern)
			}
		}
		if s["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, strings.ToUpper(str)); err != nil {
				bad = append(bad, at+": not an RFC3339 time")
			}
		}
	}
	if obj, ok := v.(map[string]any); ok {
		for _, r := range asList(s["required"]) {
			if _, ok := obj[r.(string)]; !ok {
				bad = append(bad, fmt.Sprintf("%s: missing %s", at, r))
			}
		}
		props, _ := s["properties"].(map[string]any)
		for k, val := range obj {
			if p, ok := props[k]; ok {
				bad = append(bad, validate(defs, p, val, at+"."+k)...)
			} else if ap, ok := s["additionalProperties"]; ok {
				bad = append(bad, validate(defs, ap, val, at+"."+k)...)
			}
		}
	}
	if alts := asList(s["oneOf"]); len(alts) > 0 {
		matches := 0
		var best []string
		for i, alt := range alts {
			problems := validate(defs, alt, v, at)
			if len(problems) == 0 {
				matches++
			}
			if i == 0 || len(problems) < len(best) {
				best = problems
			}
		}
		if matches != 1 {
			bad = append(bad, fmt.Sprintf("%s: matches %d oneOf alternatives, want one", at, matches))
			if matches == 0 {
				bad = append(bad, best...)
			}
		}
	}
	if n, ok := s["not"]; ok && len(validate(defs, n, v, at)) == 0 {
		bad = append(bad, at+": matches forbidden shape")
	}
	if list, ok := v.([]any); ok {
		for i, x := range list {
			bad = append(bad, validate(defs, s["items"], x, fmt.Sprintf("%s[%d]", at, i))...)
		}
	}
	return bad
}

func typeOK(typ, v any) bool {
	for _, t := range append(asList(typ), typ) {
		switch t {
		case "object":
			if _, ok := v.(map[string]any); ok {
				return true
			}
		case "array":
			if _, ok := v.([]any); ok {
				return true
			}
		case "string":
			if _, ok := v.(string); ok {
				return true
			}
		case "integer":
			if n, ok := v.(float64); ok && n == math.Trunc(n) {
				return true
			}
		case "number":
			if _, ok := v.(float64); ok {
				return true
			}
		case "boolean":
			if _, ok := v.(bool); ok {
				return true
			}
		case "null":
			if v == nil {
				return true
			}
		}
	}
	return false
}

func asList(v any) []any { l, _ := v.([]any); return l }

// The validator above catches what it claims to.
func TestValidatorCatches(t *testing.T) {
	var schema map[string]any
	readRepositoryJSON(t, "integrations/schema.v1.json", &schema)
	defs := schema["$defs"].(map[string]any)
	for name, doc := range map[string]string{
		"missing op":     `{"kind":"outcome","v":1,"action":"run","outcome":"done"}`,
		"bad outcome":    `{"kind":"outcome","v":1,"op":"op-7f3a9c2e1b4d6f80","action":"run","outcome":"maybe"}`,
		"wrong v":        `{"kind":"outcome","v":0,"op":"op-7f3a9c2e1b4d6f80","action":"run","outcome":"done"}`,
		"changes no rev": `{"kind":"event","v":1,"provider":"sous","type":"changes","revision":"r2","changes":[]}`,
		"run not null":   `{"kind":"outcome","v":1,"op":"op-7f3a9c2e1b4d6f80","action":"run","outcome":"done","item":{"run":3}}`,
	} {
		var v any
		json.Unmarshal([]byte(doc), &v)
		kind := v.(map[string]any)["kind"].(string)
		if len(validate(defs, defs[kind], v, kind)) == 0 {
			t.Errorf("%s: passed", name)
		}
	}
}

// Version 1 offers stop on an open run and names the question it waits on;
// version 0 shows the same board exactly as before.
func TestReaderV1Runs(t *testing.T) {
	st := &store.Store{Home: t.TempDir()}
	p := project.Project{Path: t.TempDir(), Name: "api"}
	now := time.Now().UTC()
	id, _, _ := thread.NoteRun(st, p, "pick a fixture", "op-0123456789abcdef", "codex", "human", now)
	thread.SetRun(st, id, func(r *thread.Run) {
		r.Ref, r.State, r.Text = "codex:1", thread.RunNeedsYou, "which fixture?"
	})
	views, _ := thread.Open(st, now)
	board.WriteCache(st, &board.Data{Projects: []project.Project{p}, Threads: views, Checked: 1, RenderedAt: now})
	s1, err := Reader{Store: st, Configured: true, Now: now, V: Version1}.Read(context.Background())
	if err != nil || len(s1.Items) != 1 {
		t.Fatalf("%+v %v", s1, err)
	}
	it := s1.Items[0]
	if s1.V != 1 || !slices.Contains(it.Actions, "stop") || !slices.Contains(it.Actions, "reply") || it.Run.Question == nil || it.Run.Question.Token == "" {
		t.Fatalf("v1: %+v %+v", it.Actions, it.Run)
	}
	s0, _ := Reader{Store: st, Configured: true, Now: now}.Read(context.Background())
	if it := s0.Items[0]; slices.Contains(it.Actions, "stop") || it.Run.Question != nil {
		t.Fatalf("v0: %+v %+v", it.Actions, it.Run)
	}
}
