package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// watchdog.go — the store layer behind S55's loop watchdog (T9, LC17/LC18).
// It holds the rule records and their append-only audit, and it moves a
// running loop by the SAME compare-and-set discipline the control core uses
// (LC2): a transition that loses its race is a no-op that writes NO event,
// and a changing act appends exactly one row.
//
// # What this file does
//
// One write path for rules (WatchdogSetTx, used by both the human door and the
// seeded default set), the enable/disable/remove variants (which append an
// audit row and never delete — a rule's history outlives the rule, P3), the
// read surface the service and the doors need, and the two loop moves the
// watchdog is the only caller of besides the control core: BlockLoopStuck (the
// unattended move, running→blocked, kind stuck) and AppendWatchdogFire (the
// notify arm's audit row, which moves nothing).
//
// # What this file does NOT do
//
// It never decides that a loop is stuck — that is internal/watchdog's pure
// Evaluate, and the daemon service that turns a firing into one of the moves
// here. It never deletes a rule row or an audit row (the triggers refuse). And
// it never retries or re-dispatches anything (OD8): a fired watchdog parks or
// blocks and stops, and only a person's resume moves the loop again.

// WatchdogRule is one watchdog_rule row. Threshold's unit is the signal's
// (milliseconds for the two time signals, a count for the two count signals);
// the unit is served by watchdog.vocabulary, never assumed here.
type WatchdogRule struct {
	ID        string
	Scope     string // workspace | component
	SubjectID string // 'workspace' or a component id (a root's rule covers its tree)
	Signal    string // no-activity | same-position | repeat-block-reason | step-over-deadline
	Threshold int64
	Action    string // notify | stop | block
	Enabled   bool
	Origin    string // system | user
	CreatedAt string
	UpdatedAt string
}

// WatchdogEvent is one watchdog_event row — the append-only audit of a rule's
// life (created | changed | removed | disabled). A firing is NOT a row here:
// it rides loop_control_event{kind:'watchdog'}, so a fire is never recorded
// twice (the migration's comment).
type WatchdogEvent struct {
	ID        int64
	RuleID    string
	At        string
	Kind      string // created | changed | removed | disabled
	ActorKind string // user | cli | system
	ActorID   string
	OldValue  *int64
	NewValue  *int64
}

// WatchdogSetIn is the one rule write's input. The key is the unique tuple
// (scope, subject_id, signal, action, threshold); a set with a key that exists
// updates that row in place (an edit), and one that does not creates a row.
// Actor is the peer identity stamped by the daemon (never a request field).
type WatchdogSetIn struct {
	Scope     string
	SubjectID string
	Signal    string
	Threshold int64
	Action    string
	Enabled   *bool // nil on a create (defaults to enabled)
	Origin    string
	ActorKind string
	ActorID   string
}

// newWatchdogID is a random hex id, prefixed so a rule row is recognizable in
// the audit the way a budget's is.
func newWatchdogID() string { return "wd-" + newID() }

// WatchdogSet is the one rule write outside an open transaction: it opens one,
// runs WatchdogSetTx on it, and commits. The Tx form is what the seeded set's
// applier uses (its row and its ledger row commit together); the non-Tx form
// is what the human door uses. Both go through the same body, so there is one
// place a rule row and its audit row are written.
func (s *Store) WatchdogSet(in WatchdogSetIn) (WatchdogRule, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return WatchdogRule{}, fmt.Errorf("store: watchdog set: %w", err)
	}
	rule, _, err := s.WatchdogSetTx(tx, in)
	if err != nil {
		tx.Rollback()
		return WatchdogRule{}, err
	}
	if err := tx.Commit(); err != nil {
		return WatchdogRule{}, fmt.Errorf("store: watchdog set: %w", err)
	}
	return rule, nil
}

