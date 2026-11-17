// Package retroseed ships the `retro.templates` set (S63 T3, RT4/RT16,
// P8): the five versioned bodies a milestone learning renders from. The
// bodies are DATA in the retro_template ROW (rule 6 — the renderer reads
// the row, never code), which is why the set is inert: a template writes
// no learning by itself and changes no verdict; the recorder's milestone
// gate does the writing, through S28's one path.
//
// The spec named `internal/retro/seed` + `internal/seed/decl_retro.go`;
// the shipped shape follows the verifyseed ruling verbatim — one package
// (directory internal/retro/seed, package retroseed to keep the engine
// import unshadowed) owning SetID, Source, the applier and the
// declaration. A decl file inside internal/seed would import this
// package, which imports internal/seed — a cycle, not a style choice.
package retroseed

import (
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/retro"
	"github.com/cjlapao/aeon/internal/seed"
)

// SetID is the set's stable name, declared once (SD2).
const SetID = "retro.templates"

// Content is one template's shipped content. The record hash covers the
// BODY — what a rendered learning is made of; version is publication
// metadata, outside the hash (the verifytemplates convention).
type Content struct {
	Template string `json:"template"`
}

// bodies is the shipped table: the five families RT4 names, each body a
// closed-grammar sentence — every substitution a registry placeholder, so
// no body can carry free text a reviewer did not read. The wording is
// deliberately flat: a learning rendered from these says what was
// counted, in the same words at every milestone.
func bodies() []struct {
	key  string
	body string
} {
	return []struct {
		key  string
		body string
	}{
		{"default",
			`Aeon outcome {outcome_kind} on blueprint {blueprint}: fact {count} for this component (covering {window} outcome(s)). Cost: {cost_floor}. Wall: {wall}. Escalations: {escalations}.`},
		{"done-with-rework",
			`Done with rework on blueprint {blueprint}: {retries} retry/retries and {gates_failed} failed gate(s); review rounds {rounds_review}, security rounds {rounds_security}. Fact {count} for this component (covering {window} outcome(s)). Cost: {cost_floor}.`},
		{"unverified",
			`Done without a verification declaration on blueprint {blueprint}: fact {count} for this component (covering {window} outcome(s)). Review rounds {rounds_review}, security rounds {rounds_security}, escalations {escalations}. Cost: {cost_floor}.`},
		{"aborted",
			`Aborted loop on blueprint {blueprint}: fact {count} for this component (covering {window} outcome(s)). Escalations: {escalations}. Cost: {cost_floor}. Wall: {wall}.`},
		{"blocked",
			`Blocked loop ({outcome_kind}) on blueprint {blueprint}: fact {count} for this component (covering {window} outcome(s)). Escalations: {escalations}. Retries: {retries}. Cost: {cost_floor}.`},
	}
}

// Source is the set's records (rule 3: never nil). Every body passes the
// CLOSED placeholder grammar here, at the source door — a template that
// cannot render can not even become a record (RT4 §2's seed-time refusal,
// the panic below makes it unbootable rather than quietly unwritable).
func Source() []seed.Record {
	out := make([]seed.Record, 0, 5)
	for _, b := range bodies() {
		if err := retro.Validate(b.body); err != nil {
			panic(fmt.Sprintf("retroseed: template %q violates the closed grammar: %v", b.key, err))
		}
		content, err := json.Marshal(Content{Template: b.body})
		if err != nil {
			panic("retroseed: marshalling " + b.key + ": " + err.Error())
		}
		out = append(out, seed.Record{
			Key:     b.key,
			Version: recordVersion(b.key),
			Content: content,
		})
	}
	return out
}

// ContentOf decodes a record's shipped content back (the applier's two
// doors share it, so Upsert and Fingerprint see one shape).
func ContentOf(r seed.Record) (Content, error) {
	var c Content
	if err := json.Unmarshal(r.Content, &c); err != nil {
		return Content{}, fmt.Errorf("retroseed: record %q: %w", r.Key, err)
	}
	return c, nil
}
