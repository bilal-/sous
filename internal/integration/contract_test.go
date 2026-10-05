package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bilal-/sous/internal/board"
	"github.com/bilal-/sous/internal/project"
	"github.com/bilal-/sous/internal/signal"
)

func TestRepositoryDiscoveryMatchesRuntimeContract(t *testing.T) {
	var record struct {
		Kind          string   `json:"kind"`
		V             int      `json:"v"`
		Provider      string   `json:"provider"`
		Protocol      string   `json:"protocol"`
		Host          string   `json:"host"`
		Discover      []string `json:"discover"`
		Documentation string   `json:"documentation"`
		Schema        string   `json:"schema"`
	}
	readRepositoryJSON(t, "integrations/herdr/provider.json", &record)
	d := Describe("test")
	if record.Kind != "discovery" || record.Host != "herdr" || record.V != d.V || record.Provider != d.Provider || record.Protocol != d.Protocol {
		t.Fatalf("repository discovery disagrees with runtime: %+v", record)
	}
	if !reflect.DeepEqual(record.Discover, d.Operations["describe"].Argv) {
		t.Fatalf("discovery command = %v; runtime = %v", record.Discover, d.Operations["describe"].Argv)
	}
	for _, path := range []string{record.Documentation, record.Schema} {
		if path == "" {
			t.Fatal("missing discovery resource")
		}
		if _, err := os.Stat(filepath.Join("../..", path)); err != nil {
			t.Fatalf("discovery resource %s: %v", path, err)
		}
	}
}

func TestRepositorySchemaNamesEveryPublicField(t *testing.T) {
	var schema struct {
		ID   string `json:"$id"`
		Defs map[string]struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	readRepositoryJSON(t, "integrations/schema.json", &schema)
	if schema.ID != Describe("test").Schema {
		t.Fatalf("schema ID %q disagrees with runtime discovery", schema.ID)
	}
	for name, value := range map[string]any{
		"description": Description{}, "operation": Operation{}, "action": Action{},
		"snapshot": Snapshot{}, "item": Item{}, "event": Event{}, "change": Change{},
		"project": project.Project{}, "source": signal.PluginStatus{},
		"upstream": board.UpstreamItem{}, "run": board.RunItem{},
	} {
		t.Run(name, func(t *testing.T) {
			shape, ok := schema.Defs[name]
			if !ok {
				t.Fatalf("schema has no %s definition", name)
			}
			// Include optional fields and those promoted from board.Item.
			fields := jsonFields(reflect.TypeOf(value))
			required := map[string]bool{}
			for _, field := range shape.Required {
				required[field] = true
			}
			for field, alwaysPresent := range fields {
				if _, ok := shape.Properties[field]; !ok {
					t.Errorf("public field %s is missing from schema", field)
				}
				if alwaysPresent && !required[field] {
					t.Errorf("always-present field %s is not required by schema", field)
				}
			}
			for field := range shape.Properties {
				if _, ok := fields[field]; !ok {
					t.Errorf("schema field %s has no public counterpart", field)
				}
			}
			for _, field := range shape.Required {
				if !fields[field] {
					t.Errorf("required schema field %s has no public counterpart", field)
				}
			}
		})
	}
}

func jsonFields(typ reflect.Type) map[string]bool {
	fields := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			for name, required := range jsonFields(field.Type) {
				fields[name] = required
			}
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			fields[name] = !strings.Contains(options, "omitempty")
		}
	}
	return fields
}

func readRepositoryJSON(t *testing.T, path string, into any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../..", path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}
