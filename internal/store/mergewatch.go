// mergewatch.go — S57 T7's post-merge watch ledger (VG5) and the
// gate-filing dedupe ledger (VG16). The watch is COLD: every answer comes
// from these rows, never from memory, so a restart resumes from
// next_check_at and a crash between RecordMergeEvent and the enqueue is
// repaired by the startup sweep reading the merge events back.
//
// The state machine is one-directional on purpose:
//
//	pending ──check──▶ unknown ──check──▶ green   (ends, the job is done)
//	    │  ▲             │  └────────▶ red     (ends, the filer has fired)
//	    │  └─────────────┘
//	    └──────────────▶ expired (the window elapsed; absence is not a
//	                     failure — a watch that never got an answer is
//	                     expired, NEVER red)
//
// green and red are terminal; the poll stops. expired is terminal for the
// same reason: the window was the watch's contract.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The watch's closed state set (mirrored by the migration's CHECK and by
// internal/gatewatch's constants; the two are held in lockstep by
// TestWatchStatesMatchTheMigration).
const (
	WatchPending = "pending"
	WatchGreen   = "green"
	WatchRed     = "red"
	WatchUnknown = "unknown"
	WatchExpired = "expired"
)

// The filing's closed kind set (VG16): which gate failure the dedupe row
// stands for.
const (
	GateFilingVerify       = "verify"
	GateFilingMergeWatch   = "merge-watch"
	GateFilingMergeRefused = "merge-refused"
)

// MergeWatch is the row's scan shape.
type MergeWatch struct {
	MergeEventID  string
	ComponentID   string
	BaseRef       string
	SHA           string
	State         string
	Attempts      int
	NextCheckAt   string
	LastCheckedAt *string
	Detail        string
	CreatedAt     string
}

// MergeWatchEnqueue names the merge the watch covers. The window is the
// caller's (the gates.watchWindow setting's resolution); the store applies
// it — a merge whose window already elapsed (the daemon was down) is born
// expired, never pending, because a pending row would read as "we still
// intend to check" when the window says no.
type MergeWatchEnqueue struct {
	MergeEventID string
	ComponentID  string
	BaseRef      string
	SHA          string
	MergedAt     time.Time // the window's start
	Window       time.Duration
}

// DueMergeWatchRow is a due row plus the merge event it watches — the
// join the poll needs so the filer and the events carry ids (loop slug,
// PR, rule revision, the measured run) and the root the poll forges
// through, without a second read.
type DueMergeWatchRow struct {
	MergeWatch
	ComponentRoot string
	LoopSlug      string
	PR            int
	RuleRevision  *int
	VerifyRunID   *string
}

// MergeWatchFilter scopes ListMergeWatches; empty means no filter. The
// loop filter is the full Scope — a loop names both halves, through the
// merge event join (I4).
type MergeWatchFilter struct {
	State       string // one of the five
	ComponentID string
	Scope       Scope // both halves, or neither
	Limit       int   // 0 = no limit
}

const mergeWatchColumns = `merge_event_id, component_id, base_ref, sha, state, attempts,
	next_check_at, last_checked_at, detail, created_at`

const mergeWatchJoinColumns = `w.merge_event_id, w.component_id, w.base_ref, w.sha, w.state,
	w.attempts, w.next_check_at, w.last_checked_at, w.detail, w.created_at,
	e.component_root, e.loop_slug, e.pr, e.rule_revision, e.verify_run_id`

func scanDueWatch(row interface{ Scan(...any) error }) (DueMergeWatchRow, error) {
	var (
		r    DueMergeWatchRow
		last sql.NullString
		rev  sql.NullInt64
		run  sql.NullString
	)
	err := row.Scan(&r.MergeEventID, &r.ComponentID, &r.BaseRef, &r.SHA, &r.State,
		&r.Attempts, &r.NextCheckAt, &last, &r.Detail, &r.CreatedAt,
		&r.ComponentRoot, &r.LoopSlug, &r.PR, &rev, &run)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DueMergeWatchRow{}, fmt.Errorf("store: merge watch: %w", ErrNotFound)
		}
		return DueMergeWatchRow{}, fmt.Errorf("store: read merge watch: %w", err)
	}
	r.LastCheckedAt = nullStringPtr(last)
	if rev.Valid {
		n := int(rev.Int64)
		r.RuleRevision = &n
	}
	r.VerifyRunID = nullStringPtr(run)
	return r, nil
}

