package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Key is one top-level setting, read from Config's own fields so the list
// can never fall behind them.
type Key struct {
	Name  string
	field int
	kind  reflect.Kind // String, Int or Slice (of strings)
}

// Keys are the top-level settings, in the order Config declares them.
// projects is a table of per-project settings, not a key.
func Keys() []Key {
	t := reflect.TypeOf(Config{})
	var keys []Key
	for i := range t.NumField() {
		name := t.Field(i).Tag.Get("toml")
		if name == "" || name == "projects" {
			continue
		}
		keys = append(keys, Key{Name: name, field: i, kind: t.Field(i).Type.Kind()})
	}
	return keys
}

// KeyNamed finds a top-level setting by name.
func KeyNamed(name string) (Key, bool) {
	for _, k := range Keys() {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

// KeyNames, for messages.
func KeyNames() string {
	var names []string
	for _, k := range Keys() {
		names = append(names, k.Name)
	}
	return strings.Join(names, ", ")
}

// Parse turns values typed on the command line into this setting's value:
// one word for a text setting, a positive whole number, or a list.
func (k Key) Parse(args []string) (any, error) {
	switch k.kind {
	case reflect.Slice:
		if len(args) == 0 {
			return nil, fmt.Errorf("%s needs at least one value", k.Name)
		}
		return args, nil
	case reflect.Int:
		n, err := strconv.Atoi(strings.Join(args, " "))
		if len(args) != 1 || err != nil || n <= 0 {
			return nil, fmt.Errorf("%s must be a whole number above 0", k.Name)
		}
		return n, nil
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("%s takes one value", k.Name)
	}
	return args[0], nil
}

// Value is this setting's value in c.
func (k Key) Value(c *Config) any { return reflect.ValueOf(*c).Field(k.field).Interface() }

// Written is what config.toml itself says: which top-level settings it
// sets, and its per-project tables. A missing file says nothing.
func Written(sousHome string) (set map[string]bool, projects map[string]map[string]string, err error) {
	b, err := os.ReadFile(Path(sousHome))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, map[string]map[string]string{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var raw map[string]any
	var c Config
	if _, err := toml.Decode(string(b), &raw); err != nil {
		return nil, nil, err
	}
	if _, err := toml.Decode(string(b), &c); err != nil {
		return nil, nil, err
	}
	set = map[string]bool{}
	for name := range raw {
		set[name] = true
	}
	if c.Projects == nil {
		c.Projects = map[string]map[string]string{}
	}
	return set, c.Projects, nil
}
