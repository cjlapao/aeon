package protocol

// # Steering a running step (S31 — T4, D3, D5, LW4, LW10)
//
// `step.steer` queues a message (and/or a route/effort change) for a step that
// is RUNNING; the runtime injects it at the next turn boundary — never
// mid-stream (LW1) — including when the turn ended in tool calls, which are
// then not run and answered with a synthetic "not run: the person steered"
// result (LW2/LW3). `step.steers` lists a step's steers; `step.steer.cancel`
// withdraws a still-pending one.
//
// The wire shapes are the brief's §5 table; the conformance fixtures state
// them. Every steer is keyed by stepId, never by loop (D12/P33).
//
// # Who refuses what
//
// The STORE keeps the row honest (steers.go: queue/deliver/cancel, the
// delivered-without-text states made unrepresentable). This file holds the
// refusals that need a METHOD to point at — LW4's "that step is not running;
// use answer / step.continue instead" and LW10's bounds (≤ 32 KiB of text,
// ≤ 16 pending per step), each naming the thing refused and the remedy.
// Validation here is the doorway, not the vault: the store re-checks its own
// invariants, and a steer that passed the doorway is exactly the row the
// runtime will meet at the next boundary.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

const (
	// maxSteerTextBytes is LW10's text bound (brief §6 T4): a steer is an
	// instruction, not a file attachment — and the bound is stated in BYTES
	// because that is what a row stores and what the model request pays for.
	maxSteerTextBytes = 32 << 10
	// maxPendingSteers is LW10's queue bound: sixteen unread instructions is
	// already a conversation the model will not read well; a seventeenth is
	// the person fighting their own queue. The refusal names the cap and the
	// method that empties it.
	maxPendingSteers = 16
)

// SteerView is one steer row as the socket reports it (brief §5's
// `step.steers` shape).
type SteerView struct {
	SteerID         int64  `json:"steerId"`
	Text            string `json:"text,omitempty"`
	Route           string `json:"route,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	State           string `json:"state"`
	CreatedAt       string `json:"createdAt"`
	DeliveredAt     string `json:"deliveredAt,omitempty"`
	Iteration       int    `json:"iteration,omitempty"`
	// Source is present only when it is NOT user (S39 CR6): the common
	// case must leave the wire untouched (byte-identical frames, D7), and
	// the label rides exactly when there is something to say.
	Source string `json:"source,omitempty"`
	// Skills are the chosen skill NAMES that rode this steer (S68, B7) —
	// the queue's receipt: the row's `text` stayed the operator's pure
	// words, and the chosen instructions render at the drain. Absent means
	// nothing chosen (byte-identical to every pre-S68 row).
	Skills []string `json:"skills,omitempty"`
	// Attachments are the STORE'S facts for the files that rode this
	// steer (S69 AT3): resolved at the door from pinned revisions — name,
	// document, revision, sniffed type, size, digest. Absent means the
	// steer carried no files (byte-identical, D7); this is what the chat
	// row renders as chips, and what a broken preview names.
	Attachments []AttachmentReceiptView `json:"attachments,omitempty"`
	// Skipped names the chosen skills that did NOT ride (known but
	// unpickable at queue time, S68 B6). It answers the queue call only —
	// a stored row never carries it, because a row records what rode, not
	// what was asked and lost.
	Skipped []string `json:"skipped,omitempty"`
}

// SteerStore is the durable queue's door (the daemon adapter's store methods;
// nil on a table without a store, and the handlers refuse by name).
type SteerStore interface {
	// QueueSteer takes the operator's words plus the skill NAMES chosen for
	// this steer (S68, B7): the adapter validates them against the live
	// catalog — an unknown name comes back as an ErrBadParams naming every
	// offender, which handleSteer passes through rather than re-labelling —
	// and files the validated names beside the text, which stays pure.
	QueueSteer(stepID int64, text, route, effort string, skills []string, attachments []AttachmentRef) (SteerView, error)
	ListSteers(stepID int64) ([]SteerView, error)
	// CancelSteer withdraws a still-pending row and reports the row AND its
	// owning step (the `step.steer.cancelled` event carries `stepId`, and the
	// cancel params alone do not know who owns the row). A row that is no
	// longer pending comes back as an error naming the state found: a refused
	// cancellation is the person's own history, and the refusal has to say
	// what they tried to withdraw.
	CancelSteer(steerID int64) (view SteerView, stepID int64, err error)
}

// ── the wire shapes ──────────────────────────────────────────────────────────

type StepSteerParams struct {
	StepID          int64  `json:"stepId"`
	Text            string `json:"text,omitempty"`
	Route           string `json:"route,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// Skills names the skills the operator CHOSE for this steer (S68, B6):
	// validated against the live catalog by the queue (unknown names refuse
	// the call naming every offender), filed on the steer row beside the
	// PURE text, and rendered into the wire message at the drain. They ride
	// the next turn only — they do not stick to the step or the loop.
	Skills []string `json:"skills,omitempty"`
	// Attachments are references to previously PUT files (S69 AT3),
	// resolved and filed exactly as loop.message does — the steer's PURE
	// text stays pure; the receipts ride beside it.
	Attachments []AttachmentRef `json:"attachments,omitempty"`
}

