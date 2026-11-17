// Package charter is the pure core of the S61 charter plane: the closed
// body schema (GI2), the one canonical spelling, the one validator every
// door shares, the one description formatter, and the one write path
// (GI1–GI4, GI17). It GRANTS NOTHING: nothing in internal/policy,
// internal/loop or internal/action reads a charter, and this package
// imports none of them — TestPackageImportsNothingThatGrants is that claim
// as a mechanism. proposed.* rides the body as a PROPOSAL: rendered
// "not applied" with a pointer to the owning slice's human door, enforced
// by nobody here.
package charter

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Acceptance is one acceptance criterion (GI2): the stable `ac-<n>` id is
// the seed of goal_criterion rows (GI5), so it survives edits.
type Acceptance struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Check string `json:"check,omitempty"` // manual | command | test | absent = unchecked
	Ref   string `json:"ref,omitempty"`   // command string / test name; never executed here
}

// Proposed is the "proposals only" corner of the body (GI2): a wish with a
// pointer, never a grant. Every field is optional; nil/empty means absent,
// and absent means UNKNOWN (P31), never zero.
type Proposed struct {
	BudgetUSD      *float64 `json:"budgetUsd,omitempty"` // ≥ 0 when present (P31: nil = unknown)
	Autonomy       string   `json:"autonomy,omitempty"`  // an S56 enum word, never typed here (OD12)
	EnvironmentRef string   `json:"environmentRef,omitempty"`
	Verify         []string `json:"verify,omitempty"` // command strings; this slice never runs them
}

// Body is the charter's closed schema (GI2): additionalProperties:false is
// Parse's law, and the caps are validate.go's.
type Body struct {
	Goal        string       `json:"goal"`
	Constraints []string     `json:"constraints,omitempty"`
	NonGoals    []string     `json:"nonGoals,omitempty"`
	Acceptance  []Acceptance `json:"acceptance,omitempty"`
	Stack       []string     `json:"stack,omitempty"`
	Proposed    *Proposed    `json:"proposed,omitempty"`
}

// Canonical renders the ONE byte spelling of a body: compact, field order
// fixed by the struct, empty lists omitted (an empty list and an absent one
// are the same nothing — Parse normalizes them, so the digest of a body
// never churns on a cosmetic comma). The same body in any input key order
// lands on the same bytes: TestCanonicalBodyIsStable pins it.
func Canonical(b Body) ([]byte, error) {
	out, err := json.Marshal(normalize(b))
	if err != nil {
		return nil, fmt.Errorf("charter: body cannot be canonicalized: %w", err)
	}
	return out, nil
}

// Parse reads a charter body and refuses what the schema does not carry:
// an unknown property is schema leakage, and trailing content after one
// JSON value means the caller sent more than the schema can hold.
func Parse(raw []byte) (Body, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return Body{}, fmt.Errorf("charter: an empty body is refused — the charter is a statement, not an absence")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var b Body
	if err := dec.Decode(&b); err != nil {
		return Body{}, fmt.Errorf("charter: body does not parse: %v", err)
	}
	if dec.More() {
		return Body{}, fmt.Errorf("charter: trailing content after the body is refused")
	}
	return normalize(b), nil
}

// normalize is the canonical-form law: empty lists are absent lists. It
// runs on the way IN (Parse) and on the way OUT (Canonical), so a body that
// round-trips through the database keeps one spelling forever.
func normalize(b Body) Body {
	b.Constraints = emptyToNil(b.Constraints)
	b.NonGoals = emptyToNil(b.NonGoals)
	b.Acceptance = emptyToNil(b.Acceptance)
	b.Stack = emptyToNil(b.Stack)
	if b.Proposed != nil {
		p := *b.Proposed
		p.Verify = emptyToNil(p.Verify)
		b.Proposed = &p
	}
	return b
}

func emptyToNil[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}
