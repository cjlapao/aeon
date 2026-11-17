package loop

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// run.go is S39 T8 (CR2): an idle loop runs a SECOND blueprint. A loop is
// not its pin — the pin is the CURRENT run's snapshot and the live position,
// and every immutable copy lives in loop_run. StartRun is the claim-before-
// write door: BeginRun's single transaction claims idle→running AND writes
// the new pin AND the new run row, so there is no window in which the loop
// is running on a half-written blueprint. The order below IS that promise:
// nothing may be WRITTEN before the claim — reading and validating first is
// not writing, and it is what lets an invalid blueprint refuse with the loop
// still resting, no run 2, no message (a refused start that moved the loop
// would be a start).
//
// What StartRun never does: it never writes the loop's D2 attention
// declaration (CR7 — what was declared at start stands for the loop's whole
// life; a second run re-declaring it would let a caller launder a
// nobody-here loop into a somebody-here one), never calls CreatePinnedLoop,
// never deletes or edits an earlier loop_run row (the history is
// append-only), and never reads message text to decide anything (CR4).
//
// A known, documented wrinkle: if the kickoff Note fails AFTER the claim,
// the run EXISTS without a linked message. That is survivable by design —
// the run's saved runtime state already carries the kickoff text, so the
// model still says the right first words; only the thread mirror is missing.
// The error is returned plainly; a future retry may fill it (SetRunKickoff is
// idempotent against the same row), and pretending otherwise by rolling the
// claim back would need BeginRun's transaction to span Note's — which it
// deliberately does not, so the claim stays one statement.

// RunRequest is a request to start run N+1 in a resting loop.
type RunRequest struct {
	LoopID, Blueprint          string
	StartedByKind, StartedByID string // user | agent | trigger | event (S40 AU9)
	GoalID                     string
	GoalVersion                int
	TaskID                     string
	With                       map[string]string
	Typed                      string                // a first message a person typed; "" = use the template/default
	Data                       blueprint.KickoffData // what the template may name
	Route                      string                // "" = inherit the loop's current route
}

// RunStarted is what a started run leaves behind.
type RunStarted struct {
	Run     store.Run
	Kickoff store.ChatMessage // zero when the run has no first message
}

// orDefault is the 3-line decision the route inherit needs: asked-for wins,
// inherited otherwise. (Not store.orDefault — that helper is the store's.)
func orDefault(got, fallback string) string {
	if got != "" {
		return got
	}
	return fallback
}

// StartRun pins a second blueprint as run N+1 of an idle loop: validate →
// claim-and-write (one transaction) → kickoff message. No runner starts
// here (the daemon's caller does — T9), no event is published, no requires
// is evaluated: this function makes rows, and only rows.
func StartRun(ctx context.Context, s *store.Store, req RunRequest) (RunStarted, error) {
	// 1. Read-only until the claim: refuse a loop that is not resting BEFORE
	// any work — the sentence names the status, and wraps the store's own
	// sentinel so callers errors.Is against the same ErrLoopNotIdle either
	// guard produced.
	pin, err := s.ReadPinnedLoop(req.LoopID)
	if err != nil {
		return RunStarted{}, err
	}
	if pin.Loop.Status != StatusIdle {
		return RunStarted{}, fmt.Errorf("loop %s is %s: %w", req.LoopID, pin.Loop.Status, store.ErrLoopNotIdle)
	}
	// 2. Validate with the START's own code. No write has happened, so an
	// invalid blueprint refuses with the loop still idle.
	bp, raw, err := pinSource(s, req.Blueprint, pin.Loop.Mode)
	if err != nil {
		return RunStarted{}, err
	}
	// 3. Fresh position from the new blueprint; the loop's identity carries
	// across: route inherited unless overridden, operator `with` copied.
	prev, err := FromPinned(pin)
	if err != nil {
		return RunStarted{}, err
	}
	initial := InitialState(bp)
	initial.Route = orDefault(req.Route, prev.Route)
	for k, v := range req.With {
		initial.With[k] = v
	}
	// 4. The first message, decided but NOT yet written — the text is chosen
	// before the claim so BeginRun's transaction is not followed by a
	// template error; the same string rides the run state and the thread.
	text, source, err := KickoffFor(&bp, req.Data, req.Typed)
	if err != nil {
		return RunStarted{}, err
	}
	initial.Kickoff = text
	state, err := (LoopState{RuntimeState: initial}).RuntimeJSON()
	if err != nil {
		return RunStarted{}, err
	}
	// 5. THE CLAIM. One transaction: idle→running + the new pin + the new run
	// row, or nothing at all. Everything above was reads.
	run, err := s.BeginRun(req.LoopID, store.NewRun{
		Blueprint: bp.ID, Version: bp.Version, Content: string(raw),
		StartedByKind: req.StartedByKind, StartedByID: req.StartedByID,
		GoalID: req.GoalID, GoalVersion: req.GoalVersion, TaskID: req.TaskID,
	}, state)
	if err != nil {
		return RunStarted{}, err
	}
	// 6. The thread mirror, AFTER the run exists (claim before write). A
	// failure here leaves a working run (its saved state carries the text)
	// and no link — the wrinkle documented above; the error is returned,
	// never swallowed.
	var msg store.ChatMessage
	if text != "" {
		sc := store.Scope{LoopSlug: pin.Loop.Slug, ComponentRoot: pin.Loop.ComponentRoot}
		msg, err = s.Note(sc, store.NewChatMessage{Author: KickoffAuthor(source), Kind: "note", Body: text, Source: source})
		if err != nil {
			return RunStarted{}, err
		}
		if err := s.SetRunKickoff(req.LoopID, run.RunSeq, msg.ID); err != nil {
			return RunStarted{}, err
		}
		// The returned row says what the DB now holds — BeginRun's snapshot
		// predates the link it does not know about.
		id := msg.ID
		run.KickoffMessageID = &id
	}
	return RunStarted{Run: run, Kickoff: msg}, nil
}
