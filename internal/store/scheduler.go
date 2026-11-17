package store

// scheduler.go — S27 T2: the ONE write path and the SQL half for the
// scheduler's tables (I2). Nothing here decides anything: the decisions
// arrive as rows to record. Controls write authority rows AND their audit
// rows in ONE transaction each — an unattributed scheduler change is the
// failure this file exists to prevent. The claim ledger enforces CN14's
// one-attempt rule with the PRIMARY KEY, and the decision log is
// append-only at the database itself (migration 20261105025313).
//
// Actors never arrive as caller-inventable strings: the daemon passes the
// kernel peer down as this package's SchedulerActor and every write stamps
// it; the kinds are refused in Go BEFORE SQL. Summaries are FIXED TEMPLATES
// over ids, enums and slugs — no user-authored text reaches the summary
// column (continuous_schema_test greps exactly that).

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SchedulerActor is who a control write is attributed to. Kind is the
// human-door vocabulary; the decision log additionally admits 'scheduler'
// (an actor row the machine itself authored — never an author of
// authority).
type SchedulerActor struct {
	Kind string // user | agent | trigger
	ID   string
}

func (a SchedulerActor) validControl() bool {
	switch a.Kind {
	case "user", "agent", "trigger":
		return a.ID != ""
	}
	return false
}

func (a SchedulerActor) validActor() bool {
	return a.validControl() || (a.Kind == "scheduler" && a.ID != "")
}

type SchedulerStateRow struct {
	Enabled       bool
	Paused        bool
	PausedReason  string // "" when not paused
	UpdatedAt     string
	UpdatedByKind string
	UpdatedByID   string
}

type SchedulerRootRow struct {
	ComponentRoot string
	Enabled       bool
	Blueprint     string
	Tech          string
	WithJSON      string
	WindowStart   string
	WindowEnd     string
	WindowDays    string
	Zone          string
	MinPriority   string // known | assumed
	CreatedAt     string
	UpdatedAt     string
	UpdatedByKind string
	UpdatedByID   string
}

type SchedulerStartRow struct {
	SubjectKind   string // queue | roadmap
	SubjectID     int64
	DecisionID    int64
	ComponentRoot string
	ClaimedAt     string
	LoopSlug      string
	Outcome       string // "" while the row reads NULL — claimed, undecided (the crash shape)
	OutcomeAt     string
}

type SchedulerDecision struct {
	ID            int64
	At            string
	ComponentRoot string
	SubjectKind   string // queue | roadmap | root | daemon
	SubjectID     int64
	Decision      string // started | deferred | skipped | refused | needs-you | lost | enabled | disabled | paused | resumed | settings
	Reason        string // a coded vocabulary token (cap-hit, capacity, grant-expired, …), never prose
	Summary       string // fixed template, computed here
	GrantRevision string
	ActorKind     string
	ActorID       string
	LoopSlug      string
}

var (
	schedulerSubjects  = map[string]bool{"queue": true, "roadmap": true, "root": true, "daemon": true}
	schedulerDecisions = map[string]bool{
		"started": true, "deferred": true, "skipped": true, "refused": true,
		"needs-you": true, "lost": true, "enabled": true, "disabled": true,
		"paused": true, "resumed": true, "settings": true,
	}
	schedulerOutcomes = map[string]bool{"started": true, "refused": true, "lost": true}
)

// ---------------------------------------------------------------------------
// controls — each write is ONE transaction with its decision row.
// ---------------------------------------------------------------------------

func (s *Store) SchedulerState() (SchedulerStateRow, error) {
	return scanSchedulerState(s.r)
}

func scanSchedulerState(q interface {
	QueryRow(string, ...any) *sql.Row
}) (SchedulerStateRow, error) {
	var r SchedulerStateRow
	var enabled, paused int
	var reason sql.NullString
	err := q.QueryRow(`SELECT enabled, paused, paused_reason, updated_at, updated_by_kind, updated_by_id
		FROM scheduler_state WHERE id = 1`).
		Scan(&enabled, &paused, &reason, &r.UpdatedAt, &r.UpdatedByKind, &r.UpdatedByID)
	if err != nil {
		return r, fmt.Errorf("store: read scheduler state: %w", err)
	}
	r.Enabled, r.Paused, r.PausedReason = enabled == 1, paused == 1, reason.String
	return r, nil
}

