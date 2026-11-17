package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// conversation.go is S39 T6: the message that starts a turn (CR3).
//
// # What a turn is, in the daemon's own words
//
// A turn is ONE ordinary step: the loop's conversation agent, run through
// the very same RunStepForLoop a blueprint step takes — streamed, steerable
// with step.steer, recoverable by S7's sweep, bounded by the same default
// iteration cap (R3: no turn-only cap). It is not a run: nothing here
// touches loop_run (the turn's step carries run_seq NULL), and — CR9 —
// nothing here writes loop.status: a resting loop STAYS idle while its turn
// runs, because the loop is not walking; one of its steps happens to be
// talking. The turn's reply is stored nowhere but the thread and the step's
// own transcript.
//
// # The order of checks, and why it is fixed
//
// The step check runs BEFORE the note is written: a message that arrives
// while a step runs is answered with the step.steer remedy and must leave
// no chat_message row behind — a thread row that the person was told to put
// in a steer instead is a ghost message nobody sent. Only a loop that is
// idle, step-free and turn-free earns a note, and the note's commit is the
// turn's starting gun.

// DefaultConverseAgent answers a loop whose blueprint names no converse:
// agent (T0-Q3 — the general chat handler; no specialist is the default
// conversation agent, and the registry's one general body is exactly that).
const DefaultConverseAgent = "chat"

