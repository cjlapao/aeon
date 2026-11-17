package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/policy"
)

// The board's seams are faked here: the daemon must not import agentsvc
// (AS11), so the invoke, the relayed write and the project-root resolver cross
// as funcs, and a test supplies fakes for them. No real store, no live provider
// — the fakes record, the assertions check the record.

// spyInvoke records every ask the board makes and returns a fixed task or
// error. A zero record count is how "refused before the invoke" is asserted
// (TestBoardQuestionNamesTargetAgent).
type spyInvoke struct {
	calls int
	ask   boardAsk
	task  boardTask
	err   error
}

func (s *spyInvoke) invoke(ctx context.Context, a boardAsk) (boardTask, error) {
	s.calls++
	s.ask = a
	if s.err != nil {
		return boardTask{}, s.err
	}
	return s.task, nil
}

// recordingRelayed records the AppendInput the board files and returns a
// learning. It is the fake of learnings.AppendRelayed: the gate asserts the
// board asks the one write path for exactly one row, with the right fields —
// not the store's own row (that is the learnings package's own tests).
type recordingRelayed struct {
	in    learnings.AppendInput
	calls int
}

func (r *recordingRelayed) relayed(ctx context.Context, in learnings.AppendInput) (learnings.Learning, *learnings.Refusal) {
	r.calls++
	r.in = in
	return learnings.Learning{ID: 1, Scope: in.Scope, ScopeKey: in.ScopeKey, Body: in.Body, Provenance: in.Provenance}, nil
}

// newBoardWithFakes builds a Board whose invoke, relayed and project-root are
// fakes; the frozen seam and clock are the constant values the assertions do
// not depend on.
func newBoardWithFakes(spy *spyInvoke, rec *recordingRelayed, projectRoot string) *Board {
	return &Board{
		invoke: spy.invoke,
		relayed: rec.relayed,
		projectRoot: func(ctx context.Context, callerAgent string) (string, error) {
			return projectRoot, nil
		},
		frozen: func(ctx context.Context, callerAgent string) ([]policy.Policy, error) {
			return nil, nil
		},
		now: func() time.Time { return time.Unix(1700000000, 0) },
	}
}

// TestBoardQuestionNamesTargetAgent — a call with no target is refused, with a
// named reason, before the invoke is ever called. A spy on the invoke asserts
// zero calls: the refusal is not a behaviour the invoke may decline, it is a
// gate the board keeps before any ask is made (D8, N2).
func TestBoardQuestionNamesTargetAgent(t *testing.T) {
	spy := &spyInvoke{task: boardTask{Status: boardTaskCompleted}}
	b := newBoardWithFakes(spy, &recordingRelayed{}, "/root")

	_, err := b.AskBoard(context.Background(), "asker", "", "what is the capital of France")
	if err == nil {
		t.Fatal("a board question with no target was accepted — it must be refused")
	}
	if !contains(err.Error(), "names its target") {
		t.Fatalf("the refusal must name the missing target: %v", err)
	}
	if spy.calls != 0 {
		t.Fatalf("the invoke was called %d times for a call with no target — it must be refused before the invoke", spy.calls)
	}
}

// TestBoardOnlyInvokesAnswerSkill — the board's only ask is the answer skill,
// and the signature makes any other unreachable. Asserted structurally, not
// just behaviourally: boardAsk has no skill field, so no call could name a
// skill other than the answer skill the invoke seam supplies. The behavioural
// half — that the invoke IS called, with the named callee and the question —
// is the complement.
func TestBoardOnlyInvokesAnswerSkill(t *testing.T) {
	spy := &spyInvoke{task: boardTask{Status: boardTaskCompleted, ResultText: "an answer", CallerAgent: "asker"}}
	b := newBoardWithFakes(spy, &recordingRelayed{}, "/root")

	_, err := b.AskBoard(context.Background(), "asker", "target-agent", "what did the board answer")
	if err != nil {
		t.Fatalf("AskBoard: %v", err)
	}
	if spy.calls != 1 {
		t.Fatalf("the invoke was called %d times, want 1", spy.calls)
	}
	if spy.ask.calleeAgent != "target-agent" || spy.ask.question != "what did the board answer" {
		t.Fatalf("the invoke was called with the wrong ask: %+v", spy.ask)
	}
	// The structural half: boardAsk has no skill field, so the board cannot
	// name a skill other than the answer skill the invoke seam supplies.
	if !boardAskHasNoSkillField() {
		t.Fatal("boardAsk carries a skill field — a skill other than answer would be reachable (scope 2)")
	}
}

