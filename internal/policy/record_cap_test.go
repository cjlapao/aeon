package policy

// In-package (the only way to reach recordEvent directly — which is the
// point): the DEF-029 plant. The reviewer's question was what happens when
// an in-package handover forgets truncateRunes: before T10 the answer was
// "an over-cap row is written, and only the READER notices". Now recordEvent
// re-checks its own bound — the cap is a property of every row, enforced at
// the single door, whoever called.

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRecordEventRefusesOverCapDetailItself(t *testing.T) {
	var inserts int
	var lastDetail string
	s := NewStore(RepoFuncs{
		EventInsert: func(_ context.Context, _, _, _, detail string, _ time.Time) error {
			inserts++
			lastDetail = detail
			return nil
		},
	})
	at := time.Date(2026, 10, 19, 11, 59, 59, 111000000, time.UTC)

	// The plant: 950 runes handed straight to the door, bypassing
	// RecordRefusal's truncateRunes — the exact shape of a sibling handover
	// (T10's, or any future one) that forgot the cap.
	rej := s.recordEvent(context.Background(), eventKindRefusal, "agent:boss", "no-bash",
		strings.Repeat("a", 950), at)
	if rej == nil {
		t.Fatalf("recordEvent ACCEPTED a %d-rune detail — the cap was only ever enforced by one caller; "+
			"the sole writer must enforce its own bound (DEF-029)", 950)
	}
	if !strings.Contains(rej.Reason, "over the named cap") || !strings.Contains(rej.Reason, "256") {
		t.Errorf("the refusal does not name the bound it enforced: %s", rej.Reason)
	}
	if inserts != 0 {
		t.Errorf("the over-cap detail reached the seam (%d inserts); the row would have lied about its own rules", inserts)
	}

	// The boundary itself: exactly the cap passes (n and n-1, rune-counted).
	for _, n := range []int{MaxEventDetailRunes, MaxEventDetailRunes - 1} {
		if r := s.recordEvent(context.Background(), eventKindRefusal, "agent:boss", "no-bash",
			strings.Repeat("a", n), at); r != nil {
			t.Errorf("a %d-rune detail (the cap is %d) was refused: %s", n, MaxEventDetailRunes, r.Reason)
		}
	}

	// Counted in RUNES, not bytes: 256 multi-byte runes (768 bytes) are
	// still within the cap. A byte-counting check would refuse them and
	// quietly punish exactly the operators whose details are not ASCII.
	multi := strings.Repeat("é", MaxEventDetailRunes)
	if len(multi) == MaxEventDetailRunes {
		t.Fatal("fixture is not multi-byte")
	}
	if r := s.recordEvent(context.Background(), eventKindRefusal, "agent:boss", "no-bash", multi, at); r != nil {
		t.Errorf("the cap counted BYTES (%d bytes / %d runes refused): %s", len(multi), MaxEventDetailRunes, r.Reason)
	}
	if inserts != 3 {
		t.Errorf("seam inserts = %d, want 3 (the three accepted rows)", inserts)
	}
	if lastDetail != multi {
		t.Errorf("the seam received %q…; the accepted multi-byte detail must travel byte for byte", lastDetail[:min(len(lastDetail), 32)])
	}
}
