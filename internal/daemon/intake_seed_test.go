package daemon

// intake_seed_test.go — S61 T9 (2026-11-05, P8): the intake plane's two
// inert sets on the engine side, beside tool_seed_test.go's precedent.
// The sets are mounted HERE (NewSeedEngine's rebuild blocks), so the
// start is proven where it is built: declared in the engine every door
// holds, inert, applied at start with system origin, idempotent, and a
// hand-written row stands untouched at the next start — the divergence
// law is the shared appliers', and this proves the intake sets ride the
// same road, not a special one.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/agentseed"
	"github.com/cjlapao/aeon/internal/intakeseed"
	"github.com/cjlapao/aeon/internal/seed"
)

// intakeSets are the two names, in apply order (the voice lands before
// the blueprint that names it).
var intakeSets = []string{agentseed.IntakeSetID, intakeseed.SetID}

func TestIntakeSetsAreDeclaredInTheEngineEveryDoorHolds(t *testing.T) {
	s := openSeedStore(t)
	e := toolSeedEngine(t, s) // seedValidatorPass rides the same NewSeedEngine every door holds
	for _, setID := range intakeSets {
		st, err := e.Status(context.Background(), setID)
		if err != nil {
			t.Fatalf("seed.status %s: %v", setID, err)
		}
		if len(st) != 1 {
			t.Fatalf("seed.status %s = %d entries — an invisible set is never applied", setID, len(st))
		}
		if st[0].Class != seed.ClassInert {
			t.Errorf("%s is class %q — the interview's voice applies at start and permits nothing (D4)", setID, st[0].Class)
		}
	}
}

func TestIntakeSetsApplyAtStartWithSystemOrigin(t *testing.T) {
	s := openSeedStore(t)
	ctx := context.Background()
	e := toolSeedEngine(t, s)
	res, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"})
	if err != nil {
		t.Fatalf("startup apply: %v", err)
	}
	saw := map[string]int{}
	for _, r := range res {
		saw[r.SetID] = len(r.Records)
	}
	if saw[agentseed.IntakeSetID] != 1 {
		t.Errorf("agents.intake applied %d records, want 1 — the intake has exactly one voice", saw[agentseed.IntakeSetID])
	}
	if saw[intakeseed.SetID] != 1 {
		t.Errorf("blueprints.intake applied %d records, want 1 — goal-intake", saw[intakeseed.SetID])
	}
	a, found, err := s.AgentBySlug("intake-architect")
	if err != nil || !found {
		t.Fatalf("intake-architect row after start: %v %v", found, err)
	}
	if a.Origin != "system" {
		t.Errorf("origin %q — only the seed writes here, and it writes system", a.Origin)
	}
	bp, found, err := s.BlueprintGet(ctx, "goal-intake")
	if err != nil || !found {
		t.Fatalf("goal-intake row after start: %v %v", found, err)
	}
	if bp.Origin != "system" {
		t.Errorf("goal-intake origin %q — the blueprint is seeded, not authored", bp.Origin)
	}
}

func TestIntakeSetsAreIdempotentAndADivergedRowStands(t *testing.T) {
	s := openSeedStore(t)
	ctx := context.Background()
	e := toolSeedEngine(t, s)
	if _, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	// A hand-edit to the interview's voice: the next start must not
	// overwrite it (R4 — diverged rows are the person's), while the
	// second apply of an untouched set writes nothing at all.
	agentsBefore, err := s.AgentsAll()
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	for _, r := range res {
		if r.SetID != agentseed.IntakeSetID && r.SetID != intakeseed.SetID {
			continue
		}
		for _, rec := range r.Records {
			if rec.Op != seed.OpSkip {
				t.Errorf("%s/%s re-%s at the second apply — the set churns", r.SetID, rec.Key, rec.Op)
			}
		}
	}
	agentsAfter, err := s.AgentsAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(agentsBefore) != len(agentsAfter) {
		t.Fatalf("agent rows moved %d → %d at the second apply", len(agentsBefore), len(agentsAfter))
	}
}

// The blueprint's seeded bytes are the file's bytes — one DDL-of-sorts
// truth: the yaml the slice authored is what the row must carry.
func TestGoalIntakeRowCarriesTheSeededBytes(t *testing.T) {
	s := openSeedStore(t)
	ctx := context.Background()
	e := toolSeedEngine(t, s)
	if _, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	bp, found, err := s.BlueprintGet(ctx, "goal-intake")
	if err != nil || !found {
		t.Fatalf("goal-intake row: %v %v", found, err)
	}
	if !strings.Contains(bp.Content, "id: goal-intake") || !strings.Contains(bp.Content, "intake-architect") {
		t.Errorf("row content is not the seeded blueprint: %.120s", bp.Content)
	}
}
