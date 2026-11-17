package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestRecoveryCommitIncludesStatusTranscriptAndReason(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "recover", t.TempDir())
	st := seedStep(t, s, sc)
	msgs := []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}}}
	if err := s.SaveRecoveryObservation(context.Background(), st.ID, "B", "model call re-issued", true); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitRecoveredStep(context.Background(), st.ID, msgs, json.RawMessage(`{"iterationsUsed":2}`), model.Cost{State: model.CostUnknown, Reason: model.ReasonStreamAbortedEarly}, "failed", "B", "model call re-issued and failed", "", true, nil, &RecoveryFailure{Kind: "recovery-failed", Detail: "model call re-issued and failed"}); err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Status != "done" || read.Outcome != "failed" {
		t.Fatalf("step=%+v", read)
	}
	transcript := s.ReadTranscript(st.ID)
	if !transcript.OK || len(transcript.Messages) != 1 {
		t.Fatalf("transcript=%+v", transcript)
	}
	record, ok, err := s.ReadRecovery(st.ID)
	if err != nil || !ok || !record.Reissued || record.Reason != "model call re-issued and failed" || record.Failure == nil || record.Failure.Kind != "recovery-failed" {
		t.Fatalf("record=%+v ok=%v err=%v", record, ok, err)
	}
	if err := s.CommitRecoveredStep(context.Background(), st.ID, msgs, json.RawMessage(`{}`), model.Cost{}, "ok", "A", "", "", false, nil, nil); err == nil {
		t.Fatal("a completed step was recovered twice")
	}
	read, _ = s.ReadStep(st.ID)
	if read.Outcome != "failed" {
		t.Fatalf("second commit changed outcome: %+v", read)
	}
}

func TestRecoverySuspensionCommitsQuestionWithStatus(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "recover-q", t.TempDir())
	st := seedStep(t, s, sc)
	msgs := []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
		{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{{Kind: model.BlockToolUse, ID: "q1", Name: "question", Input: json.RawMessage(`{"text":"Continue?"}`)}}}}
	q := &RecoveryQuestion{ToolUseID: "q1", Kind: QuestionFree, Body: "Continue?"}
	if err := s.CommitRecoveredStep(context.Background(), st.ID, msgs, json.RawMessage(`{}`), model.Cost{State: model.CostUnknown}, "", "B", "model call re-issued", "", true, q, nil); err != nil {
		t.Fatal(err)
	}
	step, _ := s.ReadStep(st.ID)
	pending, ok, err := s.PendingStepQuestion(st.ID)
	if err != nil || !ok || step.Status != "suspended" || pending.ToolUseID != "q1" {
		t.Fatalf("step=%+v question=%+v ok=%v err=%v", step, pending, ok, err)
	}
}
