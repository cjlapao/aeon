package runtime

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// This is S6's keystone. If it fails, a resumed step differs from continuation
// in a way ordinary one-process tests cannot localise. The resumer below sees
// only JSON bytes, never the original Go object graph.
func reconstructedResume(t *testing.T, serialized []byte) ResumeRequest {
	t.Helper()
	var req ResumeRequest
	if err := json.Unmarshal(serialized, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func identitySuspension(t *testing.T) ResumeRequest {
	t.Helper()
	req, _ := suspendedFixture(t)
	req.Answer = "yes"
	return req
}

func baselineFromAnswered(req ResumeRequest, client Streamer) StepResult {
	manifest := req.Manifest
	messages := append([]model.Message(nil), req.Messages...)
	messages = append(messages, model.Message{Role: model.RoleUser, Content: []model.Block{model.ToolResultBlock(req.Question.ToolUseID, req.Answer, false)}})
	state := &LoopState{Request: req.Request, Messages: messages, Manifest: &manifest, Usage: req.Usage, Cost: req.Cost,
		Compactions: append([]CompactionRecord(nil), req.Compactions...)}
	return RunLoop(context.Background(), state, LoopDeps{Client: client, Tools: newSpyTools(), HandoffSpec: DefaultHandoffSpec()})
}

func assertIdentity(t *testing.T, a, b StepResult) {
	t.Helper()
	// Excluded field: Events contain wall-clock timestamps. Messages, Outcome,
	// Handoff, Manifest and accounting are compared. time.Time's monotonic
	// component cannot survive JSON, so DeadlineAt is compared as an instant.
	if !reflect.DeepEqual(a.Messages, b.Messages) {
		t.Errorf("transcripts differ\nA=%+v\nB=%+v", a.Messages, b.Messages)
	}
	if a.Outcome != b.Outcome {
		t.Errorf("outcomes %s != %s", a.Outcome, b.Outcome)
	}
	if !reflect.DeepEqual(a.Handoff, b.Handoff) {
		t.Errorf("handoffs %+v != %+v", a.Handoff, b.Handoff)
	}
	leftManifest, rightManifest := a.Manifest, b.Manifest
	// S76 (2026-11-07): a resume from a human answer RESTARTS the working window
	// (waiting on a person is not working time), so the two paths' deadlines now
	// differ by design; TestAnAnsweredStepGetsItsWorkingWindowAgain pins the
	// restart itself. Everything else about the manifest still has to match.
	leftManifest.DeadlineAt = rightManifest.DeadlineAt
	if !reflect.DeepEqual(leftManifest, rightManifest) {
		t.Errorf("manifests %+v != %+v", a.Manifest, b.Manifest)
	}
	if !reflect.DeepEqual(a.Usage, b.Usage) || !reflect.DeepEqual(a.Cost, b.Cost) {
		t.Errorf("accounting A=%+v/%+v B=%+v/%+v", a.Usage, a.Cost, b.Usage, b.Cost)
	}
	if !reflect.DeepEqual(a.Compactions, b.Compactions) {
		t.Errorf("compaction records A=%+v B=%+v", a.Compactions, b.Compactions)
	}
}

func TestSuspendedAndResumedStepMatchesUninterruptedContinuation(t *testing.T) {
	original := identitySuspension(t)
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	copy := reconstructedResume(t, raw)
	if &copy.Messages[0] == &original.Messages[0] || &copy.Messages[0].Content[0] == &original.Messages[0].Content[0] {
		t.Fatal("JSON reconstruction shares transcript references")
	}
	baseline := baselineFromAnswered(original, newFakeClient(scriptedTurn{Text: okHandoff}))
	resumed := ResumeStep(context.Background(), copy, RunOpts{Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools(), HandoffSpec: DefaultHandoffSpec()})
	assertIdentity(t, baseline, resumed)
}

func TestIdentityHoldsWhenBothContinuationsCompact(t *testing.T) {
	original := identitySuspension(t)
	// A long completed exchange precedes the pending question. Both paths get
	// the same persisted snapshot and must buy the same summary on continuation.
	last := len(original.Messages) - 2
	prior := []model.Message{
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock(strings.Repeat("history ", 300))}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("ack")}},
	}
	original.Messages = append(append(append([]model.Message{}, original.Messages[:last]...), prior...), original.Messages[last:]...)
	original.Request.Route.ContextWindow = 400
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	copy := reconstructedResume(t, raw)
	turns := func() *fakeClient {
		return newFakeClient(scriptedTurn{Text: "history summarized"}, scriptedTurn{Text: okHandoff})
	}
	baseline := baselineFromAnswered(original, turns())
	resumed := ResumeStep(context.Background(), copy, RunOpts{Client: turns(), Tools: newSpyTools(), HandoffSpec: DefaultHandoffSpec()})
	if len(baseline.Compactions) != 1 || len(resumed.Compactions) != 1 {
		t.Fatalf("compactions A=%d B=%d", len(baseline.Compactions), len(resumed.Compactions))
	}
	assertIdentity(t, baseline, resumed)
}

func TestIdentityAcrossTwoSuspensionsKeepsOriginalBound(t *testing.T) {
	first := identitySuspension(t)
	first.Manifest.MaxIterations = 2
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	second := ResumeStep(context.Background(), reconstructedResume(t, raw), RunOpts{Client: newFakeClient(scriptedTurn{ToolUses: []model.Block{toolUse("q2", "question", `{"text":"Again?"}`)}}), Tools: newSpyTools()})
	if second.Kind != ResultSuspended || second.Manifest.IterationsUsed != 2 {
		t.Fatalf("second suspension: %+v", second)
	}
	secondRequest := ResumeRequest{Request: first.Request, Messages: second.Messages, Manifest: second.Manifest, Question: *second.Question, Answer: "yes", Usage: second.Usage, Cost: second.Cost}
	serialized, err := json.Marshal(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	copy := reconstructedResume(t, serialized)
	baseline := baselineFromAnswered(secondRequest, newFakeClient(scriptedTurn{Text: okHandoff}))
	resumed := ResumeStep(context.Background(), copy, RunOpts{Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools()})
	if resumed.Outcome != OutcomeBlocked || resumed.Manifest.IterationsUsed != 2 {
		t.Fatalf("bound reset: %+v", resumed)
	}
	assertIdentity(t, baseline, resumed)
}
