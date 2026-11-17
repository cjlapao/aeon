package protocol

// loop_control.go — S55 T5: the loop-control plane's socket doors. Stop,
// resume, cancel, the control read and the stop-request desk are methods on
// the daemon's 0600 + peer-credential surface (P4 shape, LC5), and they are
// provably absent from every non-human door (the guard amendment in
// cmd/aeon/loop_control_amend_test.go).
//
// # The actor
//
// The person behind stop/resume/cancel/dismiss is stamped from the
// CONNECTION (the daemon's ActorFor seam, the kernel's peer credential),
// NEVER from params — a params actor would let a caller forge the audit
// identity of a write to loops it does not own. The params structs carry an
// explicit `actor` field precisely so that a forged one is REJECTED by name
// (ErrBadParams) rather than silently ignored: decodeParams ignores unknown
// fields by design (T3 forward-compat), and a rejected field is the only way
// a self-chosen actor is a parse error the caller sees.
//
// # Refusals
//
// An inapplicable control act rides ErrLoopControlRefused (one code, the
// state found in Data.Kind, the remedy in the What sentence — the store's
// ControlRefusal rendered verbatim, DB19). A LOST RACE is not a refusal: the
// act was eligible when gated and the loop moved meanwhile, so the answer is
// an honest result (stopped/resumed false) that names the status the loop
// already has — a stop's note, a resume's optional refusal.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The seven method names live in method.go with every other MethodName —
// tools/protogen reads that one const block to build the app's contract —
// this file carries their params, results, handlers and the register lines.

// controlReasonBound is the byte bound on person-authored control text
// (LC2/LC4/LC15: 1–512 bytes, stored in the control event's own column,
// never folded into a summary).
const controlReasonBound = 512

// ControlEventWindow is how many of the loop's control events (newest
// first) loop.control returns — the audit a person glances at, not the full
// ledger (the full ledger is the store's job; this is the screen's). It is
// exported because the wire contract names the window and the daemon's
// seam reads the same constant, not its own copy.
const ControlEventWindow = 20

// ControlRefusal is the wire twin of the store's ControlRefusal (DB19: a
// refused transition is a result, not an error). Kind is machine-readable —
// it rides the refusal error's Data.Kind so a client can branch on it without
// parsing the sentence; Detail is the person's sentence (what / why / do).
type ControlRefusal struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// ControlActionView is one eligibility verdict (LC16): eligible is the
// button's state, and a non-eligible action carries the SAME refusal the
// operation would answer — the daemon's answer, never a guess the UI makes.
type ControlActionView struct {
	Eligible bool            `json:"eligible"`
	Refusal  *ControlRefusal `json:"refusal,omitempty"`
}

// ControlRequestView is one stop-request desk row (LC15). The request is
// INERT: it records that an agent asked a person to stop a loop, and nothing
// halts until a person acts (loop.stop) or dismisses it.
type ControlRequestView struct {
	ID          int64  `json:"id"`
	LoopID      string `json:"loopId"`
	RequestedAt string `json:"requestedAt"`
	ByAgent     string `json:"byAgent"`
	ByStepID    *int64 `json:"byStepId,omitempty"`
	Reason      string `json:"reason"`
	Status      string `json:"status"` // open|honored|dismissed
	DecidedAt   string `json:"decidedAt,omitempty"`
	DecidedBy   string `json:"decidedBy,omitempty"`
}

