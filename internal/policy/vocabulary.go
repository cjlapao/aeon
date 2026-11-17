package policy

// ════════════════════════════════════════════════════════════════════════════
// The vocabulary answer a machine can build a form from (S45 T2, PM6).
//
// # Who asks this and why
//
// The app's policy section (S45 T8) and the CLI's authoring help (T9) draw
// every picklist, toggle and placeholder from ONE daemon-side read —
// `policy.vocabulary` — so no client hardcodes what the engine owns. A UI
// that guesses the vocabulary drifts from the engine, and the drift shows up
// as the UI offering a value the write refuses (or worse: omitting one it
// accepts). This is CS13's argument carried onto the policy surface: the
// schema drives the UI, never a copy.
//
// # Three sources of truth, each read from its ONE home
//
//   - the closed sets come from the All* functions in types.go — this file
//     restates no list, so widening the set is a code change that this
//     answer follows automatically (PL3);
//   - the authorship matrix COMPUTES: it calls the rule (checkAuthorship,
//     reached through the alias below) for each (source, action) pair and
//     prints what the rule said. PL1 is never restated here — a restated
//     rule is a second rule, and the one that gets weakened is always the
//     copy (the count guard in store_test.go pins the rule's one home; the
//     alias below NAMES the function and the matrix CALLS that value, which
//     is why the guard's call-count does not move — this file adds no call
//     site, only a route to the existing rule);
//   - the per-type spec descriptors come from the validators themselves
//     (describeFields, beside each validator), derived from the same schema
//     the write boundary enforces. A form built from anything else can offer
//     what the write would refuse.
//
// Pure; no I/O (D5): Vocabulary() reads no store, no clock, no disk. The
// whole answer is a function of the compiled code.
// ════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"strconv"
)

// authorshipRule is the rule reached as a value so the matrix below computes
// from it instead of restating it (PM6). It is the same function value —
// TestAuthorshipMatrixIsComputedFromCheckAuthorship pins the identity and
// then flips the matrix by swapping this variable at a test seam, which is
// the only way to prove the matrix has no opinions of its own.
var authorshipRule = checkAuthorship

// VocabularyView is the whole closed picture in one answer: the sets, who
// may author what (computed, not declared), and what each type's spec looks
// like. Field names are the wire names policy.vocabulary carries.
type VocabularyView struct {
	Types       []string          `json:"types"`
	TargetKinds []string          `json:"targetKinds"`
	Sources     []string          `json:"sources"`
	Actions     []string          `json:"actions"`
	Authorship  []AuthorshipEntry `json:"authorship"`
	Specs       []SpecDescriptor  `json:"specs"`
}

