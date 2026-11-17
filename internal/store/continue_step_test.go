package store

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S31 T2: `step.continue {message}` (D6) rests on ContinueStepWithMessages —
// the flip to running and the transcript replacement in ONE transaction.
// These tests are the exactly-once argument, not decoration: a split pair of
// writes admits two distinct wrong histories (message lost on crash, or
// message doubled on retry), and each has its own test here.

func userText(text string) model.Message {
	return model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock(text)}}
}

func assistantText(text string) model.Message {
	return model.Message{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{model.TextBlock(text)}}
}

func TestContinueStepWithMessagesFlipsAndReplacesTogether(t *testing.T) {
	s := openAt(t, t.TempDir())
	sc := seedLoop(t, s, "cont", t.TempDir())
	st := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, []model.Message{userText("survey the repo"), assistantText("before the stop")}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepStatus(st.ID, StepSuspended, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepPauseReason(st.ID, "user"); err != nil {
		t.Fatal(err)
	}

	msgs := []model.Message{userText("survey the repo"), assistantText("before the stop"), userText("also mention the Go version")}
	if err := s.ContinueStepWithMessages(context.Background(), st.ID, msgs); err != nil {
		t.Fatal(err)
	}

	row, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != string(StepRunning) {
		t.Fatalf("status = %q, want running", row.Status)
	}
	if row.PauseReason != "" {
		t.Fatalf("pause_reason = %q, want cleared — a running step never carries a stale pause", row.PauseReason)
	}
	tr := s.ReadTranscript(st.ID)
	if !tr.OK || tr.Count != 3 {
		t.Fatalf("transcript = ok=%v count=%d, want 3 rows", tr.OK, tr.Count)
	}
	if len(tr.Messages) != 3 || tr.Messages[2].Role != model.RoleUser ||
		len(tr.Messages[2].Content) != 1 || tr.Messages[2].Content[0].Text != "also mention the Go version" {
		t.Fatalf("the message is not the second row verbatim: %+v", tr.Messages[1])
	}

	// A RETRIED continue — the duplicate-the-message history — is refused by
	// the same guard that made the flip atomic, and the transcript stands.
	if err := s.ContinueStepWithMessages(context.Background(), st.ID, append(msgs, userText("again?"))); err == nil {
		t.Fatal("a continue on a running step was accepted")
	} else if !strings.Contains(err.Error(), "no longer suspended") {
		t.Fatalf("refusal does not name the condition: %v", err)
	}
	if tr := s.ReadTranscript(st.ID); tr.Count != 3 {
		t.Fatalf("the refused continue changed the transcript to %d rows", tr.Count)
	}
}

func TestContinueStepWithMessagesRefusesNonSuspendedStates(t *testing.T) {
	s := openAt(t, t.TempDir())
	sc := seedLoop(t, s, "cont2", t.TempDir())
	st := seedStep(t, s, sc) // fresh steps start running
	if err := s.AppendMessages(st.ID, []model.Message{userText("start"), assistantText("mid-flight")}); err != nil {
		t.Fatal(err)
	}
	if err := s.ContinueStepWithMessages(context.Background(), st.ID, []model.Message{userText("start"), assistantText("mid-flight"), userText("sneak in")}); err == nil {
		t.Fatal("continuing a RUNNING step was accepted — the guard is the race net")
	}
	if tr := s.ReadTranscript(st.ID); tr.Count != 2 {
		t.Fatalf("the refused write touched the transcript (%d rows)", tr.Count)
	}
	if err := s.SetStepStatus(st.ID, StepDone, "ok"); err != nil {
		t.Fatal(err)
	}
	if err := s.ContinueStepWithMessages(context.Background(), st.ID, []model.Message{userText("after done")}); err == nil {
		t.Fatal("continuing a DONE step was accepted")
	}
}

func TestContinueStepWithMessagesKeepsStateOnRejectedTranscript(t *testing.T) {
	s := openAt(t, t.TempDir())
	sc := seedLoop(t, s, "cont3", t.TempDir())
	st := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, []model.Message{userText("open"), assistantText("kept")}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepStatus(st.ID, StepSuspended, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepPauseReason(st.ID, "user"); err != nil {
		t.Fatal(err)
	}
	// The validator's own refusal (empty transcript) must leave BOTH halves
	// of the pair untouched — the step is still suspended, still resumable.
	if err := s.ContinueStepWithMessages(context.Background(), st.ID, nil); err == nil {
		t.Fatal("an empty continuation transcript was accepted")
	}
	row, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != string(StepSuspended) || row.PauseReason != "user" {
		t.Fatalf("a rejected continuation moved the step: status=%q pause=%q", row.Status, row.PauseReason)
	}
	if tr := s.ReadTranscript(st.ID); tr.Count != 2 {
		t.Fatalf("a rejected continuation replaced the transcript (%d rows)", tr.Count)
	}
}
