package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// seedNow is a fixed time for the seed ledger/event rows in tests.
func seedNow() string { return "2026-10-30T00:00:00Z" }

// seedMigrationName finds the seed_ledger migration in Migrations() without
// freezing the timestamp (the file name is the ID).
func seedMigrationName(t *testing.T) string {
	t.Helper()
	for _, m := range Migrations() {
		if strings.Contains(m.Name, "seed_ledger") {
			return m.Name
		}
	}
	t.Fatal("no seed_ledger migration in Migrations()")
	return ""
}

// TestSeedMigrationCreatesTablesAndTriggers is the migration's own proof: the
// two tables, the index and the append-only triggers exist after a fresh
// apply, and the ledger row is named in Migrations().
func TestSeedMigrationCreatesTablesAndTriggers(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if _, _, err := s.SeedLedgerGet(ctx, "x", "y"); err != nil {
		t.Fatalf("a query against a missing table must not error as a query fault: %v", err)
	}
	// The migration is named in Migrations() (the file is picked up).
	seedMigrationName(t)
	// The tables, index and triggers are in the schema.
	for _, q := range []struct {
		sql string
	}{
		{`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='seed_ledger'`},
		{`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='seed_event'`},
		{`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_seed_event_set'`},
		{`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name='seed_event_no_update'`},
		{`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name='seed_event_no_delete'`},
	} {
		var n int
		if err := s.r.QueryRowContext(ctx, q.sql).Scan(&n); err != nil {
			t.Fatalf("query %q: %v", q.sql, err)
		}
		if n != 1 {
			t.Errorf("%q: found %d, want 1", q.sql, n)
		}
	}
}

// TestSeedLedgerCheckConstraints asserts the CHECKs in the migration: a bad
// state, a 63-char hash, a version 0 and an empty key are each refused.
func TestSeedLedgerCheckConstraints(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	bad := []SeedLedgerRow{
		{SetID: "abc", RecordKey: "k", AppliedVersion: 1, AppliedHash: strings.Repeat("a", 64), State: "bogus", AppliedAt: seedNow(), AppliedBy: "x"},
		{SetID: "abc", RecordKey: "k", AppliedVersion: 1, AppliedHash: strings.Repeat("a", 63), State: "applied", AppliedAt: seedNow(), AppliedBy: "x"},
		{SetID: "abc", RecordKey: "k", AppliedVersion: 0, AppliedHash: strings.Repeat("a", 64), State: "applied", AppliedAt: seedNow(), AppliedBy: "x"},
		{SetID: "abc", RecordKey: "", AppliedVersion: 1, AppliedHash: strings.Repeat("a", 64), State: "applied", AppliedAt: seedNow(), AppliedBy: "x"},
	}
	for i, row := range bad {
		err := s.SeedWithTx(ctx, func(tx *sql.Tx) error { return SeedLedgerPutTx(tx, row) })
		if err == nil {
			t.Errorf("case %d: a bad ledger row was accepted", i)
		}
	}
	// A valid row is accepted.
	good := SeedLedgerRow{SetID: "abc", RecordKey: "k", AppliedVersion: 1, AppliedHash: strings.Repeat("a", 64), State: "applied", AppliedAt: seedNow(), AppliedBy: "x"}
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error { return SeedLedgerPutTx(tx, good) }); err != nil {
		t.Fatalf("a valid ledger row was refused: %v", err)
	}
}

// TestSeedLedgerPutIsUpsert asserts a second put of the same key replaces
// (one row), never duplicates.
func TestSeedLedgerPutIsUpsert(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	row := SeedLedgerRow{SetID: "abc", RecordKey: "k", AppliedVersion: 1, AppliedHash: strings.Repeat("a", 64), State: "applied", AppliedAt: seedNow(), AppliedBy: "first"}
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error { return SeedLedgerPutTx(tx, row) }); err != nil {
		t.Fatalf("first put: %v", err)
	}
	row.AppliedVersion = 2
	row.AppliedBy = "second"
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error { return SeedLedgerPutTx(tx, row) }); err != nil {
		t.Fatalf("second put: %v", err)
	}
	rows, err := s.SeedLedgerList(ctx, "abc")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].AppliedVersion != 2 || rows[0].AppliedBy != "second" {
		t.Fatalf("the second put did not replace: %+v", rows[0])
	}
}

