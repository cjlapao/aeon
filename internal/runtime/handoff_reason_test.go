package runtime

import "testing"

func TestABlockedHandoffGivesItsSummaryAsTheReasonWhenItHasNone(t *testing.T) {
	blocked := Handoff{"status": "blocked", "summary": "goal_create refused every component"}
	if got := handoffReason(blocked, OutcomeBlocked); got != "goal_create refused every component" {
		t.Fatalf("a blocked step's summary is its reason, got %q", got)
	}
	if got := handoffReason(Handoff{"status": "blocked", "reason": "no room", "summary": "s"}, OutcomeBlocked); got != "no room" {
		t.Fatalf("an explicit reason wins, got %q", got)
	}
	if got := handoffReason(Handoff{"status": "reject", "summary": "s"}, OutcomeReject); got != "" {
		t.Fatalf("a reject keeps an absent reason absent, got %q", got)
	}
}
