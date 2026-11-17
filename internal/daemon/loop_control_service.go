package daemon

// loop_control_service.go — S55 T5: the daemon's implementation of the
// protocol.LoopControlService seam (Table.LoopControl). It is what the seven
// socket doors (loop.stop / loop.resume / loop.cancel / loop.control /
// loop.requestStop / loop.control.requests / loop.control.dismiss) reach:
// the T3 control core's in-process operations (with the daemon's hooks), the
// T2 store's request desk and control audit, and the daemon's own publish
// for the plane's two new event kinds (LC20).
//
// # The actor
//
// The person is stamped by the PROTOCOL layer through table.ActorFor (the
// kernel's peer credential) and arrives here already resolved — this file
// never reads an identity from a request, and no code path here can mint a
// Kind "agent" for a person act. The one agent identity on this surface is
// the stop REQUEST's, and it is derived from the loop's own in-flight step
// row (LC15: the daemon stamps the agent; the request is an agent act).
//
// # One computation, two doors
//
// loop.control's eligibility block and loop.show's control block are computed
// by the SAME loop.CheckStop/CheckResume/CheckCancel functions (LC16: the
// button and the engine cannot disagree). The raise arm is the cap-hit
// predicate below until T7 folds it into loop.CheckRaise — one predicate,
// kept beside the data it will grow (the budget rows).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// loopControlService is the daemon-side seam implementation. It is a small
// struct (not the Daemon itself) so the nil-guard is a constructor: a
// storeless daemon hands the table nil, and RegisterLoopControl leaves the
// seven doors ABSENT rather than registered-and-refusing (V13, the settings
// precedent).
type loopControlService struct {
	d *Daemon
}

// newLoopControlService is the nil guard the daemon's wiring calls (V13).
func (d *Daemon) newLoopControlService() protocol.LoopControlService {
	if d.store == nil {
		return nil
	}
	return loopControlService{d: d}
}

// --- the conversions ----------------------------------------------------------

func controlRefusalView(r *store.ControlRefusal) *protocol.ControlRefusal {
	if r == nil {
		return nil
	}
	return &protocol.ControlRefusal{Kind: r.Kind, Detail: r.Detail}
}

func controlRequestView(r store.ControlRequest) protocol.ControlRequestView {
	return protocol.ControlRequestView{
		ID: r.ID, LoopID: r.LoopID, RequestedAt: r.RequestedAt,
		ByAgent: r.ByAgent, ByStepID: r.ByStepID, Reason: r.Reason,
		Status: r.Status, DecidedAt: r.DecidedAt, DecidedBy: r.DecidedBy,
	}
}

func controlEventView(e store.ControlEvent) protocol.ControlEventView {
	v := protocol.ControlEventView{
		At: e.At, Kind: e.Kind, ActorKind: e.ActorKind, ActorID: e.ActorID,
		StepID: e.StepID, RuleID: e.RuleID, Reason: e.Reason,
	}
	// Detail is a closed JSON set (LC10); "{}" is the store's "none", which
	// renders as the field's absence, not an empty object.
	if e.Detail != "" && e.Detail != "{}" {
		v.Detail = json.RawMessage(e.Detail)
	}
	return v
}

// notFoundLoop is the door's answer for a loop the store does not have —
// the S39 run door's shape (runs.go): a param that names nothing, -32602.
func notFoundLoop(id string) error {
	return protocol.ErrBadParams{Err: fmt.Errorf("no loop %q — the id names a loop that is not in this store", id)}
}

// --- the operations -------------------------------------------------------------

// Stop is loop.stop's daemon door (LC2): eligibility, the fixed order
// (steps before runner), the compare-and-set park — the T3 operation with
// the daemon's hooks. A refusal is a RESULT the socket renders as the typed
// refusal error (one code, data.kind); a lost race is the honest no-op the
// operation returns.
func (s loopControlService) Stop(ctx context.Context, actor protocol.Actor, id, reason string) (protocol.LoopStopResult, *protocol.ControlRefusal, error) {
	result, refusal, err := s.d.StopLoop(ctx, id, store.Actor{Kind: actor.Kind, ID: actor.ID}, reason)
	if err != nil {
		return protocol.LoopStopResult{}, nil, s.mapError(id, err)
	}
	return protocol.LoopStopResult{
		LoopID: result.LoopID, Stopped: result.Stopped, Status: result.Status,
		ParkedSteps: result.ParkedSteps, Note: result.Note,
	}, controlRefusalView(refusal), nil
}

