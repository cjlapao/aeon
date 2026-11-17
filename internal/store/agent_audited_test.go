package store

// agent_audited_test.go — S73 T3 (AR2, AR4): the six doors write a row AND a
// ledger row, or they write nothing, and the ledger row names the actor the
// caller handed over — byte for byte, never one the store invented.
//
// The transaction is the claim, so it is tested by breaking the second write.
// Every one-transaction proof here works the same way the bug plane's does
// (bug_change_test.go's TestBugAuditAndWriteAreOneTransaction, which this
// file's first test is the twin of): make the ledger insertion fail, then read
// the database back and insist the row did not move. A test that only checks
// the happy path proves the two writes are sequential, which is not the claim.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// ledgerRows reads the ledger for one slug (nil slug = the whole ledger, which
// is how a migrate run's row is found — AR1 gives it no slug to match on).
//
// It is raw SQL on purpose: the ledger's own read is `agent.changes`, and T5
// builds it. A T3 test that used T5's reader to prove T3's writes would let a
// bug in the reader hide a bug in the writer.
func ledgerRows(t *testing.T, s *Store, slug *string) []AgentChange {
	t.Helper()
	q := `SELECT id, op, slug, summary, actor_kind, actor_id, occurred_at FROM agent_change`
	args := []any{}
	if slug != nil {
		q += ` WHERE slug = ?`
		args = append(args, *slug)
	}
	q += ` ORDER BY id`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	defer rows.Close()
	var out []AgentChange
	for rows.Next() {
		var c AgentChange
		if err := rows.Scan(&c.ID, &c.Op, &c.Slug, &c.Summary, &c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			t.Fatalf("scan the ledger: %v", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("walk the ledger: %v", err)
	}
	return out
}

// countAgents / countRows are the two counters every refusal test needs: a
// refusal that leaves the counts alone is a refusal that wrote nothing.
func countAgents(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM agent`).Scan(&n); err != nil {
		t.Fatalf("count the agents: %v", err)
	}
	return n
}

func countLedger(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM agent_change`).Scan(&n); err != nil {
		t.Fatalf("count the ledger: %v", err)
	}
	return n
}

// TestAgentAuditAndWriteAreOneTransaction is AR2 at its sharpest: the row write
// succeeds, the ledger write is refused (a summary past the CHECK's 1024), and
// the database must show NEITHER.
func TestAgentAuditAndWriteAreOneTransaction(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	// The row write has to succeed and the ledger write has to fail, inside one
	// transaction. The doors validate the ledger row in Go before SQL, so the
	// only honest way to make the second statement fail after the first has run
	// is to drive the pair by hand: the primitive writes the row, then the
	// ledger INSERT is aimed at the database's own CHECK with an actor kind the
	// vocabulary does not hold. That is precisely the mid-transaction failure
	// AR2 exists for — the row must go back with it.
	err := s.AgentWithTx(ctx, func(tx *sql.Tx) error {
		if err := createAgentTx(ctx, tx, Agent{Slug: "atomic-a", Name: "Atomic A", Version: "1"}, agentChangeAt); err != nil {
			return err
		}
		// The ledger write dies on its own CHECK: an actor kind outside the
		// vocabulary, inserted straight past Go's Validate. That is exactly the
		// mid-transaction failure AR2 must survive.
		_, err := tx.ExecContext(ctx, `INSERT INTO agent_change (op, slug, summary, actor_kind, actor_id, occurred_at)
			VALUES ('create', 'atomic-a', 'create atomic-a', 'agent', 'groot', ?)`, agentChangeAt)
		return err
	})
	if err == nil {
		t.Fatal("a ledger row with an unlawful actor kind was accepted — the CHECK has no teeth")
	}
	if !strings.Contains(err.Error(), "actor_kind") {
		t.Fatalf("the transaction failed for the wrong reason: %v", err)
	}
	if n := countAgents(t, s); n != 0 {
		t.Fatalf("the agent row survived its ledger's failure (%d rows) — the transaction is not one", n)
	}
	if n := countLedger(t, s); n != 0 {
		t.Fatalf("the failed write left %d ledger rows, want 0", n)
	}
}

// TestAnUnusableActorWritesNothingBeforeItRefuses is AR3's ordering claim at the
// door: `actor.Valid()` runs before the transaction opens, so a name the ledger
// cannot hold never costs a statement, and no ledger row can exist to be wrong.
func TestAnUnusableActorWritesNothingBeforeItRefuses(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	before := countAgents(t, s)
	for name, actor := range map[string]AgentActor{
		"no kind":  {Kind: "", ID: "1000"},
		"agent":    {Kind: "agent", ID: "groot"},
		"no id":    {Kind: "user", ID: ""},
		"uid zero": {Kind: "user", ID: "0"},
	} {
		if err := s.CreateAgentAudited(ctx, actor, Agent{Slug: "ghost-" + name, Name: "Ghost", Version: "1"}, agentChangeAt); err == nil {
			t.Errorf("%s: the door accepted %#v", name, actor)
		}
	}
	if after := countAgents(t, s); after != before {
		t.Fatalf("unusable actors wrote %d rows", after-before)
	}
	if n := countLedger(t, s); n != 0 {
		t.Fatalf("the refusals left %d ledger rows, want 0", n)
	}
}

// TestEachDoorMintsOneRowWithItsOwnOpAndSummary is AR1/AR2 walked over all six
// doors: one write, one ledger row, the right op, the store's own sentence, the
// caller's stamp.
func TestEachDoorMintsOneRowWithItsOwnOpAndSummary(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	created := Agent{Slug: "door-a", Name: "Door A", Version: "1"}
	if err := s.CreateAgentAudited(ctx, userAgentActor, created, agentChangeAt); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.DuplicateAgentAudited(ctx, userAgentActor, created, "Door A Copy", agentChangeAt); err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	edited, found, err := s.AgentBySlug("door-a")
	if err != nil || !found {
		t.Fatalf("re-read door-a: found=%v err=%v", found, err)
	}
	edited.Prompt = "changed"
	if n, err := s.UpdateAgentAudited(ctx, userAgentActor, edited, agentChangeAt); err != nil || n != 1 {
		t.Fatalf("edit: n=%d err=%v", n, err)
	}
	imp := Agent{Slug: "imported-one", Name: "Imported One", Version: "1", Signed: false}
	res, err := s.ImportAgentCardAudited(ctx, userAgentActor, imp)
	if err != nil || res != ImportResultCreated {
		t.Fatalf("import: res=%v err=%v", res, err)
	}
	if err := s.DeleteAgentAudited(ctx, userAgentActor, "imported-one", agentChangeAt); err != nil {
		t.Fatalf("delete: %v", err)
	}

	want := []struct {
		op, summary string
	}{
		{AgentOpCreate, `create door-a ("Door A")`},
		{AgentOpDuplicate, `duplicate door-a as door-a-copy ("Door A Copy")`},
		{AgentOpEdit, "edit door-a (9 content fields)"},
		{AgentOpImport, "import imported-one v1 (created)"},
		{AgentOpDelete, `delete imported-one ("Imported One")`},
	}
	got := ledgerRows(t, s, nil)
	if len(got) != len(want) {
		for _, g := range got {
			t.Logf("ledger row %d: op=%s slug=%v summary=%s", g.ID, g.Op, g.Slug, g.Summary)
		}
		t.Fatalf("the five writes left %d ledger rows, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Op != w.op {
			t.Errorf("row %d has op %q, want %q", i+1, got[i].Op, w.op)
		}
		if got[i].Summary != w.summary {
			t.Errorf("row %d has summary %q, want %q", i+1, got[i].Summary, w.summary)
		}
		if got[i].ActorKind != AgentActorUser || got[i].ActorID != "1000" {
			t.Errorf("row %d is stamped %s:%s, want user:1000", i+1, got[i].ActorKind, got[i].ActorID)
		}
		// The import row carries the STORE's stamp, not the caller's, because
		// ImportAgentCardAudited is the one door whose time is decided inside
		// it — the version comparison that makes the write a create or an
		// update also decides when, and the row's own created_at came from the
		// same clock before S73. Every other door is handed the moment.
		if want[i].op == AgentOpImport {
			if strings.TrimSpace(got[i].OccurredAt) == "" {
				t.Errorf("row %d (import) has no stamp at all", i+1)
			}
			continue
		}
		if got[i].OccurredAt != agentChangeAt {
			t.Errorf("row %d is stamped %q, want the caller's stamp %q", i+1, got[i].OccurredAt, agentChangeAt)
		}
	}
}

// TestTheDoorsStampTheActorTheyWereGiven is the byte-for-byte half of AR4: the
// three lawful kinds each land as what the caller said, because the store does
// not translate an identity it was handed.
func TestTheDoorsStampTheActorTheyWereGiven(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for i, actor := range []AgentActor{
		{Kind: AgentActorUser, ID: "1000"},
		cliActor,
		seedTestActor,
	} {
		slug := fmt.Sprintf("stamped-%d", i)
		if err := s.CreateAgentAudited(ctx, actor, Agent{Slug: slug, Name: slug, Version: "1"}, agentChangeAt); err != nil {
			t.Fatalf("create %s: %v", slug, err)
		}
		rows := ledgerRows(t, s, SlugPtr(slug))
		if len(rows) != 1 {
			t.Fatalf("%s left %d ledger rows", slug, len(rows))
		}
		if rows[0].Actor() != actor {
			t.Errorf("%s: the ledger says %s, the caller said %s", slug, rows[0].Actor(), actor)
		}
	}
}

// TestDeleteMintsALedgerRowNamingAGoneSlug is why there is no foreign key
// (AR1): the delete entry is a statement about a slug the table no longer holds,
// and it carries the name because after this row nothing else remembers it.
func TestDeleteMintsALedgerRowNamingAGoneSlug(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.CreateAgentAudited(ctx, userAgentActor, Agent{Slug: "going-away", Name: "Going Away", Version: "1"}, agentChangeAt); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.DeleteAgentAudited(ctx, userAgentActor, "going-away", agentChangeAt); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var left int
	if err := s.r.QueryRow(`SELECT count(*) FROM agent WHERE slug = 'going-away'`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("the row is still in the table (count %d, err %v)", left, err)
	}
	rows := ledgerRows(t, s, SlugPtr("going-away"))
	if len(rows) != 2 || rows[1].Op != AgentOpDelete {
		t.Fatalf("the ledger holds %v, want a create then a delete", summarize(rows))
	}
	if rows[1].Summary != `delete going-away ("Going Away")` {
		t.Errorf("the delete row says %q — the name is the last thing the database will hold about this agent", rows[1].Summary)
	}
	// And a delete of something that is not there refuses, naming it, with no
	// ledger entry about nothing.
	if err := s.DeleteAgentAudited(ctx, userAgentActor, "never-was", agentChangeAt); err == nil ||
		!strings.Contains(err.Error(), "never-was has no row to delete") {
		t.Fatalf("deleting an absent slug said %v", err)
	}
	if n := countLedger(t, s); n != 2 {
		t.Fatalf("the absent-slug refusal added ledger rows: %d", n)
	}
}

// TestImportMintsNothingForANoOpOrALowerVersion is AM3 meeting AR1: the ledger
// records changes, and neither a repeated (slug, version) nor a refused
// downgrade changed anything.
func TestImportMintsNothingForANoOpOrALowerVersion(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	card := Agent{Slug: "card-one", Name: "Card One", Version: "2"}
	res, err := s.ImportAgentCardAudited(ctx, userAgentActor, card)
	if err != nil || res != ImportResultCreated {
		t.Fatalf("first import: res=%v err=%v", res, err)
	}
	if n := countLedger(t, s); n != 1 {
		t.Fatalf("the create imported %d ledger rows", n)
	}
	// The same (slug, version): a no-op (AM3), and no ledger row.
	res, err = s.ImportAgentCardAudited(ctx, userAgentActor, card)
	if err != nil || res != ImportResultNoOp {
		t.Fatalf("repeated import: res=%v err=%v", res, err)
	}
	if n := countLedger(t, s); n != 1 {
		t.Fatalf("a no-op import minted a ledger row (now %d) — a no-op is not a change", n)
	}
	// A lower version: refused and named, still one row.
	lower := card
	lower.Version = "1"
	if _, err := s.ImportAgentCardAudited(ctx, userAgentActor, lower); err == nil ||
		!strings.Contains(err.Error(), "lower than the stored") {
		t.Fatalf("a lower version said %v", err)
	}
	if n := countLedger(t, s); n != 1 {
		t.Fatalf("a refused import minted a ledger row (now %d)", n)
	}
	// A higher version: an update in place, and the ledger's second row.
	higher := card
	higher.Version = "3"
	res, err = s.ImportAgentCardAudited(ctx, userAgentActor, higher)
	if err != nil || res != ImportResultUpdated {
		t.Fatalf("higher import: res=%v err=%v", res, err)
	}
	rows := ledgerRows(t, s, SlugPtr("card-one"))
	if len(rows) != 2 || rows[1].Summary != "import card-one v3 (updated)" {
		t.Fatalf("the ledger holds %v", summarize(rows))
	}
	var versions int
	if err := s.r.QueryRow(`SELECT count(*) FROM agent WHERE slug = 'card-one'`).Scan(&versions); err != nil || versions != 1 {
		t.Fatalf("AM3 says one evolving row; the table holds %d (err %v)", versions, err)
	}
}

// TestUpdateOfAnAbsentRowMintsNoLedgerRow keeps AR2's other edge: zero rows
// affected is not a change, whatever the caller intended.
func TestUpdateOfAnAbsentRowMintsNoLedgerRow(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	n, err := s.UpdateAgentAudited(ctx, userAgentActor, Agent{ID: 9999, Slug: "phantom"}, agentChangeAt)
	if err != nil || n != 0 {
		t.Fatalf("update of an absent row: n=%d err=%v", n, err)
	}
	if got := countLedger(t, s); got != 0 {
		t.Fatalf("a write that touched nothing minted %d ledger rows", got)
	}
}

// TestMigrateMintsOneRowForTheWholeRun is AR4's sentence about the run: one
// ledger row, no slug, the count of what it derived — not one row per agent.
func TestMigrateMintsOneRowForTheWholeRun(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	plantPluginAgents(t, s, "Alpha Migrate", "Bravo Migrate", "Charlie Migrate")
	created, err := s.MigrateFirstPartyAgentsAudited(ctx, userAgentActor, agentChangeAt)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if created != 3 {
		t.Fatalf("the run derived %d rows, want 3", created)
	}
	rows := ledgerRows(t, s, nil)
	if len(rows) != 1 {
		t.Fatalf("a run of three derived agents left %d ledger rows, want 1", len(rows))
	}
	if rows[0].Op != AgentOpMigrate || rows[0].Slug != nil {
		t.Fatalf("the run's row is op=%q slug=%v, want migrate with no slug", rows[0].Op, rows[0].Slug)
	}
	if rows[0].Summary != "migrate derived 3 rows from plugin_agent" {
		t.Errorf("the run says %q", rows[0].Summary)
	}
	// Idempotence: a re-run derives nothing and still mints one row — the RUN
	// happened, and a run of zero is a fact the ledger may state.
	created, err = s.MigrateFirstPartyAgentsAudited(ctx, userAgentActor, agentChangeAt)
	if err != nil || created != 0 {
		t.Fatalf("re-run: created=%d err=%v", created, err)
	}
	if rows := ledgerRows(t, s, nil); len(rows) != 2 || rows[1].Summary != "migrate derived 0 rows from plugin_agent" {
		t.Fatalf("the re-run's ledger row: %v", summarize(rows))
	}
}

// TestAMigrateRefusalRollsBackTheWholeRun is the behavioural change S73 T3 made
// when the run became a door (see its comment): the rows and the refusal cannot
// coexist. Before S73 a mid-run refusal left the rows already derived; now the
// run is one transaction, so an un-slugifiable name at the end of the list
// leaves nothing behind.
func TestAMigrateRefusalRollsBackTheWholeRun(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	plantPluginAgents(t, s, "First Fine", "Second Fine", "!!!")
	before := countAgents(t, s)
	created, err := s.MigrateFirstPartyAgentsAudited(ctx, userAgentActor, agentChangeAt)
	if err == nil || !strings.Contains(err.Error(), "slugifies to nothing") {
		t.Fatalf("a name that slugifies to nothing said %v", err)
	}
	if after := countAgents(t, s); after != before {
		t.Fatalf("the refusal left %d agent rows behind (was %d, reported %d created) — the run is not one transaction",
			after, before, created)
	}
	if n := countLedger(t, s); n != 0 {
		t.Fatalf("a refused run minted %d ledger rows", n)
	}
}

// plantPluginAgents lays plugin_agent lifecycle rows for the migrate tests. The
// migrate run reads this table, not the agent table, which is the whole point of
// it (S43's back-fill).
func plantPluginAgents(t *testing.T, s *Store, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := s.w.Exec(`INSERT INTO plugin_agent (name, source, state, imported_at) VALUES (?, 'test', 'disabled', ?)`,
			n, agentChangeAt); err != nil {
			t.Fatalf("plant plugin_agent %q: %v", n, err)
		}
	}
}

// summarize renders ledger rows for a Fatalf message, so a failure reads as
// sentences rather than as pointers.
func summarize(rows []AgentChange) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		slug := "every agent"
		if r.Slug != nil {
			slug = *r.Slug
		}
		out = append(out, fmt.Sprintf("%s/%s: %s", r.Op, slug, r.Summary))
	}
	return out
}

// seedAgentRowBeforeS73 lays an agent row on a store migrated only to an OLD
// vintage — the closeout drills (S51's bug upgrade, S52's memory upgrade), where
// `agent_change` does not exist yet and no audited door can open. A raw INSERT is
// the honest shape for "the database as it was before the ledger": the drill
// exists to prove an upgrade carries the rows forward, and a row written through
// S73's own door into a pre-S73 schema would be a fixture that could never have
// existed. The columns left out are the ones the table defaults.
func seedAgentRowBeforeS73(t *testing.T, s *Store, slug, name, at string) {
	t.Helper()
	if _, err := s.w.Exec(`INSERT INTO agent (slug, name, version, created_at, updated_at) VALUES (?, ?, '1', ?, ?)`,
		slug, name, at, at); err != nil {
		t.Fatalf("seed the pre-S73 agent row %q: %v", slug, err)
	}
}