// WatchdogSetTx is the one rule write, on a shared transaction: create-or-
// update by the unique key (scope, subject_id, signal, action, threshold), and
// the audit row the change earns (created, or changed — a set that changes
// nothing writes nothing, LC11). A no-op set (every field identical) writes no
// audit row and returns the existing row with Changed=false so the door can
// render "unchanged" rather than "updated".
func (s *Store) WatchdogSetTx(tx *sql.Tx, in WatchdogSetIn) (WatchdogRule, bool, error) {
	cur, found, err := s.FindWatchdogRuleTx(tx, in.Scope, in.SubjectID, in.Signal, in.Action, in.Threshold)
	if err != nil {
		return WatchdogRule{}, false, err
	}
	now := s.Now()
	if found {
		// An update in place: only the editable fields move (the key, the
		// origin and created_at are fixed — a rule that changes its key is a
		// different rule, and the seed's provenance is the origin).
		enabled := cur.Enabled
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		if cur.Enabled == enabled {
			// Nothing differs: a no-op set writes no audit row (LC11) and says
			// so with Changed=false.
			return cur, false, nil
		}
		res, err := tx.Exec(`UPDATE watchdog_rule SET enabled=?, updated_at=? WHERE id=?`,
			boolInt(enabled), now, cur.ID)
		if err != nil {
			return WatchdogRule{}, false, fmt.Errorf("store: update watchdog rule %s: %w", cur.ID, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return WatchdogRule{}, false, fmt.Errorf("store: update watchdog rule %s: %w", cur.ID, ErrNotFound)
		}
		// The audit's old value is the row's state BEFORE the flip — captured
		// before cur moves, or the column would carry the new value twice.
		old := cur.Enabled
		cur.Enabled = enabled
		cur.UpdatedAt = now
		if err := appendWatchdogEvent(tx, WatchdogEvent{
			RuleID: cur.ID, At: now, Kind: "changed",
			ActorKind: in.ActorKind, ActorID: in.ActorID,
			OldValue: int64Ptr(boolInt(old)), NewValue: int64Ptr(boolInt(enabled)),
		}); err != nil {
			return WatchdogRule{}, false, err
		}
		return cur, true, nil
	}
	// A create: a new row for the key, enabled by default unless the caller
	// said otherwise.
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	id := newWatchdogID()
	res, err := tx.Exec(`INSERT INTO watchdog_rule (id, scope, subject_id, signal, threshold, action, enabled, origin, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		id, in.Scope, in.SubjectID, in.Signal, in.Threshold, in.Action, boolInt(enabled), in.Origin, now, now)
	if err != nil {
		return WatchdogRule{}, false, fmt.Errorf("store: create watchdog rule: %w", err)
	}
	if _, err := res.LastInsertId(); err != nil {
		return WatchdogRule{}, false, fmt.Errorf("store: create watchdog rule: %w", err)
	}
	if err := appendWatchdogEvent(tx, WatchdogEvent{
		RuleID: id, At: now, Kind: "created",
		ActorKind: in.ActorKind, ActorID: in.ActorID,
		OldValue: nil, NewValue: int64Ptr(boolInt(enabled)),
	}); err != nil {
		return WatchdogRule{}, false, err
	}
	return WatchdogRule{
		ID: id, Scope: in.Scope, SubjectID: in.SubjectID, Signal: in.Signal,
		Threshold: in.Threshold, Action: in.Action, Enabled: enabled, Origin: in.Origin,
		CreatedAt: now, UpdatedAt: now,
	}, true, nil
}

// WatchdogRestoreTx is the seed applier's restore write (SD6): the explicit
// consent restore re-applies the record's content OVER the live row — enabled
// and origin move back to the record's, while the key, the id and created_at
// stay (the row's history outlives the restore, and a fire's rule_id keeps
// resolving). It has exactly one caller, the applier (the engine's plan is the
// only thing that decides a row is restorable), and it is NOT the door's write
// path: the door's WatchdogSetTx may never move origin (a rule's provenance is
// fixed at its birth, LC18), so a restore that needed origin moved could not
// ride it.
//
// Three outcomes, each honest: no row at all (the restore is a fresh create —
// the set's own birth, which earns its kind-'created' audit row through
// WatchdogSetTx); a row matching the record (a no-op, Changed false, nothing
// written); a diverged row — the operator's edit, disable or removal, brought
// back (the row moves, no watchdog_event: a restore is the tightening
// direction, the door's re-enable precedent, and the consent's audit is the
// engine's own op=restore ledger row, not the rule's).
func (s *Store) WatchdogRestoreTx(tx *sql.Tx, in WatchdogSetIn) (WatchdogRule, bool, error) {
	cur, found, err := s.FindWatchdogRuleTx(tx, in.Scope, in.SubjectID, in.Signal, in.Action, in.Threshold)
	if err != nil {
		return WatchdogRule{}, false, err
	}
	if !found {
		// No row: the restore is a create, through the one write path (its
		// kind-'created' audit row is the set's own birth).
		return s.WatchdogSetTx(tx, in)
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if cur.Enabled == enabled && cur.Origin == in.Origin {
		// The row already matches the record: nothing to restore (LC11).
		return cur, false, nil
	}
	now := s.Now()
	res, err := tx.Exec(`UPDATE watchdog_rule SET enabled=?, origin=?, updated_at=? WHERE id=?`,
		boolInt(enabled), in.Origin, now, cur.ID)
	if err != nil {
		return WatchdogRule{}, false, fmt.Errorf("store: restore watchdog rule %s: %w", cur.ID, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return WatchdogRule{}, false, fmt.Errorf("store: restore watchdog rule %s: %w", cur.ID, ErrNotFound)
	}
	cur.Enabled = enabled
	cur.Origin = in.Origin
	cur.UpdatedAt = now
	return cur, true, nil
}

// WatchdogSetEnabled is the enable/disable write: a single-field update by id
// that appends a 'disabled' audit row when it turns a rule off, and nothing
// when it turns one on that is already on (a no-op writes nothing, LC11).
// Disabling a rule LOOSENS a safeguard, which is why the human door demands a
// confirmation digest for it (T9); the store only records.
func (s *Store) WatchdogSetEnabled(id string, enabled bool, actorKind, actorID string) (WatchdogRule, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return WatchdogRule{}, fmt.Errorf("store: watchdog enable %s: %w", id, err)
	}
	defer tx.Rollback()
	cur, err := s.getWatchdogRuleTx(tx, id)
	if err != nil {
		return WatchdogRule{}, err
	}
	if cur.Enabled == enabled {
		// No change: a no-op writes nothing and says so with an unchanged row.
		tx.Rollback()
		return cur, nil
	}
	now := s.Now()
	res, err := tx.Exec(`UPDATE watchdog_rule SET enabled=?, updated_at=? WHERE id=?`, boolInt(enabled), now, id)
	if err != nil {
		return WatchdogRule{}, fmt.Errorf("store: enable %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return WatchdogRule{}, fmt.Errorf("store: enable %s: %w", id, ErrNotFound)
	}
	cur.Enabled = enabled
	cur.UpdatedAt = now
	// Only a disable appends (it loosens); a re-enable of a disabled rule
	// restores the default state and is the tightening direction, which the
	// door records through its own audit and the engine's status, not here.
	if !enabled {
		if err := appendWatchdogEvent(tx, WatchdogEvent{
			RuleID: id, At: now, Kind: "disabled",
			ActorKind: actorKind, ActorID: actorID,
			OldValue: int64Ptr(1), NewValue: int64Ptr(0),
		}); err != nil {
			return WatchdogRule{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return WatchdogRule{}, fmt.Errorf("store: enable %s: %w", id, err)
	}
	return cur, nil
}

// WatchdogRemove is the removal write: it appends a 'removed' audit row and
// marks the row gone by disabling it — the row is NEVER deleted (its audit and
// its id must outlive it, so the seed's ledger and the fire's rule_id keep
// resolving). A removal of a system rule loosens a safeguard and is confirmed
// at the door; the store only records.
func (s *Store) WatchdogRemove(id string, actorKind, actorID string) (WatchdogRule, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return WatchdogRule{}, fmt.Errorf("store: watchdog remove %s: %w", id, err)
	}
	defer tx.Rollback()
	cur, err := s.getWatchdogRuleTx(tx, id)
	if err != nil {
		return WatchdogRule{}, err
	}
	now := s.Now()
	res, err := tx.Exec(`UPDATE watchdog_rule SET enabled=0, updated_at=? WHERE id=?`, now, id)
	if err != nil {
		return WatchdogRule{}, fmt.Errorf("store: remove %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return WatchdogRule{}, fmt.Errorf("store: remove %s: %w", id, ErrNotFound)
	}
	if err := appendWatchdogEvent(tx, WatchdogEvent{
		RuleID: id, At: now, Kind: "removed",
		ActorKind: actorKind, ActorID: actorID,
		OldValue: int64Ptr(boolInt(cur.Enabled)), NewValue: int64Ptr(0),
	}); err != nil {
		return WatchdogRule{}, err
	}
	if err := tx.Commit(); err != nil {
		return WatchdogRule{}, fmt.Errorf("store: remove %s: %w", id, err)
	}
	cur.Enabled = false
	cur.UpdatedAt = now
	return cur, nil
}

// --- reads -------------------------------------------------------------------

func (s *Store) ListWatchdogRules() ([]WatchdogRule, error) {
	rows, err := s.r.Query(`SELECT id, scope, subject_id, signal, threshold, action, enabled, origin, created_at, updated_at
		FROM watchdog_rule ORDER BY scope, subject_id, signal, action, threshold, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list watchdog rules: %w", err)
	}
	defer rows.Close()
	var out []WatchdogRule
	for rows.Next() {
		var r WatchdogRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.Scope, &r.SubjectID, &r.Signal, &r.Threshold, &r.Action, &enabled, &r.Origin, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: list watchdog rules: %w", err)
		}
		r.Enabled = enabled != 0
		out = append(out, r)
	}
	if out == nil {
		out = []WatchdogRule{}
	}
	return out, rows.Err()
}

