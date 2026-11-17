package loop

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// S38 T14 — the gate join (OL8): the person's answer and the record's
// verdict are one act, reached ONLY through DecideBrief/DecideGoal.

func decideEnv(t *testing.T) (*Deps, *store.Store, store.Scope, LoopState) {
	t.Helper()
	s := openLoopStore(t)
	p, err := s.CreateComponent(store.NewComponent{Name: "decide group", Slug: "decide-group", Root: "/repo/decide-group"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: "/repo/alpha", ParentID: p.Component.ID}); err != nil {
		t.Fatal(err)
	}
	in := store.NewLoop{Slug: "decide", ComponentRoot: "/repo/alpha", Title: "D", Goal: "g"}
	l, err := s.CreatePinnedLoop(in, "goal-to-brief", 1, "id: goal-to-brief\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s}
	st := LoopState{ID: l.ID, Slug: "decide", ComponentRoot: "/repo/alpha"}
	return &dep, s, store.Scope{LoopSlug: "decide", ComponentRoot: "/repo/alpha"}, st
}

var userActor = store.Actor{Kind: "user", ID: "operator"}

func TestJoinApproveAndAbortDecideTheBrief(t *testing.T) {
	dep, s, sc, st := decideEnv(t)
	if _, err := s.CreateBrief(sc, store.NewBrief{Scope: "the spine"}); err != nil {
		t.Fatal(err)
	}
	gate := blueprint.Step{ID: "g", Kind: blueprint.Gate, Question: "approve-brief", Decides: "brief"}
	if err := DecideFromGate(*dep, st, gate, "approve", userActor); err != nil {
		t.Fatalf("approve: %v", err)
	}
	b, err := s.GetBrief(sc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if b.ReviewState != "approved" {
		t.Fatalf("the answer arrived without the verdict: %s", b.ReviewState)
	}
	rows, err := s.ListReviews("brief", "decide")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ByKind != "user" || rows[0].ByID != "operator" || rows[0].Decision != "approved" {
		t.Fatalf("the review row is not the person's act: %+v", rows)
	}
	// abort decides rejection on the next version, and revise changes
	// NOTHING (the lane files the next version itself).
	dep2, s2, sc2, st2 := decideEnv(t)
	if _, err := s2.CreateBrief(sc2, store.NewBrief{Scope: "again"}); err != nil {
		t.Fatal(err)
	}
	if err := DecideFromGate(*dep2, st2, gate, "revise", userActor); err != nil {
		t.Fatalf("revise must be a no-op, got %v", err)
	}
	if b, _ := s2.GetBrief(sc2, 1); b.ReviewState != "in_review" {
		t.Fatalf("revise moved state to %s — a revision is a new filing, not a judgement", b.ReviewState)
	}
	if err := DecideFromGate(*dep2, st2, gate, "abort", userActor); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if b, _ := s2.GetBrief(sc2, 1); b.ReviewState != "rejected" {
		t.Fatalf("abort did not reject: %s", b.ReviewState)
	}
	// A non-human decider is refused BEFORE SQL (OL8) — the join passes the
	// store's own refusal up as the error it is.
	if err := DecideFromGate(*dep, st, gate, "approve", store.Actor{Kind: "agent", ID: "implementer"}); err == nil ||
		!strings.Contains(err.Error(), "refused") {
		t.Fatalf("an agent decided a review: %v", err)
	}
	// A gate that decides nothing decides nothing.
	if err := DecideFromGate(*dep, st, blueprint.Step{ID: "g", Kind: blueprint.Gate, Question: "x"}, "approve", userActor); err != nil {
		t.Fatalf("a plain gate must pass through: %v", err)
	}
}

func TestDecideFromGateIsIdempotent(t *testing.T) {
	dep, s, sc, st := decideEnv(t)
	if _, err := s.CreateBrief(sc, store.NewBrief{Scope: "once"}); err != nil {
		t.Fatal(err)
	}
	gate := blueprint.Step{ID: "g", Kind: blueprint.Gate, Question: "approve-brief", Decides: "brief"}
	if err := DecideFromGate(*dep, st, gate, "approve", userActor); err != nil {
		t.Fatal(err)
	}
	// Crash-and-resume replays the answer: claimed-FROM makes the second
	// pass harmless — one row, ONE review row.
	if err := DecideFromGate(*dep, st, gate, "approve", userActor); err != nil {
		t.Fatalf("the replay was not harmless: %v", err)
	}
	rows, err := s.ListReviews("brief", "decide")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("the replay wrote a second review row: %+v", rows)
	}
	if b, _ := s.GetBrief(sc, 1); b.ReviewState != "approved" {
		t.Fatalf("replay changed the verdict: %s", b.ReviewState)
	}
}

func TestJoinDecidesThePinnedGoalVersion(t *testing.T) {
	s := openLoopStore(t)
	p, err := s.CreateComponent(store.NewComponent{Name: "goal group", Slug: "goal-group", Root: "/repo/goal-group"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: "/repo/alpha", ParentID: p.Component.ID}); err != nil {
		t.Fatal(err)
	}
	goal, rf, err := s.CreateGoal(store.NewGoal{ComponentID: p.Component.ID, Title: "frame", Type: "feature",
		SourceKind: "manual", SourceRef: "test", By: store.Actor{Kind: "user", ID: "operator"}, Submit: true})
	if err != nil || rf != nil {
		t.Fatalf("goal: %v %v", rf, err)
	}
	v := goal.Version
	in := store.NewLoop{Slug: "on-goal", ComponentRoot: "/repo/alpha", Title: "G", Goal: "g", GoalID: goal.ID, GoalVersion: &v}
	l, err := s.CreatePinnedLoop(in, "goal-to-brief", 1, "id: goal-to-brief\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	dep := Deps{Store: s}
	st := LoopState{ID: l.ID, Slug: "on-goal", ComponentRoot: "/repo/alpha"}
	gate := blueprint.Step{ID: "g", Kind: blueprint.Gate, Question: "approve-goal", Decides: "goal"}
	if err := DecideFromGate(dep, st, gate, "approve", userActor); err != nil {
		t.Fatalf("approve the goal: %v", err)
	}
	g, err := s.GetGoal(goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if g.ReviewState != "approved" {
		t.Fatalf("the pinned goal was not decided: %s", g.ReviewState)
	}
	rows, _ := s.ListReviews("goal", goal.ID)
	if len(rows) != 1 || rows[0].ByKind != "user" {
		t.Fatalf("the goal review is not the person's: %+v", rows)
	}
	// The un-pinned twin refuses with the door a person can walk through.
	in2 := store.NewLoop{Slug: "no-goal", ComponentRoot: "/repo/alpha", Title: "N", Goal: "n"}
	l2, err := s.CreatePinnedLoop(in2, "goal-to-brief", 1, "id: goal-to-brief\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	st2 := LoopState{ID: l2.ID, Slug: "no-goal", ComponentRoot: "/repo/alpha"}
	err = DecideFromGate(dep, st2, gate, "approve", userActor)
	if err == nil || !strings.Contains(err.Error(), "goalId") {
		t.Fatalf("the un-pinned goal join must name the missing door (goalId): %v", err)
	}
	// A vocabulary the join does not speak refuses loudly, never silently.
	if err := DecideFromGate(dep, st, blueprint.Step{ID: "g", Kind: blueprint.Gate, Question: "q", Decides: "epic"}, "approve", userActor); err == nil ||
		!strings.Contains(err.Error(), "vocabulary") {
		t.Fatalf("an unspeakable decides passed silently: %v", err)
	}
}