// Resume is loop.resume's daemon door (LC3): the daemon decides WHICH resume
// (parked step vs between-steps), the operation CAS's, How rides the result
// so the app reports the daemon's decision.
func (s loopControlService) Resume(ctx context.Context, actor protocol.Actor, id, message string) (protocol.LoopResumeResult, *protocol.ControlRefusal, error) {
	result, refusal, err := s.d.ResumeLoop(ctx, id, store.Actor{Kind: actor.Kind, ID: actor.ID}, message)
	if err != nil {
		return protocol.LoopResumeResult{}, nil, s.mapError(id, err)
	}
	out := protocol.LoopResumeResult{
		LoopID: result.LoopID, Resumed: result.Resumed, Status: result.Status,
		How: result.How,
	}
	if refusal != nil {
		// A lost race is a result with refused data, not an error (the
		// protocol layer renders it into LoopResumeResult.Refusal).
		out.Refusal = controlRefusalView(refusal)
		return out, nil, nil
	}
	return out, nil, nil
}

// Cancel is loop.cancel's daemon door (LC4). A successful cancel ends the
// loop `aborted` through the engine's one writer, the run `cancelled`, and
// NEVER runs the on_abort lane (OD1: certain and free) — the result says so.
func (s loopControlService) Cancel(ctx context.Context, actor protocol.Actor, id, reason string) (protocol.LoopCancelResult, *protocol.ControlRefusal, error) {
	result, refusal, err := s.d.CancelLoop(ctx, id, store.Actor{Kind: actor.Kind, ID: actor.ID}, reason)
	if err != nil {
		return protocol.LoopCancelResult{}, nil, s.mapError(id, err)
	}
	out := protocol.LoopCancelResult{LoopID: result.LoopID, Status: result.Status}
	if result.Cancelled {
		out.RunState = "cancelled"
		out.SkippedAbortLane = true
	}
	return out, controlRefusalView(refusal), nil
}

// Control is loop.control's read (LC16): the SAME eligibility the operations
// gate on (and loop.show renders), the open stop requests, and the recent
// audit. The raise verdict is loop.CheckRaise — the engine's own predicate
// (T7's fold: one function, the control door and the show door, so the
// button and the engine cannot disagree).
func (s loopControlService) Control(ctx context.Context, id string) (protocol.LoopControlResult, error) {
	st, err := loop.Pinned(ctx, s.d.store, id)
	if err != nil {
		return protocol.LoopControlResult{}, s.mapError(id, err)
	}
	verdict := func(r *store.ControlRefusal) *protocol.ControlActionView {
		if r == nil {
			return &protocol.ControlActionView{Eligible: true}
		}
		return &protocol.ControlActionView{Eligible: false, Refusal: controlRefusalView(r)}
	}
	out := protocol.LoopControlResult{
		LoopID: id,
		Status: st.Status,
		Eligibility: protocol.LoopControlEligibility{
			Stop:   verdict(loop.CheckStop(st)),
			Resume: verdict(loop.CheckResume(st)),
			Cancel: verdict(loop.CheckCancel(st)),
			Raise:  verdict(loop.CheckRaise(st)),
		},
	}
	requests, err := s.d.store.ListControlRequests(store.ControlRequestFilter{LoopID: id, Status: "open"})
	if err != nil {
		return protocol.LoopControlResult{}, fmt.Errorf("daemon: list open stop requests for %s: %w", id, err)
	}
	out.OpenRequests = make([]protocol.ControlRequestView, 0, len(requests))
	for _, r := range requests {
		out.OpenRequests = append(out.OpenRequests, controlRequestView(r))
	}
	events, err := s.d.store.ListControlEvents(id, protocol.ControlEventWindow)
	if err != nil {
		return protocol.LoopControlResult{}, fmt.Errorf("daemon: read the control audit of %s: %w", id, err)
	}
	out.Events = make([]protocol.ControlEventView, 0, len(events))
	for _, e := range events {
		out.Events = append(out.Events, controlEventView(e))
	}
	return out, nil
}

