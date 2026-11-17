package store

// S49 T1 — automation management storage: the append-only authority audit,
// the archive state, the creator stamp, and the unified fire view's reads
// (design AN3–AN6). SQL only, parameterised, in the one store. The engines
// (clock, webhook delivery, dispatcher) are not mentioned here and do not
// learn the word "archived": archive sets exactly the engine-visible off
// state T0 item 4 measured (AN6), so invisibility comes from the columns
// they already read, not from engine cooperation.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// AutomationChange is one row of the audit AN4 requires: every authority
// change, exactly once, in the change's own transaction, with a
// fixed-template summary (ids and enums — never raw user text, SEC-033).
type AutomationChange struct {
	ID         int64
	Source     string // schedule | webhook | event
	TriggerID  string
	Op         string // create|update|enable|disable|pause|resume|fire_now|archive|unarchive
	Summary    string
	ActorKind  string // user | cli
	ActorID    string
	OccurredAt string
}

// AutomationChangeInsertTx appends one audit row inside the caller's
// transaction — the table's own BEFORE UPDATE/DELETE triggers (the
// migration) are the append-only net; there is deliberately no update or
// delete accessor next to this insert.
func AutomationChangeInsertTx(tx *sql.Tx, c AutomationChange) error {
	if tx == nil {
		return errors.New("store: automation_change needs the write's own transaction")
	}
	res, err := tx.Exec(`INSERT INTO automation_change (source,trigger_id,op,summary,actor_kind,actor_id,occurred_at)
		VALUES (?,?,?,?,?,?,?)`,
		c.Source, c.TriggerID, c.Op, c.Summary, c.ActorKind, c.ActorID, c.OccurredAt)
	if err != nil {
		return fmt.Errorf("store: audit %s %s %s: %w", c.ActorKind, c.Op, c.TriggerID, err)
	}
	id, err := res.LastInsertId()
	if err == nil {
		c.ID = id
	}
	return nil
}

