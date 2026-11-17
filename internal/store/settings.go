package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ════════════════════════════════════════════════════════════════════════════
// The `setting` and `setting_event` tables — the SQL text, and nothing but
// the SQL text (I2).
//
// # The split this file exists to honour
//
// Aeon's invariant I2 (enforced by TestNoSQLLivesOutsideTheStore) keeps every
// statement in this package, so the domain rule — the closed key registry and
// its typing (CS3) — lives one layer UP, in internal/config, which holds no
// SQL at all and imports nothing here (it talks to a Repo seam, CS2). This
// file is the infrastructure that rule stands on:
//
//	internal/config/settings*.go   the registry + the write boundary + typing
//	                               (CS3/CS5) — the ONE place a value is typed
//	                               and validated before it reaches the table
//	           │  Repo seam (SettingRow / SettingEvent — the bridge is the
//	           │  daemon adapter's job, T4)
//	           ▼
//	internal/store/settings.go      SettingGet · SettingList · SettingPut ·
//	                                SettingDelete · SettingEventList — here
//
// The CHECK on key is a NET, not the validator (CS3): the registry owns the
// real key grammar and the good message, and the CHECK only keeps a
// skip-the-boundary caller from writing '' , a key with a space, or a..b. The
// value cap (4096) is the net for the registry's default string cap; a string
// key with a smaller MaxLen enforces its own cap in Go. A caller who skipped
// the boundary still cannot write a malformed key or an over-long value — the
// database refuses it — but the good message stays upstairs (the same division
// policy.go's header documents for the policy vocabulary).
//
// # What this layer does NOT do
//
//   - No key grammar beyond the CHECK net, and no "is this key declared". The
//     registry (CS3) is the only home for the closed key set; a second check
//     here would be a second place for it to drift.
//   - No scope (CS6): the primary key is `key` alone, and there is no scope
//     column — a column nothing can fill is the mistake S33 TL5 named.
//   - No typing or a Sensitive column on `setting`. Typing (CS3) and
//     sensitivity (CS8) are registry facts, not row facts; the event row's
//     `sensitive` is a historical record of what the registry said at write
//     time, not a live join.
//
// # The two NULL rules, and the one transaction
//
//   - setting_event.old_value is NULL — not "" — when there was no stored row
//     (the default was in force). A second "no value" (the empty string) would
//     be one forgotten-ignore away from reading a default as a stored blank.
//   - setting_event.new_value is NULL on an `unset` (the CHECK pairs op with
//     new_value), so the two NULLs name two different facts.
//
// SettingPut and SettingDelete each do their read-then-write inside ONE
// transaction on the single writer (s.w, DB15): the event row and the
// setting-row change commit together or not at all, so the audit trail can
// never say a change happened that the table did not, or a table change
// happened with no audit row. A read that raced a separate write would open
// exactly that gap, which is why the old value is read INSIDE the
// transaction, never before it.
//
// Errors are plain `error` here: these are infrastructure outcomes (the disk
// broke, the statement failed). Domain refusals are *Rejects upstairs in the
// boundary (S4's DB19).
// ════════════════════════════════════════════════════════════════════════════

// SettingRow is one stored value. UpdatedAt is RFC3339 UTC, written by the
// caller (the store takes no clock).
type SettingRow struct {
	Key, Value, UpdatedAt, UpdatedBy string
}

// SettingEvent is one audit row. OldValue/NewValue are pointers: nil is SQL
// NULL, and NULL means something (no row existed / unset), never "".
type SettingEvent struct {
	ID         int64
	Key, Op    string
	OldValue   *string
	NewValue   *string
	Sensitive  bool
	ShadowedBy string // "" or "env"
	Actor      string
	OccurredAt string
}

// SettingGet answers whether a key is stored and what it holds.
//
// A key with no row is (zero, false, nil), never an error: absence is a
// normal answer, not a fault (DB19). It runs on the reader pool — s.r, never
// the single writer (O6/I6).
func (s *Store) SettingGet(ctx context.Context, key string) (SettingRow, bool, error) {
	var r SettingRow
	err := s.r.QueryRowContext(ctx,
		`SELECT key, value, updated_at, updated_by FROM setting WHERE key = ?`, key).
		Scan(&r.Key, &r.Value, &r.UpdatedAt, &r.UpdatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return SettingRow{}, false, nil
	}
	if err != nil {
		return SettingRow{}, false, fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return r, true, nil
}

// SettingList reads every stored value, ordered by key.
//
// It runs on the reader pool (O6/I6). The order is the UI's, so it must be
// stable — a list the operator can read in a fixed order is a list the UI
// will inherit.
func (s *Store) SettingList(ctx context.Context) ([]SettingRow, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT key, value, updated_at, updated_by FROM setting ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("store: list settings: %w", err)
	}
	defer rows.Close()

	var out []SettingRow
	for rows.Next() {
		var r SettingRow
		if err := rows.Scan(&r.Key, &r.Value, &r.UpdatedAt, &r.UpdatedBy); err != nil {
			return nil, fmt.Errorf("store: list settings: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list settings: %w", err)
	}
	return out, nil
}

// SettingPut upserts the row AND appends ev in ONE transaction.
//
// ev.Op must be "set" and ev.Key must equal row.Key: the audit row describes
// the change this call makes, so the two cannot disagree. ev.OldValue is
// filled by Put from the row it replaced (the caller does not race a read
// against the write) and ev.NewValue from row.Value — the value being set is
// the row's value, so the event can never record a new value the table does
// not hold. Both rows commit together: if the event insert fails (a bad op,
// an over-long value, the trigger) the whole transaction rolls back and the
// setting row is not written, so the audit trail can never name a change the
// table did not make (CS8).
//
// It runs on the single writer (DB15); reads never touch it.
func (s *Store) SettingPut(ctx context.Context, row SettingRow, ev SettingEvent) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: put setting %q: %w", row.Key, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Read the value being replaced INSIDE the transaction, so a concurrent
	// write cannot slip between a separate read and this upsert (the race the
	// caller would otherwise have to own). Absence is not an error: the first
	// put of a key has no old value, which is the NULL, not "".
	var old sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT value FROM setting WHERE key = ?`, row.Key).
		Scan(&old); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("store: put setting %q: read the old value: %w", row.Key, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO setting (key, value, updated_at, updated_by)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value      = excluded.value,
			updated_at = excluded.updated_at,
			updated_by = excluded.updated_by`,
		row.Key, row.Value, row.UpdatedAt, row.UpdatedBy); err != nil {
		return fmt.Errorf("store: put setting %q: %w", row.Key, err)
	}

	if old.Valid {
		v := old.String
		ev.OldValue = &v
	}
	newValue := row.Value
	ev.NewValue = &newValue

	if err := insertSettingEvent(ctx, tx, ev); err != nil {
		return fmt.Errorf("store: put setting %q: %w", row.Key, err)
	}
	return tx.Commit()
}

