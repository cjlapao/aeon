package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TriggerScheduleRow is the row form of a schedule. It deliberately mirrors
// the trigger package's Schedule field-for-field but is NOT that type: the
// store answers to rows, the trigger package owns the contract (validation,
// the field whitelist, TR1), and the daemon carries rows between them. A
// column added here without its twin there fails the daemon adapter's
// compile, and vice versa fails the tests.
type TriggerScheduleRow struct {
	ID           string
	Kind         string
	Blueprint    string
	Workspace    string
	Goal         string
	Expr         string
	Zone         string
	WindowStart  string // "" is NULL: half a window is CHECKed away
	WindowEnd    string
	WindowDays   string
	Paused       bool
	SecretEnv    string
	// EventFilter is a webhook's `event` or `event:action,action`; empty listens to every signed delivery.
	EventFilter  string
	CoveredUntil string
	CreatedAt    string
	// CreatedByKind/CreatedByID are S49 AN3's provenance: the CHECK allows
	// only 'user', and '' on the id means "predates S49's provenance",
	// never a fabricated person. SaveTriggerSchedule does not write them
	// (defaults carry inserts); SetScheduleCreatorTx stamps the add path.
	// omitempty keeps S20's pinned renderings byte-identical while unset —
	// S49's own views carry the state.
	CreatedByKind string `json:"CreatedByKind,omitempty"`
	CreatedByID   string `json:"CreatedByID,omitempty"`
	// ArchivedAt ("" = live) and ArchivedReason are AN6's archive state.
	// Save's upsert deliberately never names these columns, so an edit
	// cannot clobber an archive.
	ArchivedAt     string `json:"ArchivedAt,omitempty"`
	ArchivedReason string `json:"ArchivedReason,omitempty"`
}

