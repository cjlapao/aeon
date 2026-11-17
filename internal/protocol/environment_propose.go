package protocol

// ════════════════════════════════════════════════════════════════════════════
// environment_propose.go — S59 T7 (EV16, EV7): the plane's ONE agent door,
// `environment.proposeRestrict`, answered by the records service
// (Table.Records — RecordsAPI.Do), NOT by the environment write service. It
// creates an INERT proposal row of kind environment_restrict — never a change
// to an environment row, a binding, or a secret reference: the thirteen write
// doors of the plane (create · update · archive · restore · bind · unbind ·
// teardown, and the four secretref writes) stay the person's human door, and
// no tool, no method and no proposal shape reaches them.
//
// An agent may SEE its own step's environment (the four read twins, answered
// by the records service over the same seams these doors answer through),
// CHECK what a command would run in, and PROPOSE that it be tightened; the
// proposal constrains nothing until a person commits it (S35 TD2), and the
// commit stamps the COMMITTER, never this door's author (T7's seam).
//
// The catalog rows (internal/records' five `environment` specs) are the one
// definition; this file is the socket's half of the propose door, exactly as
// blueprint_propose.go does it for the blueprint revision: registered
// CONDITIONALLY (V13, the RegisterSeed rule) because this is a MUTATING catalog
// action and a table without the records service leaves it ABSENT, not
// registered-and-refusing. registerRecords skips the whole environment family
// (the four reads are owned by RegisterEnvironment / RegisterSecretRef, which
// run after NewTable, and this name by RegisterEnvironmentProposeRestrict — a
// records projection here would double-register at the first wiring and keep
// the propose door alive in a build whose records service never linked).
//
// What the door refuses, and where: the catalog schema (additionalProperties:
// false) carries NO actor, source, sourceKind, status, kind, origin, legacy or
// revision field — identity is the step's, stamped on the service side from the
// peer (OL3); the row's kind is implied (environment_restrict); and the
// BASELINE the proposal is written against is read from the row by the service,
// never typed, which is what makes a stale tighten refuse instead of clobber.
// The plane's own key gate (rejectEnvironmentIdentityKeys) runs on the raw
// params BEFORE the decode, so `revision` and `source` meet a named refusal
// rather than a silently-ignored field (EV7: an environment names references,
// it does not carry sources). A backend change is not a proposal shape at all:
// strength goes up by a person alone (EV16, EV19).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
)

// environmentProposeRestrictTool is the catalog tool name the twin dispatches
// under (RecordsAPI.Do takes the tool name — the catalog's vocabulary, not a
// second method table).
const environmentProposeRestrictTool = "environment_propose_restrict"

// EnvironmentProposeRestrictParams is the propose door's documented params —
// the catalog schema's shape as a Go type so the conformance fixtures
// round-trip through it. The environment to tighten, the FULL proposed spec,
// and the reason a person reads at the desk; nothing else has anywhere to go
// (a smuggled "backend", "source", "actor" or "revision" meets the plane's key
// gate below and the desk's parse refusal after it).
type EnvironmentProposeRestrictParams struct {
	EnvironmentID string `json:"environmentId"`
	Spec          string `json:"spec"`
	Reason        string `json:"reason"`
}

// RegisterEnvironmentProposeRestrict registers the twin when (and only when)
// the records service is wired — the RegisterSeed/V13 rule the policy, autonomy
// and blueprint twins carry: a table without Table.Records leaves it ABSENT
// (ErrUnknownMethod), never registered-and-refusing, because the records
// service is what honours what makes this door safe (the stamped agent actor,
// the shared create validator, the per-agent open-proposal cap). Returns true
// when it stands (already or just now); false when the seam is missing.
func (t *Table) RegisterEnvironmentProposeRestrict() bool {
	if t.Records == nil {
		return false
	}
	if _, ok := t.methods[MethodEnvironmentProposeRestrict]; ok {
		return true // already standing (a re-registration is a no-op, not a second door)
	}
	t.register(&Method{
		Name:               MethodEnvironmentProposeRestrict,
		RequiresInitialize: true,
		Handle: func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
			// The plane's own key gate first — before the service, before the
			// decode, in the plane's own words (EV7, EV17).
			if err := rejectEnvironmentIdentityKeys(string(MethodEnvironmentProposeRestrict), params); err != nil {
				return nil, err
			}
			// One definition, two doors: the SAME RecordsAPI.Do the tool path
			// calls, the actor stamped on the service side from the step's
			// identity, never decoded from params.
			out, sentence, err := t.Records.Do(ctx, sess, environmentProposeRestrictTool, params)
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
