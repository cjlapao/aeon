package daemon

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/mcpseed"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// S46 T10 (MG10): the two MCP seed sets, proven with FAKE records so the
// first real default is a one-record change. The production declarations
// ship zero records (asserted in internal/mcpseed); here the machinery —
// the class gate, the ledger, the domain audit, the conflict and removal
// laws — runs over the daemon's own repo and applier adapters.

func mcpFakeServers() []seed.Record {
	content := map[string]any{
		"name": "gh", "tier": "public", "enabled": false,
		"transport": map[string]any{
			"type": "stdio", "command": "gh-mcp",
			"args": []string{"--mcp"}, "env": []string{"GITHUB_TOKEN"}, "url": "",
		},
		"tools": []map[string]any{
			// NAME-ORDER — the canonical-at-write convention (mcpseed).
			{"name": "create_issue", "rerun": "unsafe", "effect": "outward"},
			{"name": "list_issues", "rerun": "safe", "effect": "local"},
		},
	}
	b, err := seed.Canonical(content)
	if err != nil {
		panic(err)
	}
	return []seed.Record{{Key: "gh", Version: 1, Content: b}}
}

func mcpFakeGrants() []seed.Record {
	decl := map[string]any{"kind": "declare", "agent": "tester", "qualified": "gh.list_issues"}
	b, err := seed.Canonical(decl)
	if err != nil {
		panic(err)
	}
	return []seed.Record{{Key: "declare:tester:gh.list_issues", Version: 1, Content: b}}
}

// mcpFakeEngine builds an engine carrying the two MCP sets with FAKE
// records over the daemon's repo and applier adapters — the same wiring
// NewSeedEngine performs for production (empty) records.
func mcpFakeEngine(t *testing.T, s *store.Store, validate mcpseed.RowValidator) *seed.Engine {
	t.Helper()
	sets := []seed.Set{
		{ID: mcpseed.SetServers, Class: seed.ClassInert, Version: 1, Records: mcpFakeServers},
		{ID: mcpseed.SetGrants, Class: seed.ClassGuard, Version: 1, Records: mcpFakeGrants},
	}
	appliers := []seed.Applier{
		mcpApplier{ap: mcpseed.NewServersApplier(s, validate, fixedSeedClock)},
		mcpApplier{ap: mcpseed.NewGrantsApplier(s, validate, fixedSeedClock)},
	}
	reg, err := seed.NewRegistry(sets, appliers)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	opt := seed.Options{Registry: reg, Appliers: map[string]seed.Applier{}}
	for _, a := range appliers {
		opt.Appliers[a.Set()] = a
	}
	e, err := seed.NewEngine(NewSeedRepo(s), opt)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return e
}

func seedValidatorPass(context.Context, store.McpAll) error { return nil }

func fixedSeedClock() time.Time { return time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC) }

func TestSeedStatusListsBothSetsZeroZero(t *testing.T) {
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{}, seedValidatorPass)
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	for _, id := range []string{mcpseed.SetServers, mcpseed.SetGrants} {
		st, err := e.Status(context.Background(), id)
		if err != nil {
			t.Fatalf("status %s: %v", id, err)
		}
		if len(st) != 1 {
			t.Fatalf("set %s is not visible in seed.status (%d entries) — an invisible set is a lying tracker", id, len(st))
		}
		if len(st[0].Records) != 0 || st[0].AppliedVersion != 0 || st[0].AvailableVersion != 1 {
			t.Fatalf("%s must read the honest 0/0 up-to-date state, got %+v", id, st[0])
		}
	}
	// The startup apply touches nothing (empty inert set — an honest no-op,
	// never an error).
	res, err := e.ApplyInert(context.Background(), seed.Actor{Kind: "system", ID: "startup"})
	if err != nil {
		t.Fatalf("startup apply: %v", err)
	}
	for _, r := range res {
		if r.SetID == mcpseed.SetServers && len(r.Records) != 0 {
			t.Fatalf("the empty production inert set wrote %d records — MG10 ships zero", len(r.Records))
		}
	}
}