// ControlEventView is one control-audit row (LC10): who acted, on which
// loop, what the act was, with the person's reason in its own field.
type ControlEventView struct {
	At        string          `json:"at"`
	Kind      string          `json:"kind"` // stop|resume|cancel|raise|cap_hit|watchdog|request|dismiss
	ActorKind string          `json:"actorKind"`
	ActorID   string          `json:"actorId"`
	StepID    *int64          `json:"stepId,omitempty"`
	RuleID    string          `json:"ruleId,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Detail    json.RawMessage `json:"detail,omitempty"`
}

// LoopCapHitView is the cap the loop is blocked on (LC16's capHit). It is
// nil until S55 T7's budget rows exist — a loop blocked on cap-hit before T7
// has no budget row to name. T7 fills the fields; the shape is T5's so the
// wire does not move when the data arrives.
type LoopCapHitView struct {
	BudgetID   string `json:"budgetId"`
	Scope      string `json:"scope"`
	Dimension  string `json:"dimension"`
	SpendFloor int64  `json:"spendFloor"`
	Cap        int64  `json:"cap"`
}

// --- params -----------------------------------------------------------------

// LoopControlActor is the REJECTED field every control write carries. It is
// present in the type (not unknown) so a caller who passes one gets a named
// refusal — "the actor is the connection, not a param" — instead of the
// silent ignore unknown fields get (T3). A non-empty value is ErrBadParams.
type LoopControlActor string

// LoopStopParams is loop.stop's params (LC2). reason is person text, 1–512
// bytes when present, stored in the control event's own column.
type LoopStopParams struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
	// Actor is never read. Present to be refused by name (see above).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// LoopResumeParams is loop.resume's params (LC3). message is the S31
// stop-and-inject text: it reaches the transcript exactly once, on a
// parked-step resume (a between-steps resume has no transcript to carry it,
// and the door says so rather than dropping it).
type LoopResumeParams struct {
	ID      string `json:"id"`
	Message string `json:"message,omitempty"`
	// Actor is never read. Present to be refused by name (see above).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// LoopCancelParams is loop.cancel's params (LC4). reason is REQUIRED — a
// cancel destroys progress irreversibly, and the audit must say why.
type LoopCancelParams struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	// Actor is never read. Present to be refused by name (see above).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// LoopControlParams is loop.control's read params (LC16): the loop by id.
type LoopControlParams struct {
	ID string `json:"id"`
}

// LoopRequestStopParams is loop.requestStop's params (LC15's socket twin).
// NO agent field: the request is an agent act and the daemon stamps the
// agent identity from the loop's in-flight step (the agent it names is the
// one asking to be stopped) — a params agent would let a caller file a
// request against an agent that never asked.
type LoopRequestStopParams struct {
	LoopID string `json:"loopId"`
	Reason string `json:"reason"`
	// Actor is never read. Present to be refused by name (see above).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// LoopControlRequestsParams is loop.control.requests's read params: the
// request desk, filtered by loop and/or status (both optional; open first,
// oldest first).
type LoopControlRequestsParams struct {
	LoopID string `json:"loopId,omitempty"`
	Status string `json:"status,omitempty"`
}

// LoopControlDismissParams is loop.control.dismiss's params: the request by
// id. Dismissing is the person's "noted, not acting" — the request is
// settled, the loop is untouched.
type LoopControlDismissParams struct {
	RequestID int64 `json:"requestId"`
	// Actor is never read. Present to be refused by name (see above).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// --- results ------------------------------------------------------------------

// LoopStopResult is loop.stop's answer (LC2): a person-visible outcome in
// every case. Stopped false with a Note is the honest no-op — the loop was
// not running (the refusal rides the error) or it moved during the act (the
// note names the status it already has).
type LoopStopResult struct {
	LoopID      string  `json:"loopId"`
	Stopped     bool    `json:"stopped"`
	Status      string  `json:"status"`
	ParkedSteps []int64 `json:"parkedSteps,omitempty"`
	Note        string  `json:"note,omitempty"`
}

// LoopResumeResult is loop.resume's answer (LC3). How is the path the resume
// took: "step" (a parked step cold-restarts from its checkpoint through the
// step-continue seam) or "runner" (a between-steps CAS to running plus a
// fresh runner) — the daemon's decision, reported so the app is never
// guessing. Refusal is set on a lost race (the act was eligible; the loop
// moved first): a result with refused data, not an error.
type LoopResumeResult struct {
	LoopID  string          `json:"loopId"`
	Resumed bool            `json:"resumed"`
	Status  string          `json:"status,omitempty"`
	How     string          `json:"how,omitempty"` // step|runner
	Refusal *ControlRefusal `json:"refusal,omitempty"`
}

// LoopCancelResult is loop.cancel's answer (LC4). A successful cancel ends
// the loop `aborted` through the engine's one writer (the run's state is
// `cancelled`) and NEVER runs the blueprint's on_abort lane — certain and
// free is the whole point of a cancel (OD1), so skippedAbortLane says it
// out loud rather than leaving it to the event stream.
type LoopCancelResult struct {
	LoopID           string `json:"loopId"`
	Status           string `json:"status"`
	RunState         string `json:"runState,omitempty"`
	SkippedAbortLane bool   `json:"skippedAbortLane,omitempty"`
}

// LoopControlResult is loop.control's answer (LC16): the daemon's own
// eligibility verdicts (the SAME functions the operations run as a gate —
// the button and the engine cannot disagree), the open stop requests, the
// cap the loop is blocked on (T7's data, T5's shape), and the recent audit.
type LoopControlResult struct {
	LoopID       string                 `json:"loopId"`
	Status       string                 `json:"status"`
	Eligibility  LoopControlEligibility `json:"eligibility"`
	OpenRequests []ControlRequestView   `json:"openRequests"`
	CapHit       *LoopCapHitView        `json:"capHit,omitempty"`
	Events       []ControlEventView     `json:"events"`
}

// LoopControlEligibility is the four-action block. Raise is computed by the
// cap-hit predicate T7 folds into loop.CheckRaise — same predicate, one
// place, until the budget rows exist.
type LoopControlEligibility struct {
	Stop   *ControlActionView `json:"stop"`
	Resume *ControlActionView `json:"resume"`
	Cancel *ControlActionView `json:"cancel"`
	Raise  *ControlActionView `json:"raise"`
}

// RequestStopResult is loop.requestStop's answer (LC15): the row was filed
// and is open. Nothing halted — the request is inert by construction.
type RequestStopResult struct {
	RequestID int64  `json:"requestId"`
	Status    string `json:"status"` // open
}

// LoopControlRequestsResult is loop.control.requests's answer: the desk,
// open first, oldest first.
type LoopControlRequestsResult struct {
	Requests []ControlRequestView `json:"requests"`
}

// LoopControlDismissResult is loop.control.dismiss's answer: the request's
// settled status.
type LoopControlDismissResult struct {
	RequestID int64  `json:"requestId"`
	Status    string `json:"status"` // dismissed
}

// --- the seam ------------------------------------------------------------------

// LoopControlService is the daemon-facing seam (Table.LoopControl). The
// protocol layer carries the wire contract; the daemon implements it over
// the T3 control core (Stop/Resume/Cancel with the daemon's hooks), the T2
// store (requests, events) and its own publish (the two new kinds). Nil
// leaves the seven methods ABSENT, not registered-and-refusing (V13, the
// settings precedent).
//
// Actor is protocol.Actor (the peer identity the handler resolved through
// ActorFor) — the seam receives it stamped, never a raw claim.
type LoopControlService interface {
	Stop(ctx context.Context, actor Actor, id, reason string) (LoopStopResult, *ControlRefusal, error)
	Resume(ctx context.Context, actor Actor, id, message string) (LoopResumeResult, *ControlRefusal, error)
	Cancel(ctx context.Context, actor Actor, id, reason string) (LoopCancelResult, *ControlRefusal, error)
	Control(ctx context.Context, id string) (LoopControlResult, error)
	RequestStop(ctx context.Context, loopID, reason string) (RequestStopResult, *ControlRefusal, error)
	Requests(ctx context.Context, loopID, status string) ([]ControlRequestView, error)
	Dismiss(ctx context.Context, actor Actor, requestID int64) (ControlRequestView, *ControlRefusal, error)
}

// RegisterLoopControl registers the seven control doors over a non-nil seam
// (V13). It returns whether the methods are in the table, so the daemon's
// wiring can be asserted rather than trusted — the settings shape.
func (t *Table) RegisterLoopControl() bool {
	if t.LoopControl == nil {
		return false
	}
	t.register(&Method{Name: MethodLoopStop, RequiresInitialize: true, Handle: t.handleLoopStop})
	t.register(&Method{Name: MethodLoopResume, RequiresInitialize: true, Handle: t.handleLoopResume})
	t.register(&Method{Name: MethodLoopCancel, RequiresInitialize: true, Handle: t.handleLoopCancel})
	t.register(&Method{Name: MethodLoopControl, RequiresInitialize: true, Handle: t.handleLoopControl})
	t.register(&Method{Name: MethodLoopRequestStop, RequiresInitialize: true, Handle: t.handleLoopRequestStop})
	t.register(&Method{Name: MethodLoopControlRequests, RequiresInitialize: true, Handle: t.handleLoopControlRequests})
	t.register(&Method{Name: MethodLoopControlDismiss, RequiresInitialize: true, Handle: t.handleLoopControlDismiss})
	return true
}

// --- the actor and the refusals -------------------------------------------------

// controlActor is the write boundary's audit identity (LC5): the person
// comes from the CONNECTION (the daemon's ActorFor seam, the kernel's peer
// credential), never from params. It is the honest absence (a daemon with
// no identity seam has no persons) that refuses the write, and an empty id
// that is refused as well.
func (t *Table) controlActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("loop control is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("loop control is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	return a, nil
}

// refuseActor names the rejection of a params-supplied actor (LC5). It is a
// parse error by shape (ErrBadParams → -32602): the field is not a value
// this protocol accepts anywhere, so there is no Aeon code for "you tried".
func refuseActor(field string) error {
	return ErrBadParams{Err: fmt.Errorf("%s is not a param — the actor is the connection (the daemon's peer credential), never a value a caller chooses", field)}
}

// controlRefusedError renders the store's ControlRefusal as the socket's
// refusal (DB19): one code (ErrLoopControlRefused), the state found in
// Data.Kind, the person's sentence in What, and the same means/todo the
// drain refusal carries — the control plane's one voice.
func controlRefusedError(verb string, r *ControlRefusal) *Error {
	e := ErrLoopControlRefused.WithWhat("loop %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	return e
}

// checkControlReason validates person-authored control text: 1–512 bytes
// after trim (LC2/LC4/LC15). It returns the trimmed text, so the audit
// column never carries the padding a caller sent.
func checkControlReason(label, reason string) (string, error) {
	r := strings.TrimSpace(reason)
	if len(r) == 0 {
		return "", ErrBadParams{Err: fmt.Errorf("%s is required (1–512 bytes of text)", label)}
	}
	if len(r) > controlReasonBound {
		return "", ErrBadParams{Err: fmt.Errorf("%s is %d bytes — the bound is %d", label, len(r), controlReasonBound)}
	}
	return r, nil
}

// --- the handlers ----------------------------------------------------------------

// loopControlSeam is the one nil-guard shape the seven handlers share:
// registration is conditional on the seam, so a nil here is a
// registration/seam disagreement the guard tests catch — spoken, not
// silent (the search.query shape).
func (t *Table) loopControlSeam() error {
	if t.LoopControl == nil {
		return ErrStoreUnavailable.WithWhat("loop.* control needs the loop-control seam, and this build did not link it — the control surface is absent for the same reason (V13)")
	}
	return nil
}

func (t *Table) handleLoopStop(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.loopControlSeam(); err != nil {
		return nil, err
	}
	var p LoopStopParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the loop to stop")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	reason := strings.TrimSpace(p.Reason)
	if p.Reason != "" {
		if r, err := checkControlReason("reason", p.Reason); err != nil {
			return nil, err
		} else {
			reason = r
		}
	}
	actor, err := t.controlActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, refusal, err := t.LoopControl.Stop(ctx, actor, p.ID, reason)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, controlRefusedError("stop", refusal)
	}
	return result, nil
}

func (t *Table) handleLoopResume(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.loopControlSeam(); err != nil {
		return nil, err
	}
	var p LoopResumeParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the loop to resume")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	actor, err := t.controlActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, refusal, err := t.LoopControl.Resume(ctx, actor, p.ID, strings.TrimSpace(p.Message))
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		// A lost race is a RESULT, not an error (see the file header): the
		// act was eligible when gated and the loop moved first.
		result.Refusal = refusal
		return result, nil
	}
	return result, nil
}

func (t *Table) handleLoopCancel(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.loopControlSeam(); err != nil {
		return nil, err
	}
	var p LoopCancelParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the loop to cancel")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	reason, err := checkControlReason("reason", p.Reason)
	if err != nil {
		return nil, err
	}
	actor, err := t.controlActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, refusal, err := t.LoopControl.Cancel(ctx, actor, p.ID, reason)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, controlRefusedError("cancel", refusal)
	}
	return result, nil
}

func (t *Table) handleLoopControl(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := t.loopControlSeam(); err != nil {
		return nil, err
	}
	var p LoopControlParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the loop to read")}
	}
	return t.LoopControl.Control(ctx, p.ID)
}

func (t *Table) handleLoopRequestStop(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.loopControlSeam(); err != nil {
		return nil, err
	}
	var p LoopRequestStopParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.LoopID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("loopId is required — name the loop the request is about")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	reason, err := checkControlReason("reason", p.Reason)
	if err != nil {
		return nil, err
	}
	// No actor: the request is an AGENT act (the daemon stamps the agent
	// from the loop's in-flight step), and the person's identity is the
	// connection's anyway — resolved and discarded here to keep the door's
	// "a person may speak" check live (a daemon without an identity seam
	// has no persons, and no door).
	if _, err := t.controlActor(ctx, sess); err != nil {
		return nil, err
	}
	result, refusal, err := t.LoopControl.RequestStop(ctx, p.LoopID, reason)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, controlRefusedError("stop request", refusal)
	}
	return result, nil
}

func (t *Table) handleLoopControlRequests(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := t.loopControlSeam(); err != nil {
		return nil, err
	}
	var p LoopControlRequestsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	switch p.Status {
	case "", "open", "honored", "dismissed":
	default:
		return nil, ErrBadParams{Err: fmt.Errorf("status is open, honored or dismissed, not %q", p.Status)}
	}
	requests, err := t.LoopControl.Requests(ctx, strings.TrimSpace(p.LoopID), p.Status)
	if err != nil {
		return nil, err
	}
	if requests == nil {
		requests = []ControlRequestView{}
	}
	return LoopControlRequestsResult{Requests: requests}, nil
}

func (t *Table) handleLoopControlDismiss(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.loopControlSeam(); err != nil {
		return nil, err
	}
	var p LoopControlDismissParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.RequestID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("requestId is required — name the request to dismiss")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	actor, err := t.controlActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	req, refusal, err := t.LoopControl.Dismiss(ctx, actor, p.RequestID)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, controlRefusedError("dismiss", refusal)
	}
	return LoopControlDismissResult{RequestID: req.ID, Status: req.Status}, nil
}