// AutomationChangeList answers the audit newest-first. source and triggerID
// are optional filters; limit clamps to [1,500] and defaults to 50 — the
// same discipline every listing in this store carries.
func (s *Store) AutomationChangeList(ctx context.Context, source, triggerID string, limit int) ([]AutomationChange, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	q := `SELECT id, source, trigger_id, op, summary, actor_kind, actor_id, occurred_at
		FROM automation_change WHERE 1=1`
	args := []any{}
	if source != "" {
		q += ` AND source = ?`
		args = append(args, source)
	}
	if triggerID != "" {
		q += ` AND trigger_id = ?`
		args = append(args, triggerID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: automation change list: %w", err)
	}
	defer rows.Close()
	var out []AutomationChange
	for rows.Next() {
		var c AutomationChange
		if err := rows.Scan(&c.ID, &c.Source, &c.TriggerID, &c.Op, &c.Summary, &c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: automation change list: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ── Archive (AN6) ───────────────────────────────────────────────────────────
//
// Archive is NOT delete (AU1): the row, its fire history and its audit all
// stay. What stops the engines is exactly what they already read —
// paused for the clock and the webhook door (clock.go:119, webhook.go:172,
// measured by T0 item 4), enabled for the dispatcher (dispatcher.go:100).
// The archive functions set that engine-off state AND the archive columns
// in one transaction, and unarchive clears only the archive columns: the
// trigger comes back disabled/paused, and re-enabling is its own separate
// act on the human door (AN6).

// ScheduleArchiveTx archives a schedule or webhook row (both live in
// trigger_schedule; the kind distinguishes them at the view and the audit
// source). paused=1 is the engine-invisible off state AN6 is proved by.
func ScheduleArchiveTx(ctx context.Context, tx *sql.Tx, id, reason, at string) error {
	if tx == nil {
		return errors.New("store: archive needs the write's own transaction")
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE trigger_schedule SET paused=1, archived_at=?, archived_reason=? WHERE id=?`,
		at, reason, id)
	if err != nil {
		return fmt.Errorf("store: archive %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: archive %s: %w", id, ErrNotFound)
	}
	return nil
}

// ScheduleUnarchiveTx clears the archive state and leaves the trigger
// paused — unarchive is never a re-enable (AN6).
func ScheduleUnarchiveTx(ctx context.Context, tx *sql.Tx, id string) error {
	if tx == nil {
		return errors.New("store: unarchive needs the write's own transaction")
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE trigger_schedule SET archived_at=NULL, archived_reason='' WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("store: unarchive %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: unarchive %s: %w", id, ErrNotFound)
	}
	return nil
}

// EventArchiveTx archives an event trigger: enabled=0 is the dispatcher's
// own off state, and disabled_reason says "archived" so the dispatcher's
// skipped(disabled) answer reads truthfully while archived (AU15 — no
// replay on re-enable is that path's own standing rule).
func EventArchiveTx(ctx context.Context, tx *sql.Tx, id, reason, at string) error {
	if tx == nil {
		return errors.New("store: archive needs the write's own transaction")
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE event_trigger SET enabled=0, disabled_reason='archived', archived_at=?, archived_reason=? WHERE id=?`,
		at, reason, id)
	if err != nil {
		return fmt.Errorf("store: archive %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: archive %s: %w", id, ErrNotFound)
	}
	return nil
}

// EventUnarchiveTx clears the archive state and leaves the trigger
// disabled — enabling is a separate act on the human door.
func EventUnarchiveTx(ctx context.Context, tx *sql.Tx, id string) error {
	if tx == nil {
		return errors.New("store: unarchive needs the write's own transaction")
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE event_trigger SET archived_at=NULL, archived_reason='' WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("store: unarchive %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: unarchive %s: %w", id, ErrNotFound)
	}
	return nil
}

// SetScheduleCreatorTx stamps the creator the add path must record —
// AU1 lock 3 for the schedule family (AN3). The actor is a person or the
// CLI door, never an agent, and the refusal is before any SQL; the
// CHECK( created_by_kind = 'user') is the net below this door. created_by_id
// ” belongs to rows that predate S49, never to a row written here.
func SetScheduleCreatorTx(ctx context.Context, tx *sql.Tx, id string, a Actor) error {
	if tx == nil {
		return errors.New("store: the creator stamp needs the write's own transaction")
	}
	if err := requireUser(a); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE trigger_schedule SET created_by_kind='user', created_by_id=? WHERE id=?`, a.ID, id)
	if err != nil {
		return fmt.Errorf("store: creator stamp %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: creator stamp %s: %w", id, ErrNotFound)
	}
	return nil
}

// TriggerSchedulesScoped is the operator-facing list AN6 shapes: archived
// rows are hidden unless the caller asks (the engine's TriggerSchedules
// stays as S20 wrote it — an archived row is paused, and the clock already
// skips paused rows without knowing the new word).
func (s *Store) TriggerSchedulesScoped(ctx context.Context, includeArchived bool) ([]TriggerScheduleRow, error) {
	q := `SELECT ` + triggerScheduleColumns + ` FROM trigger_schedule`
	if !includeArchived {
		q += ` WHERE archived_at IS NULL`
	}
	q += ` ORDER BY id`
	rows, err := s.r.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("store: list trigger schedules: %w", err)
	}
	defer rows.Close()
	var out []TriggerScheduleRow
	for rows.Next() {
		r, err := scanTriggerSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list trigger schedules: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── The unified fire view (AN5) ────────────────────────────────────────────

// FireViewRow is one row of automation_fire_view — the two families' fire
// answers told in AU12's five words (the view itself carries the mapping;
// T0 item 5 fixed it and records that did_not_fire merges into skipped with
// the reason verbatim carrying the distinction).
type FireViewRow struct {
	Source      string // schedule | webhook | event
	TriggerID   string
	At          string
	Outcome     string // started|refused|limited|skipped|could-not-say
	Reason      string
	LoopID      string
	LoopSlug    string
	SubjectKind string
	SubjectID   string
	Depth       int
	CostNote    string
	FID         int64 // the source row's id — the cursor's second half
}

// FireViewFilter narrows the listing. BeforeID is a cursor the route hands
// out as "<at>|<source>:<fid>" and reads back the same way: the log is
// newest first and paging never re-reads a page (the discipline behind
// TR4's honest absences).
type FireViewFilter struct {
	Source    string
	TriggerID string
	Outcome   string
	Limit     int
	BeforeID  string
}

// AutomationFires answers the unified log and the per-outcome counts for
// the same source/trigger filters (counts ignore the outcome filter and the
// cursor — EventFireCounts' own convention, kept so one counting rule
// serves both logs).
func (s *Store) AutomationFires(ctx context.Context, f FireViewFilter) ([]FireViewRow, map[string]int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 500 {
		f.Limit = 500
	}
	where, args := ` WHERE 1=1`, []any{}
	if f.Source != "" {
		where += ` AND source = ?`
		args = append(args, f.Source)
	}
	if f.TriggerID != "" {
		where += ` AND trigger_id = ?`
		args = append(args, f.TriggerID)
	}
	counts := map[string]int{}
	countArgs := append([]any{}, args...)
	crows, err := s.r.QueryContext(ctx, `SELECT outcome, COUNT(*) FROM automation_fire_view`+where+` GROUP BY outcome`, countArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("store: fire view counts: %w", err)
	}
	defer crows.Close()
	for crows.Next() {
		var o string
		var n int
		if err := crows.Scan(&o, &n); err != nil {
			return nil, nil, fmt.Errorf("store: fire view counts: %w", err)
		}
		counts[o] = n
	}
	if err := crows.Err(); err != nil {
		return nil, nil, fmt.Errorf("store: fire view counts: %w", err)
	}

	page := where
	if f.Outcome != "" {
		page += ` AND outcome = ?`
		args = append(args, f.Outcome)
	}
	if f.BeforeID != "" {
		src, fid, err := splitFireCursor(f.BeforeID)
		if err != nil {
			return nil, nil, err
		}
		page += ` AND (at < ? OR (at = ? AND source > ?) OR (at = ? AND source = ? AND fid < ?))`
		args = append(args, src.at, src.at, src.source, src.at, src.source, fid)
	}
	rows, err := s.r.QueryContext(ctx, `SELECT source, trigger_id, at, outcome, reason,
		COALESCE(loop_id,''), COALESCE(loop_slug,''), COALESCE(subject_kind,''), COALESCE(subject_id,''),
		COALESCE(depth,0), cost_note, fid
		FROM automation_fire_view`+page+` ORDER BY at DESC, source DESC, fid DESC LIMIT ?`,
		append(args, f.Limit)...)
	if err != nil {
		return nil, nil, fmt.Errorf("store: fire view: %w", err)
	}
	defer rows.Close()
	var out []FireViewRow
	for rows.Next() {
		var r FireViewRow
		if err := rows.Scan(&r.Source, &r.TriggerID, &r.At, &r.Outcome, &r.Reason,
			&r.LoopID, &r.LoopSlug, &r.SubjectKind, &r.SubjectID, &r.Depth, &r.CostNote, &r.FID); err != nil {
			return nil, nil, fmt.Errorf("store: fire view: %w", err)
		}
		out = append(out, r)
	}
	return out, counts, rows.Err()
}

type fireCursor struct{ at, source string }

func splitFireCursor(s string) (fireCursor, int64, error) {
	at, rest, ok := strings.Cut(s, "|")
	if !ok {
		return fireCursor{}, 0, fmt.Errorf("store: fire cursor %q is not <at>|<source>:<id>", s)
	}
	source, fidText, ok := strings.Cut(rest, ":")
	if !ok {
		return fireCursor{}, 0, fmt.Errorf("store: fire cursor %q names no source", s)
	}
	fid, err := strconv.ParseInt(fidText, 10, 64)
	if err != nil {
		return fireCursor{}, 0, fmt.Errorf("store: fire cursor %q carries no row id: %w", s, err)
	}
	return fireCursor{at: at, source: source}, fid, nil
}

// ── Runs: the loops a trigger started ──────────────────────────────────────

// TriggerRunRow is a loop a trigger started, as the runs listing answers.
type TriggerRunRow struct {
	ID        string
	Slug      string
	Status    string
	StartedAt string
}

// startedByFor names the provenance word each family stamps its loops
// with (0033/0039: loop.started_by ∈ trigger|webhook|event, loop.trigger_id
// set). The mapping is the same fact automation_fire_view's source column
// encodes, phrased for the loop table.
func startedByFor(source string) (string, error) {
	switch source {
	case "schedule":
		return "trigger", nil
	case "webhook":
		return "webhook", nil
	case "event":
		return "event", nil
	}
	return "", fmt.Errorf("store: no provenance word for automation source %q (the set is schedule|webhook|event)", source)
}

// AutomationRuns lists the loops a trigger started (R-3's provenance made
// readable: loops by loop.trigger_id + the family's started_by word), with
// the total count beside the page. Order is the store's own listing
// convention — updated_at DESC, slug — because loop ids are random hex
// (newID): an id is never a clock, and pretending otherwise would order
// runs by luck.
func (s *Store) AutomationRuns(ctx context.Context, source, triggerID string, limit int) ([]TriggerRunRow, int, error) {
	startedBy, err := startedByFor(source)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var total int
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM loop WHERE trigger_id=? AND started_by=?`,
		triggerID, startedBy).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: runs %s: %w", triggerID, err)
	}
	rows, err := s.r.QueryContext(ctx, `SELECT id, slug, status, started_at
		FROM loop WHERE trigger_id=? AND started_by=? ORDER BY updated_at DESC, slug LIMIT ?`, triggerID, startedBy, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("store: runs %s: %w", triggerID, err)
	}
	defer rows.Close()
	var out []TriggerRunRow
	for rows.Next() {
		var r TriggerRunRow
		if err := rows.Scan(&r.ID, &r.Slug, &r.Status, &r.StartedAt); err != nil {
			return nil, 0, fmt.Errorf("store: runs %s: %w", triggerID, err)
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}
