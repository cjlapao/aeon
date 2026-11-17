package store

// automation.go — S40 AU3/AU4/AU5: the outbox side of the store. The one
// writer of automation_event is insertAutomationEvent, which never opens a
// transaction and never publishes: a domain write and its event commit
// together or not at all (AU4). Depth and cause are DERIVED here from the
// origin loop, never passed in (AU5) — a caller can name the loop whose agent
// caused the write, but cannot name a depth, because a depth a caller passes
// is a depth a caller can lie about. Loop tasks never emit; the database says
// so too (the manual-only trigger of 0039), and this Go guard is its twin,
// not its substitute.

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/match"
)

// The closed set (AU3): goal.approved, task.created, task.approved,
// task.completed. Nothing else is an automation event — rejected, parked and
// every edit are silent, and loop tasks are absent by rule, not by omission.
// TestTheClosedEventSetMatchesTheMigrationCheck holds this list and 0039's
// CHECK to the same four names. The words live in internal/match (S49 T2 —
// the match grammar's home) and are named here unchanged for every S40
// caller; the lock test holds match's set, these names and the CHECK to the
// same four.
const (
	EventGoalApproved  = match.EventGoalApproved
	EventTaskCreated   = match.EventTaskCreated
	EventTaskApproved  = match.EventTaskApproved
	EventTaskCompleted = match.EventTaskCompleted
)

// AutomationEvents is the closed set (AU3), in migration order.
func AutomationEvents() []string { return match.Events() }

// ErrNotAutomatable: a loop task asked for an event. Loop tasks never emit
// (AU3) — the engine board speaks through steps and events, not the outbox.
var ErrNotAutomatable = errors.New("store: automation events are for goals and manual tasks only")

// AutomationEventIn is what a domain write knows. It has NO depth field on
// purpose (AU5): depth is a property of the loop that caused the write, not
// of the write, and the helper reads it from that loop's row or fails closed.
//
// OriginLoopID names the loop whose agent caused this write — "" means a
// person did it, which is depth 0 and no cause. It is a loop id, never a
// number: an id can be looked up (and an unknown id fails closed), a number
// can be invented.
type AutomationEventIn struct {
	Event, SubjectKind, SubjectID                       string
	SubjectVersion                                      int
	RootID, ComponentID, GoalType, TaskType, SourceKind string
	Manual                                              bool   // task events only; must be true
	OriginLoopID                                        string // the loop whose agent caused this write; "" = a person
}

// AutomationEvent is an outbox row as read. RootID is the root of the tree the
// event's component belongs to (CT11's match key); the old "project" tier is
// derived, never stored.
type AutomationEvent struct {
	ID                                                  int64
	Event, SubjectKind, SubjectID                       string
	SubjectVersion                                      int
	RootID, ComponentID, GoalType, TaskType, SourceKind string
	Depth                                               int
	CausedByTrigger, CreatedAt                          string
}

// txExec is satisfied by *sql.Tx — the four emit sites all hand the helper
// the transaction their domain write is already in.
type txExec interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

