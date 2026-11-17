package store

import (
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// assistantMsg/userMsg are steers_test.go's own helpers, reused here rather
// than redeclared.

// TestReadLoopTranscriptPageSpansSteps proves the one thing step.show cannot:
// a window across TWO steps' messages, stitched by step_message's own global
// id — not by anything either step owns.
func TestReadLoopTranscriptPageSpansSteps(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "refactor", "/repo-a")
	step1 := seedStep(t, s, sc)
	step2 := seedStep(t, s, sc)

	if err := s.AppendMessages(step1.ID, []model.Message{userMsg("first step, turn one"), assistantMsg("first step, turn two")}); err != nil {
		t.Fatalf("append to step1: %v", err)
	}
	if err := s.AppendMessages(step2.ID, []model.Message{userMsg("second step, turn one"), assistantMsg("second step, turn two")}); err != nil {
		t.Fatalf("append to step2: %v", err)
	}

	// Bootstrap (no cursor): the LATEST page — step2's two messages, oldest
	// first within the page, exactly the order the live stream already uses.
	latest, err := s.ReadLoopTranscriptPage(sc, nil, nil, 2)
	if err != nil {
		t.Fatalf("read latest page: %v", err)
	}
	if len(latest.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(latest.Messages))
	}
	if latest.Messages[0].StepID != step2.ID || latest.Messages[1].StepID != step2.ID {
		t.Fatalf("the latest page should be step2's turns, got step ids %d, %d", latest.Messages[0].StepID, latest.Messages[1].StepID)
	}
	if latest.Messages[0].ID >= latest.Messages[1].ID {
		t.Fatalf("a page must be ascending by id, got %d then %d", latest.Messages[0].ID, latest.Messages[1].ID)
	}
	if !latest.HasOlder {
		t.Error("step1's turns exist before this page — HasOlder should be true")
	}
	if latest.HasNewer {
		t.Error("nothing exists after the latest page — HasNewer should be false")
	}

	// Scrolling up: `before` the oldest id on screen finds step1's turns —
	// a DIFFERENT step than the live/bootstrap page, stitched by nothing but
	// the message id.
	older, err := s.ReadLoopTranscriptPage(sc, &latest.Messages[0].ID, nil, 2)
	if err != nil {
		t.Fatalf("read older page: %v", err)
	}
	if len(older.Messages) != 2 {
		t.Fatalf("got %d older messages, want 2", len(older.Messages))
	}
	if older.Messages[0].StepID != step1.ID || older.Messages[1].StepID != step1.ID {
		t.Fatalf("the older page should be step1's turns, got step ids %d, %d", older.Messages[0].StepID, older.Messages[1].StepID)
	}
	if older.HasOlder {
		t.Error("nothing precedes step1's first turn — HasOlder should be false")
	}
	if !older.HasNewer {
		t.Error("step2's turns exist after this page — HasNewer should be true")
	}

	// Scrolling back down from there with `after` must reproduce the latest
	// page exactly — the cursor is reversible, never lossy.
	backDown, err := s.ReadLoopTranscriptPage(sc, nil, &older.Messages[len(older.Messages)-1].ID, 2)
	if err != nil {
		t.Fatalf("read newer page: %v", err)
	}
	if len(backDown.Messages) != 2 || backDown.Messages[0].ID != latest.Messages[0].ID || backDown.Messages[1].ID != latest.Messages[1].ID {
		t.Fatalf("paging down from the older page should reproduce the latest page exactly; got %+v, want %+v", backDown.Messages, latest.Messages)
	}
}

// TestReadLoopTranscriptPageNamesAnUnreadableRowWithoutLosingTheRest proves
// O4's rule applied to a reader that is not a replay: a row from a future
// Aeon names itself rather than failing the window around it.
func TestReadLoopTranscriptPageNamesAnUnreadableRowWithoutLosingTheRest(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "refactor", "/repo-a")
	step := seedStep(t, s, sc)

	if err := s.AppendMessages(step.ID, []model.Message{userMsg("readable turn"), assistantMsg("also readable")}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := s.w.Exec(`UPDATE step_message SET format_version = ? WHERE step_id = ? AND seq = 1`, model.FormatVersion+1, step.ID); err != nil {
		t.Fatalf("mark a row as written by a newer Aeon: %v", err)
	}

	page, err := s.ReadLoopTranscriptPage(sc, nil, nil, 10)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("got %d messages, want 2 — the unreadable row must still be IN the page", len(page.Messages))
	}
	if page.Messages[0].Unreadable == "" {
		t.Error("the future-format row should name itself unreadable")
	}
	if page.Messages[0].Content != nil {
		t.Error("an unreadable row's content must not be guessed at")
	}
	if page.Messages[1].Unreadable != "" {
		t.Errorf("the second, readable row must not be marked unreadable too: %q", page.Messages[1].Unreadable)
	}
	if page.Messages[1].Content == nil {
		t.Error("the second, readable row should still carry its content")
	}
}
