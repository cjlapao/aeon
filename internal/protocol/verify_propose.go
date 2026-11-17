package protocol

// ════════════════════════════════════════════════════════════════════════════
// verify_propose.go — S57 T9 (VG19): the three gate twins the catalog's
// nine gate specs register as NEW socket methods — `verify.proposeAdd`,
// `mergerule.proposeTighten` and `dod.markAsAgent` — answered by the
// records service (Table.Records — RecordsAPI.Do), NOT by the gate
// services' human doors. The six read specs reuse T8's methods and are
// registered by T8's own RegisterVerify / RegisterMergeRule / RegisterDoD
// over the gate seams; only these three names are new, so only these three
// are registered here.
//
// What each twin does at this door: the two proposes arm the proposal
// desk's payload (internal/desk's VerifyCommandProposal / MergeRule
// Proposal shape), re-validate it the way the inert-propose path does, and
// create the INERT proposal row — nothing on the gate plane changes until
// a person commits it at the socket's human door (S35 TD3). The DoD mark
// is the one twin that mutates its own plane: unmet / unverifiable freely,
// met only from a passing verify run of the caller's OWN loop on the
// current head — the store re-checks the evidence (S57 T5/T9) and an
// agent's manual evidence is refused at the store's door.
//
// Identity (OL3): none of the three params carries a source, actor,
// createdBy, status, kind, origin or componentId member — the records
// service stamps the agent identity from the step and resolves the
// component from the caller's own loop; a cross-component act is a
// person's act, not a param's. The schema is additionalProperties:false,
// so a smuggled field is a parse error, and TestNoMethodAcceptsAnActorParam
// holds these three wire types free of author-shaped fields.
//
// Registration is the RegisterPolicyPropose shape (V13): conditional on
// the records service — a table without Table.Records leaves all three
// ABSENT (ErrUnknownMethod), never registered-and-refusing, because the
// service is what honours their safety (the stamped agent actor, the
// scope resolved from the step, the evidence re-check). registerRecords
// skips all nine gate specs by family (see records.go), so this file owns
// the three twin param types (their wire names stand in method.go's S57
// block) and T8's registers own the six read names; the two
// can only collide if both claim a name, which is a register panic.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
)

// The three proposal wire names live in method.go's S57 block (protogen
// enumerates that file alone; the names moved there for T11's contract).
// The catalog tool names the twins dispatch under (RecordsAPI.Do takes the
// tool name — the catalog's vocabulary, not a second method table).
const (
	verifyProposeAddTool        = "verify_propose_add"
	mergeruleProposeTightenTool = "mergerule_propose_tighten"
	dodMarkAsAgentTool          = "dod_mark"
)

// VerifyProposeAddParams is verify.proposeAdd's documented params — the
// catalog schema's shape, as a Go type so the conformance fixtures
// round-trip through it. Add-only: op is not a param because an agent's
// propose is add or nothing (update/archive/try are human doors). No
// identity or component member exists (OL3).
type VerifyProposeAddParams struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Requirement string `json:"requirement"`
	TimeoutS    int    `json:"timeoutS"`
	Reason      string `json:"reason"`
}

// MergeRuleProposeTightenParams is mergerule.proposeTighten's documented
// params: the FULL rule (every Rule column, as T8's merge.set law says a
// rule is written whole) beside the required reason. Where the domain has
// an enum with a clear direction the catalog schema admits only the
// tightening value; the service re-checks every field with mergerule.
// Tightens as the second line of defence. No identity or component member.
type MergeRuleProposeTightenParams struct {
	Fields struct {
		Enabled                bool     `json:"enabled"`
		RequireLocal           bool     `json:"requireLocal"`
		RequireDod             string   `json:"requireDod"`
		RequiredChecks         []string `json:"requiredChecks"`
		ChecksPolicy           string   `json:"checksPolicy"`
		NoChecks               string   `json:"noChecks"`
		ReviewMode             string   `json:"reviewMode"`
		AeonReviewAgents       []string `json:"aeonReviewAgents"`
		RequireThreadsResolved bool     `json:"requireThreadsResolved"`
		Method                 string   `json:"method"`
	} `json:"fields"`
	Reason string `json:"reason"`
}

// DoDMarkAsAgentParams is dod.markAsAgent's documented params: the item,
// the new state (met / unmet / unverifiable — never unchecked: a mark is
// a statement, not a reset), the run a met names, and the audit note.
// evidenceRunId is required by the service when the state is met and
// refused otherwise; the schema cannot say "conditional", so the service
// says it. No identity, loop or component member (OL3).
type DoDMarkAsAgentParams struct {
	ItemID        string `json:"itemId"`
	State         string `json:"state"`
	EvidenceRunID string `json:"evidenceRunId,omitempty"`
	Note          string `json:"note,omitempty"`
}

// RegisterGatePropose registers the three gate twins when (and only when)
// the records service is wired — the RegisterPolicyPropose/V13 rule: a
// table without Table.Records leaves all three ABSENT, never
// registered-and-refusing. Returns true when all three stand (already or
// just now); false when the seam is missing or a different method already
// holds one of the names.
func (t *Table) RegisterGatePropose() bool {
	if t.Records == nil {
		return false
	}
	type pairT struct {
		name MethodName
		tool string
	}
	pairs := []pairT{
		{MethodVerifyProposeAdd, verifyProposeAddTool},
		{MethodMergeRuleProposeTighten, mergeruleProposeTightenTool},
		{MethodDoDMarkAsAgent, dodMarkAsAgentTool},
	}
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
				// tool path calls (records_socket.go); the agent identity
				// is stamped on the daemon side from the step, never
				// decoded from params (OL3).
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
