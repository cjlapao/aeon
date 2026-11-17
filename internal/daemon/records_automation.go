package daemon

// records_automation.go — S49 T7 (AN9, AN1, AN5, AN10, AN12): the five
// automation arms of the records service. Four are reads that forward to the
// SAME answers the person's ten socket doors run — protocol.AnswerAutomation*
// is the one rendering each door shares, so the tool door and the socket
// door cannot drift field for field (rule 4); each returns the same type,
// the same statements, the same evaluatedAt stamp. The fifth files the
// INERT automation_disable row through the shared create path: T6's create
// validator (TD3 — the SAME function both doors run, which is what refuses
// the unknown id, the archived row and the already-off automation, verbatim,
// here at the desk), the per-agent open-proposal cap (S45's named constant,
// the counted refusal naming it and proposal_list), and the author STAMPED
// from the step's identity — never typed (OL3).
//
// Three things this file does NOT do, and each is a rule rather than an
// omission:
//
//   - it never writes a schedule, a webhook or an event trigger (AU1 lock 1
//     preserved UNEDITED: the one write-shaped arm creates a proposal row in
//     the proposal tables and touches nothing else; the eleven human-door
//     writes T4 built have no arm here to reach them, and rule 2's guard
//     refuses such a tool by name);
//   - it never lets a webhook secret VALUE through. The arms return the
//     plane's own view types — health names the secret env and answers
//     whether it is SET, and no field on any of these answers can hold a
//     value (§4c's two types rather than one redaction; rule 4's test
//     walks a live secret and demands the bytes never appear);
//   - it never takes an identity from the input. The author is the stamped
//     step (by); there is no actor, status, kind, enabled or archived field
//     to smuggle into a tool schema (rule 1's schemas refuse them at parse),
//     and the strict decode below catches one in the raw JSON anyway.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
)

// automationList answers automation_list through the standing list door —
// the same `AnswerAutomationList` the socket's automation.list runs, the two
// empties included ("no automations match this filter." versus the store's
// sentence; an arm that re-wrapped the rows would be a second truth about
// which empty is which).
func (r *RecordsService) automationList(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.automationReads == nil {
		return nil, automationPlanelessRefusal("automation_list"), nil
	}
	var in protocol.AutomationListParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := protocol.AnswerAutomationList(ctx, r.automationReads, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// automationShow answers automation_show through the same `Show` the
// person's door runs: one automation in full — canonical form, plain-words
// description from the one formatter, limits, last fires and changes, the
// blueprint's verdict. An empty source or id is the plane's own refusal,
// not an empty answer (P3).
func (r *RecordsService) automationShow(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.automationReads == nil {
		return nil, automationPlanelessRefusal("automation_show"), nil
	}
	var in protocol.AutomationShowParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := protocol.AnswerAutomationShow(ctx, r.automationReads, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// automationFires answers automation_fires through the same `Fires` the
// person's door runs: the unified log, newest first, with the outcome
// tally and the paging cursor (AN12's could-not-say never rewritten into
// peace — the rows travel as the plane words them).
func (r *RecordsService) automationFires(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.automationReads == nil {
		return nil, automationPlanelessRefusal("automation_fires"), nil
	}
	var in protocol.AutomationFiresParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := protocol.AnswerAutomationFires(ctx, r.automationReads, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// automationHealth answers automation_health through the same `Answer` the
// person's door runs — blueprint present/valid, the secret env's NAME and a
// set-bool, workspace, scope, all as of the evaluatedAt the shared answer
// stamps (AN10). Advisory by construction: no arm here gates a fire, and no
// value has anywhere to ride.
func (r *RecordsService) automationHealth(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.automationReads == nil {
		return nil, automationPlanelessRefusal("automation_health"), nil
	}
	var in protocol.AutomationHealthParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := protocol.AnswerAutomationHealth(ctx, r.automationReads, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// automationProposeDisable files the INERT automation_disable row — the
// plane's ONE agent ask (AN9). The shape refusals (blank source, blank id)
// come from T6's validator below, not from a second spelling up here; the
// validator is the SAME function commit runs (TD3), and it is what refuses
// the unknown automation, the archived one and the already-off one — each
// named, verbatim, before anything is stored. The payload is BUILT here,
// field by field — never the raw input carried through — so there is no
// room for a smuggled "actor", "enabled" or "status" even if the strict
// decode above somehow missed one: the desk's parse refusal names it.
//
// Author: the stamped step identity (by), carried to created_by_*; the
// COMMITTER, not this door, owns the disable when a person decides — and
// the effect runs T4's own write paths with the committing person as actor
// (T6). Nothing outside the proposal tables is touched.
func (r *RecordsService) automationProposeDisable(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in protocol.AutomationProposeDisableParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	payload, merr := json.Marshal(desk.AutomationDisableProposal{
		Source: in.Source,
		ID:     in.ID,
		Reason: in.Reason,
	})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: automation_propose_disable payload will not encode: %w", merr)
	}
	if f := r.openProposalsCap(by); f != nil {
		return nil, f, nil
	}
	if err := r.validateProposal(ctx, string(desk.KindAutomationDisable), string(payload)); err != nil {
		// The validator's sentence travels VERBATIM (TD3: what create refuses,
		// commit refuses, in the same words — and an ask against an automation
		// that is unknown, archived or already off never becomes an inert row).
		return nil, &Failure{Sentence: err.Error()}, nil
	}
	created, err := r.proposals.CreateProposal(ctx, string(desk.KindAutomationDisable), payload, by.Kind, by.ID)
	if err != nil {
		return nil, nil, err
	}
	created.Command = protocol.ProposalCommand(created.ID)
	return protocol.PolicyProposeResult{Proposal: created, Note: protocol.PolicyProposeInertNote}, nil, nil
}

// automationPlanelessRefusal is the V13 shape for the family: the plane's
// own services are what honour these doors (the two empties, the outcome
// tally, the evaluatedAt, the secret-env set-bool), and a mount without
// them says so, never answering zero or an empty listing.
func automationPlanelessRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the automation plane, and this mount did not wire it — absent is stated, never faked (V13)", name)}
}