// EnqueueMergeWatch adds the watch for a merge, idempotent on the merge
// event: a second enqueue for the same merge is the already-watched answer
// and changes nothing (the re-entry's twin, or the startup sweep racing a
// live tick, finds the row and returns it). The caller records the merge
// event FIRST and enqueues after — a crash between the two leaves the
// event without a watch, which the sweep repairs.
func (s *Store) EnqueueMergeWatch(in MergeWatchEnqueue) (MergeWatch, error) {
	if in.MergeEventID == "" {
		return MergeWatch{}, fmt.Errorf("store: enqueue merge watch: names no merge event")
	}
	if in.ComponentID == "" {
		return MergeWatch{}, fmt.Errorf("store: enqueue merge watch: names no component — a watch without a component is a watch on nothing")
	}
	if in.BaseRef == "" {
		return MergeWatch{}, fmt.Errorf("store: enqueue merge watch: names no base ref — the watch says what branch it is watching")
	}
	if in.SHA == "" {
		return MergeWatch{}, fmt.Errorf("store: enqueue merge watch: names no sha — the checks are keyed by it")
	}
	if in.Window <= 0 {
		return MergeWatch{}, fmt.Errorf("store: enqueue merge watch: a non-positive window is a watch with no bound")
	}
	now := s.clock()
	state, detail := WatchPending, ""
	// A window that already elapsed is born expired: the daemon was down
	// across it (or the row is being repaired late), and "we will check"
	// would be a lie the poll then has to walk back.
	if !in.MergedAt.IsZero() && now.After(in.MergedAt.Add(in.Window)) {
		state, detail = WatchExpired, "window elapsed before first check"
	}
	next := now
	if state == WatchExpired {
		// A dead row keeps a far-future schedule: DueMergeWatches never
		// selects it, and the row says why in detail.
		next = now.Add(365 * 24 * time.Hour)
	}
	_, err := s.w.Exec(`INSERT INTO merge_watch (merge_event_id, component_id, base_ref, sha, state,
		attempts, next_check_at, detail, created_at)
		VALUES (?,?,?,?,?,0,?,?,?) ON CONFLICT(merge_event_id) DO NOTHING`,
		in.MergeEventID, in.ComponentID, in.BaseRef, in.SHA, state, isoUTC(next), detail, isoUTC(now))
	if err != nil {
		return MergeWatch{}, fmt.Errorf("store: enqueue merge watch: %w", err)
	}
	w, err := s.watchByID(in.MergeEventID)
	if err != nil {
		return MergeWatch{}, err
	}
	return w, nil
}

// watchByID reads one watch row by its merge-event key.
func (s *Store) watchByID(mergeEventID string) (MergeWatch, error) {
	var (
		w    MergeWatch
		last sql.NullString
	)
	err := s.r.QueryRow(`SELECT `+mergeWatchColumns+
		` FROM merge_watch WHERE merge_event_id = ?`, mergeEventID).
		Scan(&w.MergeEventID, &w.ComponentID, &w.BaseRef, &w.SHA, &w.State,
			&w.Attempts, &w.NextCheckAt, &last, &w.Detail, &w.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MergeWatch{}, fmt.Errorf("store: merge watch: %w", ErrNotFound)
		}
		return MergeWatch{}, fmt.Errorf("store: read merge watch: %w", err)
	}
	w.LastCheckedAt = nullStringPtr(last)
	return w, nil
}