// StepSteerResult is brief §5: `{steerId, state: "pending"}`.
type StepSteerResult struct {
	SteerID int64  `json:"steerId"`
	State   string `json:"state"`
	// Skipped names the chosen skills that did not ride (known but
	// unpickable at queue time, S68 B6) — the skip is reported, never
	// silently taken out of the operator's choice. Absent means nothing
	// skipped.
	Skipped []string `json:"skipped,omitempty"`
}

type StepSteersParams struct {
	StepID int64 `json:"stepId"`
}

// StepSteersResult is brief §5's array — BARE, not wrapped: the conformance
// fixture is the wire contract, and `[]` for a step with no steers reads as
// "the queue is empty" where a wrapper's null would read as failure.
type StepSteersResult = []SteerView

type StepSteerCancelParams struct {
	SteerID int64 `json:"steerId"`
}

// StepSteerCancelResult is brief §5: `{state}` — the state the row carries
// AFTER the cancel, which is the answer that settles the question "did my
// withdrawal take?".
type StepSteerCancelResult struct {
	State string `json:"state"`
}

// ── the handlers ─────────────────────────────────────────────────────────────

func (t *Table) handleSteer(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p StepSteerParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.StepID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("stepId must be positive — every steer is keyed by step, never by loop")}
	}
	text := strings.TrimSpace(p.Text)
	route := strings.TrimSpace(p.Route)
	effort := strings.TrimSpace(p.ReasoningEffort)
	if text == "" && route == "" && effort == "" {
		return nil, ErrBadParams{Err: fmt.Errorf(
			"step %d received a steer carrying no text, no route and no reasoning effort — it would inject nothing",
			p.StepID)}
	}

	// LW10, stated HERE because a bound is worth nothing unless the caller is
	// TOLD it: the store's version would arrive as a refusal with no remedy
	// attached (AGENTS §5).
	if len(text) > maxSteerTextBytes {
		return nil, ErrBadParams{Err: fmt.Errorf(
			"step %d's steer carries %d bytes of text, over this daemon's %d byte bound — send a shorter instruction",
			p.StepID, len(text), maxSteerTextBytes)}
	}
	if effort != "" && !model.ValidReasoningEffort(effort) {
		return nil, ErrBadParams{Err: fmt.Errorf("reasoningEffort %q is not one of %s", effort,
			strings.Join(model.ReasoningEfforts(), ", "))}
	}
	if route != "" {
		if t.Routes == nil {
			return nil, ErrStepUnavailable.WithWhat("this daemon cannot resolve routes, so a steer cannot change one")
		}
		if _, ok := t.Routes.Route(route); !ok {
			return nil, ErrBadParams{Err: fmt.Errorf(
				"step %d's steer names route %q, which this daemon does not know; known routes: %s",
				p.StepID, route, strings.Join(t.Routes.RouteIDs(), ", "))}
		}
	}

	// LW4: a steer needs a RUNNING step to arrive at, and every refusal names
	// the state found AND the method to use instead. A steer queued behind a
	// question would inject instructions while a gate waits for a different
	// kind of answer; a steer behind a person-stop competes with the one
	// method that restarts that step.
	if t.StepSteerState == nil {
		return nil, ErrStepUnavailable.WithWhat("this daemon cannot read a step's state to steer it")
	}
	status, pauseReason, agent, componentRoot, loopSlug, found := t.StepSteerState(p.StepID)
	if !found {
		return nil, ErrStepNotFound.WithWhat("step %d does not exist — a steer needs a running step "+
			"to arrive at", p.StepID)
	}
	switch status {
	case "running":
	default:
		switch {
		case status == "suspended" && pauseReason == "user":
			return nil, ErrSteerTargetNotRunning.WithWhat("step %d is stopped (suspended, pause reason `user`) "+
				"— your next words belong to `step.continue {message}`, which restarts it WITH them", p.StepID)
		case status == "suspended":
			return nil, ErrSteerTargetNotRunning.WithWhat("step %d is suspended waiting for an answer — use "+
				"`answer`; a steer behind a question injects instructions while the gate still expects a reply", p.StepID)
		default:
			return nil, ErrSteerTargetNotRunning.WithWhat("step %d is %s — nothing will read a steer now", p.StepID, status)
		}
	}

	if t.SteerStore == nil {
		return nil, ErrStepUnavailable.WithWhat("this daemon has no steer queue wired")
	}

	// LW10's queue bound. Counted at the doorway rather than trusted from the
	// runtime: the drain delivers everything pending, so an unbounded queue
	// is an unbounded prompt assembled without anyone's consent.
	pending, err := t.SteerStore.ListSteers(p.StepID)
	if err != nil {
		return nil, ErrStoreUnavailable.WithWhat("step %d's pending steers could not be counted: %v", p.StepID, err)
	}
	unread := 0
	for _, s := range pending {
		if s.State == "pending" {
			unread++
		}
	}
	if unread >= maxPendingSteers {
		return nil, ErrBadParams{Err: fmt.Errorf(
			"step %d already has %d pending steers — the cap is %d; withdraw one with `step.steer.cancel` "+
				"or let the next turn boundary read them", p.StepID, unread, maxPendingSteers)}
	}

	view, err := t.SteerStore.QueueSteer(p.StepID, text, route, effort, p.Skills, p.Attachments)
	if err != nil {
		// The queue's own named refusal (an unknown chosen skill, S68 B6) is
		// the CALLER's error, not an infrastructure failure: pass it through
		// with the offenders' names intact rather than re-labelling it
		// store-unavailable.
		var be ErrBadParams
		if errors.As(err, &be) {
			return nil, err
		}
		return nil, ErrStoreUnavailable.WithWhat("step %d's steer could not be queued: %v", p.StepID, err)
	}

	// D5 announces overrides; D3's queueing is a thing a person DID, and the
	// chat screen has to show the instruction the moment it is durable —
	// before any boundary decides when the model reads it.
	if t.OnEvent != nil {
		evp := StepEventPayload{StepID: p.StepID, SteerID: view.SteerID, Agent: agent,
			HasText: view.Text != "", Route: view.Route, ReasoningEffort: view.ReasoningEffort,
			Attachments: view.Attachments}
		evp.LoopID = t.loopIDFor(loopSlug, componentRoot)
		evp.componentRoot = componentRoot
		t.OnEvent("step.steer.queued", evp)
	}
	return StepSteerResult{SteerID: view.SteerID, State: view.State, Skipped: view.Skipped}, nil
}