func (s *Store) GetWatchdogRule(id string) (WatchdogRule, error) {
	var r WatchdogRule
	var enabled int
	err := s.r.QueryRow(`SELECT id, scope, subject_id, signal, threshold, action, enabled, origin, created_at, updated_at
		FROM watchdog_rule WHERE id=?`, id).
		Scan(&r.ID, &r.Scope, &r.SubjectID, &r.Signal, &r.Threshold, &r.Action, &enabled, &r.Origin, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WatchdogRule{}, fmt.Errorf("store: watchdog rule %s: %w", id, ErrNotFound)
		}
		return WatchdogRule{}, fmt.Errorf("store: get watchdog rule %s: %w", id, err)
	}
	r.Enabled = enabled != 0
	return r, nil
}

// FindWatchdogRule reads a rule by its unique key (scope, subject_id, signal,
// action, threshold) — the seed applier's create-or-update discriminator and
// the set door's. present is false when the key has no row.
func (s *Store) FindWatchdogRule(scope, subjectID, signal, action string, threshold int64) (WatchdogRule, bool, error) {
	var r WatchdogRule
	var enabled int
	err := s.r.QueryRow(`SELECT id, scope, subject_id, signal, threshold, action, enabled, origin, created_at, updated_at
		FROM watchdog_rule WHERE scope=? AND subject_id=? AND signal=? AND action=? AND threshold=?`,
		scope, subjectID, signal, action, threshold).
		Scan(&r.ID, &r.Scope, &r.SubjectID, &r.Signal, &r.Threshold, &r.Action, &enabled, &r.Origin, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WatchdogRule{}, false, nil
		}
		return WatchdogRule{}, false, fmt.Errorf("store: find watchdog rule: %w", err)
	}
	r.Enabled = enabled != 0
	return r, true, nil
}

