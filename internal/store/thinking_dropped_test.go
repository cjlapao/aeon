package store

import (
	"errors"
	"testing"
)

// S18 T5 — the step's thinking-drop count: the omission that is not in the
// transcript (M1 keeps every block) lives HERE, as a number on the step
// (M4, O1). Written once by the ONE writer; read back by every step reader.

func TestThinkingDropCountRoundTrips(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "drops", "/repo")
	st := seedStep(t, s, sc)

	if err := s.SetStepThinkingDropped(st.ID, 3); err != nil {
		t.Fatal(err)
	}
	back, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ThinkingDropped != 3 {
		t.Errorf("ReadStep says %d, want 3 — the count that reaches the bench IS this column", back.ThinkingDropped)
	}

	// ListSteps goes through the same scanner; a drift there would make the
	// thread and the detail view disagree about the same step.
	list, err := s.ListSteps(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ThinkingDropped != 3 {
		t.Errorf("ListSteps says %+v, want the one step at 3", list)
	}
}

func TestAnUnrecordedStepReadsAsNoDrops(t *testing.T) {
	// NULL is every step predating T5 and every step that dropped nothing:
	// it reads as 0, which is honest here because the number counts an
	// event that either happened or did not (unlike cost's third state —
	// this one has no unmeasured state, the adapter always knows).
	s := openTemp(t)
	sc := seedLoop(t, s, "quiet", "/repo")
	st := seedStep(t, s, sc)

	back, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ThinkingDropped != 0 {
		t.Errorf("a never-written step reads %d, want 0", back.ThinkingDropped)
	}
}

func TestTheDropCountRefusesANegativeAndAMissingStep(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "refuse", "/repo")
	st := seedStep(t, s, sc)

	if err := s.SetStepThinkingDropped(st.ID, -1); err == nil {
		t.Error("a negative drop count was accepted; it is not a number, it is a bug")
	}
	if err := s.SetStepThinkingDropped(st.ID+1000, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown step: %v, want ErrNotFound", err)
	}
	// A refused write wrote nothing.
	back, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ThinkingDropped != 0 {
		t.Errorf("the refused negative landed: %d", back.ThinkingDropped)
	}
}

func TestTheDropCountSurvivesAReopen(t *testing.T) {
	// The record is the point: reopen and the number is still there (the
	// same proof cost gets; O1 says drops deserve it too).
	dir := t.TempDir()
	s := openAt(t, dir)
	sc := seedLoop(t, s, "persist", "/repo")
	st := seedStep(t, s, sc)
	if err := s.SetStepThinkingDropped(st.ID, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openAt(t, dir)
	back, err := s2.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ThinkingDropped != 7 {
		t.Errorf("after reopen: %d, want 7", back.ThinkingDropped)
	}
}
