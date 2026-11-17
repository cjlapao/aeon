package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// retro_read.go — S63 T4: the aggregate SQL the scorecard needs, all of
// it reads (RT7: the scorecard is a pure read package; SQL stays here —
// I2). Three doors, three rules:
//
//   - RetroLatestOutcomes: the CARD'S unit (RT7) — the latest outcome per
//     (component_root, loop_slug) within the window, newest loop first,
//     optional `last N loops` cap. A loop that blocked and later finished
//     appears ONCE, as its latest.
//   - RetroEverBlockedLoops: "ever blocked" (RT7's blocked metric and
//     firstPass's disqualifier) deliberately looks at ALL rows of the
//     loop, window-free — a loop that was blocked last month and finished
//     cleanly today was not a first pass.
//   - RetroAgentRowsInWindow: the per-agent plane's rows, joined to their
//     outcome so the window and root filter ride the outcome.
//
// Every method treats an empty roots list as "every component" (the same
// span-all semantics history.Options names); a non-empty list filters
// `component_root IN (…)` — a tree roll-up is just a bigger list.

// RetroLoopRef names one loop by (root, slug) — the ever-blocked answer.
type RetroLoopRef struct {
	Root string
	Slug string
}

// RetroAgentWindowRow is a retro_agent row plus the outcome coordinates
// the scorecard needs to judge it (root, slug, kind).
type RetroAgentWindowRow struct {
	RetroAgent
	ComponentRoot string
	LoopSlug      string
	OutcomeKind   string
}

// rootsFilter appends `component_root IN (…)` when roots is non-empty.
func rootsFilter(roots []string, q *string, args *[]any) {
	if len(roots) == 0 {
		return
	}
	*q += ` AND component_root IN (` + strings.TrimSuffix(strings.Repeat("?,", len(roots)), ",") + `)`
	for _, r := range roots {
		*args = append(*args, r)
	}
}

