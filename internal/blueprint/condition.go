package blueprint

import (
	"fmt"
	"reflect"
)

func (c Condition) Evaluate(vals Values) (bool, error) {
	if c.All != nil {
		if len(c.All) == 0 {
			return false, fmt.Errorf("empty all condition")
		}
		for _, child := range c.All {
			ok, err := child.Evaluate(vals)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}
	if c.Any != nil {
		if len(c.Any) == 0 {
			return false, fmt.Errorf("empty any condition")
		}
		for _, child := range c.Any {
			ok, err := child.Evaluate(vals)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if c.Not != nil {
		ok, err := c.Not.Evaluate(vals)
		return !ok, err
	}
	field, known := FindField(c.Field)
	if !known {
		return false, fmt.Errorf("unknown manifest field %q", c.Field)
	}
	actual, ok := vals[c.Field]
	if !ok {
		return false, fmt.Errorf("missing manifest value %q", c.Field)
	}
	if !matchesType(actual, field.Type) {
		return false, fmt.Errorf("manifest value %q has wrong type", c.Field)
	}
	if issues := CheckCondition(c, c.Field); len(issues) > 0 {
		return false, fmt.Errorf("invalid condition at %s: %s", issues[0].Path, issues[0].Msg)
	}
	switch c.Op {
	case Eq:
		return reflect.DeepEqual(actual, c.Value), nil
	case Neq:
		return !reflect.DeepEqual(actual, c.Value), nil
	case In:
		switch items := c.Value.(type) {
		case []any:
			for _, item := range items {
				if reflect.DeepEqual(actual, item) {
					return true, nil
				}
			}
		case []string:
			for _, item := range items {
				if reflect.DeepEqual(actual, item) {
					return true, nil
				}
			}
		}
		return false, nil
	case Gt, Gte, Lt, Lte:
		a, b := number(actual), number(c.Value)
		switch c.Op {
		case Gt:
			return a > b, nil
		case Gte:
			return a >= b, nil
		case Lt:
			return a < b, nil
		default:
			return a <= b, nil
		}
	}
	return false, fmt.Errorf("unsupported condition operator %q", c.Op)
}

func CheckCondition(c Condition, path string) []Diagnostic {
	if c.All != nil || c.Any != nil || c.Not != nil {
		var children []Condition
		switch {
		case c.All != nil:
			children = c.All
		case c.Any != nil:
			children = c.Any
		case c.Not != nil:
			children = []Condition{*c.Not}
		}
		if len(children) == 0 {
			return []Diagnostic{{State: Invalid, Kind: "empty-condition", Path: path, Msg: "condition group must not be empty"}}
		}
		var out []Diagnostic
		for i, child := range children {
			out = append(out, CheckCondition(child, fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	}
	field, known := FindField(c.Field)
	if !known {
		return []Diagnostic{{State: Invalid, Kind: "unknown-field", Path: path, Msg: fmt.Sprintf("unknown manifest field %q", c.Field)}}
	}
	permitted := false
	for _, op := range field.Operators {
		if op == c.Op {
			permitted = true
			break
		}
	}
	if !permitted {
		return []Diagnostic{{State: Invalid, Kind: "invalid-operator", Path: path, Msg: fmt.Sprintf("%q is not permitted for %s", c.Op, field.Type)}}
	}
	if c.Op == In {
		if !isList(c.Value) {
			return []Diagnostic{{State: Invalid, Kind: "type-mismatch", Path: path, Msg: "in needs a list"}}
		}
		for _, v := range listValues(c.Value) {
			if !matchesType(v, field.Type) {
				return []Diagnostic{{State: Invalid, Kind: "type-mismatch", Path: path, Msg: "list item has wrong type"}}
			}
		}
	} else if !matchesType(c.Value, field.Type) {
		return []Diagnostic{{State: Invalid, Kind: "type-mismatch", Path: path, Msg: "condition value has wrong type"}}
	}
	if field.Type == FieldEnum {
		items := []any{c.Value}
		if c.Op == In {
			items = listValues(c.Value)
		}
		for _, item := range items {
			found := false
			for _, candidate := range field.Enum {
				if item == candidate {
					found = true
					break
				}
			}
			if !found {
				return []Diagnostic{{State: Invalid, Kind: "enum-value", Path: path, Msg: fmt.Sprintf("%v is outside %v", item, field.Enum)}}
			}
		}
	}
	return nil
}

func matchesType(v any, t FieldType) bool {
	switch t {
	case FieldString, FieldEnum:
		_, ok := v.(string)
		return ok
	case FieldBool:
		_, ok := v.(bool)
		return ok
	case FieldInt:
		switch v.(type) {
		case int, int64, float64:
			return true
		}
	case FieldCollection:
		switch v.(type) {
		case []string, []any:
			return true
		}
	}
	return false
}
func number(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return 0
}
func isList(v any) bool {
	switch v.(type) {
	case []any, []string:
		return true
	}
	return false
}
func listValues(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []string:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = item
		}
		return out
	}
	return nil
}
