package daemon

// records_loopcontrol.go — S55 T10: the seven loop-control / budget /
// watchdog catalog arms. The reads reach the SAME seam methods the socket's
// doors call (Table.LoopControl, Table.BudgetStore, Table.Watchdog) — one
// implementation, so the tool and the twin cannot drift (the parity test
// pins them field for field). The one write is the inert stop REQUEST
// (LC15): a row a person sees and acts on, never a halt.
//
// Own view (MC2): an agent's state read sees ITS OWN loop — the identity is
// stamped from the step, and no param names another loop. The writes are
// refused at the CATALOG boundary for a person (personDoorRefusal): a
// person's half of these reads is the socket's human door, by name.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// ownLoop resolves the loop the identity's step runs in — the ONE loop an
// agent's control arm may see or ask about. A missing loop is a refusal
// naming the loop, not a guess at one.
func (r *RecordsService) ownLoop(ident Identity) (store.Loop, *Failure) {
	l, err := r.st.GetLoop(store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot})
	if err != nil {
		return store.Loop{}, &Failure{Sentence: fmt.Sprintf("refused: this step's loop %s cannot be read (%v) — a control read is the loop's own rows, not an assumption", ident.LoopSlug, err)}
	}
	return l, nil
}

// s55SeamAbsent is the V13 refusal for the seven arms on a daemon whose table
// does not carry the plane's seam (a storeless mount): the plane is absent,
// so the arm says so rather than answering zero.
func s55SeamAbsent(plane string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: the %s plane is not wired in this daemon — the surface is absent, not empty (V13)", plane)}
}

// loopControlState is the loop_control_state arm (MC2 own view): the SAME
// loop.control read the socket answers — eligibility (the engine's own
// CheckStop/Resume/Cancel/Raise predicates), the open stop requests and the
// recent control audit — for the loop the step runs in. The loop is stamped
// from the step, so the decode is over an EMPTY struct: a fieldless schema
// means a smuggled loop_id never lands — it is refused at the parse door,
// the same door every other arm's input walks.
func (r *RecordsService) loopControlState(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if f, err := decode(input, &struct{}{}); f != nil || err != nil {
		return nil, f, err
	}
	l, f := r.ownLoop(ident)
	if f != nil {
		return nil, f, nil
	}
	lc := r.d.Table.LoopControl
	if lc == nil {
		return nil, s55SeamAbsent("loop-control"), nil
	}
	res, err := lc.Control(ctx, l.ID)
	if err != nil {
		return nil, nil, err
	}
	return res, nil, nil
}

// budgetList is the budget_list arm over the socket's budget.list read.
func (r *RecordsService) budgetList(ctx context.Context, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		Scope     string `json:"scope"`
		SubjectID string `json:"subject_id"`
		Enabled   *bool  `json:"enabled"`
		Status    string `json:"status"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	bs := r.d.Table.BudgetStore
	if bs == nil {
		return nil, s55SeamAbsent("budget"), nil
	}
	res, err := bs.List(ctx, in.Scope, in.SubjectID, in.Status, in.Enabled)
	if err != nil {
		return nil, nil, err
	}
	return res, nil, nil
}

// budgetShow is the budget_show arm over the socket's budget.show read.
func (r *RecordsService) budgetShow(ctx context.Context, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		BudgetID string `json:"budget_id"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	bs := r.d.Table.BudgetStore
	if bs == nil {
		return nil, s55SeamAbsent("budget"), nil
	}
	res, err := bs.Show(ctx, in.BudgetID)
	if err != nil {
		return nil, nil, err
	}
	return res, nil, nil
}

// budgetEffective is the budget_effective arm: what WOULD APPLY to this
// loop's spend, tightest first — the socket's budget.effective read with the
// loop stamped from the identity (the agent sees its own loop's tiers). Like
// loopControlState the loop is stamped, never named, so the decode is over an
// EMPTY struct — a typed loop_id is a parse refusal, not a parameter.
func (r *RecordsService) budgetEffective(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if f, err := decode(input, &struct{}{}); f != nil || err != nil {
		return nil, f, err
	}
	l, f := r.ownLoop(ident)
	if f != nil {
		return nil, f, nil
	}
	bs := r.d.Table.BudgetStore
	if bs == nil {
		return nil, s55SeamAbsent("budget"), nil
	}
	res, err := bs.Effective(ctx, l.ID, "", "")
	if err != nil {
		return nil, nil, err
	}
	return res, nil, nil
}

