// merge_event.go — the missing `merged` fact (VG14, S57 T5).
//
// The forge merges, and nothing in the store learns of it — until the
// engine writes this row AFTER a successful forge.merge dispatch. The
// row is the fact the merge gate's check-then-act re-entry asks
// ("already merged?" — a refusal today, with no record of the merge the
// refusal names) and the audit trail of what merged, when, how, and by
// whose authority (the rule's revision, the evidence, the verify run).
//
// Idempotent on the unique key (component_root, pr, merge_sha): the
// re-entry that merges the same PR head a second time — or the engine's
// retry after a crash between the dispatch and this write — finds the
// row and returns it, and writes at most one row. pr is NOT NULL
// (deviation, T5 Notes: a merge event without a PR number is not a PR
// merge, and NULLs would be distinct from each other in the unique key
// and break the idempotency), and merged_at and the evidence are never
// empty — a merge that says nothing about itself is not a merge.
//
// The write has NO actor door: its only caller is the engine (VG14 — the
// daemon writes it; T8 keeps every surface out of it), and the record
// carries the actor of the decision that allowed the merge (the rule's
// evaluation), which is the authority the audit is about. The scope
// columns are keyed without FKs: the loop rows move out on archive
// (store/archive.go) and the fact outlives them, like the verify
// evidence does.

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MergeEventRecord is what the engine writes: the fact, complete.
// Scope must be valid; PR, MergeSHA and Evidence must be set; an empty
// ID and a zero MergedAt are minted here ("me-…" and now).
type MergeEventRecord struct {
	ID           string
	Scope        Scope
	ComponentID  string // "" → NULL (a merge the engine cannot attribute to a node)
	PR           int
	MergeSHA     string
	Method       string    // "squash" | "merge" | "rebase" (the forge's, as it merged)
	BaseRef      string    // the branch the PR was merged into
	HeadSHA      string    // the head the PR was at when it merged
	RuleRevision int       // 0 → NULL (the merge the rule did not govern)
	Evidence     string    // the gate's verdict, verbatim — NOT NULL
	VerifyRunID  string    // "" → NULL
	MergedAt     time.Time // zero → now
	Actor        Actor     // zero kind → NULL (the decision's author, when named)
}

// MergeEvent is the row as stored.
type MergeEvent struct {
	ID            string
	LoopSlug      string
	ComponentRoot string
	ComponentID   *string
	PR            int
	MergeSHA      string
	Method        *string
	BaseRef       *string
	HeadSHA       *string
	RuleRevision  *int
	Evidence      string
	VerifyRunID   *string
	MergedAt      string
	ActorKind     *string
	ActorID       *string
}

// MergeEventFilter scopes the read: the loop (both halves, or none — a
// half-named loop is a caller bug, and the store says so), the node, the
// floor of merged_at (ISO strings compare in time order), and a limit.
type MergeEventFilter struct {
	Scope       Scope
	ComponentID *string
	Since       string // merged_at >= ?
	Limit       int    // 0 = no limit
}

// mergeEventColumns is the row's scan shape.
const mergeEventColumns = `id, loop_slug, component_root, component_id, pr, merge_sha, method,
	base_ref, head_sha, rule_revision, evidence, verify_run_id, merged_at, actor_kind, actor_id`

func scanMergeEvent(row interface{ Scan(...any) error }) (MergeEvent, error) {
	var (
		e                       MergeEvent
		ci, m, b, h, vr, ak, ai sql.NullString
		rr                      sql.NullInt64
	)
	err := row.Scan(&e.ID, &e.LoopSlug, &e.ComponentRoot, &ci, &e.PR, &e.MergeSHA, &m,
		&b, &h, &rr, &e.Evidence, &vr, &e.MergedAt, &ak, &ai)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MergeEvent{}, fmt.Errorf("store: merge event: %w", ErrNotFound)
		}
		return MergeEvent{}, fmt.Errorf("store: read merge event: %w", err)
	}
	e.ComponentID = nullStringPtr(ci)
	e.Method = nullStringPtr(m)
	e.BaseRef = nullStringPtr(b)
	e.HeadSHA = nullStringPtr(h)
	if rr.Valid {
		n := int(rr.Int64)
		e.RuleRevision = &n
	}
	e.VerifyRunID = nullStringPtr(vr)
	e.ActorKind = nullStringPtr(ak)
	e.ActorID = nullStringPtr(ai)
	return e, nil
}