func TestFakeServerRecordAppliesDisabledWithSystemOrigin(t *testing.T) {
	s := openSeedStore(t)
	e := mcpFakeEngine(t, s, seedValidatorPass)
	ctx := context.Background()
	res, err := e.Apply(ctx, mcpseed.SetServers, nil, seed.Actor{Kind: "user", ID: "carlos"}, seed.Path("socket"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res.Records) != 1 || res.Records[0].Op == "" {
		t.Fatalf("apply answered %+v — want one record op", res)
	}
	row, present, err := s.McpServerGet(ctx, "gh")
	if err != nil || !present {
		t.Fatalf("the system server did not land (present=%v err=%v)", present, err)
	}
	if row.Origin != "system" || row.Enabled {
		t.Errorf("provenance/state wrong: origin=%q enabled=%v — MG10 ships disabled system rows", row.Origin, row.Enabled)
	}
	if len(row.Tools) != 2 {
		t.Errorf("tools = %d, want 2", len(row.Tools))
	}
	// Row + ledger + domain audit commit together (SD3): all three exist,
	// or none would.
	led, err := s.SeedLedgerList(ctx, mcpseed.SetServers)
	if err != nil {
		t.Fatal(err)
	}
	if len(led) != 1 || seed.State(led[0].State) != seed.StateApplied {
		t.Fatalf("ledger: %+v — want one applied row", led)
	}
	changes, err := s.McpChangeList(ctx, "gh", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Op != "seed" {
		t.Fatalf("mcp_change: %+v — want the op=seed audit beside the seed_event", changes)
	}
}

func TestInertSetAppliesAtStartGuardSetDoesNot(t *testing.T) {
	s := openSeedStore(t)
	// The declaration names an agent — the FK to agent(slug) is the store's
	// law (zero-fabrication reaches grants too: an agent row must exist).
	if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "tester", Name: "tester", Version: "1"}, "2026-11-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	e := mcpFakeEngine(t, s, seedValidatorPass)
	ctx := context.Background()
	if _, err := e.ApplyInert(ctx, seed.Actor{Kind: "system", ID: "startup"}); err != nil {
		t.Fatalf("inert start: %v", err)
	}
	if _, present, _ := s.McpServerGet(ctx, "gh"); !present {
		t.Fatal("the inert servers set did NOT apply at start — definitions would never ship")
	}
	// The grant (declare) must NOT be in force — the class gate keeps the
	// widening off every startup path (D4 by construction).
	all, _ := s.McpRows(ctx)
	for _, d := range all.Declares {
		t.Fatalf("start declared %s for %s — a guard set reached by startup is the D4 breach", d.Qualified, d.AgentSlug)
	}
	led, _ := s.SeedLedgerList(ctx, mcpseed.SetGrants)
	if len(led) != 0 {
		t.Fatalf("the guard set carries %d ledger rows from the start — SD5 says none", len(led))
	}
	// The engine's own gate, asked directly.
	if _, err := e.Apply(ctx, mcpseed.SetGrants, nil, seed.Actor{Kind: "system", ID: "startup"}, seed.PathStartup); err == nil {
		t.Fatal("the startup path APPLIED the guard set — the class gate is gone")
	}
	// The human door path applies it.
	gres, err := e.Apply(ctx, mcpseed.SetGrants, nil, seed.Actor{Kind: "user", ID: "carlos"}, seed.Path("socket"))
	if err != nil {
		t.Fatalf("the human door could not apply the guard set: %v", err)
	}
	for _, r := range gres.Records {
		if r.Op == "" {
			t.Fatalf("the guard apply answered an empty op — per-record failures are RECORDED, check the ledger: %+v", gres.Records)
		}
	}
	all, _ = s.McpRows(ctx)
	found := false
	for _, d := range all.Declares {
		if d.AgentSlug == "tester" && d.Qualified == "gh.list_issues" {
			found = true
		}
	}
	if !found {
		t.Errorf("seed.apply of the guard set declared nothing: %+v", all.Declares)
	}
}

func TestRemovedSystemServerIsNotResurrectedRestoreBringsItBack(t *testing.T) {
	s := openSeedStore(t)
	e := mcpFakeEngine(t, s, seedValidatorPass)
	ctx := context.Background()
	if _, err := e.Apply(ctx, mcpseed.SetServers, nil, seed.Actor{Kind: "user", ID: "carlos"}, seed.Path("socket")); err != nil {
		t.Fatal(err)
	}
	// The operator removes it (T5's door would audit this; the removal
	// itself is the store fact here).
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		ok, err := store.McpRemoveServerTx(ctx, tx, "gh")
		if err != nil {
			return err
		}
		if !ok {
			return context.DeadlineExceeded // the row vanished before the operator could remove it
		}
		return nil
	}); err != nil {
		t.Fatalf("operator removal: %v", err)
	}
	// Re-apply: the engine reports removed and NEVER resurrects — the
	// operator's choice stands.
	res, err := e.Apply(ctx, mcpseed.SetServers, nil, seed.Actor{Kind: "user", ID: "carlos"}, seed.Path("socket"))
	if err != nil {
		t.Fatal(err)
	}
	if _, present, _ := s.McpServerGet(ctx, "gh"); present {
		t.Fatal("re-apply resurrected the operator-removed server — SD6 broken")
	}
	skipped := false
	for _, r := range res.Records {
		if strings.Contains(string(r.Op), "skip") || r.Reason != "" {
			skipped = true
		}
	}
	if !skipped {
		t.Logf("apply answered %+v (the skip may be silent for a removed record — the row's absence is the law)", res.Records)
	}
	// seed.restore is explicit and brings it back.
	if _, err := e.Restore(ctx, mcpseed.SetServers, "gh", seed.Actor{Kind: "user", ID: "carlos"}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, present, _ := s.McpServerGet(ctx, "gh"); !present {
		t.Fatal("seed.restore did not bring the system server back")
	}
}