func (s *Store) ListWatchdogEvents(ruleID string, limit int) ([]WatchdogEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.r.Query(`SELECT id, rule_id, at, kind, actor_kind, actor_id, old_value, new_value
		FROM watchdog_event WHERE rule_id=? ORDER BY id DESC LIMIT ?`, ruleID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list watchdog events: %w", err)
	}
	defer rows.Close()
	var out []WatchdogEvent
	for rows.Next() {
		var e WatchdogEvent
		var old, new sql.NullInt64
		if err := rows.Scan(&e.ID, &e.RuleID, &e.At, &e.Kind, &e.ActorKind, &e.ActorID, &old, &new); err != nil {
			return nil, fmt.Errorf("store: list watchdog events: %w", err)
		}
		if old.Valid {
			v := old.Int64
			e.OldValue = &v
		}
		if new.Valid {
			v := new.Int64
			e.NewValue = &v
		}
		out = append(out, e)
	}
	if out == nil {
		out = []WatchdogEvent{}
	}
	return out, rows.Err()
}

// WatchdogFireStats is the fire-side read the list-with-details needs: how many
// times a rule has fired (the loop_control_event rows of kind 'watchdog' that
// name it) and the newest such fire. A fire is the loop move (or the notify),
// recorded on the loop's own control audit — not on the rule's event log, so a
// fire is never recorded twice.
func (s *Store) WatchdogFireStats(ruleID string) (count int, lastAt string, err error) {
	var maxAt sql.NullString
	err = s.r.QueryRow(`SELECT COUNT(*), MAX(at) FROM loop_control_event WHERE rule_id=? AND kind='watchdog'`, ruleID).
		Scan(&count, &maxAt)
	if err != nil {
		return 0, "", fmt.Errorf("store: watchdog fire stats: %w", err)
	}
	if maxAt.Valid {
		lastAt = maxAt.String
	}
	return count, lastAt, nil
}

