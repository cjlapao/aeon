package agentseed

// ledger_test.go — S73 T4 path 9: the seed engine's own stamp on every apply
// path, and nothing at all on a road that wrote nothing.
//
// # Why this file is in the applier's package
//
// AR4's row 9 is the only road in the family whose actor is not a person, and the
// only one that does not open its own transaction: the seed engine owns the wider
// unit — content row, ledger row and the seed ledger all commit on the caller's
// tx — so the applier is the last place that can still say who it is. `mcpseed`
// established the shape (`applier.go:295-301`); this is the agents plane agreeing
// to it. The claim has three parts and a test each: insert, update and retire all
// stamp `system:seed-engine`; a refusal stamps nothing; and a rolled-back apply
// leaves no ledger row, because a ledger that outlives the transaction it
// describes would be recording a change that did not happen.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// seedLedgerRow is one `agent_change` row, read raw: this package may not call
// the door it is testing, and `agent.changes` is T5's read and not yet built.
type seedLedgerRow struct {
	Op        string
	Slug      sql.NullString
	Summary   string
	ActorKind string
	ActorID   string
}

func seedLedgerRows(t *testing.T, s *store.Store) []seedLedgerRow {
	t.Helper()
	var out []seedLedgerRow
	const q = `SELECT op, slug, summary, actor_kind, actor_id FROM agent_change ORDER BY id`
	if err := s.GormDB().Raw(q).Scan(&out).Error; err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	return out
}

// TestEverySeedApplyPathStampsTheEngine walks the three writers of the seed
// plane — insert, update, retire — and asserts each minted exactly one row, on
// the transaction the engine was already using, naming the engine.
func TestEverySeedApplyPathStampsTheEngine(t *testing.T) {
	s := openStore(t)
	ap := NewApplier(s)
	recs := Source()
	if len(recs) < 2 {
		t.Fatalf("the set has %d records; the test needs two to separate the paths", len(recs))
	}
	first, second := recs[0], recs[1]

	// Insert: two fresh records, one apply each.
	for _, rec := range []seed.Record{first, second} {
		if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
			return ap.Upsert(context.Background(), tx, rec, false)
		}); err != nil {
			t.Fatalf("apply %s: %v", rec.Key, err)
		}
	}
	rows := seedLedgerRows(t, s)
	if len(rows) != 2 {
		t.Fatalf("two inserts minted %d ledger rows, want 2: %+v", len(rows), rows)
	}
	for _, r := range rows {
		assertEngine(t, r, "inserted")
	}

	// Update: re-apply the first record over its own row.
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		return ap.Upsert(context.Background(), tx, first, false)
	}); err != nil {
		t.Fatalf("re-apply %s: %v", first.Key, err)
	}
	rows = seedLedgerRows(t, s)
	if len(rows) != 3 {
		t.Fatalf("after an update the ledger holds %d rows, want 3: %+v", len(rows), rows)
	}
	assertEngine(t, rows[2], "updated")

	// Retire: the third writer, the one AR4's table did not list (deviation 12).
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		return ap.Retire(context.Background(), tx, first.Key)
	}); err != nil {
		t.Fatalf("retire %s: %v", first.Key, err)
	}
	rows = seedLedgerRows(t, s)
	if len(rows) != 4 {
		t.Fatalf("after a retire the ledger holds %d rows, want 4 — SeedAgentRetire is a content writer and AR2 covers it", len(rows))
	}
	assertEngine(t, rows[3], "retired")

	// Retiring something that is not there is not a change: no row, no error.
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		return ap.Retire(context.Background(), tx, "no-such-agent-anywhere")
	}); err != nil {
		t.Fatalf("retiring an absent key: %v", err)
	}
	if got := seedLedgerRows(t, s); len(got) != 4 {
		t.Fatalf("retiring an absent key minted a row — %d rows now; nothing changed, so nothing is recorded", len(got))
	}
}

// TestARefusedSeedApplyMintsNothing is AR2's other half at the seed plane: a road
// that wrote nothing records nothing. Two ways to write nothing — refuse before
// the store is touched (undecodable content), and write then roll back (the
// engine's all-or-nothing, which is why the ledger rides the caller's tx).
func TestARefusedSeedApplyMintsNothing(t *testing.T) {
	s := openStore(t)
	ap := NewApplier(s)
	before := seedLedgerRows(t, s)

	bad := seed.Record{Key: "broken-record", Version: 1, Content: []byte(`{this is not json`)}
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		return ap.Upsert(context.Background(), tx, bad, false)
	}); err == nil {
		t.Fatal("undecodable content was accepted — the refusal is the point")
	}
	if got := seedLedgerRows(t, s); len(got) != len(before) {
		t.Fatalf("a refused apply minted %d ledger rows: %+v", len(got)-len(before), got[len(before):])
	}

	// The rollback: a real apply inside a transaction the caller aborts. The
	// ledger row rode that transaction, so it vanished with the row — the only
	// shape that keeps the ledger honest about a failed apply.
	rec := Source()[0]
	rollback := errors.New("the test rolls this transaction back")
	if err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		if err := ap.Upsert(context.Background(), tx, rec, false); err != nil {
			return err
		}
		return rollback
	}); err == nil {
		t.Fatal("the rollback did not propagate")
	}
	if got := seedLedgerRows(t, s); len(got) != len(before) {
		t.Fatalf("a rolled-back apply left %d ledger rows — the pair commits together or not at all", len(got)-len(before))
	}
	if _, found, err := s.AgentBySlug(slugOf(rec)); err != nil {
		t.Fatalf("read back the rolled-back row: %v", err)
	} else if found {
		t.Fatal("the rolled-back apply left its content row too")
	}
}

// --- helpers ------------------------------------------------------------------

func assertEngine(t *testing.T, r seedLedgerRow, verb string) {
	t.Helper()
	if r.ActorKind != "system" || r.ActorID != "seed-engine" {
		t.Errorf("the ledger row names %s:%s, want system:seed-engine — the engine stamps itself and never a person it did not see",
			r.ActorKind, r.ActorID)
	}
	if r.Op != "seed" {
		t.Errorf("op %q, want seed — the seven ops are a CHECK vocabulary, and a new one is a migration", r.Op)
	}
	if !r.Slug.Valid || r.Slug.String == "" {
		t.Errorf("a seed row with no slug (summary %q) — unlike a migrate run, a seed apply names one agent", r.Summary)
	}
	if !strings.Contains(r.Summary, verb) {
		t.Errorf("summary %q does not say %q — the formatters keep the three seed paths from writing three dialects",
			r.Summary, verb)
	}
}

// slugOf reads the slug out of the record's own content, because that is what the
// applier keys on; the record's Key is the set's identity, and the two agreeing
// is Source()'s job, not this test's assumption.
func slugOf(rec seed.Record) string {
	var c struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rec.Content, &c); err != nil || c.Slug == "" {
		return rec.Key
	}
	return c.Slug
}