// RequestStop is loop.requestStop's socket door (LC15): file the inert row,
// emit the event, halt NOTHING. The agent identity is derived from the
// loop's in-flight step — the request is an agent act, and the agent it
// names is the one the loop is running (a params agent would let a caller
// file against an agent that never asked). No in-flight agent step is a
// typed refusal: there is no agent to attribute the request to.
func (s loopControlService) RequestStop(ctx context.Context, loopID, reason string) (protocol.RequestStopResult, *protocol.ControlRefusal, error) {
	st, err := loop.Pinned(ctx, s.d.store, loopID)
	if err != nil {
		return protocol.RequestStopResult{}, nil, s.mapError(loopID, err)
	}
	stepID, agent, found := s.inflightAgent(st)
	if !found {
		return protocol.RequestStopResult{}, &protocol.ControlRefusal{
			Kind:   "no-agent",
			Detail: "the loop has no agent step in flight to carry this request — a stop request is an agent act, and there is no agent running to ask (loop.stop is the door for stopping running work)",
		}, nil
	}
	req, err := s.d.store.AddControlRequest(loopID, agent, reason, &stepID)
	var refusal store.ControlRefusal
	if errors.As(err, &refusal) {
		return protocol.RequestStopResult{}, controlRefusalView(&refusal), nil
	}
	if err != nil {
		return protocol.RequestStopResult{}, nil, fmt.Errorf("daemon: file the stop request for %s: %w", loopID, err)
	}
	// The request is INERT (LC15): nothing halted. The event says the agent
	// asked, so a person sees it (loop.control's openRequests, S58's inbox)
	// and acts or dismisses.
	s.d.publishLoop(protocol.KindLoopStopRequested, map[string]any{
		"loopId":    loopID,
		"requestId": req.ID,
		"byAgent":   req.ByAgent,
		"byStepId":  req.ByStepID,
		"reason":    req.Reason,
	})
	return protocol.RequestStopResult{RequestID: req.ID, Status: req.Status}, nil, nil
}

// inflightAgent derives the agent identity a stop request is stamped with:
// the in-flight step (the serial path's LastStepID when StepInFlight, plus
// any running parallel unit) whose row still says running and names its
// agent. The step ROW is the truth — the pinned state's StepStates can be
// one settle behind, and a step row that is done is not the agent asking.
func (s loopControlService) inflightAgent(st loop.LoopState) (stepID int64, agent string, found bool) {
	seen := make(map[int64]bool)
	var candidates []int64
	add := func(id int64) {
		if id <= 0 || seen[id] {
			return
		}
		seen[id] = true
		candidates = append(candidates, id)
	}
	if st.StepInFlight {
		add(st.LastStepID)
	}
	for _, ss := range st.StepStates {
		if ss.Status == "running" {
			add(ss.StepID)
		}
	}
	for _, id := range candidates {
		step, err := s.d.store.ReadStep(id)
		if err != nil {
			continue // the row is gone or was never a step of a live run
		}
		if step.Status == "running" && strings.TrimSpace(step.Agent) != "" {
			return id, step.Agent, true
		}
	}
	return 0, "", false
}

// Requests is loop.control.requests's read: the stop-request desk, filtered
// by loop and/or status (open first, oldest first — the store's order).
func (s loopControlService) Requests(_ context.Context, loopID, status string) ([]protocol.ControlRequestView, error) {
	requests, err := s.d.store.ListControlRequests(store.ControlRequestFilter{LoopID: loopID, Status: status})
	if err != nil {
		return nil, fmt.Errorf("daemon: list stop requests: %w", err)
	}
	out := make([]protocol.ControlRequestView, 0, len(requests))
	for _, r := range requests {
		out = append(out, controlRequestView(r))
	}
	return out, nil
}

// Dismiss is loop.control.dismiss's door (LC15): settle the request
// open→dismissed exactly once (the store's CAS; a decided or missing
// request is a named refusal), stamp the person from the connection, and
// emit the settlement so the desk's screen updates. The loop is untouched —
// a dismissal is "noted, not acting", never a control act on the loop.
func (s loopControlService) Dismiss(ctx context.Context, actor protocol.Actor, requestID int64) (protocol.ControlRequestView, *protocol.ControlRefusal, error) {
	req, err := s.d.store.DecideControlRequest(requestID, "dismissed", actor.Kind+":"+actor.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return protocol.ControlRequestView{}, &protocol.ControlRefusal{
				Kind:   "not-open",
				Detail: fmt.Sprintf("stop request %d is not open — it is already settled or never filed (loop.control.requests names the desk)", requestID),
			}, nil
		}
		return protocol.ControlRequestView{}, nil, fmt.Errorf("daemon: dismiss stop request %d: %w", requestID, err)
	}
	s.d.publishLoop(protocol.KindLoopControlDismissed, map[string]any{
		"loopId":    req.LoopID,
		"requestId": req.ID,
		"decidedBy": req.DecidedBy,
	})
	return controlRequestView(req), nil, nil
}

// mapError is the door's error translation: a loop the store does not have
// is a param that names nothing (the S39 run door's -32602 shape); every
// other error is infrastructure and rides up unchanged.
func (s loopControlService) mapError(id string, err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return notFoundLoop(id)
	}
	return err
}