// SetSchedulerState writes the daemon-wide switch and records WHY it moved:
// the decision word is DERIVED from the move itself (enabling is 'enabled',
// disabling 'disabled', pausing 'paused', resuming 'resumed', anything else
// a 'settings' change) — no caller can mislabel its own write.
func (s *Store) SetSchedulerState(actor SchedulerActor, enabled, paused bool, pausedReason string) (SchedulerStateRow, error) {
	if !actor.validControl() {
		return SchedulerStateRow{}, fmt.Errorf("store: scheduler state: actor %q/%q is not a control voice", actor.Kind, actor.ID)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return SchedulerStateRow{}, err
	}
	defer tx.Rollback()
	before, err := scanSchedulerStateTx(tx)
	if err != nil {
		return SchedulerStateRow{}, err
	}
	word := "settings"
	switch {
	case before.Enabled && !enabled:
		word = "disabled"
	case !before.Enabled && enabled:
		word = "enabled"
	case !before.Paused && paused:
		word = "paused"
	case before.Paused && !paused:
		word = "resumed"
	}
	now := s.Now()
	var reason any
	if paused {
		reason = pausedReason
	}
	res, err := tx.Exec(`UPDATE scheduler_state SET enabled=?, paused=?, paused_reason=?, updated_at=?, updated_by_kind=?, updated_by_id=? WHERE id = 1`,
		b2i(enabled), b2i(paused), reason, now, actor.Kind, actor.ID)
	if err != nil {
		return SchedulerStateRow{}, fmt.Errorf("store: set scheduler state: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return SchedulerStateRow{}, errors.New("store: set scheduler state: the singleton row is missing (the migration did not land)")
	}
	summary := word + " scheduler"
	if word == "settings" {
		summary = "scheduler settings changed"
	}
	if _, err := appendDecisionTx(tx, s, SchedulerDecision{
		SubjectKind: "daemon", Decision: word, Reason: pausedReason,
		Summary: summary, ActorKind: actor.Kind, ActorID: actor.ID,
	}); err != nil {
		return SchedulerStateRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return SchedulerStateRow{}, err
	}
	return s.SchedulerState()
}

func scanSchedulerStateTx(tx *sql.Tx) (SchedulerStateRow, error) {
	var r SchedulerStateRow
	var enabled, paused int
	var reason sql.NullString
	err := tx.QueryRow(`SELECT enabled, paused, paused_reason, updated_at, updated_by_kind, updated_by_id
		FROM scheduler_state WHERE id = 1`).
		Scan(&enabled, &paused, &reason, &r.UpdatedAt, &r.UpdatedByKind, &r.UpdatedByID)
	if err != nil {
		return r, fmt.Errorf("store: read scheduler state: %w", err)
	}
	r.Enabled, r.Paused, r.PausedReason = enabled == 1, paused == 1, reason.String
	return r, nil
}

const schedulerRootColumns = `component_root, enabled, blueprint, COALESCE(tech,''), COALESCE(with_json,''),
	COALESCE(window_start,''), COALESCE(window_end,''), COALESCE(window_days,''), COALESCE(zone,''),
	min_priority, created_at, updated_at, updated_by_kind, updated_by_id`

func scanSchedulerRoot(row interface{ Scan(...any) error }) (SchedulerRootRow, error) {
	var r SchedulerRootRow
	var enabled int
	err := row.Scan(&r.ComponentRoot, &enabled, &r.Blueprint, &r.Tech, &r.WithJSON,
		&r.WindowStart, &r.WindowEnd, &r.WindowDays, &r.Zone,
		&r.MinPriority, &r.CreatedAt, &r.UpdatedAt, &r.UpdatedByKind, &r.UpdatedByID)
	if err != nil {
		return r, err
	}
	r.Enabled = enabled == 1
	return r, nil
}

func (s *Store) SchedulerRoots() ([]SchedulerRootRow, error) {
	rows, err := s.r.Query(`SELECT ` + schedulerRootColumns + ` FROM scheduler_root ORDER BY component_root`)
	if err != nil {
		return nil, fmt.Errorf("store: list scheduler roots: %w", err)
	}
	defer rows.Close()
	var out []SchedulerRootRow
	for rows.Next() {
		r, err := scanSchedulerRoot(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list scheduler roots: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SchedulerRoot(componentRoot string) (SchedulerRootRow, bool, error) {
	r, err := scanSchedulerRoot(s.r.QueryRow(`SELECT `+schedulerRootColumns+` FROM scheduler_root WHERE component_root = ?`, componentRoot))
	if errors.Is(err, sql.ErrNoRows) {
		return SchedulerRootRow{}, false, nil
	}
	if err != nil {
		return SchedulerRootRow{}, false, fmt.Errorf("store: read scheduler root %s: %w", componentRoot, err)
	}
	return r, true, nil
}

// UpsertSchedulerRoot lays or edits a repository's authority row. The
// decision word is DERIVED: enabled=1 writes 'enabled', enabled=0 writes
// 'disabled' — the row IS the intent, so a caller cannot pair a disabled
// write with an 'enabled' audit line. The window columns arrive
// pair-or-absent; the database itself refuses the crossed pair.
func (s *Store) UpsertSchedulerRoot(actor SchedulerActor, row SchedulerRootRow) (SchedulerRootRow, error) {
	if !actor.validControl() {
		return SchedulerRootRow{}, fmt.Errorf("store: scheduler root %s: actor %q/%q is not a control voice", row.ComponentRoot, actor.Kind, actor.ID)
	}
	if row.ComponentRoot == "" || row.Blueprint == "" {
		return SchedulerRootRow{}, errors.New("store: scheduler root needs a component root and a blueprint")
	}
	if row.MinPriority == "" {
		row.MinPriority = "assumed"
	}
	if row.MinPriority != "known" && row.MinPriority != "assumed" {
		return SchedulerRootRow{}, fmt.Errorf("store: scheduler root %s: min_priority %q is not known or assumed", row.ComponentRoot, row.MinPriority)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return SchedulerRootRow{}, err
	}
	defer tx.Rollback()
	now := s.Now()
	var createdAt any = now
	existing, found, err := func() (SchedulerRootRow, bool, error) {
		r, err := scanSchedulerRoot(tx.QueryRow(`SELECT `+schedulerRootColumns+` FROM scheduler_root WHERE component_root = ?`, row.ComponentRoot))
		if errors.Is(err, sql.ErrNoRows) {
			return SchedulerRootRow{}, false, nil
		}
		if err != nil {
			return SchedulerRootRow{}, false, fmt.Errorf("store: read scheduler root %s: %w", row.ComponentRoot, err)
		}
		return r, true, nil
	}()
	if err != nil {
		return SchedulerRootRow{}, err
	}
	if found {
		createdAt = existing.CreatedAt
	}
	nullish := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	_, err = tx.Exec(`INSERT INTO scheduler_root
		(component_root, enabled, blueprint, tech, with_json, window_start, window_end, window_days, zone, min_priority, created_at, updated_at, updated_by_kind, updated_by_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(component_root) DO UPDATE SET
		  enabled=excluded.enabled, blueprint=excluded.blueprint, tech=excluded.tech, with_json=excluded.with_json,
		  window_start=excluded.window_start, window_end=excluded.window_end, window_days=excluded.window_days,
		  zone=excluded.zone, min_priority=excluded.min_priority, updated_at=excluded.updated_at,
		  updated_by_kind=excluded.updated_by_kind, updated_by_id=excluded.updated_by_id`,
		row.ComponentRoot, b2i(row.Enabled), row.Blueprint, nullish(row.Tech), nullish(row.WithJSON),
		nullish(row.WindowStart), nullish(row.WindowEnd), nullish(row.WindowDays), nullish(row.Zone),
		row.MinPriority, createdAt, now, actor.Kind, actor.ID)
	if err != nil {
		return SchedulerRootRow{}, fmt.Errorf("store: upsert scheduler root %s: %w", row.ComponentRoot, err)
	}
	word := "disabled"
	if row.Enabled {
		word = "enabled"
	}
	if _, err := appendDecisionTx(tx, s, SchedulerDecision{
		ComponentRoot: row.ComponentRoot, SubjectKind: "root", Decision: word,
		Summary:   fmt.Sprintf("%s scheduler for root %s", word, row.ComponentRoot),
		ActorKind: actor.Kind, ActorID: actor.ID,
	}); err != nil {
		return SchedulerRootRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return SchedulerRootRow{}, err
	}
	out, _, err := s.SchedulerRoot(row.ComponentRoot)
	return out, err
}

// DisableSchedulerRoot retires a repository's authority without deleting
// its row — the decisions and the claim ledger outlive the grant (CN16).
// False means no such root.
func (s *Store) DisableSchedulerRoot(actor SchedulerActor, componentRoot string) (bool, error) {
	if !actor.validControl() {
		return false, fmt.Errorf("store: scheduler root %s: actor %q/%q is not a control voice", componentRoot, actor.Kind, actor.ID)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE scheduler_root SET enabled = 0, updated_at = ?, updated_by_kind = ?, updated_by_id = ?
		WHERE component_root = ? AND enabled = 1`, s.Now(), actor.Kind, actor.ID, componentRoot)
	if err != nil {
		return false, fmt.Errorf("store: disable scheduler root %s: %w", componentRoot, err)
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		if _, err := appendDecisionTx(tx, s, SchedulerDecision{
			ComponentRoot: componentRoot, SubjectKind: "root", Decision: "disabled",
			Summary:   fmt.Sprintf("disabled scheduler for root %s", componentRoot),
			ActorKind: actor.Kind, ActorID: actor.ID,
		}); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n == 1, nil
}

// ---------------------------------------------------------------------------
// the claim ledger (CN14) — one row per subject, ever.
// ---------------------------------------------------------------------------

// ClaimStart takes the subject's ONLY attempt at a scheduler start. False
// is the honest loser's answer: somebody (or an earlier self) holds the PK.
func (s *Store) ClaimStart(subjectKind string, subjectID int64, componentRoot string, decisionID int64, at string) (bool, error) {
	if subjectKind != "queue" && subjectKind != "roadmap" {
		return false, fmt.Errorf("store: claim start: subject kind %q claims nothing", subjectKind)
	}
	if decisionID <= 0 {
		return false, fmt.Errorf("store: claim start for %s %d: a claim names its decision (decisionID %d)", subjectKind, subjectID, decisionID)
	}
	if at == "" {
		at = s.Now()
	}
	res, err := s.w.Exec(`INSERT INTO scheduler_start (subject_kind, subject_id, decision_id, component_root, claimed_at)
		VALUES (?,?,?,?,?) ON CONFLICT (subject_kind, subject_id) DO NOTHING`,
		subjectKind, subjectID, decisionID, componentRoot, at)
	if err != nil {
		return false, fmt.Errorf("store: claim start for %s %d: %w", subjectKind, subjectID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: claim start for %s %d: %w", subjectKind, subjectID, err)
	}
	return n == 1, nil
}

// StampStart records which loop the claim turned into (the pointer the
// reactors will follow), SCOPED to the component root the claim carries —
// I4's rule the grep enforces: a slug names a loop inside one repository,
// and a stamp under the wrong root is a refusal, not a cross-wire. It never
// sets the outcome — FinishStart does.
func (s *Store) StampStart(subjectKind string, subjectID int64, componentRoot, loopSlug string) (bool, error) {
	if componentRoot == "" {
		return false, fmt.Errorf("store: stamp start %s %d: a stamp is scoped to a component root", subjectKind, subjectID)
	}
	res, err := s.w.Exec(`UPDATE scheduler_start SET loop_slug = ? WHERE subject_kind = ? AND subject_id = ? AND component_root = ? AND loop_slug IS NULL`,
		loopSlug, subjectKind, subjectID, componentRoot)
	if err != nil {
		return false, fmt.Errorf("store: stamp start %s %d: %w", subjectKind, subjectID, err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// FinishStart decides a claim's outcome EXACTLY ONCE: an already-decided
// row is not re-decided (the crash shape and the double-decision both land
// as false).
func (s *Store) FinishStart(subjectKind string, subjectID int64, outcome string) (bool, error) {
	if !schedulerOutcomes[outcome] {
		return false, fmt.Errorf("store: finish start %s %d: outcome %q is not started, refused or lost", subjectKind, subjectID, outcome)
	}
	res, err := s.w.Exec(`UPDATE scheduler_start SET outcome = ?, outcome_at = ?
		WHERE subject_kind = ? AND subject_id = ? AND outcome IS NULL`,
		outcome, s.Now(), subjectKind, subjectID)
	if err != nil {
		return false, fmt.Errorf("store: finish start %s %d: %w", subjectKind, subjectID, err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// OpenStarts is recovery's reading: claimed, undecided, in claim order.
func (s *Store) OpenStarts() ([]SchedulerStartRow, error) {
	rows, err := s.r.Query(`SELECT ` + schedulerStartColumns + ` FROM scheduler_start
		WHERE outcome IS NULL ORDER BY claimed_at, subject_kind, subject_id`)
	if err != nil {
		return nil, fmt.Errorf("store: read open starts: %w", err)
	}
	defer rows.Close()
	var out []SchedulerStartRow
	for rows.Next() {
		r, err := scanSchedulerStart(rows)
		if err != nil {
			return nil, fmt.Errorf("store: read open starts: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StartRecord reads one subject's claim — the one-attempt check (T5's pick
// refuses a subject with ANY record) needs it even when decided.
func (s *Store) StartRecord(subjectKind string, subjectID int64) (SchedulerStartRow, bool, error) {
	r, err := scanSchedulerStart(s.r.QueryRow(`SELECT `+schedulerStartColumns+` FROM scheduler_start WHERE subject_kind = ? AND subject_id = ?`,
		subjectKind, subjectID))
	if errors.Is(err, sql.ErrNoRows) {
		return SchedulerStartRow{}, false, nil
	}
	if err != nil {
		return SchedulerStartRow{}, false, fmt.Errorf("store: read start record %s %d: %w", subjectKind, subjectID, err)
	}
	return r, true, nil
}

const schedulerStartColumns = `subject_kind, subject_id, decision_id, component_root, claimed_at,
	COALESCE(loop_slug,''), COALESCE(outcome,''), COALESCE(outcome_at,'')`

func scanSchedulerStart(row interface{ Scan(...any) error }) (SchedulerStartRow, error) {
	var r SchedulerStartRow
	err := row.Scan(&r.SubjectKind, &r.SubjectID, &r.DecisionID, &r.ComponentRoot,
		&r.ClaimedAt, &r.LoopSlug, &r.Outcome, &r.OutcomeAt)
	return r, err
}

// StartsSince counts the scheduler's STARTED loops in a root since t — the
// rate window's reading.
func (s *Store) StartsSince(componentRoot string, since time.Time) (int, error) {
	var n int
	err := s.r.QueryRow(`SELECT COUNT(*) FROM scheduler_start
		WHERE component_root = ? AND outcome = 'started' AND claimed_at >= ?`,
		componentRoot, isoUTC(since)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: starts since for %s: %w", componentRoot, err)
	}
	return n, nil
}

// StartTimesSince reads the same window StartsSince counts, as instants
// oldest first (S27 T5): CN12's admission needs the OLDEST start, because
// the window's leave-instant is that start plus an hour — a count answers
// "is it full" but never "when does it open".
func (s *Store) StartTimesSince(componentRoot string, since time.Time) ([]time.Time, error) {
	rows, err := s.r.Query(`SELECT claimed_at FROM scheduler_start
		WHERE component_root = ? AND outcome = 'started' AND claimed_at >= ?
		ORDER BY claimed_at ASC`, componentRoot, isoUTC(since))
	if err != nil {
		return nil, fmt.Errorf("store: start times since for %s: %w", componentRoot, err)
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var at string
		if err := rows.Scan(&at); err != nil {
			return nil, fmt.Errorf("store: start times since for %s: %w", componentRoot, err)
		}
		t, err := time.Parse("2006-01-02T15:04:05.000Z", at)
		if err != nil {
			return nil, fmt.Errorf("store: start times since for %s: unreadable at %q", componentRoot, at)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// the decision log — append-only, deduped for the chatty words.
// ---------------------------------------------------------------------------

// AppendDecision writes one line. Consecutive 'deferred'/'skipped' for the
// SAME subject with the SAME reason write NOTHING new and answer the held
// row's id — a tick that changes nothing must not grow the log (the signal
// stays a signal). Every other word, and any change of reason, is a row.
func (s *Store) AppendDecision(d SchedulerDecision) (int64, error) {
	return appendDecisionTx(s.w, s, d)
}

type decisionWriter interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}

func appendDecisionTx(w decisionWriter, s *Store, d SchedulerDecision) (int64, error) {
	if !schedulerSubjects[d.SubjectKind] {
		return 0, fmt.Errorf("store: decision subject kind %q is not queue, roadmap, root or daemon", d.SubjectKind)
	}
	if !schedulerDecisions[d.Decision] {
		return 0, fmt.Errorf("store: decision word %q is not in the closed set", d.Decision)
	}
	if d.Summary == "" {
		return 0, fmt.Errorf("store: decision %q names no summary", d.Decision)
	}
	if !(SchedulerActor{Kind: d.ActorKind, ID: d.ActorID}).validActor() {
		return 0, fmt.Errorf("store: decision actor %q/%q is refused", d.ActorKind, d.ActorID)
	}
	if d.At == "" {
		d.At = s.Now()
	}
	// CN16's dedupe covers deferred and skipped. OD7 adds weight-exceeds-cap
	// and CN13 adds template-invalid to the same rule: these refusals are
	// facts a snapshot restates every tick (the candidate outlives the plan
	// that named it), and a plan-level "once" is impossible in a pure
	// function of a snapshot — without this, the refused repository writes
	// one row per tick forever, the exact growth CN16 exists to stop.
	// "Never retried" (T11 scenario 7) needs one more shape: a refusal and
	// its needs-you pair each write ONCE SINCE the subject's last
	// non-refusal-family row — an alternation would defeat a plain
	// last-row comparison, and a restart must not restart the writing
	// either. Every other refusal and every needs-you keeps its
	// every-write: they ride single events, not ticks.
	if d.Decision == "needs-you" && d.Reason != "" && d.Reason != "template-invalid" {
		// The blocked family's needs-you is the last-row law's too: TWO
		// reactors read the same blocked event and each may file the
		// same sentence (the T11 audit caught the race — two rows from
		// one block). The reactor-side peek is the fast path; the store
		// is the arbiter, inside the same write. template-invalid stays
		// with the refusal pair's rule below, not the last row's.
		var heldID int64
		var lastDecision, lastReason string
		err := w.QueryRow(`SELECT id, decision, reason FROM scheduler_decision
			WHERE subject_kind = ? AND subject_id = ? AND component_root = ?
			ORDER BY id DESC LIMIT 1`,
			d.SubjectKind, d.SubjectID, d.ComponentRoot).Scan(&heldID, &lastDecision, &lastReason)
		if err == nil && lastDecision == "needs-you" && lastReason == d.Reason {
			return heldID, nil
		}
	}
	if d.Decision == "deferred" || d.Decision == "skipped" {
		var heldID int64
		var lastDecision, lastReason string
		// The component root joins the identity: a repository-level row's
		// subject IS its root (kind root, id 0 — the root names itself),
		// and without the root in the comparison one repository's
		// deferral would swallow every other repository's sentence
		// (measured in S27 T11 across two windowed roots).
		err := w.QueryRow(`SELECT id, decision, reason FROM scheduler_decision
			WHERE subject_kind = ? AND subject_id = ? AND component_root = ?
			ORDER BY id DESC LIMIT 1`,
			d.SubjectKind, d.SubjectID, d.ComponentRoot).Scan(&heldID, &lastDecision, &lastReason)
		if err == nil && lastDecision == d.Decision && lastReason == d.Reason {
			return heldID, nil // the dedupe: no growth, the held id answers
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("store: read the last decision for %s %d: %w", d.SubjectKind, d.SubjectID, err)
		}
	}
	if (d.Decision == "refused" || d.Decision == "needs-you") &&
		(d.Reason == "weight-exceeds-cap" || d.Reason == "template-invalid") {
		var seen int
		var heldID int64
		err := w.QueryRow(`SELECT COUNT(*), COALESCE(MAX(id), 0) FROM scheduler_decision
			WHERE subject_kind = ? AND subject_id = ? AND component_root = ? AND decision = ? AND reason = ?
			  AND id > COALESCE((SELECT MAX(id) FROM scheduler_decision
			  	  WHERE subject_kind = ? AND subject_id = ? AND component_root = ?
			  	    AND decision NOT IN ('refused', 'needs-you')), 0)`,
			d.SubjectKind, d.SubjectID, d.ComponentRoot, d.Decision, d.Reason,
			d.SubjectKind, d.SubjectID, d.ComponentRoot).Scan(&seen, &heldID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("store: read the refusal pair for %s %d: %w", d.SubjectKind, d.SubjectID, err)
		}
		if seen > 0 {
			return heldID, nil // the never-retried dedupe: the held id answers
		}
	}
	res, err := w.Exec(`INSERT INTO scheduler_decision
		(at, component_root, subject_kind, subject_id, decision, reason, summary, grant_revision, actor_kind, actor_id, loop_slug)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		d.At, d.ComponentRoot, d.SubjectKind, d.SubjectID, d.Decision, d.Reason, d.Summary,
		d.GrantRevision, d.ActorKind, d.ActorID, d.LoopSlug)
	if err != nil {
		return 0, fmt.Errorf("store: append decision %q: %w", d.Decision, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: append decision %q: %w", d.Decision, err)
	}
	return id, nil
}

type DecisionQuery struct {
	ComponentRoot string
	SubjectKind   string
	SubjectID     int64
	HasSubject    bool
	Decision      string
	Reason        string // S27 T7: the coded token (capacity, cap-hit, …); equality only
	Since         string // RFC3339 lower bound on at, "" = none
	Limit         int    // default 100
}

// Decisions reads the newest first — the UI feed and the CLI's show lines.
func (s *Store) Decisions(q DecisionQuery) ([]SchedulerDecision, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	stmt := `SELECT id, at, component_root, subject_kind, subject_id, decision, reason, summary,
		grant_revision, actor_kind, actor_id, loop_slug FROM scheduler_decision WHERE 1=1`
	args := []any{}
	if q.ComponentRoot != "" {
		stmt += ` AND component_root = ?`
		args = append(args, q.ComponentRoot)
	}
	if q.SubjectKind != "" {
		stmt += ` AND subject_kind = ?`
		args = append(args, q.SubjectKind)
	}
	if q.HasSubject {
		stmt += ` AND subject_id = ?`
		args = append(args, q.SubjectID)
	}
	if q.Decision != "" {
		if !schedulerDecisions[q.Decision] {
			return nil, fmt.Errorf("store: decisions filter %q is not in the closed set", q.Decision)
		}
		stmt += ` AND decision = ?`
		args = append(args, q.Decision)
	}
	if q.Reason != "" {
		stmt += ` AND reason = ?`
		args = append(args, q.Reason)
	}
	if q.Since != "" {
		stmt += ` AND at >= ?`
		args = append(args, q.Since)
	}
	stmt += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.Query(stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read decisions: %w", err)
	}
	defer rows.Close()
	var out []SchedulerDecision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("store: read decisions: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanDecision(row interface{ Scan(...any) error }) (SchedulerDecision, error) {
	var d SchedulerDecision
	err := row.Scan(&d.ID, &d.At, &d.ComponentRoot, &d.SubjectKind, &d.SubjectID, &d.Decision,
		&d.Reason, &d.Summary, &d.GrantRevision, &d.ActorKind, &d.ActorID, &d.LoopSlug)
	return d, err
}

// LastDecision is T5's anti-thrash and T3's aging anchor: the newest line
// for one subject.
func (s *Store) LastDecision(subjectKind string, subjectID int64) (SchedulerDecision, bool, error) {
	d, err := scanDecision(s.r.QueryRow(`SELECT id, at, component_root, subject_kind, subject_id, decision, reason, summary,
		grant_revision, actor_kind, actor_id, loop_slug FROM scheduler_decision
		WHERE subject_kind = ? AND subject_id = ? ORDER BY id DESC LIMIT 1`, subjectKind, subjectID))
	if errors.Is(err, sql.ErrNoRows) {
		return SchedulerDecision{}, false, nil
	}
	if err != nil {
		return SchedulerDecision{}, false, fmt.Errorf("store: last decision for %s %d: %w", subjectKind, subjectID, err)
	}
	return d, true, nil
}

// FirstDeferral anchors the aging rule (CN11): the FIRST 'deferred' line of
// the CURRENT streak for one subject — the streak being the run of
// deferred/skipped lines after the most recent line that broke it (any
// other word). reason "" matches any deferred. False means no open streak.
func (s *Store) FirstDeferral(subjectKind string, subjectID int64, reason string) (SchedulerDecision, bool, error) {
	stmt := `SELECT id, at, component_root, subject_kind, subject_id, decision, reason, summary,
		grant_revision, actor_kind, actor_id, loop_slug FROM scheduler_decision
		WHERE subject_kind = ? AND subject_id = ? AND decision = 'deferred'
		  AND id > COALESCE((SELECT MAX(id) FROM scheduler_decision
			WHERE subject_kind = ? AND subject_id = ?
			  AND decision NOT IN ('deferred','skipped')), 0)`
	args := []any{subjectKind, subjectID, subjectKind, subjectID}
	if reason != "" {
		stmt += ` AND reason = ?`
		args = append(args, reason)
	}
	d, err := scanDecision(s.r.QueryRow(stmt+` ORDER BY id ASC LIMIT 1`, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return SchedulerDecision{}, false, nil
	}
	if err != nil {
		return SchedulerDecision{}, false, fmt.Errorf("store: first deferral for %s %d: %w", subjectKind, subjectID, err)
	}
	return d, true, nil
}

// LastSchedulerStart answers "when did this root's scheduler last START a
// loop" — the idle threshold's anchor. False means never.
func (s *Store) LastSchedulerStart(componentRoot string) (time.Time, bool, error) {
	var at string
	err := s.r.QueryRow(`SELECT at FROM scheduler_decision
		WHERE component_root = ? AND decision = 'started' AND actor_kind = 'scheduler'
		ORDER BY id DESC LIMIT 1`, componentRoot).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("store: last scheduler start for %s: %w", componentRoot, err)
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", at)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("store: last scheduler start for %s: unreadable at %q", componentRoot, at)
	}
	return t, true, nil
}

// ---------------------------------------------------------------------------
// the occupancy reading (CN8 — the words S20/S58 already use).
// ---------------------------------------------------------------------------

type OccupyingLoop struct {
	Slug   string
	Status string // running | waiting | blocked
}

// OccupyingLoops is the StillGoing set over a repository's loop rows.
func (s *Store) OccupyingLoops(componentRoot string) ([]OccupyingLoop, error) {
	rows, err := s.r.Query(`SELECT slug, status FROM loop
		WHERE component_root = ? AND status IN ('running','waiting','blocked') ORDER BY slug`, componentRoot)
	if err != nil {
		return nil, fmt.Errorf("store: occupying loops for %s: %w", componentRoot, err)
	}
	defer rows.Close()
	var out []OccupyingLoop
	for rows.Next() {
		var l OccupyingLoop
		if err := rows.Scan(&l.Slug, &l.Status); err != nil {
			return nil, fmt.Errorf("store: occupying loops for %s: %w", componentRoot, err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

type LoopSlot struct {
	ID   string
	Slug string
}

// RunningLoopSlots feeds the daemon's `active ∩ running` intersection
// (CN8): the runner owns `active`, this owns `running`.
func (s *Store) RunningLoopSlots() ([]LoopSlot, error) {
	rows, err := s.r.Query(`SELECT id, slug FROM loop WHERE status = 'running' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: running loop slots: %w", err)
	}
	defer rows.Close()
	var out []LoopSlot
	for rows.Next() {
		var l LoopSlot
		if err := rows.Scan(&l.ID, &l.Slug); err != nil {
			return nil, fmt.Errorf("store: running loop slots: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// the dependency graph — AddDependency refuses BEFORE the edge, naming the
// cycle: the triggers catch self/direct-reverse/cross-root, the walk here
// catches every longer cycle (a trigger cannot recurse portably).
// ---------------------------------------------------------------------------

type DependencyEdge struct {
	ItemID        int64
	DependsOnID   int64
	CreatedAt     string
	CreatedByKind string
	CreatedByID   string
}

func (s *Store) AddDependency(actor SchedulerActor, itemID, dependsOnID int64) error {
	if !actor.validControl() {
		return fmt.Errorf("store: roadmap dependency: actor %q/%q is not a control voice", actor.Kind, actor.ID)
	}
	if itemID == dependsOnID {
		return fmt.Errorf("store: roadmap: item %d cannot depend on itself", itemID)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var aRoot, bRoot string
	err = tx.QueryRow(`SELECT i.component_root FROM roadmap_item i WHERE i.id = ?`, itemID).Scan(&aRoot)
	if err == sql.ErrNoRows {
		return fmt.Errorf("store: roadmap: item %d does not exist", itemID)
	}
	if err != nil {
		return fmt.Errorf("store: roadmap dependency: %w", err)
	}
	err = tx.QueryRow(`SELECT i.component_root FROM roadmap_item i WHERE i.id = ?`, dependsOnID).Scan(&bRoot)
	if err == sql.ErrNoRows {
		return fmt.Errorf("store: roadmap: item %d does not exist", dependsOnID)
	}
	if err != nil {
		return fmt.Errorf("store: roadmap dependency: %w", err)
	}
	if aRoot != bRoot {
		return fmt.Errorf("store: roadmap: items %d and %d live in different repositories (%s vs %s) — one item's dependencies belong to one repository",
			itemID, dependsOnID, aRoot, bRoot)
	}
	var dup int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM roadmap_dependency WHERE item_id = ? AND depends_on_id = ?`, itemID, dependsOnID).Scan(&dup); err != nil {
		return fmt.Errorf("store: roadmap dependency: %w", err)
	}
	if dup > 0 {
		return fmt.Errorf("store: roadmap: item %d already depends on item %d", itemID, dependsOnID)
	}
	var reverse int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM roadmap_dependency WHERE item_id = ? AND depends_on_id = ?`, dependsOnID, itemID).Scan(&reverse); err != nil {
		return fmt.Errorf("store: roadmap dependency: %w", err)
	}
	if reverse > 0 {
		// The same sentence the trigger would raise — answered here so the
		// Go door and the database never disagree on the words.
		return fmt.Errorf("store: roadmap_dependency: the reverse edge already exists (a 2-cycle would deadlock the graph)")
	}
	// The cycle walk: an edge item → dependsOn closes a cycle exactly when
	// item already sits in dependsOn's upstream closure (dependsOn depends,
	// directly or transitively, on item — the new edge would complete the
	// ring). A trigger cannot recurse portably; this walk is the store's
	// half of CN14's graph.
	cycles, err := tx.Query(`WITH RECURSIVE upstream(id) AS (
			SELECT depends_on_id FROM roadmap_dependency WHERE item_id = ?
			UNION
			SELECT d.depends_on_id FROM roadmap_dependency d JOIN upstream u ON d.item_id = u.id
		) SELECT id FROM upstream`, dependsOnID)
	if err != nil {
		return fmt.Errorf("store: roadmap dependency walk: %w", err)
	}
	blocked := false
	for cycles.Next() {
		var id int64
		if err := cycles.Scan(&id); err == nil && id == itemID {
			blocked = true
		}
	}
	cycles.Close()
	if err := cycles.Err(); err != nil {
		return fmt.Errorf("store: roadmap dependency walk: %w", err)
	}
	if blocked {
		return fmt.Errorf("store: roadmap: item %d cannot depend on item %d — item %d already depends on item %d; the graph would cycle and neither item could ever become ready",
			itemID, dependsOnID, dependsOnID, itemID)
	}
	if _, err := tx.Exec(`INSERT INTO roadmap_dependency (item_id, depends_on_id, created_at, created_by_kind, created_by_id)
		VALUES (?,?,?,?,?)`, itemID, dependsOnID, s.Now(), actor.Kind, actor.ID); err != nil {
		return fmt.Errorf("store: add roadmap dependency %d → %d: %w", itemID, dependsOnID, err)
	}
	return tx.Commit()
}

// RemoveDependency drops one edge; false means there was no such edge.
// (The forward walk never blocks a cancel, so removal always helps — the
// refusal is for edges that do not exist, never for "would close a cycle".)
func (s *Store) RemoveDependency(itemID, dependsOnID int64) (bool, error) {
	res, err := s.w.Exec(`DELETE FROM roadmap_dependency WHERE item_id = ? AND depends_on_id = ?`, itemID, dependsOnID)
	if err != nil {
		return false, fmt.Errorf("store: remove roadmap dependency %d → %d: %w", itemID, dependsOnID, err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

type DependencyItem struct {
	ID     int64
	Status string
	Title  string
}

// RoadmapDependencies lists one item's direct upstream edges with the named
// items' current status — roadmap.show's dependency section (T5). It carries
// the roadmap prefix because `Store.Dependencies` is already taken by the
// framework's store-initialisation contract (framework.go:106).
func (s *Store) RoadmapDependencies(itemID int64) ([]DependencyItem, error) {
	return s.dependencyWalk(itemID, `d.item_id = ?`, `d.depends_on_id = i.id`)
}

// Dependents lists one item's direct downstream items — the sentence the
// cancel-rejection names (T5: "item 7 has 3 items depending on it").
func (s *Store) Dependents(itemID int64) ([]DependencyItem, error) {
	return s.dependencyWalk(itemID, `d.depends_on_id = ?`, `d.item_id = i.id`)
}

func (s *Store) dependencyWalk(itemID int64, where, join string) ([]DependencyItem, error) {
	rows, err := s.r.Query(`SELECT i.id, i.status, i.title FROM roadmap_dependency d
		JOIN roadmap_item i ON `+join+` WHERE `+where+` ORDER BY i.id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("store: roadmap dependencies of %d: %w", itemID, err)
	}
	defer rows.Close()
	var out []DependencyItem
	for rows.Next() {
		var d DependencyItem
		if err := rows.Scan(&d.ID, &d.Status, &d.Title); err != nil {
			return nil, fmt.Errorf("store: roadmap dependencies of %d: %w", itemID, err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// OpenDependencies is T3's readiness condition: the upstream items of
// itemID that are not 'done' — empty means the dependency gate is clear.
func (s *Store) OpenDependencies(itemID int64) ([]DependencyItem, error) {
	all, err := s.RoadmapDependencies(itemID)
	if err != nil {
		return nil, err
	}
	var out []DependencyItem
	for _, d := range all {
		if d.Status != "done" {
			out = append(out, d)
		}
	}
	return out, nil
}
