package daemon

// records_continuous.go — S27 T8 (CN23): the scheduler family's four agent
// ARMS. Each forwards to the SAME ContinuousService the person's four read
// doors answer through (standing rule 4 — one implementation, two doors),
// scoped to the step's OWN repository: Identify resolves step → loop →
// component root, and that root is STAMPED over every query after the
// shared filter grammar has parsed it (protocol.ParseContinuousDecisionFilters).
// The agent never names a repository — a param that could only be
// overwritten is a param that lies (OL3) — and a foreign repository is
// simply absent, not refused with a revealing sentence (MC2).
//
// V13 honesty: a mount that did not wire the Continuous seam refuses each
// arm BY NAME and answers nothing — no empty plan, no status of zero, no
// ledger that reads as "nothing decided". A step whose identity resolves
// no component root is likewise refused by name (T0's pre-flight rule: the
// header-only answer ships only when the store cannot resolve the root AT
// ALL; here the arm is the store's own read, so an unresolved identity is a
// refusal, and TestSchedulerToolResultsMatchSocketResults pins what the
// resolved path answers).
//
// There is no enable, disable, pause, resume, root.set, root.disable,
// enableTree or stopAll arm to reach — the eight controls are human doors
// on the 0600 surface (S27 CN2) and no code here, or in the catalog, can
// name one.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
)

// schedulerPlaneRefusal is the V13 arm refusal: named, honest, never an
// empty answer.
func schedulerPlaneRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the scheduler plane, and this mount did not wire it — no status is answered, and no status is answered empty (V13)", name)}
}

// schedulerRootRefusal is the stamped-scope refusal: the repository an
// agent may read the scheduler for is resolved from the store, never
// guessed (OL3).
func schedulerRootRefusal(tool string) *Failure {
	return &Failure{Sentence: "refused: " + tool + " reads the scheduler for the step's own repository, and this step's identity resolves none — the root is read from the store, never guessed (OL3)"}
}

// schedulerToolError renders a shared-grammar refusal (ErrBadParams from
// the same parser the person's door ran) into the tool's failure channel —
// DB19: sentences, never the error channel.
func schedulerToolError(err error) *Failure {
	return &Failure{Sentence: "refused: " + err.Error()}
}

// schedulerStatus answers scheduler_status: the daemon's own view, with
// this repository's rows kept and every other repository's rows absent.
// The header (master, pause, settings, slots, next tick, statement) is
// machine truth and stays — CN22's honest panel reads the same fields. The
// needs-you count is re-answered through the SAME door's own root filter,
// so an agent counts its own repository's open items, not the machine's.
func (r *RecordsService) schedulerStatus(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.continuousPlane == nil {
		return nil, schedulerPlaneRefusal("scheduler_status"), nil
	}
	if ident.ComponentRoot == "" {
		return nil, schedulerRootRefusal("scheduler_status"), nil
	}
	if f, err := decode(input, &struct{}{}); f != nil || err != nil {
		return nil, f, err
	}
	view, err := r.continuousPlane.Status(ctx)
	if err != nil {
		return nil, nil, err
	}
	var mine []protocol.ContinuousRepoView
	for _, repo := range view.Repos {
		if repo.ComponentRoot == ident.ComponentRoot {
			mine = append(mine, repo)
		}
	}
	view.Repos = mine
	att, err := r.continuousPlane.Attention(ctx, ident.ComponentRoot)
	if err != nil {
		return nil, nil, err
	}
	view.Attention = protocol.ContinuousAttentionSum{Open: att.Open}
	return view, nil, nil
}

// schedulerPreview answers scheduler_preview through the SAME plan the
// next tick will execute — the door's own root filter, stamped.
func (r *RecordsService) schedulerPreview(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.continuousPlane == nil {
		return nil, schedulerPlaneRefusal("scheduler_preview"), nil
	}
	if ident.ComponentRoot == "" {
		return nil, schedulerRootRefusal("scheduler_preview"), nil
	}
	if f, err := decode(input, &struct{}{}); f != nil || err != nil {
		return nil, f, err
	}
	out, err := r.continuousPlane.Preview(ctx, ident.ComponentRoot)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// schedulerDecisionsToolParams mirrors the catalog schema — the ledger's
// filter words and NOTHING else. There is no componentRoot field: a step
// cannot ask about another repository, and decode's DisallowUnknownFields
// makes one a parse error named as such.
type schedulerDecisionsToolParams struct {
	Subject  string `json:"subject"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Since    string `json:"since"`
	Limit    *int   `json:"limit"`
}

// schedulerDecisions answers scheduler_decisions: the shared filter grammar
// (the SAME parse the person's scheduler.decisions ran) and then the
// stamped root — field-for-field the rows the door answers for this root.
func (r *RecordsService) schedulerDecisions(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.continuousPlane == nil {
		return nil, schedulerPlaneRefusal("scheduler_decisions"), nil
	}
	if ident.ComponentRoot == "" {
		return nil, schedulerRootRefusal("scheduler_decisions"), nil
	}
	var p schedulerDecisionsToolParams
	if f, err := decode(input, &p); f != nil || err != nil {
		return nil, f, err
	}
	q, err := protocol.ParseContinuousDecisionFilters(protocol.ContinuousDecisionFilters{
		Subject: p.Subject, Decision: p.Decision, Reason: p.Reason,
		Since: p.Since, Limit: p.Limit,
	})
	if err != nil {
		return nil, schedulerToolError(err), nil
	}
	q.ComponentRoot = ident.ComponentRoot
	rows, err := r.continuousPlane.Decisions(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	return rows, nil, nil
}

// schedulerAttention answers scheduler_attention: what WAITS FOR A PERSON
// in this step's repository, the door's own root filter stamped.
func (r *RecordsService) schedulerAttention(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.continuousPlane == nil {
		return nil, schedulerPlaneRefusal("scheduler_attention"), nil
	}
	if ident.ComponentRoot == "" {
		return nil, schedulerRootRefusal("scheduler_attention"), nil
	}
	if f, err := decode(input, &struct{}{}); f != nil || err != nil {
		return nil, f, err
	}
	out, err := r.continuousPlane.Attention(ctx, ident.ComponentRoot)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}