// RetroLatestOutcomes is the RT7 unit: latest outcome per (root, slug)
// within [since, until] ("" bounds are open), `lastN` loops cap when > 0.
// The window function keeps ONE row per loop; ORDER BY id DESC makes
// `lastN` "the last N loops that reached a terminal in the window".
func (s *Store) RetroLatestOutcomes(roots []string, since, until string, lastN int) ([]RetroOutcome, error) {
	q := `SELECT ` + retroOutcomeColumns + ` FROM (SELECT ` + retroOutcomeColumns + `,
			ROW_NUMBER() OVER (PARTITION BY component_root, loop_slug ORDER BY id DESC) AS rn
		FROM retro_outcome WHERE 1=1`
	args := []any{}
	rootsFilter(roots, &q, &args)
	if since != "" {
		q += ` AND recorded_at >= ?`
		args = append(args, since)
	}
	if until != "" {
		q += ` AND recorded_at <= ?`
		args = append(args, until)
	}
	q += `) WHERE rn = 1 ORDER BY id DESC`
	if lastN > 0 {
		q += ` LIMIT ?`
		args = append(args, lastN)
	}
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: latest retro outcomes: %w", err)
	}
	defer rows.Close()
	var out []RetroOutcome
	for rows.Next() {
		o, err := scanRetroOutcome(rows)
		if err != nil {
			return nil, fmt.Errorf("store: latest retro outcomes: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// RetroEverBlockedLoops is the window-free "ever blocked" set: every
// (root, slug) with ANY blocked row, whatever its window.
func (s *Store) RetroEverBlockedLoops(roots []string) ([]RetroLoopRef, error) {
	q := `SELECT DISTINCT component_root, loop_slug FROM retro_outcome WHERE outcome_kind LIKE 'blocked%'`
	args := []any{}
	if len(roots) > 0 {
		q += ` AND component_root IN (` + strings.TrimSuffix(strings.Repeat("?,", len(roots)), ",") + `)`
		for _, r := range roots {
			args = append(args, r)
		}
	}
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: ever-blocked loops: %w", err)
	}
	defer rows.Close()
	var out []RetroLoopRef
	for rows.Next() {
		var r RetroLoopRef
		if err := rows.Scan(&r.Root, &r.Slug); err != nil {
			return nil, fmt.Errorf("store: ever-blocked loops: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RetroAgentRowsInWindow joins agent rows to their outcome's window and
// roots; agent filters to one slug/name when non-empty.
func (s *Store) RetroAgentRowsInWindow(roots []string, since, until, agent string) ([]RetroAgentWindowRow, error) {
	q := `SELECT a.id, a.outcome_id, a.agent, a.agent_slug_known,
			a.steps, a.steps_done, a.steps_rejected, a.steps_blocked,
			a.verify_pass, a.verify_fail, a.verify_unverified,
			a.cost_usd_floor, a.cost_partial_steps, a.cost_unknown_steps, a.agent_ms,
			o.component_root, o.loop_slug, o.outcome_kind
		FROM retro_agent a JOIN retro_outcome o ON o.id = a.outcome_id WHERE 1=1`
	args := []any{}
	// The roots filter must name the joined column once the alias is on.
	if len(roots) > 0 {
		q += ` AND o.component_root IN (` + strings.TrimSuffix(strings.Repeat("?,", len(roots)), ",") + `)`
		for _, r := range roots {
			args = append(args, r)
		}
	}
	if since != "" {
		q += ` AND o.recorded_at >= ?`
		args = append(args, since)
	}
	if until != "" {
		q += ` AND o.recorded_at <= ?`
		args = append(args, until)
	}
	if agent != "" {
		q += ` AND a.agent = ?`
		args = append(args, agent)
	}
	q += ` ORDER BY a.id`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: retro agent rows in window: %w", err)
	}
	defer rows.Close()
	var out []RetroAgentWindowRow
	for rows.Next() {
		var (
			w     RetroAgentWindowRow
			a     = &w.RetroAgent
			known int
			steps, sDone, sRej, sBlk,
			vPass, vFail, vUnver,
			cPart, cUnk, agentMS sql.NullInt64
			floor sql.NullFloat64
		)
		if err := rows.Scan(&a.ID, &a.OutcomeID, &a.Agent, &known,
			&steps, &sDone, &sRej, &sBlk, &vPass, &vFail, &vUnver,
			&floor, &cPart, &cUnk, &agentMS,
			&w.ComponentRoot, &w.LoopSlug, &w.OutcomeKind); err != nil {
			return nil, fmt.Errorf("store: retro agent rows in window: %w", err)
		}
		a.AgentSlugKnown = known != 0
		a.Steps = nullInt(steps)
		a.StepsDone = nullInt(sDone)
		a.StepsRejected = nullInt(sRej)
		a.StepsBlocked = nullInt(sBlk)
		a.VerifyPass = nullInt(vPass)
		a.VerifyFail = nullInt(vFail)
		a.VerifyUnverified = nullInt(vUnver)
		a.CostUSDFloor = nullFloat(floor)
		a.CostPartialSteps = nullInt(cPart)
		a.CostUnknownSteps = nullInt(cUnk)
		a.AgentMS = nullInt64(agentMS)
		out = append(out, w)
	}
	return out, rows.Err()
}

// RetroStatusCounts answers the two numbers retro.status carries that a
// COUNT can answer: recorded fact rows, and learnings the retro engine
// has written. The retro_learning link table is the learnings count —
// exactly one row per milestone learning the engine wrote (RT4), so the
// number never re-derives what a tally would have to keep.
func (s *Store) RetroStatusCounts() (outcomes, learningsWritten int, err error) {
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM retro_outcome`).Scan(&outcomes); err != nil {
		return 0, 0, fmt.Errorf("store: count retro outcomes: %w", err)
	}
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM retro_learning`).Scan(&learningsWritten); err != nil {
		return 0, 0, fmt.Errorf("store: count retro learnings: %w", err)
	}
	return outcomes, learningsWritten, nil
}

// RetroLearningLink is the read-back of the one learning link a retro
// row carries (UNIQUE (outcome_id): zero or one). found=false is the
// honest zero, not an error.
func (s *Store) RetroLearningLink(outcomeID int64) (learningID int64, found bool, err error) {
	err = s.r.QueryRow(`SELECT learning_id FROM retro_learning WHERE outcome_id = ?`, outcomeID).Scan(&learningID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("store: read retro learning link: %w", err)
	}
	return learningID, true, nil
}

// LearningWindowRow is one LIVE learning for the digest's `learned`
// section: id, scope, provenance and the stored body (already-validated
// words the digest quotes VERBATIM, RT12 — never re-rendered).
type LearningWindowRow struct {
	ID             int64
	Scope          string
	ScopeKey       string
	Body           string
	Provenance     string
	ObservedCount  *int
	SourceLoopSlug string
	CreatedAt      string
}

// LearningsInWindow reads the live (never-retracted, LM2) learnings for
// the named scope_keys — the digest's repository set — created inside
// the window. Empty roots means every project-scoped learning; empty
// until means no upper bound. Order is (provenance, id): honesty-order
// grouping is the caller's fixed loop, and id keeps it deterministic.
func (s *Store) LearningsInWindow(roots []string, since, until string) ([]LearningWindowRow, error) {
	// LM2: the live predicate is the package's ONE constant — composed
	// here, never re-spelled (the memory guard counts its spellings).
	q := `SELECT id, scope, scope_key, body, provenance, observed_count,
	        COALESCE(source_loop_slug, ''), created_at
	  FROM learning WHERE scope = 'project'` + learningLiveSQL
	var args []any
	if len(roots) > 0 {
		q += ` AND scope_key IN (` + placeholders(len(roots)) + `)`
		for _, r := range roots {
			args = append(args, r)
		}
	}
	if since != "" {
		q += ` AND created_at >= ?`
		args = append(args, since)
	}
	if until != "" {
		q += ` AND created_at <= ?`
		args = append(args, until)
	}
	q += ` ORDER BY provenance, id`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: learnings in window: %w", err)
	}
	defer rows.Close()
	var out []LearningWindowRow
	for rows.Next() {
		var (
			lr  LearningWindowRow
			obs *int
			oc  sql.NullInt64
		)
		if err := rows.Scan(&lr.ID, &lr.Scope, &lr.ScopeKey, &lr.Body, &lr.Provenance,
			&oc, &lr.SourceLoopSlug, &lr.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: learnings in window: %w", err)
		}
		if oc.Valid {
			v := int(oc.Int64)
			obs = &v
		}
		lr.ObservedCount = obs
		out = append(out, lr)
	}
	return out, rows.Err()
}
