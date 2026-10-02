package cli

import (
	"encoding/json"
	"reflect"
)

// writeJSON prints v as indented JSON on stdout. Every read verb's --json.
// An empty list is [] and an empty map {}, never null (AXI 5), however the
// command built v.
func (e *Env) writeJSON(v any) int {
	enc := json.NewEncoder(e.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(noNulls(reflect.ValueOf(v)).Interface()); err != nil {
		return fail(e, 1, "%v", err)
	}
	return 0
}

var marshaler = reflect.TypeFor[json.Marshaler]()

// noNulls copies v with every nil slice and map made empty. Values that
// marshal themselves (times, say) and nil pointers are left as they are.
func noNulls(v reflect.Value) reflect.Value {
	if !v.IsValid() || v.Type().Implements(marshaler) {
		return v
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		p := reflect.New(v.Type().Elem())
		p.Elem().Set(noNulls(v.Elem()))
		return p
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(noNulls(v.Elem()))
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				out.Field(i).Set(noNulls(v.Field(i)))
			}
		}
		return out
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 { // []byte is base64 text
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(noNulls(v.Index(i)))
		}
		return out
	case reflect.Map:
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for it := v.MapRange(); it.Next(); {
			out.SetMapIndex(it.Key(), noNulls(it.Value()))
		}
		return out
	}
	return v
}
