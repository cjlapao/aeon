package store

import (
	"strings"
	"testing"
)

// s68_skills_test.go — the CHOSEN-skills receipts (S68 B6/B7) as durable
// facts: the two columns are each store a list of names beside the human's
// own words, and the third state (NULL = nothing chosen) is what keeps every
// pre-S68 row reading exactly as it did.

func TestChosenSkillsRoundTripThroughTheThread(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "chat", "/r")

	// A turn that chose nothing: the column is NULL and reads back nil, not
	// an empty slice standing in for a choice (P31's third state).
	plain, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "no skills here"})
	if err != nil {
		t.Fatalf("note: %v", err)
	}
	if plain.Skills != nil {
		t.Fatalf("a turn that chose nothing reads back %v, want nil", plain.Skills)
	}

	// A turn that chose skills: the NAMES ride beside the body, in order,
	// and survive a re-read (the durable receipt the restart reloads from).
	chose := []string{"caveman", "verify-before-done", "caveman"}
	if _, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "with skills", Skills: chose}); err != nil {
		t.Fatalf("note with skills: %v", err)
	}
	rows, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("thread has %d rows, want 2", len(rows))
	}
	if rows[0].Skills != nil {
		t.Errorf("the first row carries %v, want nil", rows[0].Skills)
	}
	// The store keeps the NAMES as given — dedup is the door's, not the
	// column's; the receipt says what was filed.
	if strings.Join(rows[1].Skills, ",") != "caveman,verify-before-done,caveman" {
		t.Errorf("the second row carries %v, want the names as filed", rows[1].Skills)
	}
	// ReadChatMessage (the restart door) agrees with Thread.
	one, err := s.ReadChatMessage(rows[1].ID)
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	if strings.Join(one.Skills, ",") != "caveman,verify-before-done,caveman" {
		t.Errorf("ReadChatMessage carried %v, want the names as filed", one.Skills)
	}
}

func TestChosenSkillsRoundTripThroughTheSteerRow(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "chat", "/r")
	st := seedStep(t, s, sc)

	// A steer with no chosen skills: NULL, reading back nil.
	plain, err := s.QueueSteer(st.ID, NewSteer{Text: "just words"})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if plain.Skills != nil {
		t.Fatalf("a steer with nothing chosen reads %v, want nil", plain.Skills)
	}

	// A steer that chose skills: text stayed the operator's PURE words and
	// the names filed beside it (B7's whole argument — the bubble reads the
	// text alone, the drain renders the lines from these names).
	rode, err := s.QueueSteer(st.ID, NewSteer{Text: "Also mention Go.", Skills: []string{"caveman"}})
	if err != nil {
		t.Fatalf("queue with skills: %v", err)
	}
	if rode.Text != "Also mention Go." {
		t.Fatalf("the row's text was not kept pure: %q", rode.Text)
	}
	if len(rode.Skills) != 1 || rode.Skills[0] != "caveman" {
		t.Fatalf("the row carries %v, want [caveman]", rode.Skills)
	}
	// The drain reads through PendingSteers — the same names must come back.
	pending, err := s.PendingSteers(st.ID)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 2 || strings.Join(pending[1].Skills, ",") != "caveman" {
		t.Fatalf("PendingSteers lost the receipt: %+v", pending)
	}
}

func TestACorruptSkillsReceiptIsAnErrorNotAnEmptyChoice(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "chat", "/r")

	// Thread row: a receipt that cannot be read must SAY so, never let the
	// operator's chosen skills vanish into a silent empty list.
	if _, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "corrupt", Skills: []string{"x"}}); err != nil {
		t.Fatalf("note: %v", err)
	}
	if err := s.GormDB().Exec(`UPDATE chat_message SET skills = 'not-json'`).Error; err != nil {
		t.Fatalf("corrupt the receipt: %v", err)
	}
	if _, err := s.Thread(sc, 0); err == nil || !strings.Contains(err.Error(), "chosen-skills receipt") {
		t.Fatalf("a corrupt chat receipt must error naming corruption, got %v", err)
	}

	s2 := openTemp(t)
	sc2 := seedLoop(t, s2, "chat", "/r")
	st := seedStep(t, s2, sc2)
	if _, err := s2.QueueSteer(st.ID, NewSteer{Text: "x", Skills: []string{"y"}}); err != nil {
		t.Fatalf("queue: %v", err)
	}
	if err := s2.GormDB().Exec(`UPDATE step_steer SET skills = '{'`).Error; err != nil {
		t.Fatalf("corrupt the steer receipt: %v", err)
	}
	if _, err := s2.ReadSteer(st.ID); err == nil {
		t.Fatal("a corrupt steer receipt returned no error")
	} else if !strings.Contains(err.Error(), "chosen-skills receipt") {
		t.Fatalf("a corrupt steer receipt must name corruption, got %v", err)
	}
}
