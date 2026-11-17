package daemon

// records_policy.go — S45 T7 (PM9): the seven policy/proposal catalog arms,
// implemented ON the records service so each has its socket twin and its
// tool through the one Do (internal/records' catalog is the definition).
//
// THE READS (policy_list, policy_show, policy_why, policy_in_force,
// proposal_list) answer through the SAME functions the socket handlers
// answer through — protocol.PolicyListAnswer & friends (policy_reads.go,
// the seed_status precedent generalised) and the same protocol.ProposalStore
// the socket's proposal methods use. There is no second select and no
// second renderer anywhere on these paths; TestPolicyToolReadsMatchSocket
// Reads pins the two doors field for field every run.
//
// THE PROPOSES (policy_propose_add, policy_propose_remove) create an INERT
// proposal row through the SAME create path as `proposal.create`: the
// shared validator seam first (S35 T2 — TD3: a payload the commit door
// would refuse is refused at create, never written), then the
// protocol.ProposalStore the socket writes through. They never touch the
// policy table: no read of the authoring surface appears in this file, and
// `policy.why` is byte-identical before and after a propose (the inertness
// arbiter test drives it through a tool call, S35 TD2 restated for the
// agent door).
//
// What the service adds that the schema guarantees:
//   - the proposed row's `source` is FORCED to agent — not from input,
//     which carries no such field (OL3); PL1's checkAuthorship therefore
//     refuses any `allow` with its own sentence even if the schema were
//     bypassed, and the schema itself cannot express an allow (the action
//     enum is deny-or-ask);
//   - created_by is the actor Do was handed: the step's agent on the tool
//     path (Invoker stamps it from the step row), the peer's uid on the
//     socket half (records_socket.go) — never a field the caller types,
//     and decode refuses one that arrives;
//   - a caller holds at most desk.MaxOpenProposalsPerAgent OPEN proposals,
//     named in the refusal together with the way to see them
//     (proposal_list); each stored payload is capped at 8 KiB, so a
//     runaway loop cannot bury the desk;
//   - `reason` is required, 1–512 bytes, and rides in the payload's
//     `reason` member for the person deciding (S35's TD1 payload shape
//     gains the one optional member; the create/commit validators tolerate
//     it — both decode policy rows with permissive unmarshal, and the
//     commit effects read the fields they own).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
)

// maxProposePayloadBytes is behaviour 4's payload ceiling: 8 KiB of stored
// proposal, refuse-above, never truncate. A policy spec that will not fit
// is a spec a person could not read at the desk anyway.
const maxProposePayloadBytes = 8 * 1024

// — the five reads —

