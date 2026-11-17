package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policyseed"
	"github.com/cjlapao/aeon/internal/seed"
)

// TestStartupNeverAppliesPolicyCorpus is D4 made executable for the
// declared corpus set (S45 T12, PM15/SD5): the set is mounted on the one
// engine the daemon holds, it is class guard, and the startup door cannot
// reach it. A start that seeded corpus rows would be the automatic seeder
// D4 welded shut, in a costume — so this test fails the build the day the
// class gate, the registry pairing, or ApplyInertAtStart drifts.
func TestStartupNeverAppliesPolicyCorpus(t *testing.T) {
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}

	// The set is mounted — the proof is not "nothing is there", it is
	// "something is there and start refuses to touch it".
	st, err := e.Status(context.Background(), policyseed.SetID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st) != 1 || len(st[0].Records) == 0 {
		t.Fatalf("the corpus set is not mounted on the daemon's engine: %+v", st)
	}

	ApplyInertAtStart(context.Background(), e)

	rows, err := s.PolicyListAll(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.Origin == "system" && strings.HasPrefix(r.ID, "corpus.") {
			t.Fatalf("the start wrote corpus rule %s — D4 says nothing seeds automatically", r.ID)
		}
	}
	// 2026-11-15 (operator ruling): the shipped DEFAULT set (permissions.firstParty) applies at start, so the
	// store is no longer empty after it. What D4 still pins is that nothing ELSE is written: every row
	// the start wrote is a first-party tool rule, tagged system, and not one is a command rule.
	if len(rows) == 0 {
		t.Fatal("the start wrote no policy rows; the default first-party set should have applied")
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "firstparty.") || r.Origin != "system" || r.Type != "tool_access" {
			t.Fatalf("the start wrote %s (type %s, origin %s); only permissions.firstParty tool rules may seed unasked", r.ID, r.Type, r.Origin)
		}
	}

	// The ledger answer: no applied row for the corpus set — not even a
	// skipped trace beyond what Plan (a read) could leave, and ApplyInert
	// never even planned it (the class gate refuses the set by name).
	led, err := s.SeedLedgerList(context.Background(), policyseed.SetID)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if len(led) != 0 {
		t.Fatalf("the start applied %d corpus ledger rows — the startup path cannot reach a guard set (SD5)", len(led))
	}

	// And the engine's own gate, asked directly: a startup-path apply of
	// the corpus set is refused by class, naming the path and the class.
	_, err = e.Apply(context.Background(), policyseed.SetID, nil,
		seed.Actor{Kind: "system", ID: "startup"}, seed.PathStartup)
	if err == nil {
		t.Fatal("the startup path APPLIED a guard set — the class gate is gone")
	}
	if !strings.Contains(err.Error(), "startup") && !strings.Contains(err.Error(), "guard") {
		t.Fatalf("err = %v; the refusal must name the path or the class", err)
	}
}