// messageLoop is the socket door's daemon half (protocol.MethodLoopMessage).
// Refusals are DATA with the daemon's own sentence — never a socket error
// the app interprets — and the loop's status is none of its business.
func (d *Daemon) messageLoop(ctx context.Context, p protocol.LoopMessageParams) (any, error) {
	if d.store == nil {
		return nil, protocol.ErrStoreUnavailable
	}
	pin, err := d.store.ReadPinnedLoop(p.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, protocol.ErrBadParams{Err: fmt.Errorf("no loop %q", p.ID)}
		}
		return nil, err
	}
	sc := store.Scope{LoopSlug: pin.Loop.Slug, ComponentRoot: pin.Loop.ComponentRoot}

	// The step check BEFORE the note: a running step is spoken to with
	// step.steer, not talked over; a suspended one has a door of its own
	// (answer, or step.continue). Either refusal adds nothing to the thread.
	steps, err := d.store.ListSteps(sc)
	if err != nil {
		return nil, err
	}
	for _, s := range steps {
		switch s.Status {
		case "running":
			return map[string]any{"refused": map[string]any{
				"kind":   "step-running",
				"stepId": s.ID,
				"detail": fmt.Sprintf("step %d is running in this loop — message it with step.steer {stepId, text}", s.ID),
			}}, nil
		case "suspended":
			return map[string]any{"refused": map[string]any{
				"kind":   "step-waiting",
				"stepId": s.ID,
				"detail": fmt.Sprintf("step %d is waiting on a question or a stop — answer it, or step.continue {stepId, message}", s.ID),
			}}, nil
		}
	}
	if pin.Loop.Status != "idle" {
		return map[string]any{"refused": map[string]any{
			"kind":   "not-idle",
			"detail": fmt.Sprintf("this loop is %s, not idle: while it runs, message a running step with step.steer", pin.Loop.Status),
		}}, nil
	}
	// S68 B6: the chosen skills are checked HERE, before the claim and
	// before any row — an unknown name refuses the whole call naming every
	// offender, and a refusal adds nothing to the thread (the same rule as
	// the step checks above). Skipped-but-reported names are left out of
	// the row: the receipt says what rode; the step's opening note says
	// what did not.
	res, resErr := d.resolveChosenSkills(p.Skills)
	if resErr != nil {
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("loop %s: %v", p.ID, resErr)}
	}
	var launch *chosenLaunch
	if res != nil {
		launch = &chosenLaunch{Lines: res.Lines, Note: chosenSkipNote(res.Skipped)}
	}
	// S69 AT3: the attachment references resolve HERE — before the claim,
	// before any row — so a refusal adds nothing to the thread (the same
	// rule as the skills above and the step checks before them). Filed
	// receipts are the store's facts: same bytes dedupe, same name different
	// bytes is a different frozen document, and the row can only ever pin a
	// revision that exists.
	var receipts []store.AttachmentReceipt
	if p.Attachments != nil {
		resolved, err := protocol.ResolveAttachmentRefs(storeAttachmentResolver{d.store},
			pin.Loop.Slug, pin.Loop.ComponentRoot, p.Attachments)
		if err != nil {
			return nil, err
		}
		for _, r := range resolved {
			receipts = append(receipts, store.AttachmentReceipt{
				Name: r.Name, Document: r.Document, Rev: r.Rev,
				MediaType: r.MediaType, Size: r.Size, SHA256: r.SHA256,
			})
		}
	}
	if !d.claimTurn(pin.Loop.ID) {
		return map[string]any{"refused": map[string]any{
			"kind":   "turn-in-flight",
			"detail": "a conversation turn is already running in this loop",
		}}, nil
	}

	msg, err := d.store.Note(sc, store.NewChatMessage{Author: "human", Kind: "note", Body: p.Text,
		Skills: rodeNames(p.Skills, res), Attachments: receipts})
	if err != nil {
		d.releaseTurn(pin.Loop.ID)
		return nil, err
	}
	d.publishMessage(pin.Loop.ID, msg)

	st, err := loop.FromPinned(pin)
	if err != nil {
		d.releaseTurn(pin.Loop.ID)
		return nil, err
	}
	thread, err := d.store.Thread(sc, 0)
	if err != nil {
		d.releaseTurn(pin.Loop.ID)
		return nil, err
	}
	agent := loop.ConverseAgent(st.Blueprint, DefaultConverseAgent)
	// S68 B3/B4: the person's new message rides the user message verbatim
	// and ALONE; the conversation frame (who they are, what the loop is for,
	// the honest trim note) rides the system message; the thread rides as
	// real alternating turns above the new words. Nothing of the daemon's
	// gets attributed to the person, and none of the person's gets rewritten.
	// (T3 lands the `skills` field on this door beside `route`.)
	words := loop.TurnWords(p.Text)
	// S69: the new row's receipts ride THIS turn's prompt (the person's
	// words leading, content blocks beside them — the expansion at the
	// compile boundary turns references into bytes); the thread's rows carry
	// their own through HistoryTurn. ComponentRoot scopes the resolution.
	newAtts := make([]protocol.AttachmentReceiptView, 0, len(receipts))
	for _, r := range receipts {
		newAtts = append(newAtts, protocol.AttachmentReceiptView{
			Name: r.Name, Document: r.Document, Rev: r.Rev,
			MediaType: r.MediaType, Size: r.Size, SHA256: r.SHA256,
		})
	}
	frame, history := loop.TurnParts(st, thread, msg.ID, loop.TurnThreadWindow)
	go d.runTurn(st, agent, words, frame, history, p.Route, launch, newAtts, pin.Loop.ComponentRoot)
	return map[string]any{"messageId": msg.ID, "seq": msg.Seq, "turn": "started"}, nil
}

// claimTurn is the backstop for the sliver between the note and the step
// row: false when a turn is already in flight for this loop.
func (d *Daemon) claimTurn(loopID string) bool {
	d.turnMu.Lock()
	defer d.turnMu.Unlock()
	if _, busy := d.turns[loopID]; busy {
		return false
	}
	d.turns[loopID] = struct{}{}
	return true
}

func (d *Daemon) releaseTurn(loopID string) {
	d.turnMu.Lock()
	defer d.turnMu.Unlock()
	delete(d.turns, loopID)
}

