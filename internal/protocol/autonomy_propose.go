package protocol

// ════════════════════════════════════════════════════════════════════════════
// autonomy_propose.go — S56 T8 (AL11, 2026-11-01): the two PROPOSE twins for
// autonomy, `autonomy.proposeRevoke` and `autonomy.proposeDeny`, as socket
// methods answered by the records service (Table.Records — RecordsAPI.Do),
// NOT by the autonomy authoring service (autonomy.go). They create an INERT
// proposal row of the two restrictive kinds (autonomy_revoke / autonomy_deny)
// — never a grant: the authoring surface owns writes to the grant tables,
// and neither twin touches it. There is deliberately NO third twin: no
// method, tool or proposal shape may create or widen an allow (AL11's lock
// at the wire level; the store's CHECK and T7's guards are the other two).
//
// The catalog rows (internal/records' autonomy_propose_revoke /
// autonomy_propose_deny) are the one definition; this file is the socket's
// half of the two doors, exactly as policy_propose.go does it for the
// policy twins: registered CONDITIONALLY (V13, the RegisterSeed pattern)
// because these are MUTATING catalog actions and a table without the
// records service leaves them ABSENT, not registered-and-refusing.
// registerRecords skips the five autonomy names (see records.go and the
// deviation recorded there — RegisterAutonomy runs after NewTable and
// registers unconditionally, so the skip must be explicit).
//
// What the twins refuse, and where: the schema (the catalog's,
// additionalProperties:false) carries NO actor, createdBy, proposedBy,
// status, kind, state or conditions field — state is implied (revoke /
// deny) and no agent-reachable schema carries the word allow anywhere.
// The service stamps proposed_by from the agent identity it already stamps
// (S45 T7's rule), and the per-agent open-proposal cap and the 8 KiB
// payload bound are S45's named constants, reused. The result REUSES
// PolicyProposeResult and PolicyProposeInertNote: the inertness sentence is
// about the proposal machinery, not the policy, and a second sentence would
// be a second truth.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
)

// The catalog tool names the twins dispatch under (RecordsAPI.Do takes the
// tool name — the catalog's vocabulary, not a second method table).
const (
	autonomyProposeRevokeTool = "autonomy_propose_revoke"
	autonomyProposeDenyTool   = "autonomy_propose_deny"
)

// AutonomyProposeRevokeParams is proposeRevoke's documented params — the
// catalog schema's shape as a Go type so the conformance fixtures
// round-trip through it. The grant id and the why; nothing else has
// anywhere to go (a smuggled "state" meets the service's strict decode).
type AutonomyProposeRevokeParams struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// AutonomyProposeDenyParams is proposeDeny's documented params: the node
// (a root or a child — a veto may sit on either, OD4) and the subject it
// vetoes, in the two-shape vocabulary (an action by name, or the one class
// shorthand). state is implied (deny) — there is no field to carry an
// allow, which is the whole point (AL11).
type AutonomyProposeDenyParams struct {
	NodeID  string `json:"nodeId"`
	Subject struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"subject"`
	Reason string `json:"reason"`
}

// RegisterAutonomyPropose registers the two twins when (and only when) the
// records service is wired — the RegisterSeed/V13 rule, the policy twins'
// own: a table without Table.Records leaves both ABSENT (ErrUnknownMethod),
// never registered-and-refusing, because an unwired runtime cannot honour
// what makes these two safe (the stamped agent actor, the shared
// validator, the per-agent cap). Returns true when both stand (already or
// just now); false when the seam is missing.
func (t *Table) RegisterAutonomyPropose() bool {
	if t.Records == nil {
		return false
	}
	type pairT struct {
		name MethodName
		tool string
	}
	pairs := []pairT{{MethodAutonomyProposeRevoke, autonomyProposeRevokeTool}, {MethodAutonomyProposeDeny, autonomyProposeDenyTool}}
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
