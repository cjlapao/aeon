package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// S45 T12 (PM15/PM16): the policy.origin column, its backfill, and the two
// transaction doors the declared policy.corpus seed set writes through.
// TestPolicyOriginMigration mirrors the policy_change migration test's
// shape — fresh apply AND upgrade of a pre-migration database — because
// the backfill only exists for the second one.

func TestPolicyOriginMigrationAddsColumnAndBackfillsTheCorpus(t *testing.T) {
	// Fresh apply: the column, its CHECK vocabulary, its index.
	s := openTemp(t)
	assertColumns(t, s, "policy", "origin")
	assertCatalogue(t, s, "index", "idx_policy_origin")

	// Upgrade path: a database seeded just BEFORE this migration — the
	// corpus rows exist as bare ids with no provenance column — then the
	// pending file runs and tells the truth about the rows it finds.
	dir := t.TempDir()
	path := dir + "/state.db"
	g, closeDB := rawGorm(t, path)
	// Vintage pinned BY IDENTITY — everything strictly before this
	// migration's own file — so a later slice appending a migration cannot
	// make this clause check a database that already carries the column
	// (`len-1` did exactly that when S55 appended files after this one;
	// the S22 T1 lesson).
	applyMigrations(t, g, migrationsBefore(t, "_policy_origin"))
	// Two legacy rows, inserted in the pre-origin shape (the column does
	// not exist yet): one corpus row the old CLI verb seeded, one rule an
	// operator authored by hand.
	for _, id := range []string{"corpus.memory-steward-loop.a1b2c3d4e5f6", "hand-rule"} {
		if err := g.Exec(`
			INSERT INTO policy (id, type, target_kind, target_id, action, spec, source, not_after, created_at, updated_at)
			VALUES (?, 'command_access', 'agent', 'someone', 'allow', '{"command":"ls*","state":"allow"}', 'operator', NULL, '2026-10-01T08:00:00Z', '2026-10-01T08:00:00Z')`,
			id).Error; err != nil {
			t.Fatalf("legacy seed row %s: %v", id, err)
		}
	}
	closeDB()

	s = openAt(t, dir)
	var origin string
	if err := s.r.QueryRow(`SELECT origin FROM policy WHERE id = 'corpus.memory-steward-loop.a1b2c3d4e5f6'`).Scan(&origin); err != nil {
		t.Fatalf("read the corpus row: %v", err)
	}
	if origin != "system" {
		t.Fatalf("the backfill left the corpus row at origin %q; the migration must say 'system' where the old verb's id prefix says it", origin)
	}
	if err := s.r.QueryRow(`SELECT origin FROM policy WHERE id = 'hand-rule'`).Scan(&origin); err != nil {
		t.Fatalf("read the hand row: %v", err)
	}
	if origin != "user" {
		t.Fatalf("the backfill left the hand-authored row at origin %q; everything not the corpus is the operator's", origin)
	}
}

// The column's OWN shape: NOT NULL, and the DEFAULT answers 'user' when no
// door names a provenance. The three-value vocabulary itself is the DOORS'
// (see TestPolicyAddTxCarriesOriginAndRefusesAnother for the write door and
// the boundary's ParseOrigin for the read doors) — deliberately not a DDL
// CHECK, so the policy table's five vocabulary CHECKs stay exactly the
// policy vocabulary (PM11; the frozen rebuilt-CHECK test pins them).
func TestPolicyOriginColumnIsNotNullWithUserDefault(t *testing.T) {
	s := openTemp(t)
	if _, err := s.w.Exec(`
		INSERT INTO policy (id, type, target_kind, target_id, action, spec, source, not_after, created_at, updated_at)
		VALUES ('unspoken', 'command_access', 'agent', 'someone', 'deny', '{}', 'operator', NULL, '2026-10-01T08:00:00Z', '2026-10-01T08:00:00Z')`); err != nil {
		t.Fatalf("insert without origin: %v", err)
	}
	var origin string
	if err := s.r.QueryRow(`SELECT origin FROM policy WHERE id = 'unspoken'`).Scan(&origin); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if origin != "user" {
		t.Fatalf("origin %q; the column's DEFAULT is the ctx door's honest answer", origin)
	}
	if _, err := s.w.Exec(`
		INSERT INTO policy (id, type, target_kind, target_id, action, spec, source, not_after, created_at, updated_at, origin)
		VALUES ('nameless', 'command_access', 'agent', 'someone', 'deny', '{}', 'operator', NULL, '2026-10-01T08:00:00Z', '2026-10-01T08:00:00Z', NULL)`); err == nil {
		t.Fatal("a NULL origin was stored; provenance is never unknown, only unspoken (which is 'user')")
	}
}

