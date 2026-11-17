package store

import (
	"strings"
	"testing"
)

// recordsEnv seeds a fresh store with the parents every records test needs:
// a group, two components under it, one loop — built through the store's own
// doors, because records hang off real rows.
type recordsEnv struct {
	s           *Store
	projectID   string
	componentID string
	loop        Scope
	user        Actor
	agent       Actor
}

func newRecordsEnv(t *testing.T) recordsEnv {
	t.Helper()
	s := openTemp(t)
	e := recordsEnv{s: s,
		user:  Actor{Kind: "user", ID: "cj"},
		agent: Actor{Kind: "agent", ID: "reddit-agent"}}
	// A root that becomes a "project" once a child hangs under it (CT1: the
	// project tier is derived, never stored).
	root, err := s.CreateComponent(NewComponent{Name: "the group", Slug: "the-group", Root: "/repo/group"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	e.projectID = root.Component.ID
	c, err := s.CreateComponent(NewComponent{Name: "alpha", Slug: "alpha", Root: "/repo/alpha", ParentID: e.projectID})
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	e.componentID = c.Component.ID
	if _, err := s.CreateLoop(NewLoop{Slug: "walk", ComponentRoot: "/repo/alpha", Title: "The walk", Goal: "g"}); err != nil {
		t.Fatalf("create loop: %v", err)
	}
	e.loop = Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}
	return e
}

func mustGoal(t *testing.T, e recordsEnv, submit bool) Goal {
	t.Helper()
	g, r, err := e.s.CreateGoal(NewGoal{ComponentID: e.projectID, Title: "Fix the flake",
		Type: "bug", SourceKind: "manual", By: e.user, Submit: submit})
	if err != nil || r != nil {
		t.Fatalf("create goal: %v %+v", err, r)
	}
	return g
}

func TestGoalCannotSkipReview(t *testing.T) {
	e := newRecordsEnv(t)
	draft := mustGoal(t, e, false)
	if draft.ReviewState != "draft" {
		t.Fatalf("a new goal is %q, want draft", draft.ReviewState)
	}
	filed := mustGoal(t, e, true)
	if filed.ReviewState != "in_review" {
		t.Fatalf("a submitted goal is %q, want in_review", filed.ReviewState)
	}
	// Neither door reaches 'approved', and Submit is not a back door through
	// some further state: the two reachable states are the two honest ones.
	for _, g := range []Goal{draft, filed} {
		if g.ReviewState == "approved" || g.ReviewState == "rejected" || g.ReviewState == "parked" {
			t.Fatalf("a goal arrived judged: %q", g.ReviewState)
		}
	}
	// Submitting a draft is the other legal entrance.
	g, r, err := e.s.SubmitGoalForReview(draft.ID)
	if err != nil || r != nil {
		t.Fatalf("submit: %v %+v", err, r)
	}
	if g.ReviewState != "in_review" {
		t.Fatalf("submitted draft is %q, want in_review", g.ReviewState)
	}
	// And resubmitting something already waiting says so.
	if _, r, err := e.s.SubmitGoalForReview(draft.ID); err != nil || r == nil || r.Kind != "goal-not-draft" {
		t.Fatalf("resubmit answered %v %+v, want refusal goal-not-draft", err, r)
	}
}

func TestDecideGoalClaimedFrom(t *testing.T) {
	e := newRecordsEnv(t)
	g := mustGoal(t, e, true)
	first, r, err := e.s.DecideGoal(g.ID, g.Version, "approved", e.user, "looks right")
	if err != nil || r != nil {
		t.Fatalf("first decide: %v %+v", err, r)
	}
	if !first.Changed || first.State != "approved" || first.DecidedBy != "cj" {
		t.Fatalf("first decide answered %+v", first)
	}
	// The second decider cannot claim what is already claimed — and is told
	// WHO decided, not merely "no".
	other := Actor{Kind: "user", ID: "someone-else"}
	second, r, err := e.s.DecideGoal(g.ID, g.Version, "rejected", other, "I disagree")
	if err != nil || r != nil {
		t.Fatalf("second decide: %v %+v", err, r)
	}
	if second.Changed {
		t.Fatal("the second decision CHANGED the state — the claimed-FROM guard is missing")
	}
	if second.State != "approved" || second.DecidedBy != "cj" || second.DecidedAt == "" {
		t.Fatalf("the loser must be told who won: %+v", second)
	}
	// Exactly one review row — the loser wrote nothing.
	revs, err := e.s.ListReviews("goal", g.ID)
	if err != nil || len(revs) != 1 {
		t.Fatalf("review rows: %d (%v), want exactly one", len(revs), err)
	}
	if revs[0].ByKind != "user" || revs[0].ByID != "cj" || revs[0].Decision != "approved" {
		t.Fatalf("the ledger row: %+v", revs[0])
	}
}

func TestDecideRefusesNonHumanActor(t *testing.T) {
	e := newRecordsEnv(t)
	g := mustGoal(t, e, true)
	for _, kind := range []string{"agent", "trigger"} {
		res, r, err := e.s.DecideGoal(g.ID, g.Version, "approved", Actor{Kind: kind, ID: "x"}, "self-approval")
		if err != nil {
			t.Fatalf("%s decide: %v", kind, err)
		}
		if r == nil || r.Kind != "decider-not-human" {
			t.Fatalf("%s was accepted to decide: %+v %+v", kind, res, r)
		}
		if !strings.Contains(r.Sentence, "person") {
			t.Errorf("the sentence must name the rule: %q", r.Sentence)
		}
	}
	// Zero rows written, and the goal still waits.
	revs, err := e.s.ListReviews("goal", g.ID)
	if err != nil || len(revs) != 0 {
		t.Fatalf("a non-human wrote %d review rows (%v), want zero", len(revs), err)
	}
	cur, err := e.s.GetGoal(g.ID)
	if err != nil || cur.ReviewState != "in_review" {
		t.Fatalf("the goal moved without a person: %+v %v", cur, err)
	}
	// And an unknown decision from a human is a refusal too, not an error.
	if _, r, err := e.s.DecideGoal(g.ID, g.Version, "yes-please", e.user, ""); err != nil || r == nil || r.Kind != "decision-unknown" {
		t.Fatalf("bogus decision answered %v %+v", err, r)
	}
}

func TestMaterialEditBumpsVersionAndReopensReview(t *testing.T) {
	e := newRecordsEnv(t)
	est := 5.0
	conf := 2
	g, r, err := e.s.CreateGoal(NewGoal{ComponentID: e.projectID, Title: "Ship it", Type: "feature",
		SourceKind: "manual", By: e.user, Submit: true,
		Components: []GoalComponent{{ComponentID: e.componentID, Rationale: "the only one", EstimateUSD: &est, Confidence: &conf, Basis: []string{"measured"}}}})
	if err != nil || r != nil {
		t.Fatalf("create: %v %+v", err, r)
	}
	if _, r, err := e.s.DecideGoal(g.ID, g.Version, "approved", e.user, ""); err != nil || r != nil {
		t.Fatalf("decide: %v %+v", err, r)
	}
	// A material edit — even to an APPROVED goal — re-opens review: what was
	// approved is no longer what is on the table.
	edited, r, err := e.s.UpdateGoal(GoalEdit{ID: g.ID, Title: "Ship it (reworded)", By: e.user})
	if err != nil || r != nil {
		t.Fatalf("update: %v %+v", err, r)
	}
	if edited.Version != 2 {
		t.Fatalf("version is %d after a material edit, want 2", edited.Version)
	}
	if edited.ReviewState != "in_review" {
		t.Fatalf("state is %q after the edit, want in_review — an approved goal nobody has re-read is not approved", edited.ReviewState)
	}
	// The component set copied forward (the approval covered them too).
	full, err := e.s.GetGoal(g.ID)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if len(full.Components) != 1 || full.Components[0].ComponentID != e.componentID || full.Components[0].EstimateUSD == nil || *full.Components[0].EstimateUSD != 5 {
		t.Fatalf("the component set did not copy to v2: %+v", full.Components)
	}
	// A non-edit is a Noop: same version, nothing reopened.
	noop, r, err := e.s.UpdateGoal(GoalEdit{ID: g.ID, By: e.user})
	if err != nil || r != nil {
		t.Fatalf("noop: %v %+v", err, r)
	}
	if noop.Version != 2 || noop.ReviewState != "in_review" {
		t.Fatalf("a no-op edit bumped/reopened: %+v", noop)
	}
	// The ledger still shows only the one decision; the bump has its own
	// version now, and deciding v1 after the bump loses cleanly.
	if _, r, err := e.s.DecideGoal(g.ID, 1, "approved", e.user, "stale"); err != nil || r != nil {
		t.Fatalf("stale decide: %v %+v", err, r)
	}
	revs, _ := e.s.ListReviews("goal", g.ID)
	if len(revs) != 1 || revs[0].SubjectVersion != 1 {
		t.Fatalf("the ledger moved: %+v", revs)
	}
	if cur, err := e.s.GetGoal(g.ID); err != nil || cur.ReviewState != "in_review" || cur.Version != 2 {
		t.Fatalf("deciding v1 after a bump must not touch v2: %+v %v", cur, err)
	}
}

func TestGoalRollupIsAFloorWhenAnEstimateIsUnknown(t *testing.T) {
	e := newRecordsEnv(t)
	c2, err := e.s.CreateComponent(NewComponent{Name: "beta", Slug: "beta", Root: "/repo/beta"})
	if err != nil {
		t.Fatalf("second component: %v", err)
	}
	hi, lo := 3, 1
	ten := 10.0
	g, r, err := e.s.CreateGoal(NewGoal{ComponentID: e.projectID, Title: "Two stakes", Type: "feature",
		SourceKind: "manual", By: e.user,
		Components: []GoalComponent{
			{ComponentID: e.componentID, EstimateUSD: &ten, Confidence: &hi},
			{ComponentID: c2.Component.ID, Confidence: &lo}, // no estimate — UNKNOWN, not zero
		}})
	if err != nil || r != nil {
		t.Fatalf("create: %v %+v", err, r)
	}
	full, err := e.s.GetGoal(g.ID)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	minConf, sum, floor := GoalRollup(full)
	if minConf == nil || *minConf != 1 {
		t.Fatalf("rollup confidence %v, want the WEAKEST stake (1)", minConf)
	}
	if !floor {
		t.Fatal("an unknown estimate did not make the cost a floor — unknown became zero")
	}
	if sum != 10 {
		t.Fatalf("sum of known %v, want 10", sum)
	}
	// All-known is not a floor.
	five, eight := 5.0, 8.0
	g2, r, err := e.s.CreateGoal(NewGoal{ComponentID: e.projectID, Title: "Both known", Type: "chore",
		SourceKind: "manual", By: e.user,
		Components: []GoalComponent{{ComponentID: e.componentID, EstimateUSD: &five}, {ComponentID: c2.Component.ID, EstimateUSD: &eight}}})
	if err != nil || r != nil {
		t.Fatalf("create 2: %v %+v", err, r)
	}
	full2, err := e.s.GetGoal(g2.ID)
	if err != nil {
		t.Fatalf("reread 2: %v", err)
	}
	if _, _, floor := GoalRollup(full2); floor {
		t.Fatal("a fully-estimated goal reads as a floor")
	}
}

func TestGoalCreateRefusals(t *testing.T) {
	e := newRecordsEnv(t)
	cases := []struct {
		in   NewGoal
		kind string
	}{
		{NewGoal{ComponentID: e.projectID, Title: "x", Type: "bug", SourceKind: "manual", By: Actor{Kind: "ghost", ID: "x"}}, "actor-unknown"},
		{NewGoal{ComponentID: e.projectID, Title: "", Type: "bug", SourceKind: "manual", By: e.user}, "title-empty"},
		{NewGoal{ComponentID: e.projectID, Title: "x", Type: "epic-poem", SourceKind: "manual", By: e.user}, "type-unknown"},
		{NewGoal{ComponentID: e.projectID, Title: "x", Type: "bug", SourceKind: "carrier-pigeon", By: e.user}, "source-kind-unknown"},
	}
	for i, c := range cases {
		if _, r, err := e.s.CreateGoal(c.in); err != nil || r == nil || r.Kind != c.kind {
			t.Errorf("case %d: got err=%v refusal=%+v, want %s", i, err, r, c.kind)
		}
	}
	// Unknown component in the set is refused with a sentence, not a raw FK
	// violation.
	if _, _, err := e.s.CreateGoal(NewGoal{ComponentID: e.projectID, Title: "x", Type: "bug",
		SourceKind: "manual", By: e.user, Components: []GoalComponent{{ComponentID: "c-nope"}}}); err == nil ||
		!strings.Contains(err.Error(), "component \"c-nope\" does not exist") {
		t.Fatalf("unknown component answered %v, want the naming sentence", err)
	}
}
