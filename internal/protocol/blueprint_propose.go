package protocol

// ════════════════════════════════════════════════════════════════════════════
// blueprint_propose.go — S48 T10 (BP9, 2026-11-03): the one PROPOSE twin for
// blueprints, `blueprint.propose`, as a socket method answered by the
// records service (Table.Records — RecordsAPI.Do), NOT by the blueprint
// write service. It creates an INERT proposal row of kind blueprint_save —
// never a row in blueprint / blueprint_revision: the four write doors
// (save · duplicate · delete · revert) are the person's human door
// (T8's guard), and no tool, no method and no proposal shape reaches them.
// An agent may SEE (the four read twins ride the standing doors), CHECK
// (validate/lint answer text with nothing persisted), and PROPOSE; the
// proposal constrains nothing until a person commits it (S35 TD2), and the
// commit stamps the COMMITTER, never this door's author (T9).
//
// The catalog row (internal/records' blueprint_propose) is the one
// definition; this file is the socket's half of the door, exactly as
// autonomy_propose.go does it for the autonomy twins: registered
// CONDITIONALLY (V13, the RegisterSeed rule) because this is a MUTATING
// catalog action and a table without the records service leaves it ABSENT,
// not registered-and-refusing. registerRecords skips the whole blueprint
// family (the four reads are owned by RegisterBlueprintReads, which runs
// after NewTable, and this name by RegisterBlueprintPropose — a records
// projection here would double-register at the first wiring and keep the
// propose door alive in a build whose records service never linked).
//
// What the door refuses, and where: the catalog schema
// (additionalProperties:false) carries NO actor, source, status, kind,
// origin or rev field — identity is the step's, stamped on the service
// side from the peer (OL3), and the row's kind is implied
// (blueprint_save) with nowhere for a smuggled verb to go. The payload
// carries the WHOLE proposed source (a patch would let a change hide in
// the lines it does not name); the shared create validator (TD3) runs the
// save door's own Analyze, so a blueprint the save door refuses is refused
// HERE, verbatim, and never becomes an inert row.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
)

// blueprintProposeTool is the catalog tool name the twin dispatches under
// (RecordsAPI.Do takes the tool name — the catalog's vocabulary, not a
// second method table).
const blueprintProposeTool = "blueprint_propose"

// BlueprintProposeParams is propose's documented params — the catalog
// schema's shape as a Go type so the conformance fixtures round-trip
// through it. Name, the FULL source, the create-only intent, and a note
// for the person deciding; nothing else has anywhere to go (a smuggled
// "mode" or "actor" meets the service's strict decode and the desk's
// parse refusal).
type BlueprintProposeParams struct {
	Name       string `json:"name"`
	Content    string `json:"content"`
	CreateOnly bool   `json:"create_only"`
	Note       string `json:"note"`
}

// RegisterBlueprintPropose registers the twin when (and only when) the
// records service is wired — the RegisterSeed/V13 rule the policy and
// autonomy twins carry: a table without Table.Records leaves it ABSENT
// (ErrUnknownMethod), never registered-and-refusing, because the records
// service is what honours what makes this door safe (the stamped agent
// actor, the shared create validator, the per-agent open-proposal cap).
// Returns true when it stands (already or just now); false when the seam
// is missing.
func (t *Table) RegisterBlueprintPropose() bool {
	if t.Records == nil {
		return false
	}
	if _, ok := t.methods[MethodBlueprintPropose]; ok {
		return true // already standing (a re-registration is a no-op, not a second door)
	}
	t.register(&Method{
		Name:               MethodBlueprintPropose,
		RequiresInitialize: true,
		Handle: func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
			// One definition, two doors: the SAME RecordsAPI.Do the tool
			// path calls, the actor stamped on the service side from the
			// step's identity, never decoded from params.
			out, sentence, err := t.Records.Do(ctx, sess, blueprintProposeTool, params)
			if err != nil {
				return nil, err
			}
			if sentence != "" {
				return nil, ErrBadParams{Err: errors.New(sentence)}
			}
			return out, nil
		},
	})
	return true
}