// turnParams builds the step request. MaxIterations stays ZERO on purpose
// (R3): a turn gets the same bound as a blueprint step — whatever the
// daemon's default is — because a turn is not a privileged step. S68: the
// person's words ride `prompt`; the frame rides `taskFrame`; the thread
// rides `history` as real alternating turns.
func turnParams(st loop.LoopState, agent, words, frame string, history []loop.ThreadTurn, route string, chosen *chosenLaunch, attachments []protocol.AttachmentReceiptView, componentRoot string) protocol.StepParams {
	if route == "" {
		route = st.Route
	}
	turns := make([]protocol.HistoryTurn, 0, len(history))
	for _, h := range history {
		tt := protocol.HistoryTurn{Role: h.Role, Text: h.Text}
		for _, r := range h.Attachments {
			tt.Attachments = append(tt.Attachments, protocol.AttachmentReceiptView{
				Name: r.Name, Document: r.Document, Rev: r.Rev,
				MediaType: r.MediaType, Size: r.Size, SHA256: r.SHA256,
			})
		}
		turns = append(turns, tt)
	}
	params := protocol.StepParams{Prompt: words, TaskFrame: frame, History: turns, Agent: agent, WorkspaceRoot: st.Root, Route: route, LoopSlug: st.Slug}
	if chosen != nil {
		// S68 B6: the chosen skills ride THIS turn's system message only —
		// the lines the door resolved, and the note naming what could not.
		params.Skills = chosen.Lines
		params.ChosenSkillsNote = chosen.Note
	}
	if len(attachments) > 0 {
		params.Attachments = attachments
		params.ComponentRoot = componentRoot
	}
	return params
}

// runTurn speaks one turn and files what it said. The identity is the
// server's (LoopStepIdentity, never client-supplied): the loop id, no lane,
// and the step named "turn" — the same seam a blueprint step crosses, with
// no walk position to mark (a turn has none).
func (d *Daemon) runTurn(st loop.LoopState, agent, words, frame string, history []loop.ThreadTurn, route string, chosen *chosenLaunch, attachments []protocol.AttachmentReceiptView, componentRoot string) {
	defer d.releaseTurn(st.ID)
	res, err := d.Table.RunStepForLoop(context.Background(), turnParams(st, agent, words, frame, history, route, chosen, attachments, componentRoot),
		protocol.LoopStepIdentity{LoopID: st.ID, Step: "turn"},
		func(stepID int64) error {
			// S31 D2's promise, kept by the turn too: the dispatch is named
			// BEFORE the step's first event can exist. lane is empty — a turn
			// stands outside the ladder.
			d.publishLoop(protocol.KindLoopStepDispatched, map[string]any{
				"loopId": st.ID, "lane": "", "step": "turn",
				"stepId": stepID, "agent": agent, "route": route,
			})
			return nil
		})
	sc := store.Scope{LoopSlug: st.Slug, ComponentRoot: st.ComponentRoot}
	if err != nil {
		// The person hears about a dead turn IN the thread, not only in a
		// stream they may not be watching — and it is the system speaking,
		// with no source to invent.
		if msg, nerr := d.store.Note(sc, store.NewChatMessage{Author: "system", Kind: "note",
			Body: "the turn could not run: " + err.Error()}); nerr == nil {
			d.publishMessage(st.ID, msg)
		}
		if d.log != nil {
			d.log.Infof("conversation turn for loop %s could not run: %v", st.ID, err)
		}
		return
	}
	// The reply lands in the thread only as the model's OWN handoff summary
	// — the same structured promise a blueprint step's ok carries. A turn
	// that suspended (a question, a stop) mirrors nothing: its answer is
	// the step's own business until the ordinary door continues it (the
	// recorded T6 gap: a continued turn's final reply is not mirrored).
	var handoff struct {
		Summary string `json:"summary"`
	}
	if len(res.Handoff) > 0 {
		if err := json.Unmarshal(res.Handoff, &handoff); err != nil {
			handoff.Summary = ""
		}
	}
	if handoff.Summary != "" {
		if msg, nerr := d.store.Note(sc, store.NewChatMessage{Author: "agent", Kind: "note", Body: handoff.Summary}); nerr == nil {
			d.publishMessage(st.ID, msg)
		}
	}
	// S70 T2d: a converse turn that RECEIVED peer messages owes them replies
	// too — its handoff summary is its answer, filed through the same
	// door. (A turn that suspended answers nothing yet.)
	d.mailboxReplyTurns(res.StepID, handoff.Summary)
}
