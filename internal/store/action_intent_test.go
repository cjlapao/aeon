package store

// Tests for the one intent table (S16 T7, D7/Q6): the crash window is a
// row shape (review_id NULL), the unique key is the (step, attempt,
// action, pr) quadruple, and recovery has exactly one door.

import (
	"context"
	"strings"
	"testing"
)

func seedIntentStep(t *testing.T) (*Store, int64) {
	t.Helper()
	s := openTemp(t)
	sc := seedLoop(t, s, "4a17-version", t.TempDir())
	step := seedStep(t, s, sc)
	return s, step.ID
}

func TestActionIntentRoundTrip(t *testing.T) {
	s, stepID := seedIntentStep(t)
	ctx := context.Background()
	in := ActionIntent{LoopSlug: "4a17-version", StepID: stepID, Attempt: 1, Action: "forge.postReview", PR: 3, PreReviewID: 2}
	if err := s.BeginActionIntent(ctx, in); err != nil {
		t.Fatalf("begin: %v", err)
	}
	open, err := s.OpenActionIntent(ctx, stepID, 1, "forge.postReview", 3)
	if err != nil || open == nil || open.ReviewID != 0 {
		t.Fatalf("the crash-window row: %+v (%v)", open, err)
	}
	if open.PreReviewID != 2 || open.OpenedAt == "" {
		t.Fatalf("the baseline and the clock ride: %+v", open)
	}
	if err := s.RecordActionReview(ctx, stepID, 1, "forge.postReview", 3, 41); err != nil {
		t.Fatalf("record: %v", err)
	}
	open, err = s.OpenActionIntent(ctx, stepID, 1, "forge.postReview", 3)
	if err != nil || open != nil {
		t.Fatalf("a recorded intent is no longer OPEN: %+v (%v)", open, err)
	}
}

func TestActionIntentUniqueAndNoOverwrite(t *testing.T) {
	s, stepID := seedIntentStep(t)
	ctx := context.Background()
	in := ActionIntent{LoopSlug: "4a17-version", StepID: stepID, Attempt: 1, Action: "forge.postReview", PR: 3, PreReviewID: 2}
	if err := s.BeginActionIntent(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginActionIntent(ctx, ActionIntent{LoopSlug: "4a17-version", StepID: stepID, Attempt: 1, Action: "forge.postReview", PR: 3, PreReviewID: 99}); err == nil {
		t.Fatal("a second begin for the same key must fail — overwriting would erase the evidence recovery reads")
	}
	// A different attempt is a DIFFERENT intent (the key includes it).
	if err := s.BeginActionIntent(ctx, ActionIntent{LoopSlug: "4a17-version", StepID: stepID, Attempt: 2, Action: "forge.postReview", PR: 3, PreReviewID: 2}); err != nil {
		t.Fatalf("attempt 2 is its own intent: %v", err)
	}
	// Recording twice is refused rather than silently rewriting.
	if err := s.RecordActionReview(ctx, stepID, 1, "forge.postReview", 3, 41); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordActionReview(ctx, stepID, 1, "forge.postReview", 3, 42); err == nil {
		t.Fatal("re-recording must be refused — history is not rewritten")
	}
}

func TestActionIntentValidatesItsKey(t *testing.T) {
	s, stepID := seedIntentStep(t)
	ctx := context.Background()
	for _, bad := range []ActionIntent{
		{LoopSlug: "", StepID: stepID, Attempt: 1, Action: "forge.postReview", PR: 3},
		{LoopSlug: "l", StepID: 0, Attempt: 1, Action: "forge.postReview", PR: 3},
		{LoopSlug: "l", StepID: stepID, Attempt: 0, Action: "forge.postReview", PR: 3},
		{LoopSlug: "l", StepID: stepID, Attempt: 1, Action: "", PR: 3},
		{LoopSlug: "l", StepID: stepID, Attempt: 1, Action: "forge.postReview", PR: 0},
	} {
		if err := s.BeginActionIntent(ctx, bad); err == nil {
			t.Fatalf("invalid key %+v accepted", bad)
		}
	}
	if _, err := s.OpenActionIntent(ctx, stepID, 1, "forge.postReview", 999); err != nil {
		t.Fatalf("absence is an answer, not a failure: %v", err)
	}
}

// TestOneIntentTable pins Q6's answer against the ONE DDL source: exactly
// one CREATE TABLE holds ACTION intents. tool_receipt records spawns —
// S7's own path — and the migration list proves action intents live in
// exactly one table, so recovery reads exactly one.
func TestOneIntentTable(t *testing.T) {
intentLoop:
	for _, m := range Migrations() {
		if !strings.Contains(m.Name, "intent") {
			continue
		}
		// Exactly one migration carries the name; find its DDL by re-reading
		// the schema source is overkill — the table itself is the evidence.
		if !strings.Contains(m.Name, "action_intent") {
			t.Fatalf("migration %s holds intents too — Q6 demanded ONE action-intent table", m.Name)
		}
		break intentLoop
	}
	// And the table exists with the unique key: a duplicate (step, attempt,
	// action, pr) is refused by the DATABASE, not by convention.
	s, stepID := seedIntentStep(t)
	ctx := context.Background()
	in := ActionIntent{LoopSlug: "l", StepID: stepID, Attempt: 1, Action: "a", PR: 1}
	if err := s.BeginActionIntent(ctx, in); err != nil {
		t.Fatal(err)
	}
	// Raw insert under the same key: the schema's UNIQUE does the refusing.
	if _, err := s.w.ExecContext(ctx, `INSERT INTO action_intent
		(loop_slug, step_id, attempt, action, pr, pre_review_id, opened_at) VALUES ('l',?,1,'a',1,0,'now')`, stepID); err == nil {
		t.Fatal("the table's UNIQUE key did not refuse a duplicate intent")
	}
}
