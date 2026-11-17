package blueprint

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// model.go — the structure a form edits, and the text it saves.
//
// The management surface draws a blueprint as data and never parses YAML in
// the browser: the daemon turns text into the structure (ParseModel) and the
// structure back into text (RenderModel). Neither judges the blueprint — the
// validator does, over the rendered text, so a form cannot produce a flow the
// loader would read differently from the one it showed.

// ParseModel reads blueprint text into its structure, leniently: an unknown
// key or a value the validator would refuse is still shown, so the person can
// see and fix it. Only text that is not YAML at all is an error.
func ParseModel(raw []byte) (Blueprint, error) {
	var b Blueprint
	if err := yaml.Unmarshal(raw, &b); err != nil {
		return Blueprint{}, fmt.Errorf("the flow's text is not valid YAML: %w", err)
	}
	if b.Questions == nil {
		b.Questions = map[string]Question{}
	}
	return b, nil
}

// RenderModel writes a structure as blueprint text: keys in the schema's own
// order, two-space indent, no empty fields. Comments of an earlier text are
// not carried — the structure holds none.
func RenderModel(b Blueprint) (string, error) {
	if b.SchemaVersion == 0 {
		b.SchemaVersion = SupportedSchemaVersion
	}
	if b.Version == 0 {
		b.Version = 1
	}
	if b.Questions == nil {
		b.Questions = map[string]Question{}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(b); err != nil {
		return "", fmt.Errorf("the flow could not be written as text: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Vocabulary is every closed set a form offers, so it never invents one.
type Vocabulary struct {
	StepKinds     []string            `json:"stepKinds"`
	After         []string            `json:"after"`
	ThenActions   []string            `json:"thenActions"`
	Operators     []string            `json:"operators"`
	Decides       []string            `json:"decides"`
	EmitTypes     []string            `json:"emitTypes"`
	RequireTypes  []string            `json:"requireTypes"`
	RequireStates []string            `json:"requireStates"`
	Fields        []Field             `json:"fields"`
	Actions       []ActionDescription `json:"actions"`
}

// ActionDescription is one code action a blueprint may name and the arguments
// it takes.
type ActionDescription struct {
	Name string           `json:"name"`
	Args []ActionArgument `json:"args"`
}

// ActionArgument is one argument of an action; Source says who fills it
// (code, manifest, handoff, evidence), and only `params` the author writes
// belong in a flow.
type ActionArgument struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Source   string `json:"source"`
}

// BaseVocabulary is the vocabulary the blueprint package owns; the caller adds
// the actions (their registry lives elsewhere).
func BaseVocabulary() Vocabulary {
	ops := []string{string(Eq), string(Neq), string(In), string(Gt), string(Gte), string(Lt), string(Lte)}
	return Vocabulary{
		StepKinds:     []string{string(ModelStep), string(CodeAction), string(Gate)},
		After:         []string{AfterFinish, AfterConverse},
		ThenActions:   []string{string(Escalate), string(FailItem), string(Block)},
		Operators:     ops,
		Decides:       []string{"goal", "brief"},
		EmitTypes:     append([]string(nil), catalogEntities...),
		RequireTypes:  []string{"goal", "task"},
		RequireStates: []string{"approved"},
		Fields:        Fields(),
	}
}
