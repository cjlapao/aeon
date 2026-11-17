package protocol

// ════════════════════════════════════════════════════════════════════════════
// charter_propose.go — S61 T8 (GI17, 2026-11-05): the two PROPOSE twins for
// the goal-intake plane, `charter.propose` and `intake.propose`, as socket
// methods answered by the records service (Table.Records — RecordsAPI.Do),
// NOT by the charter or intake doors themselves (charter.go, intake.go).
// They create an INERT proposal row of the two kinds the desk validates
// (`charter_save`, `intake_plan`) — never a revision, never a plan in
// motion: the charter door owns saved charters, the intake engine owns a
// committed plan, and neither twin touches either. The catalog rows
// (internal/records' charter_propose / intake_propose) are the one
// definition; this file is the socket's half of the two doors, exactly as
// autonomy_propose.go does it for the autonomy twins: registered
// CONDITIONALLY (V13) because these are MUTATING catalog actions and a
// table without the records service leaves them ABSENT, not
// registered-and-refusing. registerRecords skips both names;
// RegisterIntakePlanePropose owns them.
//
// What the twins refuse, and where: the schema (the catalog's,
// additionalProperties:false) carries NO actor, intakeId, path, option,
// status or kind field. The actor is the step's identity, stamped by the
// service (S45 T7's rule); the intake row for `intake.propose` is STAMPED
// from the calling loop's own intake session — linkage an agent cannot
// type and therefore cannot forge (a typed "intakeId" meets the strict
// decode and the parse refusal names it). The charter body and the plan's
// charter text ride as DATA (GI2): a proposal row is bytes a person will
// read at the desk, and the inertness sentence the result carries is the
// machinery's own (PolicyProposeInertNote reused — a second sentence would
// be a second truth).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
)

// The catalog tool names the twins dispatch under (RecordsAPI.Do takes the
// tool name — the catalog's vocabulary, not a second method table).
const (
	charterProposeTool = "charter_propose"
	intakeProposeTool  = "intake_propose"
)

// CharterProposeParams is charter.propose's documented params — the catalog
// schema's shape as a Go type so conformance round-trips through it. The
// root, the charter document as data, and the why; nothing else has
// anywhere to go (a smuggled "actor" or "status" meets the strict decode).
type CharterProposeParams struct {
	RootID string          `json:"root_id"`
	Body   json.RawMessage `json:"body"`
	Reason string          `json:"reason"`
}

// IntakeProposeParams is intake.propose's documented params: the plan
// WITHOUT the intakeId — the daemon stamps that from the calling loop's
// own intake row, so the wire shape has no field to carry a forged
// linkage. camelCase keys mirror the desk payload one for one.
type IntakeProposeParams struct {
	Charter     json.RawMessage   `json:"charter"`
	Components  []json.RawMessage `json:"components"`
	ProjectName string            `json:"projectName,omitempty"`
	Epics       []json.RawMessage `json:"epics,omitempty"`
	Goals       []json.RawMessage `json:"goals,omitempty"`
	Items       []json.RawMessage `json:"items,omitempty"`
	Reason      string            `json:"reason"`
}

// RegisterIntakePlanePropose registers the two twins when (and only when)
// the records service is wired — the autonomy twins' own rule: a table
// without Table.Records leaves both ABSENT (ErrUnknownMethod), never
// registered-and-refusing, because an unwired runtime cannot honour what
// makes these two safe (the stamped agent actor, the loop-to-intake
// identity check, the shared validator, the per-agent cap). Returns true
// when both stand (already or just now); false when the seam is missing.
func (t *Table) RegisterIntakePlanePropose() bool {
	if t.Records == nil {
		return false
	}
	type pairT struct {
		name MethodName
		tool string
	}
	pairs := []pairT{{MethodCharterPropose, charterProposeTool}, {MethodIntakePropose, intakeProposeTool}}
	for _, pair := range pairs {
		if _, ok := t.methods[pair.name]; ok {
			continue // already standing (a re-registration is a no-op, not a second door)
		}
		tool := pair.tool
		t.register(&Method{
			Name:               pair.name,
			RequiresInitialize: true,
			Handle: func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
				// One definition, two doors: the SAME RecordsAPI.Do the
				// tool path calls, the actor stamped on THIS side from the
				// peer credential, never decoded.
				out, sentence, err := t.Records.Do(ctx, sess, tool, params)
				if err != nil {
					return nil, err
				}
				if sentence != "" {
					return nil, ErrBadParams{Err: errors.New(sentence)}
				}
				return out, nil
			},
		})
	}
	return true
}