// insertAutomationEvent is the ONLY writer of automation_event. It never
// opens a transaction and never publishes: it executes inside the caller's,
// so a rollback of the domain write is a rollback of the event, and a failure
// here (including the database's own manual-only trigger) rolls the domain
// write back. The grep test pins the one-writer property; this comment pins
// why a second writer could not be trusted anyway — it would be a second
// place that has to remember AU3, AU4 and AU5 at once.
func insertAutomationEvent(tx txExec, now string, in AutomationEventIn) error {
	if !isAutomationEvent(in.Event) {
		return fmt.Errorf("store: %q is not an automation event", in.Event)
	}
	if in.SubjectKind == "task" && !in.Manual {
		return ErrNotAutomatable
	}
	depth, cause := 0, ""
	if in.OriginLoopID != "" {
		var d int
		var kind, id sql.NullString
		err := tx.QueryRow(`SELECT automation_depth, started_by, trigger_id FROM loop WHERE id = ?`, in.OriginLoopID).
			Scan(&d, &kind, &id)
		if err != nil { // unknown loop => fail CLOSED: never reset depth to 0
			return fmt.Errorf("store: automation origin loop %q: %w", in.OriginLoopID, err)
		}
		depth = d
		if kind.String == "event" {
			// Only EVENT-started loops cascade. S20's clock/webhook fires
			// ('trigger','webhook') are a person's standing instructions, not
			// a cascade link — depth rides, cause does not (AU5).
			cause = id.String
		}
	}
	_, err := tx.Exec(`INSERT INTO automation_event
		(event, subject_kind, subject_id, subject_version, root_id, component_id, goal_type, task_type, source_kind, depth, caused_by_trigger, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.Event, in.SubjectKind, in.SubjectID, in.SubjectVersion,
		in.RootID, in.ComponentID, in.GoalType, in.TaskType, in.SourceKind,
		depth, cause, now)
	if err != nil {
		return fmt.Errorf("store: write the %s automation event: %w", in.Event, err)
	}
	return nil
}

func isAutomationEvent(e string) bool { return match.IsEvent(e) }

// isCompletedTaskStatus is the completion gate for task.completed (AU3):
// exactly StatusComplete — the one constant that means the work is done
// (machine.go:38; there is NO "completed" status, T0-Q1 measured the
// vocabulary). Verified/merged are ladder rungs, not completion; emitting at
// both would fire the same intention twice under two names.
func isCompletedTaskStatus(st TaskStatus) bool {
	switch st {
	case StatusComplete:
		return true
	default:
		return false
	}
}

// singleGoalComponent is the component an approved goal belongs to when it
// belongs to exactly one — the match rule of a component-scoped trigger needs
// a component, and a goal that spans two belongs to neither for matching
// purposes ("" = matches component-scoped triggers never, project-scoped ones
// still yes). COUNT+MIN in one row: txExec has QueryRow, and scanning N rows
// to learn a fact two aggregates answer is extra moving parts for free.
func singleGoalComponent(tx txExec, goalID string, version int) string {
	var n int
	var comp string
	err := tx.QueryRow(`SELECT COUNT(*), COALESCE(MIN(component_id), '') FROM goal_component WHERE goal_id = ? AND goal_version = ?`,
		goalID, version).Scan(&n, &comp)
	if err != nil || n != 1 {
		return ""
	}
	return comp
}

// componentRoot derives the ROOT of the tree a component belongs to (CT1),
// "" when there is none — a task's root is never passed in by a caller who
// might be wrong about it. The old "project" tier is derived, never stored,
// so the outbox's root_id is the tree's root, not a group.
func componentRoot(tx txExec, componentID string) string {
	var root sql.NullString
	err := tx.QueryRow(`SELECT COALESCE(parent_id, id) FROM component WHERE id = ?`, componentID).Scan(&root)
	if err != nil {
		return ""
	}
	return root.String
}

// ── readers and the dispatcher's cursor ────────────────────────────────────

// ReadAutomationEvents is the dispatcher's read: outbox rows after `after`,
// ascending, bounded by limit (default 100 — a dispatcher pass, not a
// listing page).
func (s *Store) ReadAutomationEvents(after int64, limit int) ([]AutomationEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.r.Query(`SELECT id, event, subject_kind, subject_id, subject_version,
		root_id, component_id, goal_type, task_type, source_kind, depth, caused_by_trigger, created_at
		FROM automation_event WHERE id > ? ORDER BY id ASC LIMIT ?`, after, limit)
	if err != nil {
		return nil, fmt.Errorf("store: read automation events after %d: %w", after, err)
	}
	defer rows.Close()
	var out []AutomationEvent
	for rows.Next() {
		var e AutomationEvent
		if err := rows.Scan(&e.ID, &e.Event, &e.SubjectKind, &e.SubjectID, &e.SubjectVersion,
			&e.RootID, &e.ComponentID, &e.GoalType, &e.TaskType, &e.SourceKind,
			&e.Depth, &e.CausedByTrigger, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan automation event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReadAutomationEvent reads one outbox row by id — the dispatcher reads the
// event it is about to claim by the id its cursor moved to.
func (s *Store) ReadAutomationEvent(id int64) (AutomationEvent, error) {
	var e AutomationEvent
	err := s.r.QueryRow(`SELECT id, event, subject_kind, subject_id, subject_version,
		root_id, component_id, goal_type, task_type, source_kind, depth, caused_by_trigger, created_at
		FROM automation_event WHERE id = ?`, id).Scan(&e.ID, &e.Event, &e.SubjectKind, &e.SubjectID, &e.SubjectVersion,
		&e.RootID, &e.ComponentID, &e.GoalType, &e.TaskType, &e.SourceKind,
		&e.Depth, &e.CausedByTrigger, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AutomationEvent{}, fmt.Errorf("store: automation event %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return AutomationEvent{}, fmt.Errorf("store: read automation event %d: %w", id, err)
	}
	return e, nil
}

// AutomationHead is the newest outbox id (0 when empty) — the frontier a
// fresh dispatcher starts from is decided by T5's ruling, not here.
func (s *Store) AutomationHead() (int64, error) {
	var n int64
	if err := s.r.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM automation_event`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: automation head: %w", err)
	}
	return n, nil
}

// AutomationCursor is where the named reader has reached (0 when it has no
// row yet — "never read" and "read nothing new" share the answer honestly).
func (s *Store) AutomationCursor(name string) (int64, error) {
	var n int64
	if err := s.r.QueryRow(`SELECT last_id FROM automation_cursor WHERE name = ?`, name).Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("store: automation cursor %q: %w", name, err)
	}
	return n, nil
}