// TestAnswerFiledAsRelayedLearning — a completed task files exactly one
// relayed learning, scoped to the project (M6) with provenance relayed (M1),
// the body the task's result text. The gate the write-protection (M1) depends
// on: the only caller permitted that provenance is the board's fileRelayed, and
// it files one row, with the right fields.
func TestAnswerFiledAsRelayedLearning(t *testing.T) {
	spy := &spyInvoke{task: boardTask{
		ID:          7,
		Status:      boardTaskCompleted,
		ResultText:  "the answer the board received",
		CallerAgent: "asker",
	}}
	rec := &recordingRelayed{}
	b := newBoardWithFakes(spy, rec, "/project/root")

	_, err := b.AskBoard(context.Background(), "asker", "target-agent", "the question")
	if err != nil {
		t.Fatalf("AskBoard: %v", err)
	}
	if rec.calls != 1 {
		t.Fatalf("the relayed write was called %d times, want exactly 1", rec.calls)
	}
	in := rec.in
	if in.Scope != learnings.ScopeProject {
		t.Errorf("the filed learning is scope %q, want project (M6)", in.Scope)
	}
	if in.Provenance != learnings.ProvenanceRelayed {
		t.Errorf("the filed learning is provenance %q, want relayed (M1)", in.Provenance)
	}
	if in.Body != "the answer the board received" {
		t.Errorf("the filed learning's body is %q, want the task's result text", in.Body)
	}
	if in.ScopeKey != "/project/root" {
		t.Errorf("the filed learning's scope_key is %q, want the asking loop's project_root (M6)", in.ScopeKey)
	}
}

// A call against an agent with no signed card (T6 not yet run, or a made-up
// name) surfaces the invoke's own refusal verbatim — never a second, reworded
// message. A reworded refusal is a second truth (DB19); the board carries the
// invoke's words through.
func TestBoardSurfacesTheInvokesOwnRefusalVerbatim(t *testing.T) {
	spy := &spyInvoke{err: errors.New("agent \"target-agent\" has no signed card, so no skill of it can be asked")}
	b := newBoardWithFakes(spy, &recordingRelayed{}, "/root")

	_, err := b.AskBoard(context.Background(), "asker", "target-agent", "the question")
	if err == nil {
		t.Fatal("a call against an agent with no card was accepted — the invoke's refusal must surface")
	}
	if err.Error() != "agent \"target-agent\" has no signed card, so no skill of it can be asked" {
		t.Fatalf("the refusal was reworded, want the invoke's own words verbatim: %v", err)
	}
}

// A waiting (not-yet-completed) task files nothing: the answer arrives later, on
// resume, and this call has already returned (P2, AS4 — suspension is exiting,
// not waiting). The relayed write is not called for a waiting task.
func TestWaitingTaskFilesNothing(t *testing.T) {
	spy := &spyInvoke{task: boardTask{Status: "waiting", CallerAgent: "asker"}}
	rec := &recordingRelayed{}
	b := newBoardWithFakes(spy, rec, "/root")

	_, err := b.AskBoard(context.Background(), "asker", "target-agent", "the question")
	if err != nil {
		t.Fatalf("AskBoard: %v", err)
	}
	if rec.calls != 0 {
		t.Fatalf("a waiting task filed %d learnings, want 0 — the answer arrives on resume, not here", rec.calls)
	}
}

// contains reports whether s holds sub, for the message checks.
func contains(s, sub string) bool {
	return len(sub) > 0 && stringsContain(s, sub)
}

func stringsContain(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// boardAskHasNoSkillField is the structural assertion of scope 2: the board's
// ask has no field that could name a skill, so a skill other than the answer
// skill is unreachable by construction, not by convention. It reflects the
// boardAsk type's fields rather than trusting a reviewer's eye.
func boardAskHasNoSkillField() bool {
	typ := reflect.TypeOf(boardAsk{})
	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i).Name; f == "Skill" || f == "SkillID" || f == "skill" {
			return false
		}
	}
	return true
}