// RecordMergeEvent writes the fact, idempotently: a record whose unique
// key (component_root, pr, merge_sha) stands already is NOT an error —
// it is the already-merged answer, and the row that stands is returned
// (the caller's re-entry succeeds, and the audit holds exactly one row
// for the merge).
func (s *Store) RecordMergeEvent(rec MergeEventRecord) (MergeEvent, error) {
	if !rec.Scope.Valid() {
		return MergeEvent{}, fmt.Errorf("store: record merge event: needs both a slug and a component root, got %s", rec.Scope)
	}
	if rec.PR == 0 {
		return MergeEvent{}, fmt.Errorf("store: record merge event: names no PR — a merge event without a PR number is not a PR merge")
	}
	if rec.MergeSHA == "" {
		return MergeEvent{}, fmt.Errorf("store: record merge event: names no merge sha")
	}
	if rec.Evidence == "" {
		return MergeEvent{}, fmt.Errorf("store: record merge event: carries no evidence — the verdict that allowed it is the record's reason to exist")
	}
	id := rec.ID
	if id == "" {
		id = "me-" + newID()
	}
	mergedAt := s.Now()
	if !rec.MergedAt.IsZero() {
		mergedAt = isoUTC(rec.MergedAt)
	}
	_, err := s.w.Exec(`INSERT INTO merge_event (id, loop_slug, component_root, component_id, pr, merge_sha,
		method, base_ref, head_sha, rule_revision, evidence, verify_run_id, merged_at, actor_kind, actor_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, rec.Scope.LoopSlug, rec.Scope.ComponentRoot, nullable(rec.ComponentID), rec.PR, rec.MergeSHA,
		nullableNil(rec.Method), nullableNil(rec.BaseRef), nullableNil(rec.HeadSHA),
		nilIfZeroOrNull(rec.RuleRevision), rec.Evidence, nullableNil(rec.VerifyRunID), mergedAt,
		nullable(rec.Actor.Kind), nullable(rec.Actor.ID))
	if err == nil {
		return s.getMergeEventByKey(rec.Scope.ComponentRoot, rec.PR, rec.MergeSHA)
	}
	if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return MergeEvent{}, fmt.Errorf("store: record merge event: %w", err)
	}
	// The key stands: the merge was recorded (by this re-entry's twin, or
	// a crash between the dispatch and a first write that recovered).
	return s.getMergeEventByKey(rec.Scope.ComponentRoot, rec.PR, rec.MergeSHA)
}

// getMergeEventByKey is the idempotency's read: the row the unique key
// names, or an honest error when the conflict was a race no row won.
func (s *Store) getMergeEventByKey(componentRoot string, pr int, mergeSHA string) (MergeEvent, error) {
	e, err := scanMergeEvent(s.r.QueryRow(`SELECT `+mergeEventColumns+
		` FROM merge_event WHERE component_root = ? AND pr = ? AND merge_sha = ?`, componentRoot, pr, mergeSHA))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return MergeEvent{}, fmt.Errorf("store: merge event for pr %d at %s: the unique key conflicts but no row stands", pr, mergeSHA)
		}
		return MergeEvent{}, fmt.Errorf("store: merge event for pr %d at %s: %w", pr, mergeSHA, err)
	}
	return e, nil
}

// ListMergeEvents reads the loop's merge facts, newest first — the audit
// trail the gate's re-entry and the UI's merge panel both read.
func (s *Store) ListMergeEvents(f MergeEventFilter) ([]MergeEvent, error) {
	if f.Scope.LoopSlug == "" && f.Scope.ComponentRoot != "" || f.Scope.LoopSlug != "" && f.Scope.ComponentRoot == "" {
		return nil, fmt.Errorf("store: list merge events: a loop names both halves — a slug alone is not a loop")
	}
	q := `SELECT ` + mergeEventColumns + ` FROM merge_event WHERE 1=1`
	var args []any
	if f.Scope.Valid() {
		where, wargs := f.Scope.Where("")
		q += ` AND ` + where
		args = append(args, wargs...)
	}
	if f.ComponentID != nil {
		if *f.ComponentID == "" {
			q += ` AND component_id IS NULL`
		} else {
			q += ` AND component_id = ?`
			args = append(args, *f.ComponentID)
		}
	}
	if f.Since != "" {
		q += ` AND merged_at >= ?`
		args = append(args, f.Since)
	}
	q += ` ORDER BY merged_at DESC, id DESC`
	if f.Limit > 0 {
		q += ` LIMIT ` + fmt.Sprint(f.Limit)
	}
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list merge events: %w", err)
	}
	defer rows.Close()
	var out []MergeEvent
	for rows.Next() {
		e, err := scanMergeEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list merge events: %w", err)
	}
	return out, nil
}

// nilIfZeroOrNull is the rule revision's honest NULL: 0 is the absence
// (the merge the rule did not govern), and the column says NULL.
func nilIfZeroOrNull(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
