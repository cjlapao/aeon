package daemon

import (
	"context"
	"database/sql"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// seedRepo is the bridge between the seed engine and the store (S43-extra
// T6, SD3): it implements seed.Repo over *store.Store, mapping the store's
// seed accessors to the engine's seam. It is mapping only — no domain rule.
//
// The engine's WithTx hands it back the *sql.Tx the store opened, boxed as a
// seed.Tx (see seedTx below): the *sql.Tx and the ledger/audit writes are one
// transaction, so a crash leaves the previous state intact (SD3). PutLedger
// and InsertEvent unwrap that seed.Tx back to the *sql.Tx and call the
// store's package-level SeedLedgerPutTx / SeedEventInsertTx, so no SQL lives
// in the daemon (the TestNoSQLLivesOutsideTheStore guard): the SQL is in the
// store, the daemon only adapts the type.
type seedRepo struct {
	store *store.Store
}

// NewSeedRepo builds the bridge over a store.
func NewSeedRepo(s *store.Store) *seedRepo { return &seedRepo{store: s} }

// WithTx opens one transaction on the store (the single writer) and runs fn
// over a seed.Tx that wraps the *sql.Tx. The *sql.Tx and the ledger/audit
// writes are one transaction (SD3).
func (r *seedRepo) WithTx(ctx context.Context, fn func(tx seed.Tx) error) error {
	return r.store.SeedWithTx(ctx, func(tx *sql.Tx) error {
		return fn(&seedTx{tx})
	})
}

// PutLedger writes one ledger row on the shared transaction — through the
// store's package-level SeedLedgerPutTx (the SQL lives in internal/store).
// The transaction is unwrapped from the seed.Tx the engine handed it.
func (r *seedRepo) PutLedger(tx seed.Tx, row seed.LedgerRow) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return store.SeedLedgerPutTx(sqlTx, store.SeedLedgerRow{
		SetID:          row.SetID,
		RecordKey:      row.RecordKey,
		AppliedVersion: row.AppliedVersion,
		AppliedHash:    row.AppliedHash,
		State:          row.State,
		AppliedAt:      row.AppliedAt,
		AppliedBy:      row.AppliedBy,
	})
}

// InsertEvent appends one audit row on the shared transaction — through the
// store's package-level SeedEventInsertTx (the SQL lives in internal/store).
func (r *seedRepo) InsertEvent(tx seed.Tx, ev seed.Event) error {
	sqlTx, err := unwrapSeedTx(tx)
	if err != nil {
		return err
	}
	return store.SeedEventInsertTx(sqlTx, store.SeedEvent{
		SetID:       ev.SetID,
		RecordKey:   ev.RecordKey,
		Op:          ev.Op,
		FromVersion: ev.FromVersion,
		ToVersion:   ev.ToVersion,
		Reason:      ev.Reason,
		Actor:       ev.Actor,
		OccurredAt:  ev.OccurredAt,
	})
}

// Ledger reads the store's seed ledger rows for a set (every set when
// setID is ""), mapped to the engine's view.
func (r *seedRepo) Ledger(ctx context.Context, setID string) ([]seed.LedgerRow, error) {
	rows, err := r.store.SeedLedgerList(ctx, setID)
	if err != nil {
		return nil, err
	}
	out := make([]seed.LedgerRow, len(rows))
	for i, row := range rows {
		out[i] = toLedgerRow(row)
	}
	return out, nil
}

// LedgerGet reads one ledger row, mapped to the engine's view.
func (r *seedRepo) LedgerGet(ctx context.Context, setID, key string) (seed.LedgerRow, bool, error) {
	row, ok, err := r.store.SeedLedgerGet(ctx, setID, key)
	if err != nil || !ok {
		return seed.LedgerRow{}, ok, err
	}
	return toLedgerRow(row), true, nil
}

// Events reads the store's seed audit rows for a set and key, mapped to the
// engine's view, newest first.
func (r *seedRepo) Events(ctx context.Context, setID, key string, limit int) ([]seed.Event, error) {
	rows, err := r.store.SeedEventList(ctx, setID, key, limit)
	if err != nil {
		return nil, err
	}
	out := make([]seed.Event, len(rows))
	for i, row := range rows {
		out[i] = toEvent(row)
	}
	return out, nil
}

// seedTx is the concrete seed.Tx the bridge hands the engine: it wraps the
// *sql.Tx the store opened, adapting the *sql.Tx's sql.Result / *sql.Rows
// returns to the engine's Result / Rows seam. A *sql.Tx does not itself
// satisfy seed.Tx (its ExecContext returns sql.Result, not Result), so the
// seam needs this adapter; it is the one place the two meet.
type seedTx struct{ *sql.Tx }

func (t *seedTx) ExecContext(ctx context.Context, query string, args ...any) (seed.Result, error) {
	r, err := t.Tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return resultAdapter{r}, nil
}

func (t *seedTx) QueryContext(ctx context.Context, query string, args ...any) (seed.Rows, error) {
	rows, err := t.Tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &rowsAdapter{rows}, nil
}

// resultAdapter adapts a *sql.Result to the engine's Result seam.
type resultAdapter struct{ sql.Result }

func (r resultAdapter) LastInsertId() (int64, error) { return r.Result.LastInsertId() }
func (r resultAdapter) RowsAffected() (int64, error)  { return r.Result.RowsAffected() }

// rowsAdapter adapts a *sql.Rows to the engine's Rows seam.
type rowsAdapter struct{ *sql.Rows }

func (r rowsAdapter) Next() bool              { return r.Rows.Next() }
func (r rowsAdapter) Scan(dest ...any) error  { return r.Rows.Scan(dest...) }
func (r rowsAdapter) Close() error           { return r.Rows.Close() }

// unwrapSeedTx unwraps the seed.Tx the engine handed back to the *sql.Tx
// that WithTx handed it. The engine only ever gets the tx from WithTx, so
// this is always a *seedTx; a different value is a bridge misuse, named.
func unwrapSeedTx(tx seed.Tx) (*sql.Tx, error) {
	if seedTx, ok := tx.(*seedTx); ok {
		return seedTx.Tx, nil
	}
	return nil, errTxIsNotATransaction
}

func toLedgerRow(row store.SeedLedgerRow) seed.LedgerRow {
	return seed.LedgerRow{
		SetID:          row.SetID,
		RecordKey:      row.RecordKey,
		AppliedVersion: row.AppliedVersion,
		AppliedHash:    row.AppliedHash,
		State:          row.State,
		AppliedAt:      row.AppliedAt,
		AppliedBy:      row.AppliedBy,
	}
}

func toEvent(row store.SeedEvent) seed.Event {
	return seed.Event{
		SetID:       row.SetID,
		RecordKey:   row.RecordKey,
		Op:          row.Op,
		FromVersion: row.FromVersion,
		ToVersion:   row.ToVersion,
		Reason:      row.Reason,
		Actor:      row.Actor,
		OccurredAt:  row.OccurredAt,
	}
}
