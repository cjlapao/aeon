package store

// S49 T4 — the human write paths with their audit and their actor. AN3's
// rule in Go: a person's write to an automation row and its automation_change
// row commit together or not at all, and requireUser fires BEFORE any SQL —
// the third AU1 lock, under the protocol door and the daemon service. The
// summaries are fixed templates of ids and enums (SEC-033: a hostile goal
// sentence must never reach the audit's summary column, so it never passes
// a template's argument).
//
// SaveTriggerScheduleAs is SaveTriggerSchedule WITH the provenance and the
// audit: the plain function stays for the engine-shaped callers exactly as
// S20 wrote it (its upsert never names created_by_* or the archive columns —
// an edit cannot clobber provenance it was not given). The archive wrappers
// set AN6's engine-invisible state through T1's tx primitives and audit it;
// unarchive leaves the disabled/paused state alone, because re-enabling is
// its own separate act on the human door.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// automationAudit appends the change row inside the write's own transaction.
// Summaries arrive already templated by the caller (ids and enums only); the
// actor kind is 'user' by construction — the CLI dials the same socket and
// its peer uid IS a person (AU1 has no second kind).
func automationAudit(tx *sql.Tx, source, triggerID, op, summary string, a Actor, at string) error {
	return AutomationChangeInsertTx(tx, AutomationChange{
		Source: source, TriggerID: triggerID, Op: op, Summary: summary,
		ActorKind: "user", ActorID: a.ID,
		OccurredAt: at,
	})
}

// SaveTriggerScheduleAs writes a schedule row AS a person: requireUser
// before any SQL, the created_by stamp on first insert (SetScheduleCreatorTx
// — the column CHECK accepts 'user' only), and one audit row in the same
// transaction whenever the write changes a column. An unchanged re-save
// writes nothing and audits nothing: AN4 counts changes, not touches.
func (s *Store) SaveTriggerScheduleAs(ctx context.Context, a Actor, r TriggerScheduleRow, op, summary string) (created, changed bool, err error) {
	if err := requireUser(a); err != nil {
		return false, false, err
	}
	if r.ID == "" || r.Kind == "" || r.Blueprint == "" || r.CreatedAt == "" {
		return false, false, fmt.Errorf("store: a trigger row needs id, kind, blueprint and created_at")
	}
	if r.Kind == "schedule" && r.Zone == "" {
		return false, false, fmt.Errorf("store: a schedule row needs a zone — TR6 refuses the host's silent default")
	}
	tx, err := s.w.Begin()
	if err != nil {
		return false, false, fmt.Errorf("store: save trigger as %s: %w", r.ID, err)
	}
	defer func() { _ = tx.Rollback() }()
	cur, scanErr := scanTriggerSchedule(tx.QueryRowContext(ctx,
		`SELECT `+triggerScheduleColumns+` FROM trigger_schedule WHERE id=?`, r.ID))
	found := scanErr == nil
	switch {
	case found:
	case errors.Is(scanErr, sql.ErrNoRows):
	default:
		return false, false, fmt.Errorf("store: read trigger schedule %s: %w", r.ID, scanErr)
	}
	changed = found && scheduleMutablesDiffer(cur, r)
	if found && !changed {
		// The same row, re-saved: no UPDATE (updated columns move for
		// nobody), no audit, and the answer says so.
		if err := tx.Commit(); err != nil {
			return false, false, fmt.Errorf("store: save trigger as %s: %w", r.ID, err)
		}
		return false, false, nil
	}
	if found {
		if _, err := tx.ExecContext(ctx, `
			UPDATE trigger_schedule SET
				kind=?, blueprint=?, expr=?, zone=?,
				window_start=?, window_end=?, window_days=?,
				paused=?, secret_env=?, workspace_root=?, goal=?, event_filter=? WHERE id=?`,
			r.Kind, r.Blueprint, r.Expr, r.Zone, nullable(r.WindowStart), nullable(r.WindowEnd), r.WindowDays,
			boolArg(r.Paused), r.SecretEnv, r.Workspace, r.Goal, r.EventFilter, r.ID); err != nil {
			return false, false, fmt.Errorf("store: save trigger as %s: %w", r.ID, err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO trigger_schedule (id, kind, blueprint, expr, zone, window_start, window_end, window_days, paused, secret_env, covered_until, created_at, workspace_root, goal, event_filter)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.Kind, r.Blueprint, r.Expr, r.Zone, nullable(r.WindowStart), nullable(r.WindowEnd), r.WindowDays,
			boolArg(r.Paused), r.SecretEnv, r.CoveredUntil, r.CreatedAt, r.Workspace, r.Goal, r.EventFilter); err != nil {
			return false, false, fmt.Errorf("store: save trigger as %s: %w", r.ID, err)
		}
		if err := SetScheduleCreatorTx(ctx, tx, r.ID, a); err != nil {
			return false, false, err
		}
	}
	if err := automationAudit(tx, r.Kind, r.ID, op, summary, a, s.Now()); err != nil {
		return false, false, err
	}
	if err := tx.Commit(); err != nil {
		return false, false, fmt.Errorf("store: save trigger as %s: %w", r.ID, err)
	}
	// A creation IS a change: `changed` answers "did the state move", and
	// a row that did not exist and now does has moved as loudly as one
	// whose cron moved.
	return !found, changed || !found, nil
}

// scheduleMutablesDiffer compares the columns a human write may move. id and
// kind are immutable by door design; created_at, covered_until, created_by_*
// and the archive columns are not the update door's to move — the UPDATE
// above names none of them, and the pause path carries the loaded row
// through, so what stays unstated stays unstored.
func scheduleMutablesDiffer(cur, want TriggerScheduleRow) bool {
	return cur.Blueprint != want.Blueprint || cur.Workspace != want.Workspace ||
		cur.Goal != want.Goal || cur.Expr != want.Expr || cur.Zone != want.Zone ||
		cur.WindowStart != want.WindowStart || cur.WindowEnd != want.WindowEnd ||
		cur.WindowDays != want.WindowDays || cur.Paused != want.Paused ||
		cur.SecretEnv != want.SecretEnv || cur.EventFilter != want.EventFilter
}

// ArchiveAutomation sets AN6's engine-invisible state for either family and
// audits it, once. An already-archived trigger answers changed=false and
// writes nothing — the second archive is the same intent already true, not
// a second change. The reason lands in the row's archived_reason column;
// the audit summary carries ids and enums only (SEC-033).
func (s *Store) ArchiveAutomation(ctx context.Context, a Actor, source, id, reason string) (changed bool, err error) {
	if err := requireUser(a); err != nil {
		return false, err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return false, fmt.Errorf("store: archive %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	archived, exists, err := automationArchivedTx(ctx, tx, source, id)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, fmt.Errorf("store: archive %s: %w", id, ErrNotFound)
	}
	if archived {
		return false, nil
	}
	at := s.Now()
	switch source {
	case "event":
		if err := EventArchiveTx(ctx, tx, id, reason, at); err != nil {
			return false, err
		}
	default:
		if err := ScheduleArchiveTx(ctx, tx, id, reason, at); err != nil {
			return false, err
		}
	}
	if err := automationAudit(tx, source, id, "archive", "archived "+source+" "+id, a, s.Now()); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: archive %s: %w", id, err)
	}
	return true, nil
}

// UnarchiveAutomation clears the archive state and leaves the trigger
// disabled/paused (AN6 — re-enabling is a separate act). An unarchived row
// answers changed=false and audits nothing.
func (s *Store) UnarchiveAutomation(ctx context.Context, a Actor, source, id string) (changed bool, err error) {
	if err := requireUser(a); err != nil {
		return false, err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return false, fmt.Errorf("store: unarchive %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	archived, exists, err := automationArchivedTx(ctx, tx, source, id)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, fmt.Errorf("store: unarchive %s: %w", id, ErrNotFound)
	}
	if !archived {
		return false, nil
	}
	switch source {
	case "event":
		if err := EventUnarchiveTx(ctx, tx, id); err != nil {
			return false, err
		}
	default:
		if err := ScheduleUnarchiveTx(ctx, tx, id); err != nil {
			return false, err
		}
	}
	if err := automationAudit(tx, source, id, "unarchive", "unarchived "+source+" "+id, a, s.Now()); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("store: unarchive %s: %w", id, err)
	}
	return true, nil
}

// automationArchivedTx answers (archived, row exists) for either family.
func automationArchivedTx(ctx context.Context, tx *sql.Tx, source, id string) (archived, exists bool, err error) {
	table := "trigger_schedule"
	if source == "event" {
		table = "event_trigger"
	}
	var v *string
	err = tx.QueryRowContext(ctx, `SELECT archived_at FROM `+table+` WHERE id=?`, id).Scan(&v)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, false, nil
	case err != nil:
		return false, false, fmt.Errorf("store: archive state of %s: %w", id, err)
	}
	return v != nil && *v != "", true, nil
}