func (t *Table) handleSteers(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p StepSteersParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.StepID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("stepId must be positive")}
	}
	if t.SteerStore == nil {
		return nil, ErrStepUnavailable.WithWhat("this daemon has no steer queue to list")
	}
	views, err := t.SteerStore.ListSteers(p.StepID)
	if err != nil {
		return nil, ErrStoreUnavailable.WithWhat("step %d's steers could not be listed: %v", p.StepID, err)
	}
	if views == nil {
		views = []SteerView{}
	}
	return StepSteersResult(views), nil
}

func (t *Table) handleSteerCancel(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p StepSteerCancelParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.SteerID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("steerId must be positive")}
	}
	if t.SteerStore == nil {
		return nil, ErrStepUnavailable.WithWhat("this daemon has no steer queue to cancel through")
	}
	view, stepID, err := t.SteerStore.CancelSteer(p.SteerID)
	if err != nil {
		// A refusal the ADAPTER classified — naming `delivered` (the model
		// already read it), `cancelled` (withdrawn once), or no such row —
		// is a protocol answer already, and it travels with the state's
		// name rather than being re-guessed from prose. Anything unclassified
		// is the store failing, and is named as that.
		var already *Error
		if errors.As(err, &already) {
			return nil, already
		}
		return nil, ErrStoreUnavailable.WithWhat("steer %d could not be cancelled: %v", p.SteerID, err)
	}
	if t.OnEvent != nil {
		evp := StepEventPayload{StepID: stepID, SteerID: p.SteerID}
		if t.StepSteerState != nil {
			_, _, agent, componentRoot, loopSlug, found := t.StepSteerState(stepID)
			if found {
				evp.Agent = agent
				evp.LoopID = t.loopIDFor(loopSlug, componentRoot)
				evp.componentRoot = componentRoot
			}
		}
		t.OnEvent("step.steer.cancelled", evp)
	}
	return StepSteerCancelResult{State: view.State}, nil
}
