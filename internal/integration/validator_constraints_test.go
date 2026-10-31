package integration

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Examples must fail validation when required data or JSON types are wrong.
func TestSchemaV1RejectsMalformedFixtures(t *testing.T) {
	var schema map[string]any
	readRepositoryJSON(t, "integrations/schema.v1.json", &schema)
	defs := schema["$defs"].(map[string]any)
	for _, kind := range []string{"item.added", "item.updated"} {
		t.Run(kind+"_without_item", func(t *testing.T) {
			doc := map[string]any{"type": kind, "key": "n:fixture", "id": "1"}
			if bad := validate(defs, defs["change"], doc, "change"); len(bad) == 0 {
				t.Fatal("accepted malformed change: schema requires item for added/updated")
			}
		})
	}
	for _, tc := range []struct {
		name  string
		field string
		value any
	}{
		{"string_version", "v", "1"},
		{"fractional_checked", "checked", 0.5},
		{"negative_checked", "checked", -1.0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var doc map[string]any
			readRepositoryJSON(t, "integrations/examples/v1/snapshot.json", &doc)
			if bad := validate(defs, defs["snapshot"], doc, "snapshot"); len(bad) != 0 {
				t.Fatalf("valid fixture rejected before mutation: %v", bad)
			}
			doc[tc.field] = tc.value
			if bad := validate(defs, defs["snapshot"], doc, "snapshot"); len(bad) == 0 {
				t.Fatalf("accepted malformed snapshot: %s=%v (%T)", tc.field, tc.value, tc.value)
			}
		})
	}
}

// JSON Schema unions must accept one matching shape and reject ambiguity.
func TestValidatorJSONTypesAndUnions(t *testing.T) {
	for _, tc := range []struct {
		name, schema, value string
		valid               bool
	}{
		{"missing reference", `{"$ref":"#/$defs/missing"}`, `{}`, false},
		{"pattern", `{"type":"string","pattern":"^[0-9a-f]{64}$"}`, `"bad"`, false},
		{"date time", `{"type":"string","format":"date-time"}`, `"yesterday"`, false},
		{"nullable time", `{"type":["string","null"],"format":"date-time"}`, `null`, true},
		{"timestamp", `{"type":"string","format":"date-time"}`, `"2026-10-01T12:34:56.123Z"`, true},
		{"forbidden kind", `{"type":"object","not":{"required":["kind"]}}`, `{"kind":"error"}`, false},
		{"absent forbidden kind", `{"type":"object","not":{"required":["kind"]}}`, `{"error":"failed"}`, true},
		{"extra property", `{"type":"object","additionalProperties":false}`, `{"extra":true}`, false},
		{"typed const", `{"const":1}`, `"1"`, false},
		{"typed enum", `{"enum":[1,true]}`, `"true"`, false},
		{"integer", `{"type":"integer"}`, `2.0`, true},
		{"fraction", `{"type":"integer"}`, `2.5`, false},
		{"number", `{"type":"number"}`, `2.5`, true},
		{"minimum", `{"type":"number","minimum":0}`, `-0.5`, false},
		{"minimum boundary", `{"type":"number","minimum":0}`, `0`, true},
		{"object or null", `{"type":["object","null"]}`, `null`, true},
		{"array or string", `{"type":["array","string"]}`, `"ok"`, true},
		{"one shape", `{"oneOf":[{"type":"string"},{"type":"number"}]}`, `1`, true},
		{"no shape", `{"oneOf":[{"type":"string"},{"type":"number"}]}`, `null`, false},
		{"two shapes", `{"oneOf":[{"type":"number"},{"type":"integer"}]}`, `1`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var schema, value any
			if err := json.Unmarshal([]byte(tc.schema), &schema); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.value), &value); err != nil {
				t.Fatal(err)
			}
			bad := validate(nil, schema, value, "fixture")
			if (len(bad) == 0) != tc.valid {
				t.Fatalf("valid=%v, want %v: %v", len(bad) == 0, tc.valid, bad)
			}
		})
	}
}

// Schema vocabulary is checked even in branches that no example exercises.
// This prevents unsupported rules or dangling references under not/oneOf from
// being mistaken for an ordinary nonmatching document.
func TestSchemaV1ValidationVocabulary(t *testing.T) {
	var schema map[string]any
	readRepositoryJSON(t, "integrations/schema.v1.json", &schema)
	defs := schema["$defs"].(map[string]any)
	var check func(any, string)
	check = func(node any, at string) {
		if _, ok := node.(bool); ok {
			return
		}
		s, ok := node.(map[string]any)
		if !ok {
			t.Errorf("%s: not a schema", at)
			return
		}
		for key, value := range s {
			switch key {
			case "$schema", "$id", "title", "description", "const", "enum", "required", "minimum":
			case "type":
				for _, typ := range append(asList(value), value) {
					if _, list := typ.([]any); list {
						continue
					}
					if !slices.Contains([]any{"object", "array", "string", "number", "integer", "boolean", "null"}, typ) {
						t.Errorf("%s: unsupported type %v", at, typ)
					}
				}
			case "$ref":
				ref, _ := value.(string)
				if _, exists := defs[strings.TrimPrefix(ref, "#/$defs/")]; !strings.HasPrefix(ref, "#/$defs/") || !exists {
					t.Errorf("%s: unknown reference %v", at, value)
				}
			case "format":
				if value != "date-time" {
					t.Errorf("%s: unsupported format %v", at, value)
				}
			case "pattern":
				pattern, ok := value.(string)
				if !ok {
					t.Errorf("%s: pattern is not a string", at)
					continue
				}
				if _, err := regexp.Compile(pattern); err != nil {
					t.Errorf("%s: %v", at, err)
				}
			case "$defs", "properties":
				for name, child := range value.(map[string]any) {
					check(child, at+"/"+key+"/"+name)
				}
			case "items", "additionalProperties", "not":
				check(value, at+"/"+key)
			case "oneOf":
				for i, child := range value.([]any) {
					check(child, fmt.Sprintf("%s/oneOf/%d", at, i))
				}
			default:
				t.Errorf("%s: unsupported validation keyword %s", at, key)
			}
		}
	}
	check(schema, "schema")
}
