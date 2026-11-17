package store

import "testing"

func TestBriefVersionsPerLoop(t *testing.T) {
	e := newRecordsEnv(t)
	if _, err := e.s.CreateLoop(NewLoop{Slug: "second", ComponentRoot: "/repo/alpha", Title: "Second", Goal: "g"}); err != nil {
		t.Fatalf("second loop: %v", err)
	}
	b1, err := e.s.CreateBrief(e.loop, NewBrief{Scope: "the flake only", Acceptance: []string{"green twice"}})
	if err != nil {
		t.Fatalf("brief 1: %v", err)
	}
	if b1.Version != 1 || b1.ReviewState != "in_review" {
		t.Fatalf("first brief: v%d/%s, want v1/in_review (born judged)", b1.Version, b1.ReviewState)
	}
	b2, err := e.s.CreateBrief(e.loop, NewBrief{Scope: "the flake, properly"})
	if err != nil {
		t.Fatalf("brief 2: %v", err)
	}
	if b2.Version != 2 {
		t.Fatalf("second brief on the same loop is v%d, want 2", b2.Version)
	}
	// A different loop's counter is its own — MAX is scoped per loop, or two
	// loops would starve each other's versions.
	bOther, err := e.s.CreateBrief(Scope{LoopSlug: "second", ComponentRoot: "/repo/alpha"}, NewBrief{})
	if err != nil {
		t.Fatalf("other loop brief: %v", err)
	}
	if bOther.Version != 1 {
		t.Fatalf("a fresh loop's first brief is v%d, want 1 — the counter leaked across loops", bOther.Version)
	}
	lat, err := e.s.LatestBrief(e.loop)
	if err != nil || lat.Version != 2 || lat.ID != b2.ID {
		t.Fatalf("latest: %+v %v, want v2", lat, err)
	}
	if _, err := e.s.GetBrief(e.loop, 1); err != nil {
		t.Fatalf("v1 must still be readable — rewrites never overwrite: %v", err)
	}
	// JSON arrays round-trip, and absent means empty, not null.
	if len(b1.Acceptance) != 1 || b1.Acceptance[0] != "green twice" || b1.Constraints == nil || len(b1.Constraints) != 0 {
		t.Fatalf("arrays did not round-trip: %+v", b1)
	}
}

func TestBriefClaimsDecisionOnce(t *testing.T) {
	e := newRecordsEnv(t)
	b, err := e.s.CreateBrief(e.loop, NewBrief{Scope: "s"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// A non-human cannot decide a brief either — the same door as goals.
	if _, r, err := e.s.DecideBrief(e.loop, b.Version, "approved", e.agent, ""); err != nil || r == nil || r.Kind != "decider-not-human" {
		t.Fatalf("agent decided a brief: %v %+v", err, r)
	}
	first, r, err := e.s.DecideBrief(e.loop, b.Version, "rejected", e.user, "scope crept")
	if err != nil || r != nil || !first.Changed {
		t.Fatalf("human decide: %v %+v %+v", first, r, err)
	}
	second, r, err := e.s.DecideBrief(e.loop, b.Version, "approved", Actor{Kind: "user", ID: "other"}, "I like it")
	if err != nil || r != nil {
		t.Fatalf("second decide: %v %+v", err, r)
	}
	if second.Changed || second.State != "rejected" || second.DecidedBy != "cj" {
		t.Fatalf("the loser must read the ledger, not write it: %+v", second)
	}
	revs, err := e.s.ListReviews("brief", "walk")
	if err != nil || len(revs) != 1 || revs[0].Decision != "rejected" {
		t.Fatalf("brief ledger: %+v %v", revs, err)
	}
}