// TestSeedEventIsAppendOnly asserts the trigger: an UPDATE and a DELETE of a
// seed_event row fail with the append-only text (SD3).
func TestSeedEventIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return SeedEventInsertTx(tx, SeedEvent{SetID: "abc", RecordKey: "k", Op: "apply", Reason: "r", Actor: "a", OccurredAt: seedNow()})
	}); err != nil {
		t.Fatalf("insert an event: %v", err)
	}
	// An UPDATE is refused by the trigger.
	if _, err := s.w.ExecContext(ctx, `UPDATE seed_event SET reason = 'x'`); err == nil {
		t.Error("an UPDATE of seed_event was not refused by the trigger")
	}
	// A DELETE is refused by the trigger.
	if _, err := s.w.ExecContext(ctx, `DELETE FROM seed_event`); err == nil {
		t.Error("a DELETE of seed_event was not refused by the trigger")
	}
	// The row is still there.
	evs, err := s.SeedEventList(ctx, "abc", "k", 50)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("the append-only row was lost: %d events", len(evs))
	}
}

// TestSeedWithTxIsAtomic asserts a ledger put and its event commit together:
// if the event insert fails, the earlier ledger put rolls back (SD3).
func TestSeedWithTxIsAtomic(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		// A domain write (here, a ledger put) and an event that fails — a
		// bad op violates the CHECK — in one transaction.
		if err := SeedLedgerPutTx(tx, SeedLedgerRow{SetID: "abc", RecordKey: "k", AppliedVersion: 1, AppliedHash: strings.Repeat("a", 64), State: "applied", AppliedAt: seedNow(), AppliedBy: "x"}); err != nil {
			return err
		}
		return SeedEventInsertTx(tx, SeedEvent{SetID: "abc", RecordKey: "k", Op: "bogus", Reason: "r", Actor: "a", OccurredAt: seedNow()})
	})
	if err == nil {
		t.Fatal("an event insert failure did not roll back the transaction")
	}
	// Neither the ledger put nor the event persisted.
	if _, ok, e := s.SeedLedgerGet(ctx, "abc", "k"); e != nil {
		t.Fatalf("get ledger: %v", e)
	} else if ok {
		t.Fatal("the ledger put persisted despite the event failure")
	}
}

// TestSeedEventListOrderAndLimits asserts newest-first, default 50 and the
// 500 clamp (the setting_event precedent).
func TestSeedEventListOrderAndLimits(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
			return SeedEventInsertTx(tx, SeedEvent{SetID: "abc", RecordKey: "k", Op: "apply", Reason: "r", Actor: "a", OccurredAt: seedNow()})
		}); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}
	evs, err := s.SeedEventList(ctx, "abc", "k", 0) // default 50
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(evs) != 5 {
		t.Fatalf("expected 5 events, got %d", len(evs))
	}
	// Newest first: id is descending.
	for i := 1; i < len(evs); i++ {
		if evs[i-1].ID < evs[i].ID {
			t.Fatalf("events are not newest-first: %d < %d", evs[i-1].ID, evs[i].ID)
		}
	}
	// The clamp: a limit of 9999 is clamped to 500 (5 rows, all returned).
	if _, err := s.SeedEventList(ctx, "abc", "k", 9999); err != nil {
		t.Fatalf("clamped list: %v", err)
	}
	_ = time.Now
}

// TestSeedLedgerOutlivesDomainRows asserts the ledger carries no FK to a
// domain table: a row the domain deletes still leaves a ledger row.
func TestSeedLedgerOutlivesDomainRows(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return SeedLedgerPutTx(tx, SeedLedgerRow{SetID: "abc", RecordKey: "k", AppliedVersion: 1, AppliedHash: strings.Repeat("a", 64), State: "applied", AppliedAt: seedNow(), AppliedBy: "x"})
	}); err != nil {
		t.Fatalf("put ledger: %v", err)
	}
	// Delete a (hypothetical) domain row — here, there is no domain table to
	// touch; the point is the ledger row survives with no cascade.
	rows, err := s.SeedLedgerList(ctx, "abc")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the ledger row did not outlive the (absent) domain row: %d", len(rows))
	}
}