// budgetSpend is the budget_spend arm over the socket's spend.report read.
func (r *RecordsService) budgetSpend(ctx context.Context, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		GroupBy string `json:"group_by"`
		Since   string `json:"since"`
		Until   string `json:"until"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	bs := r.d.Table.BudgetStore
	if bs == nil {
		return nil, s55SeamAbsent("budget"), nil
	}
	res, err := bs.SpendReport(ctx, protocol.SpendReportParams{GroupBy: in.GroupBy, Since: in.Since, Until: in.Until})
	if err != nil {
		return nil, nil, err
	}
	return res, nil, nil
}

// watchdogList is the watchdog_list arm over the socket's watchdog.list read.
func (r *RecordsService) watchdogList(ctx context.Context, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		Scope     string `json:"scope"`
		SubjectID string `json:"subject_id"`
		Enabled   *bool  `json:"enabled"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	ws := r.d.Table.Watchdog
	if ws == nil {
		return nil, s55SeamAbsent("watchdog"), nil
	}
	res, err := ws.List(ctx, in.Scope, in.SubjectID, in.Enabled)
	if err != nil {
		return nil, nil, err
	}
	return res, nil, nil
}

// requestStopLimits are the arm's own shape checks BEFORE the store's counted
// limits: reason required, 1 to 512 bytes — a stop request without a reason
// is a flag nobody can act on, and a wall of text is not a reason either.
const requestStopReasonMaxBytes = 512

// loopRequestStop is the loop_request_stop arm (LC15, the ONE write): file
// the inert stop request the person will see (and act on, or dismiss) —
// never a halt. The agent is stamped from the step identity (by_agent,
// by_step_id), never argued; the loop is the caller's own unless the id
// names it (MC2: another loop's id is refused, naming both loops). The row
// commits through the store's one door (the counted limits ride with it, and
// the refusal names the limit and the door that shows the open ones), and
// the plane's loop.stop.requested event goes out the same publish the
// socket's door uses — so a person sees the ask in both doors at once.
func (r *RecordsService) loopRequestStop(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		LoopID string `json:"loop_id"`
		Reason string `json:"reason"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if n := len([]byte(in.Reason)); n == 0 {
		return nil, &Failure{Sentence: "refused: a stop request needs a reason — what should a person stop, and why (1 to 512 bytes)"}, nil
	} else if n > requestStopReasonMaxBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: the stop request's reason is %d bytes — the desk keeps 1 to %d, and a longer text is not a reason", n, requestStopReasonMaxBytes)}, nil
	}
	own, f := r.ownLoop(ident)
	if f != nil {
		return nil, f, nil
	}
	loopID := own.ID
	if in.LoopID != "" {
		if in.LoopID != own.ID {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: loop %s is not this loop's own loop (%s) — a stop request is filed against the loop the agent runs in, and another loop is a person's ask through the socket's loop.requestStop", in.LoopID, own.Slug)}, nil
		}
	}
	req, err := r.st.AddControlRequest(loopID, ident.Agent, in.Reason, &ident.StepID)
	if err != nil {
		var refusal store.ControlRefusal
		if errors.As(err, &refusal) {
			return nil, &Failure{Sentence: "refused: " + refusal.Detail}, nil
		}
		return nil, nil, err
	}
	// The row is INERT (LC15): nothing halted. The event says the agent
	// asked — the SAME emission the socket's door makes (one payload shape,
	// the notify hint rides the kind's closed set).
	r.d.publishLoop(protocol.KindLoopStopRequested, map[string]any{
		"loopId":    req.LoopID,
		"requestId": req.ID,
		"byAgent":   req.ByAgent,
		"byStepId":  req.ByStepID,
		"reason":    req.Reason,
	})
	return protocol.RequestStopResult{RequestID: req.ID, Status: req.Status}, nil, nil
}
