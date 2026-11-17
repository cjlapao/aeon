package history

import (
	"reflect"
	"testing"
)

// TestVocabularyEqualsTheConstants is HV2's derivation pin: the served view is
// the package's own constants, read at call time — not a restatement that can
// rot. The proof moves a constant at the seam (mutate the slice, restore with
// defer): if the answer moved with it, the vocabulary IS the constants; if it
// were a cached literal, this test would fail standing still.
func TestVocabularyEqualsTheConstants(t *testing.T) {
	v := Vocabulary()
	if !reflect.DeepEqual(v.Lanes, []string{string(ByLoop), string(ByPhase), string(ByTask), string(ByAgent)}) {
		t.Errorf("lanes = %v, want the four GroupBy constants", v.Lanes)
	}
	if !reflect.DeepEqual(v.Outcomes, []string{OutcomeRunning, OutcomeWaiting, OutcomeBlocked, OutcomeDone, OutcomeIdle, OutcomeAborted, OutcomeFailed}) {
		t.Errorf("outcomes = %v, want the statuses the engine writes", v.Outcomes)
	}
	if !reflect.DeepEqual(v.Filters, []string{"projectRoot", "componentRoot", "groupBy", "since"}) {
		t.Errorf("filters = %v, want the params the reads take", v.Filters)
	}

	// Move the seam, not the answer: a lane word that changes in the package
	// must change in the vocabulary with no edit to vocabulary.go.
	original := LaneWords[0]
	LaneWords[0] = "moved"
	defer func() { LaneWords[0] = original }()
	moved := Vocabulary()
	if moved.Lanes[0] != "moved" {
		t.Errorf("the vocabulary did not move with its constant (%q) — it is a restatement, not a derivation", moved.Lanes[0])
	}
}

// TestVocabularyAnswersAreFreshCopies: the caller cannot corrupt the package's
// slices through an answer — a surface that sorted its own rows in place must
// not rewrite what every later reader is served.
func TestVocabularyAnswersAreFreshCopies(t *testing.T) {
	v := Vocabulary()
	v.Outcomes[0] = "corrupted"
	if OutcomeWords[0] == "corrupted" {
		t.Fatal("a served answer shares the package's slice — the package's own constants are one sort away from a client's convenience")
	}
	if Vocabulary().Outcomes[0] == "corrupted" {
		t.Error("the corruption survived the call — the view is cached, not derived")
	}
}
