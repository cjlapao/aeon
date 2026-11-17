package model

import "fmt"

// Provenance records WHO PRODUCED an assistant turn (M26).
//
// # Why this exists at all
//
// Three things need it and none of them can reconstruct it later:
//
//   - M27 replays `thinking` blocks **only to the provider that produced
//     them** — a rule that cannot run without a record of the producer.
//   - Cost attribution (M30's sibling) splits by provider and route.
//   - A transcript spanning providers must answer "which model wrote this?"
//     per turn, which stops being exotic the moment a loop can switch.
//
// # Why the ROUTE and not just provider and model (O4)
//
// Two routes can point at the same provider and model with different prices
// or base URLs. The ledger is the one place that difference must survive, and
// "a local server behind an `openai-compatible` route" is identifiable only
// by the route it went out on.
//
// # Why per turn and never per loop (I4)
//
// A loop that switches provider mid-run has turns from both. A loop-level
// field would report the LAST provider as though it produced everything, and
// it is the grep test (`TestProvenanceIsNotLoopLevel`) that stops that
// shortcut being added later "as a convenience".
//
// # Who sets it, and where
//
// The producer of the turn — the runtime assembly point of a model call, and
// the ask path's handler — reads the triple off the VERY Route the call went
// out on. It is never reconstructed at read time and never invented: a user
// turn has no producer, and a `tool_result` was produced by a tool, not a
// model. Fabricating provenance for either would make this field mean two
// things.
type Provenance struct {
	Route    string `json:"route"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// NewProvenance is the way a producer stamps a turn: the triple is read from
// the Route the call actually went out on — one place it can be correct, and
// no second derivation to drift from (O4).
func NewProvenance(r Route) Provenance {
	return Provenance{Route: r.ID, Provider: r.Provider, Model: r.Model}
}

// Complete refuses the three near-misses that would otherwise enter the
// record as facts: absent, and present-but-hollow in any of the three.
func (p Provenance) Complete() error {
	if p.Route == "" {
		return fmt.Errorf("provenance is missing its route (O4): cost attribution and " +
			"'which server answered' both key on it")
	}
	if p.Provider == "" {
		return fmt.Errorf("provenance of route %q is missing its provider: the M27 replay "+
			"rule — thinking goes only to its producer — keys on it", p.Route)
	}
	if p.Model == "" {
		return fmt.Errorf("provenance of route %q is missing its model: a model swap behind a "+
			"route must remain visible in the record", p.Route)
	}
	return nil
}

// ValidateProvenance is the per-message half of validation — the half a
// fragment append can check without the whole transcript. AppendMessages is
// its single production caller: the rule is a WRITE rule, enforced where new
// turns enter the record, and deliberately NOT part of ValidateTranscript.
// Transcripts written before migration 0027 have no producer on record; a
// read-time rule would refuse S7's recovery of its own history — rejecting
// the past rather than protecting the future, and inventing a producer for
// them would be the exact fabrication M26 forbids.
//
// Assistant turns MUST carry it (they were produced by someone, and the name
// belongs in the record); every other role MUST NOT (M26): inventing
// provenance for a user turn or a tool result would make the field mean two
// things.
func (m Message) ValidateProvenance() error {
	switch m.Role {
	case RoleAssistant:
		if m.Provenance == nil {
			return fmt.Errorf("an assistant turn must record its provenance (M26): the route, " +
				"provider and model that produced it")
		}
		return m.Provenance.Complete()
	case RoleSystem, RoleUser:
		if m.Provenance != nil {
			return fmt.Errorf("a %s turn has no producing model, so provenance on it would "+
				"mean nothing — and worse, would train readers that the field is optional noise", m.Role)
		}
	}
	return nil
}
