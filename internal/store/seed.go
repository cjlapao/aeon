package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SeedLedgerRow is one row of the seed ledger: what the seed process applied
// for one record of one set, at which version, hash and by whom (SD3). The
// ledger is the engine's memory of versions and hashes so status can say
// what is out of date, removed or diverged. It carries no domain content.
type SeedLedgerRow struct {
	SetID          string
	RecordKey      string
	AppliedVersion int
	AppliedHash    string
	State          string
	AppliedAt      string
	AppliedBy      string
}

// SeedEvent is one row of the seed audit: an append-only record of an
// apply/update/skip/restore/retire/adopt and who made it (SD10). It is
// append-only by trigger (the setting_event precedent).
type SeedEvent struct {
	ID          int64
	SetID       string
	RecordKey   string
	Op          string
	FromVersion *int
	ToVersion   *int
	Reason      string
	Actor       string
	OccurredAt  string
}

// SeedLedgerList reads the ledger rows for a set (every set when setID is
// empty), ordered by set then key. It runs on the reader pool (O6/I6).
func (s *Store) SeedLedgerList(ctx context.Context, setID string) ([]SeedLedgerRow, error) {
	var (
		where string
		args  = []any{}
	)
	if setID != "" {
		where = "WHERE set_id = ?"
		args = append(args, setID)
	}
	rows, err := s.r.QueryContext(ctx, fmt.Sprintf(`
		SELECT set_id, record_key, applied_version, applied_hash, state, applied_at, applied_by
		FROM seed_ledger
		%s
		ORDER BY set_id, record_key`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list seed ledger: %w", err)
	}
	defer rows.Close()
	var out []SeedLedgerRow
	for rows.Next() {
		var r SeedLedgerRow
		if err := rows.Scan(&r.SetID, &r.RecordKey, &r.AppliedVersion, &r.AppliedHash, &r.State, &r.AppliedAt, &r.AppliedBy); err != nil {
			return nil, fmt.Errorf("store: list seed ledger: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list seed ledger: %w", err)
	}
	return out, nil
}

// SeedLedgerGet reads one ledger row; false is no row (not an error).
func (s *Store) SeedLedgerGet(ctx context.Context, setID, key string) (SeedLedgerRow, bool, error) {
	var r SeedLedgerRow
	err := s.r.QueryRowContext(ctx,
		`SELECT set_id, record_key, applied_version, applied_hash, state, applied_at, applied_by
		 FROM seed_ledger WHERE set_id = ? AND record_key = ?`, setID, key).
		Scan(&r.SetID, &r.RecordKey, &r.AppliedVersion, &r.AppliedHash, &r.State, &r.AppliedAt, &r.AppliedBy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SeedLedgerRow{}, false, nil
		}
		return SeedLedgerRow{}, false, fmt.Errorf("store: get seed ledger %q/%q: %w", setID, key, err)
	}
	return r, true, nil
}

// SeedWithTx opens one transaction on the single writer and runs fn over it,
// committing on success and rolling back on error. It is the shared
// transaction a domain Applier and the ledger/audit writers commit together
// on (SD3): a domain row and its ledger row are one transaction, so a crash
// leaves the previous state intact. It reuses the store's existing
// read-then-write idiom (s.w.BeginTx, DB15); it does not invent a second.
func (s *Store) SeedWithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: seed with tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: seed commit: %w", err)
	}
	return nil
}

// SeedLedgerPutTx upserts one ledger row on the transaction that owns it.
// It is a helper of SeedWithTx (the engine's Repo, T3/T6 call it there),
// so it takes the transaction, not the store. The upsert replaces an
// existing (set_id, record_key) row, so a re-apply never duplicates.
func SeedLedgerPutTx(tx *sql.Tx, row SeedLedgerRow) error {
	_, err := tx.Exec(
		`INSERT INTO seed_ledger (set_id, record_key, applied_version, applied_hash, state, applied_at, applied_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(set_id, record_key) DO UPDATE SET
			applied_version = excluded.applied_version,
			applied_hash    = excluded.applied_hash,
			state           = excluded.state,
			applied_at      = excluded.applied_at,
			applied_by      = excluded.applied_by`,
		row.SetID, row.RecordKey, row.AppliedVersion, row.AppliedHash, row.State, row.AppliedAt, row.AppliedBy)
	if err != nil {
		return fmt.Errorf("store: put seed ledger %q/%q: %w", row.SetID, row.RecordKey, err)
	}
	return nil
}

// SeedEventInsertTx appends one audit row on the transaction that owns it.
// It is the single writer of seed_event: there is no update or delete path
// (the trigger refuses both, SD3), so an append is the only mutation.
func SeedEventInsertTx(tx *sql.Tx, ev SeedEvent) error {
	_, err := tx.Exec(
		`INSERT INTO seed_event (set_id, record_key, op, from_version, to_version, reason, actor, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.SetID, ev.RecordKey, ev.Op, ev.FromVersion, ev.ToVersion, ev.Reason, ev.Actor, ev.OccurredAt)
	if err != nil {
		return fmt.Errorf("store: insert seed event %q/%q: %w", ev.SetID, ev.RecordKey, err)
	}
	return nil
}

// SeedEventList reads the audit rows for a set (and a record key, when
// given), newest first. It clamps limit to [1,500], defaulting to 50
// (the setting_event precedent).
func (s *Store) SeedEventList(ctx context.Context, setID, key string, limit int) ([]SeedEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var (
		where string
		args  = []any{}
	)
	if setID != "" {
		where += " AND set_id = ?"
		args = append(args, setID)
	}
	if key != "" {
		where += " AND record_key = ?"
		args = append(args, key)
	}
	args = append(args, limit)
	rows, err := s.r.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, set_id, record_key, op, from_version, to_version, reason, actor, occurred_at
		FROM seed_event
		WHERE 1=1 %s
		ORDER BY id DESC
		LIMIT ?`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list seed events: %w", err)
	}
	defer rows.Close()
	var out []SeedEvent
	for rows.Next() {
		var (
			ev   SeedEvent
			from sql.NullInt32
			to   sql.NullInt32
		)
		if err := rows.Scan(&ev.ID, &ev.SetID, &ev.RecordKey, &ev.Op, &from, &to, &ev.Reason, &ev.Actor, &ev.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: list seed events: scan: %w", err)
		}
		if from.Valid {
			v := int(from.Int32)
			ev.FromVersion = &v
		}
		if to.Valid {
			v := int(to.Int32)
			ev.ToVersion = &v
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list seed events: %w", err)
	}
	return out, nil
}
