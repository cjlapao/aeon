package blueprint

import "strings"

type FieldType string

const (
	FieldString     FieldType = "string"
	FieldInt        FieldType = "int"
	FieldBool       FieldType = "bool"
	FieldEnum       FieldType = "enum"
	FieldCollection FieldType = "collection"
)

type Field struct {
	Path      string     `json:"path"`
	Type      FieldType  `json:"type"`
	Enum      []string   `json:"enum,omitempty"`
	Operators []Operator `json:"operators"`
	Doc       string     `json:"doc"`
}

var fieldVocabulary = []Field{
	{Path: "mode", Type: FieldEnum, Enum: []string{"simple", "standard", "poc"}, Operators: []Operator{Eq, Neq, In}, Doc: "Loop execution mode"},
	{Path: "flags.*", Type: FieldBool, Operators: []Operator{Eq, Neq}, Doc: "Boolean facts declared by prior steps"},
	{Path: "counters.*", Type: FieldInt, Operators: []Operator{Eq, Neq, Gt, Gte, Lt, Lte, In}, Doc: "Named retry and round counters"},
	{Path: "answers.*", Type: FieldString, Operators: []Operator{Eq, Neq, In}, Doc: "Answer to a named gate"},
	{Path: "tasks", Type: FieldCollection, Operators: []Operator{Eq, Neq}, Doc: "Task identifiers for iterate"},
	{Path: "tasks.count", Type: FieldInt, Operators: []Operator{Eq, Neq, Gt, Gte, Lt, Lte, In}, Doc: "Number of tasks"},
}

func Fields() []Field {
	out := make([]Field, len(fieldVocabulary))
	copy(out, fieldVocabulary)
	for i := range out {
		out[i].Enum = append([]string(nil), out[i].Enum...)
		out[i].Operators = append([]Operator(nil), out[i].Operators...)
	}
	return out
}
func Slots() []string {
	fields := Fields()
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.Path
	}
	return out
}
func FindField(path string) (Field, bool) {
	for _, f := range fieldVocabulary {
		if f.Path == path {
			return f, true
		}
		if strings.HasSuffix(f.Path, ".*") && strings.HasPrefix(path, strings.TrimSuffix(f.Path, "*")) && len(path) > len(f.Path)-1 {
			return f, true
		}
	}
	return Field{}, false
}

type Values map[string]any

// NewValues supplies declared defaults. Missing values added by a caller after
// validation are still an error in Evaluate; absence never means false.
func NewValues() Values { return Values{"mode": "standard", "tasks": []string{}, "tasks.count": 0} }