// DueMergeWatches reads the rows the next poll must check: pending or
// unknown, scheduled at or before now, oldest first. It joins the merge
// event so the poll carries the ids the filer and the events name.
// Terminal rows (green/red/expired) never come back.
func (s *Store) DueMergeWatches(now time.Time, limit int) ([]DueMergeWatchRow, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT ` + mergeWatchJoinColumns + `
		FROM merge_watch w
		JOIN merge_event e ON e.id = w.merge_event_id
		WHERE w.state IN ('pending','unknown') AND w.next_check_at <= ?
		ORDER BY w.next_check_at, w.merge_event_id
		LIMIT ?`
	rows, err := s.r.Query(query, isoUTC(now), limit)
	if err != nil {
		return nil, fmt.Errorf("store: due merge watches: %w", err)
	}
	defer rows.Close()
	var out []DueMergeWatchRow
	for rows.Next() {
		r, err := scanDueWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: due merge watches: %w", err)
	}
	return out, nil
}

// UpdateMergeWatch records one poll's answer: the new state, its one-line
// detail, the next schedule and the attempt count. last_checked_at stamps
// the poll (a row that was never checked keeps NULL — the honest absence).
// Writing a terminal state is the only way a row stops being due.
func (s *Store) UpdateMergeWatch(mergeEventID, state, detail string, next time.Time, attempts int) error {
	if state != WatchPending && state != WatchGreen && state != WatchRed &&
		state != WatchUnknown && state != WatchExpired {
		return fmt.Errorf("store: update merge watch: state %q is not one of the five", state)
	}
	if detail != "" && len(detail) > 256 {
		return fmt.Errorf("store: update merge watch: the detail is %d chars, the cap is 256 — a one-liner, not a report", len(detail))
	}
	res, err := s.w.Exec(`UPDATE merge_watch SET state = ?, detail = ?, next_check_at = ?,
		attempts = ?, last_checked_at = ? WHERE merge_event_id = ?`,
		state, detail, isoUTC(next), attempts, isoUTC(s.clock()), mergeEventID)
	if err != nil {
		return fmt.Errorf("store: update merge watch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: update merge watch: no watch for merge event %s", mergeEventID)
	}
	return nil
}

// ExpireStaleMergeWatches turns every pending/unknown row whose window
// (from the merge's merged_at) has elapsed into expired. A row that
// carried a detail (its last check said "none-reported" or "unreadable")
// keeps it — the expired row says WHY it never ended; a row that was never
// checked gets the honest "window elapsed". Returns how many it turned.
func (s *Store) ExpireStaleMergeWatches(now time.Time, window time.Duration) (int, error) {
	if window <= 0 {
		return 0, fmt.Errorf("store: expire stale merge watches: a non-positive window is a watch with no bound")
	}
	res, err := s.w.Exec(`UPDATE merge_watch SET state = 'expired',
		detail = CASE WHEN detail = '' THEN 'window elapsed' ELSE detail END
		WHERE state IN ('pending','unknown')
		AND merge_event_id IN (
			SELECT w.merge_event_id FROM merge_watch w
			JOIN merge_event e ON e.id = w.merge_event_id
			WHERE e.merged_at <= ?)`, isoUTC(now.Add(-window)))
	if err != nil {
		return 0, fmt.Errorf("store: expire stale merge watches: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: expire stale merge watches: %w", err)
	}
	return int(n), nil
}

// ListMergeWatches reads the ledger the UI and the audits walk.
func (s *Store) ListMergeWatches(f MergeWatchFilter) ([]DueMergeWatchRow, error) {
	if f.State != "" && f.State != WatchPending && f.State != WatchGreen &&
		f.State != WatchRed && f.State != WatchUnknown && f.State != WatchExpired {
		return nil, fmt.Errorf("store: list merge watches: state %q is not one of the five", f.State)
	}
	if f.Scope.LoopSlug == "" && f.Scope.ComponentRoot != "" || f.Scope.LoopSlug != "" && f.Scope.ComponentRoot == "" {
		return nil, fmt.Errorf("store: list merge watches: a loop names both halves — a slug alone is not a loop")
	}
	where := []string{}
	args := []any{}
	if f.State != "" {
		where = append(where, "w.state = ?")
		args = append(args, f.State)
	}
	if f.ComponentID != "" {
		where = append(where, "w.component_id = ?")
		args = append(args, f.ComponentID)
	}
	if f.Scope.Valid() {
		w, wargs := f.Scope.Where("e")
		where = append(where, w)
		args = append(args, wargs...)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	query := `SELECT ` + mergeWatchJoinColumns + `
		FROM merge_watch w
		JOIN merge_event e ON e.id = w.merge_event_id` + clause +
		` ORDER BY w.created_at DESC, w.merge_event_id`
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list merge watches: %w", err)
	}
	defer rows.Close()
	var out []DueMergeWatchRow
	for rows.Next() {
		r, err := scanDueWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list merge watches: %w", err)
	}
	return out, nil
}

// MergeEventRef is the sweep's view of a merge event: just enough to
// decide whether it gets a watch and, if so, what the watch is.
type MergeEventRef struct {
	ID            string
	ComponentRoot string
	ComponentID   string
	PR            int
	MergeSHA      string
	MergedAt      string // ISO UTC (the window's start)
}

// MergeEventsWithoutWatch reads the crash window: merge events that stand
// with no watch row. The startup sweep gives each of them a watch (or
// skips the roots whose forge reads no checks), so a daemon that died
// between RecordMergeEvent and the enqueue does not lose the watch.
func (s *Store) MergeEventsWithoutWatch() ([]MergeEventRef, error) {
	rows, err := s.r.Query(`SELECT e.id, e.component_root, e.component_id, e.pr, e.merge_sha, e.merged_at
		FROM merge_event e
		LEFT JOIN merge_watch w ON w.merge_event_id = e.id
		WHERE w.merge_event_id IS NULL
		ORDER BY e.merged_at, e.id`)
	if err != nil {
		return nil, fmt.Errorf("store: merge events without watch: %w", err)
	}
	defer rows.Close()
	var out []MergeEventRef
	for rows.Next() {
		var (
			r   MergeEventRef
			cid sql.NullString
		)
		if err := rows.Scan(&r.ID, &r.ComponentRoot, &cid, &r.PR, &r.MergeSHA, &r.MergedAt); err != nil {
			return nil, fmt.Errorf("store: merge events without watch: %w", err)
		}
		r.ComponentID = cid.String
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: merge events without watch: %w", err)
	}
	return out, nil
}

// --- the gate-filing dedupe ledger (VG16) ---------------------------------

// ClaimGateFiling is the dedupe decision. An existing subject bumps its
// count and says so (existing=true, the new count) — the filer files
// NOTHING on an existing claim, because the bug already stands. A new
// subject gets its row (count 1) and files the bug. The row is the claim,
// the bug is the consequence: a crash between the two leaves count 1 with
// no bug_id, and the next filing of the same subject files, then stamps —
// the count never lies about how many times the gate failed.
func (s *Store) ClaimGateFiling(subject, kind string) (bool, int, error) {
	if subject == "" {
		return false, 0, fmt.Errorf("store: claim gate filing: names no subject")
	}
	if kind != GateFilingVerify && kind != GateFilingMergeWatch && kind != GateFilingMergeRefused {
		return false, 0, fmt.Errorf("store: claim gate filing: kind %q is not one of the three", kind)
	}
	var count int
	err := s.r.QueryRow(`SELECT count FROM gate_filing WHERE subject = ?`, subject).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.w.Exec(`INSERT INTO gate_filing (subject, kind, first_seen_at, count) VALUES (?,?,?,1)`,
			subject, kind, isoUTC(s.clock())); err != nil {
			return false, 0, fmt.Errorf("store: claim gate filing: %w", err)
		}
		return false, 1, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("store: claim gate filing: %w", err)
	}
	if _, err := s.w.Exec(`UPDATE gate_filing SET count = count + 1 WHERE subject = ?`, subject); err != nil {
		return false, 0, fmt.Errorf("store: claim gate filing: %w", err)
	}
	return true, count + 1, nil
}

// SetGateFilingBug stamps the filed bug's id on the claim. It touches only
// a claim that has none: a claim that already stands keeps its first bug —
// the dedupe is per subject, and the first bug is the one the ledger names.
func (s *Store) SetGateFilingBug(subject string, bugID int64) error {
	res, err := s.w.Exec(`UPDATE gate_filing SET bug_id = ? WHERE subject = ? AND bug_id IS NULL`,
		bugID, subject)
	if err != nil {
		return fmt.Errorf("store: set gate filing bug: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: set gate filing bug: no unfilled claim for subject %s", subject)
	}
	return nil
}

// GateFiling is the ledger row (the read the audits and the UI use).
type GateFiling struct {
	Subject     string
	BugID       *int64
	Kind        string
	FirstSeenAt string
	Count       int
}

// GateFilingBySubject reads one claim. Absent is ErrNotFound.
func (s *Store) GateFilingBySubject(subject string) (GateFiling, error) {
	var (
		g   GateFiling
		bug sql.NullInt64
	)
	err := s.r.QueryRow(`SELECT subject, bug_id, kind, first_seen_at, count FROM gate_filing WHERE subject = ?`,
		subject).Scan(&g.Subject, &bug, &g.Kind, &g.FirstSeenAt, &g.Count)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GateFiling{}, fmt.Errorf("store: gate filing: %w", ErrNotFound)
		}
		return GateFiling{}, fmt.Errorf("store: gate filing: %w", err)
	}
	if bug.Valid {
		g.BugID = &bug.Int64
	}
	return g, nil
}

// ListGateFilings reads the whole ledger, oldest first.
func (s *Store) ListGateFilings() ([]GateFiling, error) {
	rows, err := s.r.Query(`SELECT subject, bug_id, kind, first_seen_at, count FROM gate_filing ORDER BY first_seen_at, subject`)
	if err != nil {
		return nil, fmt.Errorf("store: list gate filings: %w", err)
	}
	defer rows.Close()
	var out []GateFiling
	for rows.Next() {
		var (
			g   GateFiling
			bug sql.NullInt64
		)
		if err := rows.Scan(&g.Subject, &bug, &g.Kind, &g.FirstSeenAt, &g.Count); err != nil {
			return nil, fmt.Errorf("store: list gate filings: %w", err)
		}
		if bug.Valid {
			g.BugID = &bug.Int64
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list gate filings: %w", err)
	}
	return out, nil
}