// WatchdogFire is one fire row read from loop_control_event — the kind-
// 'watchdog' control event that names the rule. Detail is the closed-key JSON
// (ruleId/signal/action/episode); the daemon renders the wire view from it.
type WatchdogFire struct {
	ID     int64
	LoopID string
	At     string
	Detail string
}

// ListWatchdogFires is the fire-side read the show door needs: the rule's
// kind-'watchdog' control events, newest first, capped. A fire is the loop
// move (or the notify) on the loop's own control audit — this is the rule's
// slice of it (who it moved, and when), not the rule's audit (that is
// ListWatchdogEvents).
func (s *Store) ListWatchdogFires(ruleID string, limit int) ([]WatchdogFire, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.r.Query(`SELECT id, loop_id, at, detail FROM loop_control_event
		WHERE rule_id=? AND kind='watchdog' ORDER BY id DESC LIMIT ?`, ruleID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list watchdog fires: %w", err)
	}
	defer rows.Close()
	var out []WatchdogFire
	for rows.Next() {
		var f WatchdogFire
		if err := rows.Scan(&f.ID, &f.LoopID, &f.At, &f.Detail); err != nil {
			return nil, fmt.Errorf("store: list watchdog fires: %w", err)
		}
		out = append(out, f)
	}
	if out == nil {
		out = []WatchdogFire{}
	}
	return out, rows.Err()
}

// --- the observation reads ----------------------------------------------------
//
// These are the store's reads the daemon service turns into a watchdog
// Observation (T9). Each is a pure read; the service assembles the Observation
// and calls the pure Evaluate. They read the tables the signals are defined
// over, and they return honest empties (a zero time, a zero count) where a
// source is absent — the service treats an absent source as "the signal does
// not fire", and the availability probe (below) is what says a signal is
// genuinely off at all.

// WatchdogLastActivity is the newest of a loop's activity sources: a step row's
// start/end, a pin update, or the loop's own start. It is the no-activity
// clock. A loop with none of the sources (should not happen for a running
// loop) reads zero, and the signal then does not fire.
func (s *Store) WatchdogLastActivity(loopID string) (time.Time, error) {
	var latest sql.NullString
	err := s.r.QueryRow(`SELECT MAX(updated_at) FROM (
		SELECT updated_at FROM loop WHERE id=?
		UNION ALL
		SELECT updated_at FROM loop_blueprint WHERE loop_id=?
		UNION ALL
		SELECT started_at FROM step WHERE loop_slug=(SELECT slug FROM loop WHERE id=?)
		UNION ALL
		SELECT ended_at FROM step WHERE loop_slug=(SELECT slug FROM loop WHERE id=?) AND ended_at IS NOT NULL
	)`, loopID, loopID, loopID, loopID).Scan(&latest)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: last activity of %s: %w", loopID, err)
	}
	if !latest.Valid || latest.String == "" {
		return time.Time{}, nil
	}
	t, err := parseStoreTime(latest.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: last activity of %s: %w", loopID, err)
	}
	return t, nil
}

// WatchdogPositionDispatches is the same-position count: how many step rows the
// loop has at its current position (phase=lane AND taskid=item). The step row
// has no step-name column and no updated_at, so the position is (phase,
// taskid) and the count is over those two — a re-dispatch of the same position
// is a new row at the same (phase, taskid), and the count is the retries the
// reject counters did not cap.
func (s *Store) WatchdogPositionDispatches(sc Scope, phase, taskID string) (int, error) {
	where, args := sc.Where("") // the step table's scope columns are loop_slug/component_root
	q := `SELECT COUNT(*) FROM step WHERE ` + where + ` AND phase=?`
	args = append(args, phase)
	if taskID != "" {
		q += ` AND taskid=?`
		args = append(args, taskID)
	}
	var n int
	if err := s.r.QueryRow(q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: position dispatches: %w", err)
	}
	return n, nil
}