// AdvanceAutomationCursor moves the named cursor forward, and ONLY forward:
// the UPSERT takes MAX of stored and new, so a replayed advance (a dispatcher
// that died after starting loops but before recording) can never re-open
// events already dispatched. `at` is injected, never clocked here — the
// dispatcher owns time (T5), same as every other store function.
func (s *Store) AdvanceAutomationCursor(name string, id int64, at time.Time) error {
	if _, err := s.w.Exec(`INSERT INTO automation_cursor (name, last_id, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET last_id = MAX(last_id, excluded.last_id), updated_at = excluded.updated_at`,
		name, id, isoUTC(at)); err != nil {
		return fmt.Errorf("store: advance automation cursor %q to %d: %w", name, id, err)
	}
	return nil
}

// ── S40 T5: the dispatcher's reads (AU5/AU11) ───────────────────────────────

// MaxAutomationDepth is AU5's line: an event whose depth exceeds this is
// refused, never started. The counter lives on loop rows (T1's column);
// the dispatcher compares event depth against it BEFORE claiming anything
// deeper than the line can start.
const MaxAutomationDepth = 3

// Spend is a trigger's recent cost as AU11 says to speak it: a sum that
// never invents a number for an unknown. Unknown steps are NOT summed as
// zero — zero is a number, and a confident zero over unpriced work is the
// wrong answer with decimals on it.
type Spend struct {
	USD     float64 // sum of known + partial step costs (partial = a floor)
	Floor   bool    // at least one partial step: the USD is a floor, not a total
	Unknown bool    // at least one unknown (or NULL) step: NEVER added as zero
}

// EventTriggerSpend sums the costs of steps under loops this trigger
// started since `since`. The join is the house query (T0-Q8, measured
// `0.7|1|1|3` on real rows): step joins loop on (loop_slug, component_root)
// — S38's rename moved the column, not the pattern.
func (s *Store) EventTriggerSpend(triggerID string, since time.Time) (Spend, error) {
	var sp Spend
	var partials, unknowns int
	err := s.r.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN st.cost_state IN ('known','partial') THEN st.cost_usd END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IN ('partial') THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN st.cost_state IS NULL OR st.cost_state IN ('unknown') THEN 1 END), 0)
		FROM loop l JOIN step st ON st.loop_slug = l.slug AND st.component_root = l.component_root
		WHERE l.started_by = 'event' AND l.trigger_id = ? AND l.started_at >= ?`,
		triggerID, isoUTC(since)).Scan(&sp.USD, &partials, &unknowns)
	if err != nil {
		return Spend{}, fmt.Errorf("store: event trigger spend for %s: %w", triggerID, err)
	}
	sp.Floor = partials > 0
	sp.Unknown = unknowns > 0
	return sp, nil
}

// CountRecentEventFires counts the trigger's fires that CONSUMED a run slot
// since `since` — started loops and still-open claims (a claim whose start
// has not answered is a run in flight, and forgetting them lets a crash
// loop double-start past the hour's limit) — not counting `exceptFireID`
// (the caller's own claim, which must not reserve a slot against itself).
// Refused, skipped and limited fires spent nothing and are not counted.
func (s *Store) CountRecentEventFires(triggerID string, since time.Time, exceptFireID int64) (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM event_trigger_fire
		WHERE trigger_id = ? AND claimed_at >= ? AND id <> ?
		  AND (outcome = 'started' OR outcome IS NULL)`,
		triggerID, isoUTC(since), exceptFireID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count recent fires for %s: %w", triggerID, err)
	}
	return n, nil
}

// OverlappingEventFire is S20's StillGoing rule copied into SQL (T0-Q2's
// set: running, waiting, blocked own the one-at-a-time slot; done, aborted
// and S39's resting idle release it). The agreement between this IN-list
// and trigger.StillGoing is pinned by a test in internal/automation — the
// rule is imported by test, not re-decided here.
func (s *Store) OverlappingEventFire(triggerID, subjectKind, subjectID string) (loopSlug, status string, found bool, err error) {
	err = s.r.QueryRow(`SELECT f.loop_slug, l.status FROM event_trigger_fire f
		JOIN loop l ON l.id = f.loop_id
		WHERE f.trigger_id = ? AND f.subject_kind = ? AND f.subject_id = ?
		  AND f.outcome = 'started' AND l.status IN ('running','waiting','blocked')
		ORDER BY f.id DESC LIMIT 1`,
		triggerID, subjectKind, subjectID).Scan(&loopSlug, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("store: overlapping fire for %s/%s %s: %w", triggerID, subjectKind, subjectID, err)
	}
	return loopSlug, status, true, nil
}
