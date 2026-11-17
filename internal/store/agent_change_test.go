package store

// agent_change_test.go — S73 T3 (AR1, AR3): the ledger's own rules, tested at
// the database they are enforced in.
//
// The ledger's two claims are that it records every change (proved from the
// writer's side in agent_audited_test.go) and that nothing can edit or remove
// what it recorded. The second is a trigger, and a trigger is only real when a
// test walks into it, so this file walks in — naming the sentence it expects to
// be refused with, because a refusal that arrives for the wrong reason is a
// passing test about a broken database.

import (
	"context"
	"strings"
	"testing"
)

// userAgentActor is the store test's person: the same uid the MCP and platform
// test suites use, so a reader who has seen one audited-plane test has seen
// this one's actor too.
var userAgentActor = AgentActor{Kind: AgentActorUser, ID: "1000"}

// cliActor is the direct door's stamp, and seedTestActor the engine's — see
// agent_audited_test.go for which road legitimately produces which.
var (
	cliActor      = AgentActor{Kind: AgentActorCLI, ID: "cli"}
	seedTestActor = AgentActor{Kind: AgentActorSystem, ID: "seed-engine"}
)

// agentChangeAt is the stamp the tests pin, so an assertion can name the exact
// string it expects rather than a window around now().
const agentChangeAt = "2026-11-06T09:00:00Z"

// TestAgentChangeRefusesAnUpdateAndADeleteByTrigger is AR1's append-only half:
// both statements die with the trigger's own sentence, named.
func TestAgentChangeRefusesAnUpdateAndADeleteByTrigger(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	// Lay one row through the only door there is.
	if err := s.CreateAgentAudited(ctx, userAgentActor, Agent{Slug: "ledger-a", Name: "Ledger A", Version: "1"}, agentChangeAt); err != nil {
		t.Fatalf("create: %v", err)
	}
	const sentence = "agent_change is append-only (S73 AR1)"
	if _, err := s.w.Exec(`UPDATE agent_change SET summary = ?`, "rewritten history"); err == nil {
		t.Fatal("an UPDATE of the ledger succeeded — the trigger is not there")
	} else if !strings.Contains(err.Error(), sentence) {
		t.Fatalf("the UPDATE failed for the wrong reason: %v", err)
	}
	if _, err := s.w.Exec(`DELETE FROM agent_change WHERE op = ?`, AgentOpCreate); err == nil {
		t.Fatal("a DELETE of the ledger succeeded — the trigger is not there")
	} else if !strings.Contains(err.Error(), sentence) {
		t.Fatalf("the DELETE failed for the wrong reason: %v", err)
	}
	// And the row is still what it was.
	var summary string
	if err := s.r.QueryRow(`SELECT summary FROM agent_change WHERE slug = ?`, "ledger-a").Scan(&summary); err != nil {
		t.Fatalf("read the ledger back: %v", err)
	}
	if summary != AgentChangeSummaryCreate("ledger-a", "Ledger A") {
		t.Fatalf("the summary after two refused mutations is %q", summary)
	}
}

// TestAgentActorValidRefusesTheFourUnusableValues is AR3's Go-side half: the
// actor is refused before SQL, and each refusal says which half is unusable.
func TestAgentActorValidRefusesTheFourUnusableValues(t *testing.T) {
	for name, c := range map[string]struct {
		agent AgentActor
		says  string
	}{
		"blank kind":             {AgentActor{Kind: "", ID: "1000"}, "names an actor kind"},
		"kind outside the CHECK": {AgentActor{Kind: "agent", ID: "groot"}, "not one of the kinds"},
		"blank id":               {AgentActor{Kind: AgentActorUser, ID: "  "}, "needs an id"},
		"uid 0":                  {AgentActor{Kind: AgentActorUser, ID: "0"}, `"0" is not an identity`},
	} {
		err := c.agent.Valid()
		if err == nil {
			t.Errorf("%s: Valid() accepted %#v", name, c.agent)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: Valid() said %q, which does not name %q", name, err, c.says)
		}
	}
	for name, actor := range map[string]AgentActor{
		"user":   {Kind: AgentActorUser, ID: "1000"},
		"cli":    cliActor,
		"system": seedTestActor,
	} {
		if err := actor.Valid(); err != nil {
			t.Errorf("%s: the lawful actor %#v was refused: %v", name, actor, err)
		}
	}
}