// WatchdogReasonRun is the repeat-block-reason input: walking the loop's
// control events newest-first, it reads the TAIL RUN of park events that all
// carry the same reason kind (a park is a cap_hit, a stop, or a watchdog fire)
// and returns that reason's digest, how many times it has run consecutively,
// and when the run began. A loop that has never been blocked reads repeats=0
// (the signal then does not fire). The reason is the kind (a closed enum),
// never the person's text — the text is in the event's own column.
func (s *Store) WatchdogReasonRun(loopID string) (hash string, repeats int, runSince time.Time, err error) {
	rows, err := s.r.Query(`SELECT kind, at, detail FROM loop_control_event
		WHERE loop_id=? AND kind IN ('cap_hit','stop','watchdog') ORDER BY id DESC LIMIT 64`, loopID)
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("store: reason run of %s: %w", loopID, err)
	}
	defer rows.Close()
	type park struct {
		kind   string
		reason string
		at     string
	}
	var parks []park
	for rows.Next() {
		var p park
		var detail string
		if err := rows.Scan(&p.kind, &p.at, &detail); err != nil {
			return "", 0, time.Time{}, fmt.Errorf("store: reason run of %s: %w", loopID, err)
		}
		p.reason = reasonKindFor(p.kind, detail)
		parks = append(parks, p)
	}
	if err := rows.Err(); err != nil {
		return "", 0, time.Time{}, err
	}
	if len(parks) == 0 {
		return "", 0, time.Time{}, nil
	}
	// The tail run: the consecutive events, newest-first, that share the
	// newest reason kind.
	latest := parks[0].reason
	count := 1
	for _, p := range parks[1:] {
		if p.reason != latest {
			break
		}
		count++
	}
	// runSince is the OLDEST event in that tail run (the run's beginning).
	oldest := parks[0].at
	for i := 1; i < count && i < len(parks); i++ {
		oldest = parks[i].at
	}
	runSince, err = parseStoreTime(oldest)
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("store: reason run of %s: %w", loopID, err)
	}
	return reasonHash(latest), count, runSince, nil
}

// reasonKindFor maps a park event's kind to the reason kind the repeat signal
// counts: a cap-hit, a stop, or a watchdog (stuck) fire. The detail's
// reasonKind key, when present, refines it (a cap-hit's dimension); otherwise
// the kind alone is the reason.
func reasonKindFor(kind, detail string) string {
	switch kind {
	case "cap_hit":
		return "cap-hit"
	case "stop":
		return "stopped"
	case "watchdog":
		return "stuck"
	default:
		return kind
	}
}

// WatchdogRunningStep is the step-over-deadline input: the loop's running step
// (the earliest-deadline one, so a loop with several running steps is judged
// by the one closest to breaching) and its manifest DeadlineAt. A loop with no
// running step reads a zero time, and the signal then does not fire.
func (s *Store) WatchdogRunningStep(sc Scope) (runningSince, deadline time.Time, err error) {
	rows, err := s.r.Query(`SELECT started_at, context_manifest FROM step
		WHERE loop_slug=? AND component_root=? AND status='running'
		ORDER BY id LIMIT 8`, sc.LoopSlug, sc.ComponentRoot)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("store: running step: %w", err)
	}
	defer rows.Close()
	var bestDeadline time.Time
	for rows.Next() {
		var started sql.NullString
		var manifest sql.NullString // the manifest is nullable: a step can run with none
		if err := rows.Scan(&started, &manifest); err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("store: running step: %w", err)
		}
		runningSince, err = parseStoreTime(started.String)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("store: running step: %w", err)
		}
		dl, ok := manifestDeadline(manifest.String)
		if !ok {
			continue // this step declared no deadline: it cannot be over one
		}
		if bestDeadline.IsZero() || dl.Before(bestDeadline) {
			bestDeadline = dl
		}
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, time.Time{}, err
	}
	if runningSince.IsZero() {
		return time.Time{}, time.Time{}, nil // no running step
	}
	return runningSince, bestDeadline, nil
}

// manifestDeadline reads a step's context_manifest JSON for its deadlineAt
// (RFC 3339, absolute) and returns it, and whether it was present. A manifest
// with no deadline, or an unparseable one, is absent (the signal is off for
// that step), never an error that fails the whole tick.
func manifestDeadline(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	var cm struct {
		DeadlineAt string `json:"deadlineAt"`
	}
	if err := json.Unmarshal([]byte(raw), &cm); err != nil || cm.DeadlineAt == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, cm.DeadlineAt)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// SignalAvailability is the per-signal answer the vocabulary serves: whether