// SaveTriggerSchedule inserts or replaces a schedule row — the write the CLI
// add verb (T8) lands. Validation is the trigger package's door (write-time
// refusal sentences live there, next to the shape they explain); the store
// enforces the schema's own CHECKs and does not re-adjudicate intent.
func (s *Store) SaveTriggerSchedule(ctx context.Context, r TriggerScheduleRow) error {
	if r.ID == "" || r.Kind == "" || r.Blueprint == "" || r.CreatedAt == "" {
		return fmt.Errorf("store: a trigger row needs id, kind, blueprint and created_at")
	}
	if r.Kind == "schedule" && r.Zone == "" {
		// Zone is the expression's wall-clock contract (TR6) — the SCHEDULE
		// kind requires it; the webhook kind must NOT carry one (a clock it
		// never reads), and the trigger package's validator says so loudly.
		return fmt.Errorf("store: a schedule row needs a zone — TR6 refuses the host's silent default")
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO trigger_schedule (id, kind, blueprint, expr, zone, window_start, window_end, window_days, paused, secret_env, covered_until, created_at, workspace_root, goal, event_filter)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			kind=excluded.kind, blueprint=excluded.blueprint, expr=excluded.expr, zone=excluded.zone,
			window_start=excluded.window_start, window_end=excluded.window_end, window_days=excluded.window_days,
			paused=excluded.paused, secret_env=excluded.secret_env, created_at=excluded.created_at,
			workspace_root=excluded.workspace_root, goal=excluded.goal, event_filter=excluded.event_filter`,
		r.ID, r.Kind, r.Blueprint, r.Expr, r.Zone, nullable(r.WindowStart), nullable(r.WindowEnd), r.WindowDays,
		boolArg(r.Paused), r.SecretEnv, r.CoveredUntil, r.CreatedAt, r.Workspace, r.Goal, r.EventFilter)
	if err != nil {
		return fmt.Errorf("store: save trigger schedule %s: %w", r.ID, err)
	}
	return nil
}

func (s *Store) TriggerSchedule(ctx context.Context, id string) (TriggerScheduleRow, error) {
	row := s.r.QueryRowContext(ctx, `SELECT `+triggerScheduleColumns+` FROM trigger_schedule WHERE id=?`, id)
	r, err := scanTriggerSchedule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TriggerScheduleRow{}, fmt.Errorf("store: trigger schedule %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return TriggerScheduleRow{}, fmt.Errorf("store: read trigger schedule %s: %w", id, err)
	}
	return r, nil
}

// TriggerSchedules lists every row; the scheduler's ActiveSchedules reads
// the firing subset.
func (s *Store) TriggerSchedules(ctx context.Context) ([]TriggerScheduleRow, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+triggerScheduleColumns+` FROM trigger_schedule ORDER BY id`)
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

const triggerScheduleColumns = `id, kind, blueprint, expr, zone,
	COALESCE(window_start,''), COALESCE(window_end,''), window_days,
	paused, secret_env, covered_until, created_at, workspace_root, goal,
	created_by_kind, created_by_id, COALESCE(archived_at,''), archived_reason, event_filter`

func scanTriggerSchedule(row interface{ Scan(...any) error }) (TriggerScheduleRow, error) {
	var r TriggerScheduleRow
	var paused int
	err := row.Scan(&r.ID, &r.Kind, &r.Blueprint, &r.Expr, &r.Zone,
		&r.WindowStart, &r.WindowEnd, &r.WindowDays,
		&paused, &r.SecretEnv, &r.CoveredUntil, &r.CreatedAt, &r.Workspace, &r.Goal,
		&r.CreatedByKind, &r.CreatedByID, &r.ArchivedAt, &r.ArchivedReason, &r.EventFilter)
	r.Paused = paused != 0
	return r, err
}

// SetTriggerCovered advances the reconciliation frontier (T4 reads it; the
// tick writes it after answering an instant).
func (s *Store) SetTriggerCovered(ctx context.Context, id, coveredUntil string) error {
	res, err := s.w.ExecContext(ctx, `UPDATE trigger_schedule SET covered_until=? WHERE id=?`, coveredUntil, id)
	if err != nil {
		return fmt.Errorf("store: covered_until %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: covered_until %s: %w", id, ErrNotFound)
	}
	return nil
}

// SetTriggerPaused is TR12's operational half: pause is a row flag, not a
// policy — reversible at the CLI and the app, never the narrow surface (T8).
func (s *Store) SetTriggerPaused(ctx context.Context, id string, paused bool) error {
	res, err := s.w.ExecContext(ctx, `UPDATE trigger_schedule SET paused=? WHERE id=?`, boolArg(paused), id)
	if err != nil {
		return fmt.Errorf("store: pause %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: pause %s: %w", id, ErrNotFound)
	}
	return nil
}

// boolArg renders the paused/paused-by columns the way attendedArg renders
// attendedness's siblings: SQLite has no bool, and CHECK (paused IN (0,1))
// wants the word this store uses for it.
func boolArg(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TriggerFireRow is one answered question: "did this fire?" and the closed
// five-value answer beside the instant it belongs to.
type TriggerFireRow struct {
	ScheduleID string
	Instant    string
	Outcome    string
	Reason     string
	LoopID     string
	At         string
}

// RecordTriggerFire writes the answer. A second answer for the same
// (schedule, instant) is refused by the row's UNIQUE — and correctly so:
// the frontier is what makes an instant a single question, and a caller
// asking twice has a frontier bug worth surfacing, not silently absorbing.
func (s *Store) RecordTriggerFire(ctx context.Context, r TriggerFireRow) error {
	if r.ScheduleID == "" || r.Instant == "" || r.Outcome == "" || r.At == "" {
		return fmt.Errorf("store: a fire record needs schedule_id, instant, outcome and at")
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO trigger_fire (schedule_id, instant, outcome, reason, loop_id, at) VALUES (?,?,?,?,?,?)`,
		r.ScheduleID, r.Instant, r.Outcome, r.Reason, r.LoopID, r.At)
	if err != nil {
		return fmt.Errorf("store: record fire %s@%s: %w", r.ScheduleID, r.Instant, err)
	}
	return nil
}