// TestAgentChangeValidateRefusesWhatTheCheckWouldCatch keeps the Go-side and the
// SQL-side vocabularies honest: a shape the migration refuses must be refused in
// Go first, naming the field, so a caller never sees a constraint name.
func TestAgentChangeValidateRefusesWhatTheCheckWouldCatch(t *testing.T) {
	slug := "groot"
	blank := ""
	long := strings.Repeat("far too long ", 200)
	for name, c := range map[string]struct {
		change AgentChange
		says   string
	}{
		"unknown op":        {AgentChange{Op: "export", Slug: &slug, Summary: "x", ActorKind: "user", ActorID: "1000", OccurredAt: agentChangeAt}, "not an op"},
		"blank slug":        {AgentChange{Op: AgentOpCreate, Slug: &blank, Summary: "x", ActorKind: "user", ActorID: "1000", OccurredAt: agentChangeAt}, "is not the same as one that is absent"},
		"missing slug":      {AgentChange{Op: AgentOpEdit, Slug: nil, Summary: "x", ActorKind: "user", ActorID: "1000", OccurredAt: agentChangeAt}, "must name the slug"},
		"empty summary":     {AgentChange{Op: AgentOpCreate, Slug: &slug, Summary: "", ActorKind: "user", ActorID: "1000", OccurredAt: agentChangeAt}, "1 to 1024"},
		"over-long summary": {AgentChange{Op: AgentOpCreate, Slug: &slug, Summary: long, ActorKind: "user", ActorID: "1000", OccurredAt: agentChangeAt}, "1 to 1024"},
		"no kind":           {AgentChange{Op: AgentOpCreate, Slug: &slug, Summary: "x", ActorKind: "", ActorID: "1000", OccurredAt: agentChangeAt}, "actor kind"},
		"no when":           {AgentChange{Op: AgentOpCreate, Slug: &slug, Summary: "x", ActorKind: "user", ActorID: "1000", OccurredAt: ""}, "no when"},
		"nil transaction":   {AgentChange{Op: AgentOpCreate, Slug: &slug, Summary: "x", ActorKind: "user", ActorID: "1000", OccurredAt: agentChangeAt}, "the caller's transaction"},
	} {
		var err error
		if name == "nil transaction" {
			err = AgentChangeInsertTx(context.Background(), nil, c.change)
		} else {
			err = c.change.Validate()
		}
		if err == nil {
			t.Errorf("%s: accepted %q", name, c.change.Summary)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: said %q, which does not name %q", name, err, c.says)
		}
	}
}

// TestOnlyAMigrateRowMayCarryANullSlug walks the migration's
// `ck_agent_change_slug_names_a_row` from both sides: the run that has no single
// row to name gets through, and a create that tried the same trick is refused by
// name. This is the constraint the History tab's `every agent` row rests on, so
// it is tested against SQL and not against the Go validator that walks in front
// of it.
func TestOnlyAMigrateRowMayCarryANullSlug(t *testing.T) {
	s := openTemp(t)
	if _, err := s.w.Exec(`INSERT INTO agent_change (op, slug, summary, actor_kind, actor_id, occurred_at)
		VALUES ('migrate', NULL, 'migrate derived 0 rows from plugin_agent', 'user', '1000', ?)`, agentChangeAt); err != nil {
		t.Fatalf("the migrate run with a NULL slug should be statable: %v", err)
	}
	_, err := s.w.Exec(`INSERT INTO agent_change (op, slug, summary, actor_kind, actor_id, occurred_at)
		VALUES ('create', NULL, 'create nothing at all', 'user', '1000', ?)`, agentChangeAt)
	if err == nil {
		t.Fatal("a create with a NULL slug was accepted — the CHECK has no teeth")
	}
	if !strings.Contains(err.Error(), "ck_agent_change_slug_names_a_row") {
		t.Fatalf("the NULL-slug create failed for the wrong reason: %v", err)
	}
}

// TestAgentChangeSummariesAreTheStoreSentences pins the formatters, because the
// nine roads share them and a History tab renders these strings: a wording
// change is a user-visible change and should cost a test.
func TestAgentChangeSummariesAreTheStoreSentences(t *testing.T) {
	for _, c := range []struct {
		got, want string
	}{
		{AgentChangeSummaryCreate("groot", "Groot"), `create groot ("Groot")`},
		{AgentChangeSummaryDuplicate("groot", "groot-copy", "Groot Copy"), `duplicate groot as groot-copy ("Groot Copy")`},
		{AgentChangeSummaryEdit("groot"), "edit groot (9 content fields)"},
		{AgentChangeSummaryDelete("groot", ""), `delete groot (no name recorded)`},
		{AgentChangeSummaryImport("groot", "2", true, ""), "import groot v2 (created)"},
		{AgentChangeSummaryImport("groot", "2", false, "41"), "import groot v2 (updated, proposal 41)"},
		{AgentChangeSummaryMigrate(33), "migrate derived 33 rows from plugin_agent"},
		{AgentChangeSummarySeed("groot", "inserted"), "seed groot inserted"},
	} {
		if c.got != c.want {
			t.Errorf("summary is %q, want %q", c.got, c.want)
		}
	}
}