func TestPolicyAddTxCarriesOriginAndRefusesAnother(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	row := PolicyRow{
		ID: "seeded-row", Type: "command_access", TargetKind: "agent", TargetID: "someone",
		Action: "allow", Source: "operator", Subject: "agent:someone",
		SpecJSON: `{"command":"ls*","state":"allow"}`, CreatedAt: "2026-11-01T12:00:00Z",
		Origin: "system",
	}
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return s.PolicyAddTx(ctx, tx, row)
	}); err != nil {
		t.Fatalf("PolicyAddTx: %v", err)
	}
	var origin string
	if err := s.r.QueryRow(`SELECT origin FROM policy WHERE id = 'seeded-row'`).Scan(&origin); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if origin != "system" {
		t.Fatalf("origin %q; the transaction door is the one place the column is NAMED — that is its whole purpose", origin)
	}

	// A provenance outside the three is refused BEFORE the statement, by
	// name — the same fail-fast discipline the ctx door runs.
	row.ID = "bad-provenance"
	row.Origin = "machine"
	err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return s.PolicyAddTx(ctx, tx, row)
	})
	if err == nil || !strings.Contains(err.Error(), "machine") {
		t.Fatalf("err = %v; the refusal must name the origin it refused", err)
	}
	// The refused row is not half-written.
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM policy WHERE id = 'bad-provenance'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the refused row exists (n=%d, err=%v)", n, err)
	}

	// A nil transaction is the missing commit, refused rather than run.
	if err := s.PolicyAddTx(ctx, nil, row); err == nil {
		t.Fatal("PolicyAddTx accepted a nil transaction")
	}
}

func TestPolicySeedAuditTxWritesSeedRefusesEveryOtherOp(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	good := PolicyChange{
		Op: "seed", Summary: "applied the policy.corpus seed set: 2 rule(s) written, 0 left unchanged",
		ActorKind: "cli", ActorID: "cli", OccurredAt: "2026-11-01T12:00:00Z",
	}
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return s.PolicySeedAuditTx(ctx, tx, good)
	}); err != nil {
		t.Fatalf("PolicySeedAuditTx: %v", err)
	}
	rows, err := s.PolicyChangeList(ctx, "", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Op != "seed" {
		t.Fatalf("the seed audit row is not there: %+v", rows)
	}

	// The door writes ONE op. An add/remove/commit/reject through the seed
	// door would put the corpus applier's fingerprints on writes it did
	// not make (T3's audit discipline: each door signs only its own kind).
	for _, op := range []string{"add", "remove", "commit", "reject", "seeded", ""} {
		c := good
		c.Op = op
		if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
			return s.PolicySeedAuditTx(ctx, tx, c)
		}); err == nil {
			t.Fatalf("PolicySeedAuditTx accepted op %q; only 'seed' crosses this door", op)
		}
	}
	if err := s.PolicySeedAuditTx(ctx, nil, good); err == nil {
		t.Fatal("PolicySeedAuditTx accepted a nil transaction")
	}
}

func TestPolicyEventInsertTxGatesThenWrites(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.PolicyEventInsertTx(ctx, nil, "authored", "agent:someone", "any", "d", time.Now()); err == nil {
		t.Fatal("a nil transaction was accepted")
	}
	// The half-subject is refused before the statement, naming it.
	err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return s.PolicyEventInsertTx(ctx, tx, "authored", "noSplit", "any", "d", time.Now())
	})
	if err == nil || !strings.Contains(err.Error(), "noSplit") {
		t.Fatalf("err = %v; the refusal must name the subject it refused", err)
	}
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		// The event's policy_id is a real FK: author the rule first on the
		// same transaction — the exact pairing the seed door performs.
		if err := s.PolicyAddTx(ctx, tx, PolicyRow{
			ID: "rule-x", Type: "command_access", TargetKind: "agent", TargetID: "someone",
			Action: "allow", Source: "operator", Subject: "agent:someone",
			SpecJSON: `{"command":"ls*","state":"allow"}`, CreatedAt: "2026-11-01T12:00:00Z",
			Origin: "system",
		}); err != nil {
			return err
		}
		return s.PolicyEventInsertTx(ctx, tx, "authored", "agent:someone", "rule-x", "the detail", time.Now())
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	events, err := s.PolicyEventQuery(ctx, "agent:someone", 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(events) != 1 || events[0].PolicyID != "rule-x" {
		t.Fatalf("the tx-written event is not there: %+v", events)
	}
}
