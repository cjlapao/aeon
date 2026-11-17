package protocol

// ════════════════════════════════════════════════════════════════════════════
// policy_propose.go — S45 T7 (PM9): the two propose twins, `policy.proposeAdd`
// and `policy.proposeRemove`, as socket METHODS answered by the records
// service (Table.Records — RecordsAPI.Do), NOT by the authoring service.
// They create an INERT proposal row — never a policy row: the authoring
// surface (policy_authoring.go) owns writes to policy, and neither twin
// touches it. The catalog row (internal/records' policy_propose_add /
// policy_propose_remove) is the one definition; this file is the socket's
// half of the two doors, exactly as registerRecords projects every other
// catalog Spec — except these two are registered CONDITIONALLY here rather
// than by the projection: the RegisterSeed pattern (V13), because they are
// the first MUTATING catalog actions, and a table without the records
// service leaves them ABSENT, not registered-and-refusing. registerRecords
// skips both names (see records.go) so this file owns their registration;
// RequiresInitialize true, Debug false — product methods on the wide
// socket, gated like every other write.
//
// What the twins refuse, and where the refusal lives: the schema (the
// catalog's, additionalProperties:false) carries NO source, actor,
// createdBy, status or kind field; the records service forces the proposed
// row's source to "agent" — so PL1 refuses any allow with its own sentence
// even if the schema were bypassed — and stamps created_by from the calling
// agent (the peer's uid on this door, the step's agent on the tool door).
// `proposal.discard` is deliberately NOT among them: retracting a proposal
// stays a human act in this slice. Nothing here, or anywhere in the
// catalog, commits, rejects, discards, adds, removes or seeds — PM10's
// guards (cmd/aeon/policy_amend_test.go) prove it; the canaries already
// name these two as what S45 permits.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
)

// The two twins' wire names are the MethodPolicyProposeAdd /
// MethodPolicyProposeRemove constants declared in method.go's S45 block —
// the one spelling of every wire name (protogen's list is read from those
// declarations); the catalog's Method field is the grant-side spelling and
// records_parity_test.go walks both directions every run, so the two can
// only drift by failing that test.

// The catalog tool names the twins dispatch under (RecordsAPI.Do takes the
// tool name — the catalog's vocabulary, not a second method table).
const (
	policyProposeAddTool    = "policy_propose_add"
	policyProposeRemoveTool = "policy_propose_remove"
)

// PolicyProposeAddParams is policy.proposeAdd's documented params — the
// catalog schema's shape, as a Go type so the conformance fixtures
// round-trip through it. No source/actor/createdBy/status/kind member
// exists (OL3: identity is stamped, status is the store's); `action` is
// deny-or-ask and allow is not representable (the enum lives in the
// catalog schema; PL1 is the second line, in the boundary).
type PolicyProposeAddParams struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Target struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"target"`
	Action   string          `json:"action"`
	Spec     json.RawMessage `json:"spec"`
	NotAfter string          `json:"notAfter,omitempty"`
	Reason   string          `json:"reason"`
}

// PolicyProposeRemoveParams is policy.proposeRemove's documented params:
// the one id and the one required reason. Nothing else has anywhere to go.
type PolicyProposeRemoveParams struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// PolicyProposeResult is what both twins answer: the stored inert row (the
// same ProposalRow proposal.create/list answer with, Command included)
// beside the sentence that says what the row IS — inert, decided by nobody,
// enforced by nothing until a person commits it at the socket's human door
// (S35 TD2, restated for the tool door by PM9).
type PolicyProposeResult struct {
	Proposal ProposalRow `json:"proposal"`
	// Note is the fixed inertness statement — a field, never prose the
	// client must parse off a refusal prefix (SEC-033's rule, carried to
	// the success shape: what IS and what REFUSED are different answers).
	Note string `json:"note"`
}

// PolicyProposeInertNote is that fixed sentence, exported so the daemon's
// records service answers with THE same words the fixture states (one
// definition; a second sentence is a second truth).
const PolicyProposeInertNote = "inert — nothing is enforced until a person commits this proposal at the socket's human door (TD2)"

// RegisterPolicyPropose registers the two twins when (and only when) the
// records service is wired — the RegisterSeed/V13 rule for the catalog's
// first mutating actions: a table without Table.Records leaves both
// ABSENT (ErrUnknownMethod), never registered-and-refusing, because an
// unwired runtime cannot honour what makes these two safe (the stamped
// agent actor, the forced source, the per-agent cap). Returns true when
// both stand (already or just now); false when the seam is missing or a
// different method already holds one of the names.
func (t *Table) RegisterPolicyPropose() bool {
	if t.Records == nil {
		return false
	}
	type pairT struct {
		name MethodName
		tool string
	}
	pairs := []pairT{{MethodPolicyProposeAdd, policyProposeAddTool}, {MethodPolicyProposeRemove, policyProposeRemoveTool}}
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
				// tool path calls (records_socket.go), the actor stamped
				// on THIS side from the peer credential, never decoded.
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