func (r *RecordsService) policyList(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.policyStore == nil {
		return nil, policylessRefusal("policy_list"), nil
	}
	var in struct {
		Type   string `json:"type"`
		Target string `json:"target"`
		Source string `json:"source"`
		Action string `json:"action"`
		Origin string `json:"origin"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	res, err := protocol.PolicyListAnswer(ctx, r.policyStore, protocol.PolicyListParams{
		Type: in.Type, Target: in.Target, Source: in.Source, Action: in.Action, Origin: in.Origin,
	}, time.Now())
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return res, nil, nil
}

func (r *RecordsService) policyShow(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.policyStore == nil {
		return nil, policylessRefusal("policy_show"), nil
	}
	var in struct {
		ID string `json:"id"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	res, err := protocol.PolicyGetAnswer(ctx, r.policyStore, protocol.PolicyGetParams{ID: in.ID}, time.Now())
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return res, nil, nil
}

func (r *RecordsService) policyWhy(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.policyStore == nil {
		return nil, policylessRefusal("policy_why"), nil
	}
	var in protocol.PolicyWhyParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	res, err := protocol.PolicyWhyAnswer(ctx, r.policyStore, in, time.Now())
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return res, nil, nil
}

func (r *RecordsService) policyInForce(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.policyStore == nil {
		return nil, policylessRefusal("policy_in_force"), nil
	}
	var in struct {
		Agent string `json:"agent"`
		Loop  string `json:"loop"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	res, err := protocol.PolicyInForceAnswer(ctx, r.policyStore, protocol.PolicyInForceParams{
		Agent: in.Agent, Loop: in.Loop,
	}, time.Now())
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return res, nil, nil
}

func (r *RecordsService) proposalList(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.proposals == nil {
		return nil, proposalsUnwiredRefusal("proposal_list"), nil
	}
	var in struct {
		Status string `json:"status"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	rows, err := r.proposals.ListProposals(in.Status)
	if err != nil {
		return nil, nil, err
	}
	return protocol.ProposalListAnswer(rows), nil, nil
}

// — the two proposes —

// proposeAddInput is policy_propose_add's input, mirrored from the catalog
// schema: there is NO source/actor/createdBy/status/kind member to decode
// (decode's DisallowUnknownFields makes one that arrives a parse refusal),
// and `action` reaches the validator as a string so the second line of
// defence — PL1, over the FORCED source — is what refuses an allow.
type proposeAddInput struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Target struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"target"`
	Action   string          `json:"action"`
	Spec     json.RawMessage `json:"spec"`
	NotAfter string          `json:"notAfter"`
	Reason   string          `json:"reason"`
}

// proposeAddPayload is the STORED payload: the policy row the proposal
// proposes (source FORCED to "agent", not from input) with the one
// optional `reason` member S35 TD1's payload gains (validators tolerate
// it; the commit effect reads the fields it owns).
type proposeAddPayload struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Target   proposeTarget   `json:"target"`
	Action   string          `json:"action"`
	Spec     json.RawMessage `json:"spec"`
	Source   string          `json:"source"`
	NotAfter *string         `json:"not_after,omitempty"`
	Reason   string          `json:"reason"`
}

type proposeTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (r *RecordsService) policyProposeAdd(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in proposeAddInput
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if f := reasonRefusal(in.Reason, "policy_propose_add"); f != nil {
		return nil, f, nil
	}
	var notAfter *string
	if in.NotAfter != "" {
		at, perr := time.Parse(time.RFC3339Nano, in.NotAfter)
		if perr != nil {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: policy_propose_add notAfter %q is not an RFC3339 instant like 2026-10-14T23:59:59Z (%v) — an expiry that cannot be read is never stored as no-expiry", in.NotAfter, perr)}, nil
		}
		s := at.Format(time.RFC3339Nano)
		notAfter = &s
	}
	// The proposed row is BUILT here — never taken from input beyond the
	// fields the schema admits — and its source is FORCED to agent. An
	// allow cannot be expressed in the schema; if the schema were bypassed
	// (a direct Do call), the shared validator's ValidateForAdd →
	// checkAuthorship meets it with PL1's own sentence (behaviour 1), and
	// a committed row could not have come from this door at all.
	payload, merr := json.Marshal(proposeAddPayload{
		ID: in.ID, Type: in.Type,
		Target: proposeTarget{Kind: in.Target.Kind, ID: in.Target.ID},
		Action: in.Action, Spec: in.Spec,
		Source:   string(policy.Agent), // the stamp, not the input
		NotAfter: notAfter,
		Reason:   in.Reason,
	})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: policy_propose_add payload will not encode: %w", merr)
	}
	return r.createInertProposal(ctx, by, string(desk.KindPolicyAdd), payload)
}

func (r *RecordsService) policyProposeRemove(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return nil, &Failure{Sentence: "refused: policy_propose_remove needs a non-empty id — a removal from nothing is a proposal a person would waste a commit on (TD3)"}, nil
	}
	if f := reasonRefusal(in.Reason, "policy_propose_remove"); f != nil {
		return nil, f, nil
	}
	// The desk's own PolicyRemovePayload shape (one id) plus the optional
	// reason member; validatePolicyRemove decodes the id permissively and
	// refuses an id the policy table does not carry.
	payload, merr := json.Marshal(struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}{ID: in.ID, Reason: in.Reason})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: policy_propose_remove payload will not encode: %w", merr)
	}
	return r.createInertProposal(ctx, by, string(desk.KindPolicyRemove), payload)
}

// createInertProposal is the SHARED tail of both proposes and the exact
// sequence proposal.create runs: size cap, per-caller cap, the shared
// validator (TD3 — refused means no row), then the write through the SAME
// protocol.ProposalStore the socket creates with. The result is the stored
// row with THE commit command and the inertness sentence, so the agent can
// tell a person what to run — and run nothing itself.
func (r *RecordsService) createInertProposal(ctx context.Context, by Actor, kind string, payload json.RawMessage) (any, *Failure, error) {
	return r.createInertProposalBounded(ctx, by, kind, payload, maxProposePayloadBytes)
}

// createInertProposalBounded is createInertProposal with the payload ceiling
// named by the caller — the body of the function is UNCHANGED (cap, open
// cap, verbatim validator, stamped create), because the S61 T8 intake arms
// change only the one number the intake plan's own desk contract states:
// a plan payload may stand at the desk's IntakePlanCapsOf().PayloadBytes
// (128 KiB), where every other ask keeps the 8 KiB ceiling. One function,
// two ceilings, zero copied policy logic (S61 T8, GI17).
func (r *RecordsService) createInertProposalBounded(ctx context.Context, by Actor, kind string, payload json.RawMessage, maxBytes int) (any, *Failure, error) {
	if len(payload) > maxBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: the proposed payload is %d bytes and the cap is %d — a runaway loop must not bury the desk; shorten the spec or file no proposal", len(payload), maxBytes)}, nil
	}
	if f := r.openProposalsCap(by); f != nil {
		return nil, f, nil
	}
	if err := r.validateProposal(ctx, kind, string(payload)); err != nil {
		// The validator's sentence travels VERBATIM — PL1's refusal is
		// PL1's own words (behaviour 1), and the create/commit parity test
		// compares this line with proposal.create's.
		return nil, &Failure{Sentence: err.Error()}, nil
	}
	row, err := r.proposals.CreateProposal(ctx, kind, payload, by.Kind, by.ID)
	if err != nil {
		return nil, nil, err
	}
	row.Command = protocol.ProposalCommand(row.ID)
	return protocol.PolicyProposeResult{Proposal: row, Note: protocol.PolicyProposeInertNote}, nil, nil
}

// openProposalsCap is behaviour 4's rate half: a caller may hold at most
// desk.MaxOpenProposalsPerAgent rows still `proposed`. The refusal names
// the limit and the way to see them (proposal_list) — a counted refusal,
// never silence.
func (r *RecordsService) openProposalsCap(by Actor) *Failure {
	rows, err := r.proposals.ListProposals("proposed")
	if err != nil {
		return &Failure{Sentence: fmt.Sprintf("refused: the open proposals could not be counted (%v) — the cap is enforced by counting, so a store that cannot answer stops the write", err)}
	}
	held := 0
	for _, p := range rows {
		if p.CreatedByKind == by.Kind && p.CreatedByID == by.ID {
			held++
		}
	}
	if held >= desk.MaxOpenProposalsPerAgent {
		return &Failure{Sentence: fmt.Sprintf("refused: %s %s already holds %d open proposals — that is the per-agent cap; see them with proposal_list and ask a person to decide one before filing more", by.Kind, by.ID, desk.MaxOpenProposalsPerAgent)}
	}
	return nil
}

// reasonRefusal is the required-reason rule the schema declares and the
// service enforces: present, non-blank, at most 512 bytes. An unreasoned
// proposal is a question a person cannot decide. The bytes live in the desk
// (2026-11-01, S56 T8): the autonomy propose arms enforce the same rule in
// the seam's desk half, and one definition keeps the two doors' words
// identical — the policy arms' bytes are unchanged by the delegation.
func reasonRefusal(reason, tool string) *Failure {
	if s := desk.ProposeReasonSentence(reason, tool); s != "" {
		return &Failure{Sentence: s}
	}
	return nil
}

// proposeDoors is the V13 half: both proposes need the proposal store AND
// the shared validator seam. A mount without them refuses rather than
// writing an unvalidated row (TD3: validation at create is what keeps the
// desk from inheriting the commit door's refusals as written rows).
func (r *RecordsService) proposeDoors() (*Failure, bool) {
	if r.proposals == nil {
		return proposalsUnwiredRefusal("the propose tools"), false
	}
	if r.validateProposal == nil {
		return &Failure{Sentence: "refused: proposals need the shared create validator wired (TD3) — this mount would write an unvalidated row, and a proposal the commit door refuses is a proposal that should never have been stored"}, false
	}
	return nil, true
}

func policylessRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the policy store, and this build did not link it — the policy surface is absent for the same reason (V13), never answered zero", name)}
}

func proposalsUnwiredRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the proposal store, and this mount did not wire it — absent is stated, never faked (V13)", name)}
}

// wireRefusal renders a shared-read refusal as the tool door's sentence:
// the concrete part of an Aeon error (the boundary's own words travel
// verbatim, policy.go's rule), the composed text otherwise. The socket
// door renders the SAME error as its wire shape — one fact, two doors.
func wireRefusal(err error) *Failure {
	if err == nil {
		return nil
	}
	var ae *protocol.Error
	if errors.As(err, &ae) {
		if strings.TrimSpace(ae.Data.What) != "" {
			return &Failure{Sentence: ae.Data.What}
		}
		return &Failure{Sentence: ae.Message}
	}
	var be protocol.ErrBadParams
	if errors.As(err, &be) && be.Err != nil {
		return &Failure{Sentence: be.Err.Error()}
	}
	return &Failure{Sentence: err.Error()}
}