// SettingDelete removes the row and appends ev ("unset") in one transaction;
// a key with no row returns (false, nil) and writes NO event (an audit trail
// of no-ops is noise, CS8). The prior value is read inside the transaction
// and recorded as the event's old_value; new_value is NULL (the CHECK pairs
// op='unset' with new_value IS NULL), so the two NULLs name two facts — the
// value that was removed, and the absence that replaced it.
//
// It runs on the single writer (DB15).
func (s *Store) SettingDelete(ctx context.Context, key string, ev SettingEvent) (bool, error) {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: delete setting %q: %w", key, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Read the value being removed INSIDE the transaction: it is the event's
	// old_value, and reading it after a separate write would open the same
	// gap a raced read would. Absence is the answer, not an error.
	var old sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT value FROM setting WHERE key = ?`, key).
		Scan(&old); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil // no row: nothing to remove, no event
		}
		return false, fmt.Errorf("store: delete setting %q: read the old value: %w", key, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM setting WHERE key = ?`, key); err != nil {
		return false, fmt.Errorf("store: delete setting %q: %w", key, err)
	}

	v := old.String
	ev.OldValue = &v
	ev.NewValue = nil

	if err := insertSettingEvent(ctx, tx, ev); err != nil {
		return false, fmt.Errorf("store: delete setting %q: %w", key, err)
	}
	return true, tx.Commit()
}

// SettingEventList reads the audit trail newest first.
//
// key=="" spans every key; a named key is that key's trail. limit is clamped
// to [1,500], defaulting to 50 (an unset limit reads as "all events" and is a
// defect, not an answer — the same rule PolicyEventQuery states). Newest first
// is the trail the UI shows; the (key, id) index makes a key-filtered read
// use it, and id orders the unfiltered read.
//
// It runs on the reader pool (O6/I6).
func (s *Store) SettingEventList(ctx context.Context, key string, limit int) ([]SettingEvent, error) {
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
	if key != "" {
		where = "WHERE key = ?"
		args = append(args, key)
	}
	args = append(args, limit)

	rows, err := s.r.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, key, op, old_value, new_value, sensitive,
		       COALESCE(shadowed_by, ''), actor, occurred_at
		FROM setting_event
		%s
		ORDER BY id DESC
		LIMIT ?`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list setting events: %w", err)
	}
	defer rows.Close()

	var out []SettingEvent
	for rows.Next() {
		var (
			ev       SettingEvent
			old, new sql.NullString
			sens     int
			shadow   string
		)
		if err := rows.Scan(&ev.ID, &ev.Key, &ev.Op, &old, &new, &sens, &shadow, &ev.Actor, &ev.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: list setting events: scan: %w", err)
		}
		ev.OldValue = nullStringPtr(old)
		ev.NewValue = nullStringPtr(new)
		ev.Sensitive = sens == 1
		ev.ShadowedBy = shadow
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list setting events: %w", err)
	}
	return out, nil
}

// insertSettingEvent writes one audit row on the transaction that owns the
// change it records. It is the single writer of setting_event: there is no
// Update or Delete path (the trigger refuses both, CS8), so an append is the
// only mutation, and a missing field here cannot appear anywhere else.
//
// old_value and new_value map nil → NULL (the two NULLs, above); a
// shadowed_by of "" maps to NULL (the CHECK admits only NULL or 'env');
// sensitive maps bool → 0/1.
func insertSettingEvent(ctx context.Context, tx *sql.Tx, ev SettingEvent) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO setting_event (key, op, old_value, new_value, sensitive, shadowed_by, actor, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.Key, ev.Op, nullStringArg(ev.OldValue), nullStringArg(ev.NewValue),
		boolArg(ev.Sensitive), nullableShadow(ev.ShadowedBy), ev.Actor, ev.OccurredAt); err != nil {
		return fmt.Errorf("insert setting_event for %q: %w", ev.Key, err)
	}
	return nil
}

// nullStringArg maps a *string onto the nullable column: nil is SQL NULL, so
// the two NULLs (old_value absent / new_value unset) are written, not "".
func nullStringArg(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// nullStringPtr maps the column back: NULL is a nil *string, so a read
// restores the "no row / unset" fact rather than collapsing it to "".
func nullStringPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

// nullableShadow maps "" onto NULL (the CHECK admits only NULL or 'env') and
// leaves a named value to fail the CHECK, so a bad shadow is refused, not
// silently normalised to NULL.
func nullableShadow(s string) any {
	if s == "" {
		return nil
	}
	return s
}