// the signal's source columns exist in THIS store's schema, and why not when
// they do not. A signal whose source is absent is "available:false" — the
// honest third state (P31) — and a rule on it is refused at the door rather
// than silently inert.
type SignalAvailability struct {
	Available bool
	Reason    string
}

// WatchdogSignalAvailability probes whether each signal's source columns are
// present and readable in this store. On a migrated store every source is
// available; the probe exists so a partial schema — a store a migration did
// not reach — reports a signal as UNAVAILABLE rather than silently inert.
// Each probe is one LIMIT 0 SELECT over the exact columns the signal reads,
// so a missing table or column is a SQLite error that names the column.
func (s *Store) WatchdogSignalAvailability() (map[string]SignalAvailability, error) {
	probes := map[string]string{
		"no-activity":         `SELECT updated_at FROM loop LIMIT 0`,
		"same-position":       `SELECT phase, taskid FROM step LIMIT 0`,
		"repeat-block-reason": `SELECT kind, detail FROM loop_control_event LIMIT 0`,
		"step-over-deadline":  `SELECT status, started_at, context_manifest FROM step LIMIT 0`,
	}
	out := make(map[string]SignalAvailability, len(probes))
	for signal, q := range probes {
		// LIMIT 0 returns no rows: sql.ErrNoRows is the SUCCESS answer
		// (the columns exist), anything else is the missing table or column.
		err := s.r.QueryRow(q).Scan(new(int))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			out[signal] = SignalAvailability{Available: false, Reason: err.Error()}
			continue
		}
		out[signal] = SignalAvailability{Available: true}
	}
	return out, nil
}

// --- the watchdog's loop moves -------------------------------------------------

// BlockLoopStuck is the unattended move: a compare-and-set of a RUNNING loop to
// blocked, with the pin's lastReason set to 'stuck' and a kind-'watchdog'
// control event appended (the closed-key detail carrying the rule id, signal,
// action and episode). It is a CAS on status='running'; a loop that moved
// elsewhere meanwhile is a no-op (Changed false) that writes NO event (LC10:
// one row per CHANGING act). The watchdog service is its only caller besides
// the control core (T9's call-graph guard), and it never stamps a user actor
// (the actor is the daemon's {system, watchdog}).
func (s *Store) BlockLoopStuck(loopID, actorKind, actorID, reason string, ruleID string, detail map[string]any) (Parked, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return Parked{}, fmt.Errorf("store: block stuck %s: %w", loopID, err)
	}
	defer tx.Rollback()
	now := s.Now()
	// The compare-and-set: running -> blocked. Zero rows means the loop was
	// not running (a lost race, or it never was) — read its real status for
	// the no-op answer, write nothing.
	res, err := tx.Exec(`UPDATE loop SET status='blocked', updated_at=? WHERE id=? AND status='running'`, now, loopID)
	if err != nil {
		return Parked{}, fmt.Errorf("store: block %s: %w", loopID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Parked{}, fmt.Errorf("store: block %s: %w", loopID, err)
	}
	if n == 0 {
		var status string
		if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				tx.Rollback()
				return Parked{}, fmt.Errorf("store: block %s: %w", loopID, ErrNotFound)
			}
			return Parked{}, fmt.Errorf("store: read the status of %s: %w", loopID, err)
		}
		tx.Rollback()
		return Parked{Changed: false, Status: status}, nil
	}
	// The block changed the status: the pin's lastReason becomes 'stuck' so
	// the row and the pin agree (the same JSON merge a park uses).
	if err := s.setRuntimeLastReason(tx, loopID, "stuck", now); err != nil {
		return Parked{}, fmt.Errorf("store: block %s: %w", loopID, err)
	}
	// One event row, only because the act changed state (LC10). The detail is
	// the closed-key rule/signal/action/episode; the reason is the event's own
	// person column (the daemon's fixed sentence, never user text).
	if err := appendControlEvent(tx, ControlEvent{
		LoopID: loopID, At: now, Kind: "watchdog",
		ActorKind: actorKind, ActorID: actorID, Reason: reason, RuleID: ruleID,
		Detail: marshalControlDetail(detail),
	}); err != nil {
		return Parked{}, fmt.Errorf("store: block %s: %w", loopID, err)
	}
	if err := tx.Commit(); err != nil {
		return Parked{}, fmt.Errorf("store: block %s: %w", loopID, err)
	}
	return Parked{Changed: true, Status: "blocked"}, nil
}

