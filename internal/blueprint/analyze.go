package blueprint

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Analyze reports unknown keys alongside independent validation failures. Load
// remains strict for execution; diagnostics can inspect a malformed draft.
func Analyze(raw []byte, opts ValidationOptions) Report {
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return Report{Diagnostics: []Diagnostic{{State: Invalid, Kind: "yaml", Path: "$", Msg: err.Error()}}}
	}
	if len(node.Content) == 0 {
		return Report{Diagnostics: []Diagnostic{{State: Invalid, Kind: "yaml", Path: "$", Msg: "empty blueprint"}}}
	}
	unknown := unknownDiagnostics(node.Content[0], reflect.TypeOf(Blueprint{}), "")
	var b Blueprint
	if err := node.Decode(&b); err != nil {
		return Report{Diagnostics: append(unknown, Diagnostic{State: Invalid, Kind: "yaml", Path: "$", Msg: err.Error()})}
	}
	r := Validate(b, opts)
	r.Diagnostics = append(r.Diagnostics, unknown...)
	sort.Slice(r.Diagnostics, func(i, j int) bool {
		a, c := r.Diagnostics[i], r.Diagnostics[j]
		if a.Path != c.Path {
			return a.Path < c.Path
		}
		return a.Kind < c.Kind
	})
	return r
}

func unknownDiagnostics(n *yaml.Node, t reflect.Type, path string) []Diagnostic {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() == reflect.Map && n.Kind == yaml.MappingNode {
		var out []Diagnostic
		for i := 0; i+1 < len(n.Content); i += 2 {
			child := n.Content[i].Value
			if path != "" {
				child = path + "." + child
			}
			out = append(out, unknownDiagnostics(n.Content[i+1], t.Elem(), child)...)
		}
		return out
	}
	if t.Kind() != reflect.Struct || n.Kind != yaml.MappingNode {
		return nil
	}
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if tag != "" && tag != "-" {
			fields[tag] = f.Type
		}
	}
	var out []Diagnostic
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i].Value, n.Content[i+1]
		child := key
		if path != "" {
			child = path + "." + key
		}
		ft, ok := fields[key]
		if !ok {
			out = append(out, Diagnostic{State: Invalid, Kind: "unknown-key", Path: child, Msg: fmt.Sprintf("unknown key %q", key), Hint: suggestion(key, fields)})
			continue
		}
		if val.Kind == yaml.SequenceNode {
			for j, item := range val.Content {
				out = append(out, unknownDiagnostics(item, ft, fmt.Sprintf("%s[%d]", child, j))...)
			}
		} else {
			out = append(out, unknownDiagnostics(val, ft, child)...)
		}
	}
	return out
}
