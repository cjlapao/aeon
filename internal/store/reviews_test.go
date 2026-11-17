package store

import "testing"

func TestReviewAppendOnlyRoundTrip(t *testing.T) {
	e := newRecordsEnv(t)
	g := mustGoal(t, e, true)
	if _, r, err := e.s.DecideGoal(g.ID, g.Version, "parked", e.user, "not now"); err != nil || r != nil {
		t.Fatalf("decide: %v %+v", err, r)
	}
	revs, err := e.s.ListReviews("goal", g.ID)
	if err != nil || len(revs) != 1 {
		t.Fatalf("reviews: %+v %v", revs, err)
	}
	if revs[0].Decision != "parked" || revs[0].Reason != "not now" || revs[0].ByKind != "user" {
		t.Fatalf("row: %+v", revs[0])
	}
	// TestReviewIsAppendOnly lives in records_guard_test.go — the grep, not
	// a wish; this test proves the ledger READS back what the Decide wrote.
}