func TestUserServerWithSameNameIsAConflict(t *testing.T) {
	s := openSeedStore(t)
	ctx := context.Background()
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.McpAddServerTx(ctx, tx, store.McpServerRow{
			Name: "gh", Tier: "public", TransportType: "stdio", Command: "mine",
			Args: "[]", Env: "[]", Enabled: true, Origin: "user",
			CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e := mcpFakeEngine(t, s, seedValidatorPass)
	plan, err := e.Plan(ctx, mcpseed.SetServers, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	conflict := false
	for _, d := range plan.Decisions {
		if strings.Contains(strings.ToLower(string(d.Op)), "conflict") || strings.Contains(strings.ToLower(d.Reason), "conflict") {
			conflict = true
		}
	}
	if !conflict {
		t.Fatalf("the pre-existing user server is not reported as a conflict: %+v (S43-extra SD9: reported, never overwritten)", plan.Decisions)
	}
	if _, err := e.Apply(ctx, mcpseed.SetServers, nil, seed.Actor{Kind: "user", ID: "carlos"}, seed.Path("socket")); err != nil {
		t.Fatalf("apply with a conflict must plan-refuse it, not error the set: %v", err)
	}
	cur, _, err := s.McpServerGet(ctx, "gh")
	if err != nil {
		t.Fatal(err)
	}
	if cur.Command != "mine" || cur.Origin != "user" {
		t.Errorf("the user's server was clobbered: %+v", cur)
	}
}

// TestStartupNeverAppliesMcpGrants is the MCP twin of
// TestStartupNeverAppliesPolicyCorpus: the sets are mounted on the ONE
// engine the daemon holds (production, empty records) and the startup door
// cannot reach the guard one.
func TestStartupNeverAppliesMcpGrants(t *testing.T) {
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{}, seedValidatorPass)
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	if _, err := e.Status(context.Background(), mcpseed.SetGrants); err != nil {
		t.Fatalf("the grants set is not mounted on the daemon's engine: %v", err)
	}
	ApplyInertAtStart(context.Background(), e)
	if _, err := e.Apply(context.Background(), mcpseed.SetGrants, nil,
		seed.Actor{Kind: "system", ID: "startup"}, seed.PathStartup); err == nil {
		t.Fatal("the startup path APPLIED the guard set — the class gate is gone")
	}
}