// RecordAutomationAudit appends one audit row in its own transaction — the
// door for facts that are not row edits, of which there is exactly one
// today: fire-now, whose state change IS the trigger_fire answer the engine
// wrote (the engines stay byte-identical, so the audit rides beside that
// answer in a second transaction naming the fire's id — the one audit-
// outside-the-write the design allows, and fire-now cannot double-write
// because (schedule_id, instant) is UNIQUE).
func (s *Store) RecordAutomationAudit(ctx context.Context, a Actor, c AutomationChange) error {
	if err := requireUser(a); err != nil {
		return err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: audit %s: %w", c.Op, err)
	}
	defer func() { _ = tx.Rollback() }()
	if c.OccurredAt == "" {
		c.OccurredAt = s.Now()
	}
	c.ActorKind = "user"
	c.ActorID = a.ID
	if err := AutomationChangeInsertTx(tx, c); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: audit %s: %w", c.Op, err)
	}
	return nil
}

// LatestTriggerFireID answers the fire-now audit's target: the id of the
// newest recorded answer for one schedule. Fire-now's instant IS the asking
// moment, so "newest" is the row that fire just wrote; the UNIQUE key makes
// a doubled fire-now impossible, and the same trigger being ticked by the
// clock in the same instant is the residual race this design states rather
// than hides (the audit would then name the tick's answer — an answer that
// exists either way).
func (s *Store) LatestTriggerFireID(ctx context.Context, scheduleID string) (int64, bool, error) {
	var id int64
	err := s.r.QueryRowContext(ctx,
		`SELECT id FROM trigger_fire WHERE schedule_id=? ORDER BY at DESC, id DESC LIMIT 1`, scheduleID).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("store: latest fire %s: %w", scheduleID, err)
	}
	return id, true, nil
}

// sameCost compares two optional caps (nil is "no cap", distinct from 0).
func sameCost(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