// AppendWatchdogFire is the notify arm's audit row: a kind-'watchdog' control
// event that names the rule and the episode but moves NOTHING (the loop keeps
// running — a notify is a screen event, not a state change). It is the fire's
// one row, and it is appended only for the notify action; the stop/block arms
// write their event through their own CAS (BlockLoopStuck / ParkLoopStopped).
func (s *Store) AppendWatchdogFire(loopID, actorKind, actorID, reason, ruleID string, detail map[string]any) error {
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: watchdog fire %s: %w", loopID, err)
	}
	defer tx.Rollback()
	now := s.Now()
	if err := appendControlEvent(tx, ControlEvent{
		LoopID: loopID, At: now, Kind: "watchdog",
		ActorKind: actorKind, ActorID: actorID, Reason: reason, RuleID: ruleID,
		Detail: marshalControlDetail(detail),
	}); err != nil {
		return fmt.Errorf("store: watchdog fire %s: %w", loopID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: watchdog fire %s: %w", loopID, err)
	}
	return nil
}

// --- the in-transaction helpers ------------------------------------------------

// FindWatchdogRuleTx reads a rule by its unique key on the shared transaction
// — the seed applier's fingerprint and create-or-update discriminator run on
// the engine's transaction (the plan and the write see one snapshot), and the
// set door's Tx form does. present is false when the key has no row.
func (s *Store) FindWatchdogRuleTx(tx *sql.Tx, scope, subjectID, signal, action string, threshold int64) (WatchdogRule, bool, error) {
	var r WatchdogRule
	var enabled int
	err := tx.QueryRow(`SELECT id, scope, subject_id, signal, threshold, action, enabled, origin, created_at, updated_at
		FROM watchdog_rule WHERE scope=? AND subject_id=? AND signal=? AND action=? AND threshold=?`,
		scope, subjectID, signal, action, threshold).
		Scan(&r.ID, &r.Scope, &r.SubjectID, &r.Signal, &r.Threshold, &r.Action, &enabled, &r.Origin, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WatchdogRule{}, false, nil
		}
		return WatchdogRule{}, false, fmt.Errorf("store: find watchdog rule: %w", err)
	}
	r.Enabled = enabled != 0
	return r, true, nil
}

func (s *Store) getWatchdogRuleTx(tx *sql.Tx, id string) (WatchdogRule, error) {
	var r WatchdogRule
	var enabled int
	err := tx.QueryRow(`SELECT id, scope, subject_id, signal, threshold, action, enabled, origin, created_at, updated_at
		FROM watchdog_rule WHERE id=?`, id).
		Scan(&r.ID, &r.Scope, &r.SubjectID, &r.Signal, &r.Threshold, &r.Action, &enabled, &r.Origin, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WatchdogRule{}, fmt.Errorf("store: watchdog rule %s: %w", id, ErrNotFound)
		}
		return WatchdogRule{}, fmt.Errorf("store: get watchdog rule %s: %w", id, err)
	}
	r.Enabled = enabled != 0
	return r, nil
}

// appendWatchdogEvent writes one watchdog_event row, on the shared
// transaction. The triggers make the table append-only (P3); this is the one
// place a row is written.
func appendWatchdogEvent(tx *sql.Tx, e WatchdogEvent) error {
	_, err := tx.Exec(`INSERT INTO watchdog_event (rule_id, at, kind, actor_kind, actor_id, old_value, new_value)
		VALUES (?,?,?,?,?,?,?)`,
		e.RuleID, e.At, e.Kind, e.ActorKind, e.ActorID, nullableInt64(e.OldValue), nullableInt64(e.NewValue))
	if err != nil {
		return fmt.Errorf("store: append watchdog event: %w", err)
	}
	return nil
}

// --- small helpers -----------------------------------------------------------------

// parseStoreTime reads the store's timestamp form (isoUTC's inverse,
// "2006-01-02T15:04:05.000Z") back into a time. A zero-length string is an
// honest absence, not an error (a loop with no activity reads zero).
func parseStoreTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02T15:04:05.000Z", s)
}

func int64Ptr(i int) *int64 {
	v := int64(i)
	return &v
}

func reasonHash(kind string) string {
	sum := sha256.Sum256([]byte("watchdog-reason:" + kind))
	return hex.EncodeToString(sum[:])
}
