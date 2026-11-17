package daemon

// S55 T3 — LC6: an exhaustion escalation question is answerable through the
// daemon's ONE loop-answer door.
//
// The loop engine's CommitGate inside handleExhaustion raises an escalation
// question whose options are exactly [continue abort] — a gate the engine, not
// a person, asked. The socket's loop.answer fronts every question the app
// shows, so an escalation question arrives at AnswerBlueprint. But neither of
// the ladder's doors can serve it:
//
//   - AnswerGate routes through the BLUEPRINT STEP at the current position —
//     and an escalation is not a step (it is the cap exhausted on the lane,
//     not a gate the lane asked), so the gate door would advance the wrong
//     thing;
//   - AnswerActionAsk serves only PendingAction asks — and an escalation
//     carries no pending action.
//
// Without the escalation arm the question is ASKED and can never be ANSWERED —
// the T7 walk's F9 orphan, the door that was missing its handle. This is the
// one table row the loop-level suite could not reach: the routing lives in the
// daemon, not the engine, so it is proved here, through the real AnswerBlueprint
// over the real store, on a loop the engine's own write path made waiting.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// escalationGateBlueprint is a loop with NO on_abort handler: an "abort" answer
// at its escalation question moves it to BLOCKED (AnswerEscalation's effect
// when there is no terminal handler) rather than a re-dispatch. That is the
// proof the answer routed through the escalation arm — a gate-step answer would
// advance the position, never say "blocked with no terminal handler" — and a
// blocked loop starts no daemon runner, so the answer is settled and the test
// is self-contained (a "continue" answer would wake the runner, which the test
// does not own).
const escalationGateBlueprint = `id: lc6-escalation
schemaVersion: 1
version: 1
lanes:
  - id: work
    step: {kind: gate, question: go}
questions:
  go:
    prompt: Continue?
    answers: [yes, no]
`

func TestEscalationAnswerableThroughLoopAnswer(t *testing.T) {
	s := recoveryStore(t)
	// New calls initBlueprints, so d.Table.AnswerBlueprintLoop is wired over the
	// real store (its dep's Store is d.store = s) — the same value the socket
	// answers from, not a stand-in.
	d := New(Options{Store: s})

	root := t.TempDir()
	bpPath := filepath.Join(t.TempDir(), "lc6-escalation.yaml")
	if err := os.WriteFile(bpPath, []byte(escalationGateBlueprint), 0600); err != nil {
		t.Fatal(err)
	}
	// A component the loop lives in (the org fixture's shape); the loop's
	// ComponentRoot is this component's Root.
	p, err := s.CreateComponent(store.NewComponent{Name: "lc6 group", Slug: "lc6-group", Root: "/repo/lc6-group"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: root, ParentID: p.Component.ID}); err != nil {
		t.Fatal(err)
	}

	// The loop through the engine's OWN write path (running, with a position at
	// its lane) — the same shape the daemon's runner produces, so the fixture is
	// real, not a hand-rolled row.
	st, err := loop.Start(context.Background(), s, store.NewLoop{
		Slug: "lc6-escalation", ComponentRoot: root, Root: root, Goal: "g", Mode: "standard",
	}, bpPath, nil, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	id := st.ID

	// The escalation question the engine commits: a gate with [continue abort].
	// CommitGate writes it and injects its id into the runtime JSON, so the
	// pinned loop reads QuestionID with PendingAction nil — exactly the daemon's
	// escalation arm's discriminator (waiting, no pending action, a question,
	// options [continue abort] not yet answered).
	raw, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitGate(id, store.Scope{LoopSlug: st.Slug, ComponentRoot: root},
		"T2 retries 2/2 exhausted at 'verify' — then: escalate: raise the cap, or accept the task cannot converge",
		[]string{"continue", "abort"}, raw); err != nil {
		t.Fatalf("CommitGate: %v", err)
	}
	// Sanity: the loop is now waiting at the escalation question the daemon arm
	// must recognise.
	waiting, err := loop.Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != "waiting" || waiting.QuestionID == 0 || waiting.PendingAction != nil {
		t.Fatalf("setup: loop is %s/%d/pending=%v, want waiting/questionID/pending-nil", waiting.Status, waiting.QuestionID, waiting.PendingAction)
	}

	// The answer through the daemon's ONE loop-answer door. The escalation arm
	// must route it to AnswerEscalation — not the gate door (the step at the
	// position is not an escalation) and not the action-ask door (no pending
	// action). "abort" accepts that the task cannot converge.
	res, err := d.Table.AnswerBlueprintLoop(context.Background(), protocol.LoopAnswerParams{ID: id, Answer: "abort"})
	if err != nil {
		t.Fatalf("AnswerBlueprintLoop (escalation answer) = error %v — the question is answerable, not refused", err)
	}
	// A refusal would be {refused:...}; a handled answer is the answer map. The
	// two are different shapes, so the assertion is the routing itself.
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("AnswerBlueprintLoop = %T, want the answer map (a refusal is {refused:...}) — the escalation door did not handle it", res)
	}
	// The escalation's effect on an abort with no terminal handler is BLOCKED —
	// the distinctive state that proves the answer went through AnswerEscalation
	// and not the gate-step answer (which would advance the position and never
	// say blocked-with-no-terminal-handler).
	if m["status"] != "blocked" {
		t.Fatalf("status = %v, want blocked — the escalation's effect on an abort with no terminal handler", m["status"])
	}
	if m["answer"] != "abort" {
		t.Fatalf("answer = %v, want abort (the person's word recorded on the question)", m["answer"])
	}
	// The loop moved off waiting, and the reason is the escalation's own — a
	// gate-step answer would never write it.
	after, err := loop.Pinned(context.Background(), s, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "blocked" {
		t.Fatalf("pinned status = %q, want blocked (the loop moved off waiting)", after.Status)
	}
	if after.LastReason != "aborted by user; blueprint has no terminal handler" {
		t.Fatalf("lastReason = %q, want the escalation's abort sentence — the gate door would never produce it", after.LastReason)
	}
	// The question is answered (not left hanging), with the person's word.
	qid, _ := m["questionId"].(int64)
	q, err := s.ReadQuestion(qid)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Answered() || q.Answer != "abort" {
		t.Fatalf("question = %+v, want answered with 'abort'", q)
	}
}