// AuthorshipEntry is one (source, action) cell of the matrix: allowed, or
// refused with the boundary's own reason carried verbatim (DB19 — the
// refusal that names the value it refused is the actionable one).
type AuthorshipEntry struct {
	Source  string `json:"source"`
	Action  string `json:"action"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// SpecDescriptor is one type's spec shape as its validator reads it.
type SpecDescriptor struct {
	Type   string      `json:"type"`
	Fields []SpecField `json:"fields"`
}

// SpecField is one declared field of a pinned spec. Kind is the closed three
// the form needs: "string", "number", "enum". EnumValues is populated only
// for the state fields — and probed from the validator itself, not recalled
// from a help string, so a value the form offers is a value the write
// accepts (PM6's whole claim, mechanically).
type SpecField struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Required    bool     `json:"required"`
	EnumValues  []string `json:"enumValues,omitempty"`
	Description string   `json:"description"`
	// Picker names the richer control a screen should offer for this field
	// instead of free text ("tool-address", S74 TA11). Absent means the control
	// follows Kind. The daemon says it so no client recalls which field is a
	// tool name; an unknown value falls back to text on the client.
	Picker string `json:"picker,omitempty"`
}

// PickerToolAddress is the Picker value of a ToolAccess toolName field.
const PickerToolAddress = "tool-address"

// Vocabulary assembles the answer from the three homes named in this file's
// banner. Sources × actions in the matrix keep the closed sets' own order
// (operator, agent, mcp_server, import × allow, deny, ask) so the answer is
// stable byte-for-byte across calls and builds.
func Vocabulary() VocabularyView {
	v := VocabularyView{
		Types:       make([]string, 0, len(policyTypes)),
		TargetKinds: make([]string, 0, len(targetKinds)),
		Sources:     make([]string, 0, len(sources)),
		Actions:     make([]string, 0, len(actions)),
		Authorship:  make([]AuthorshipEntry, 0, len(sources)*len(actions)),
		Specs:       make([]SpecDescriptor, 0, len(policyTypes)),
	}
	for _, t := range policyTypes {
		v.Types = append(v.Types, string(t))
	}
	for _, k := range targetKinds {
		v.TargetKinds = append(v.TargetKinds, string(k))
	}
	for _, s := range sources {
		v.Sources = append(v.Sources, string(s))
	}
	for _, a := range actions {
		v.Actions = append(v.Actions, string(a))
	}

	// The matrix: ask the rule, print what it said. Allowed cells carry no
	// reason (omitempty — an empty reason is not a refusal with an empty
	// excuse); refused cells carry the boundary's sentence verbatim, because
	// a UI that paraphrases PL1 is a second PL1.
	for _, s := range sources {
		for _, a := range actions {
			entry := AuthorshipEntry{Source: string(s), Action: string(a), Allowed: true}
			if rej := authorshipRule(Policy{Action: a, Source: s}); rej != nil {
				entry.Allowed = false
				entry.Reason = rej.Reason
			}
			v.Authorship = append(v.Authorship, entry)
		}
	}

	// One descriptor per compiled type, in AllPolicyTypes order. A type
	// registered without a validator gets the type name and no fields — the
	// honest answer of a schema nobody wrote, never an invented one.
	for _, t := range policyTypes {
		desc := SpecDescriptor{Type: string(t), Fields: []SpecField{}}
		if val, ok := ValidatorFor(t); ok {
			if fd, ok := val.(fieldsDescriber); ok {
				if fields := fd.describeFields(); fields != nil {
					desc.Fields = fields
				}
			}
		}
		v.Specs = append(v.Specs, desc)
	}
	return v
}

// fieldsDescriber is the describing half of a validator next to describer
// (describe.go): the field list the write boundary enforces, rendered for a
// form.
type fieldsDescriber interface {
	describeFields() []SpecField
}

// describeFields derives a descriptor from the schema that gates the write —
// the same s.fields the unknown-key refusal enumerates, so a descriptor and
// a refusal cannot disagree. The state fields' enum values are PROBED from
// the registered validator (acceptsState below), which is the only
// definition of "what the validator accepts".
func (s specSchema) describeFields() []SpecField {
	out := make([]SpecField, 0, len(s.fields))
	for _, f := range s.fields {
		field := SpecField{
			Name:        f.name,
			Kind:        "string",
			Required:    f.required,
			Description: specFieldNote(f.name),
		}
		if f.kind == numberKind {
			field.Kind = "number"
		}
		if s.typ == ToolAccess && f.name == "toolName" {
			field.Picker = PickerToolAddress
		}
		if f.name == "state" {
			field.Kind = "enum"
			field.EnumValues = make([]string, 0, len(actions))
			for _, a := range actions {
				if s.acceptsState(a) {
					field.EnumValues = append(field.EnumValues, string(a))
				}
			}
		}
		out = append(out, field)
	}
	return out
}

// acceptsState probes THIS type's own validator with a candidate row: the
// spec is filled from the schema itself (every string field gets a probe
// name, every number field a 1, and the state field gets the candidate,
// matching the row's action), so the probe needs no per-type branch (PL3
// again — no switch anywhere in this file). The answer is what the validator
// returned, not what this file believes.
func (s specSchema) acceptsState(act Action) bool {
	val, ok := ValidatorFor(s.typ)
	if !ok {
		return false
	}
	raw := make(map[string]json.RawMessage, len(s.fields))
	for _, f := range s.fields {
		switch {
		case f.kind == numberKind:
			raw[f.name] = json.RawMessage("1")
		case f.name == "state":
			raw[f.name] = json.RawMessage(strconv.Quote(string(act)))
		default:
			raw[f.name] = json.RawMessage(strconv.Quote("probe"))
		}
	}
	spec, err := json.Marshal(raw)
	if err != nil {
		return false // cannot happen from these inputs; false is the deny-leaning answer (D10)
	}
	return val.Validate(Policy{
		ID:     "vocabulary-probe",
		Type:   s.typ,
		Target: Target{Kind: AllAgents},
		Action: act,
		Source: Operator,
		Spec:   spec,
	}) == nil
}

// specFieldNote is the form-help text per declared field name — prose about
// what the field means, never a second list of what it may contain (the
// enum values and the required flags come from the schema and the
// validator). A field nobody named gets a generic line, never a guess.
func specFieldNote(name string) string {
	if note, ok := specFieldNotes[name]; ok {
		return note
	}
	return "a declared field of this type's pinned spec"
}

var specFieldNotes = map[string]string{
	"toolName": "the exact tool this rule names — compared exactly, case-sensitively, in the honest name",
	"command":  "the command pattern the matcher accepts — '*' anywhere except edges or doubled, bare name or trailing star",
	"route":    "the route (or remote: egress peer) this rule names — a single trailing '*' is the one pattern form",
	"effect":   "the kind of effect (reading, writing, spending) this rule governs tools by",
	"token":    "the cost token whose spend this cap bounds",
	"max":      "the cap on that token — a finite, non-negative number; zero is a real cap",
	"skill":    "the exact skill reference (callee/skill) this rule names — no wildcards",
	"trigger":  "the trigger kind (manual, schedule, webhook) this rule governs — no wildcards",
	"state":    "the row's action restated inside the spec — it must agree with the row's action, or the row enforces nothing",
}