// LastFiredLoopBefore answers the overlap question (TR5/Q2): what loop did
// this schedule LAST start (outcome fired, instant strictly before the
// instant being answered), and what does the loop's own row say about it
// NOW. The status is read live, never cached in the fire row — a loop that
// was running at fire time and is done now must not block forever, and a
// suspended one is exactly what Q2 rules on. Lexicographic on instant is
// correct because both sides are UTC RFC3339Nano (triggerInstantFormat,
// written by RecordTriggerFire and nowhere else).
func (s *Store) LastFiredLoopBefore(ctx context.Context, scheduleID, beforeInstant string) (loopID, status string, found bool, err error) {
	row := s.r.QueryRowContext(ctx, `
		SELECT tf.loop_id, l.status
		FROM trigger_fire tf JOIN loop l ON l.id = tf.loop_id
		WHERE tf.schedule_id=? AND tf.outcome='fired' AND tf.instant < ? AND tf.loop_id <> ''
		ORDER BY tf.instant DESC LIMIT 1`, scheduleID, beforeInstant)
	err = row.Scan(&loopID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("store: last fired loop %s: %w", scheduleID, err)
	}
	return loopID, status, true, nil
}

// TriggerGapRow is one honest absence-of-answers: the instants between
// (covered_from, covered_to] that passed while the daemon was down, as a
// RANGE and a COUNT (0034's stated shape). It is not a trigger_fire row —
// a gap answers no single instant, and pretending otherwise would put the
// fourth answer in a timestamped column.
type TriggerGapRow struct {
	ScheduleID  string
	CoveredFrom string
	CoveredTo   string
	Missed      int
	At          string
}

// SaveTriggerGap records the gap Reconcile found. CHECK (missed > 0) makes
// an empty gap a refusal at the row, and the scheduler never calls it with
// zero anyway — the CHECK is for the caller that appears later.
func (s *Store) SaveTriggerGap(ctx context.Context, r TriggerGapRow) error {
	if r.ScheduleID == "" || r.CoveredFrom == "" || r.CoveredTo == "" || r.At == "" || r.Missed <= 0 {
		return fmt.Errorf("store: a gap row needs schedule, from, to, at and missed > 0")
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO trigger_gap (schedule_id, covered_from, covered_to, missed, at) VALUES (?,?,?,?,?)`,
		r.ScheduleID, r.CoveredFrom, r.CoveredTo, r.Missed, r.At)
	if err != nil {
		return fmt.Errorf("store: record gap %s (%s..%s × %d): %w", r.ScheduleID, r.CoveredFrom, r.CoveredTo, r.Missed, err)
	}
	return nil
}

// TriggerGaps lists a schedule's outages newest-first — the second half of
// `trigger history` (fires and gaps printed together, chronology honest).
func (s *Store) TriggerGaps(ctx context.Context, id string, limit int) ([]TriggerGapRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.r.QueryContext(ctx, `
		SELECT schedule_id, covered_from, covered_to, missed, at
		FROM trigger_gap WHERE schedule_id=? ORDER BY covered_to DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, fmt.Errorf("store: gaps %s: %w", id, err)
	}
	defer rows.Close()
	var out []TriggerGapRow
	for rows.Next() {
		var r TriggerGapRow
		if err := rows.Scan(&r.ScheduleID, &r.CoveredFrom, &r.CoveredTo, &r.Missed, &r.At); err != nil {
			return nil, fmt.Errorf("store: gaps %s: %w", id, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TriggerFireHistory reads a schedule's answers newest-first (the demo's
// `trigger history` shape). LIMIT bounds the read — a week-long outage is
// a range+count story (T4), not ten thousand printed rows.
func (s *Store) TriggerFireHistory(ctx context.Context, id string, limit int) ([]TriggerFireRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.r.QueryContext(ctx, `
		SELECT schedule_id, instant, outcome, COALESCE(reason,''), COALESCE(loop_id,''), at
		FROM trigger_fire WHERE schedule_id=? ORDER BY instant DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, fmt.Errorf("store: fire history %s: %w", id, err)
	}
	defer rows.Close()
	var out []TriggerFireRow
	for rows.Next() {
		var r TriggerFireRow
		if err := rows.Scan(&r.ScheduleID, &r.Instant, &r.Outcome, &r.Reason, &r.LoopID, &r.At); err != nil {
			return nil, fmt.Errorf("store: fire history %s: %w", id, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
