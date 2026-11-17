package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// S33 — what a loop actually cost, captured as it happens.
//
// The name is S31's nav tab's; the data never leaves this store (brief §0 —
// if this file's existence made S23's network-trace test fail, this file is
// wrong, not the test). Four loop-tier detail tables (turn, compaction, file,
// command) and three rollup tiers that OUTLIVE them (component, project,
// workspace), bumped by every detail write in that write's own transaction
// (TL2) — never a batch job, never at archive time, never on demand (D7).
//
// # The one column the design's DDL did not carry
//
// design.md TL5's step_turn DDL has no `category` column while TL12's
// RecordTurn signature carries one, and TL6 tags transcript turns at their
// structurally-distinct append points. The row that method writes needs a
// home for what it carries, so the migration adds `category` with the
// four-value closed CHECK; `tool_schema` is absent on purpose (TL6: not
// measurable today — a column nothing can ever fill is a lie waiting to be
// read). Recorded in the slice's task Notes.
//
// # No wall_ms_state anywhere
//
// plan.md's Q1, measured: NO writer in the codebase sets step.wall_ms — the
// column is NULL on every step, settled or not. Nothing here reads it. Every
// rollup wall total is summed from step_turn.wall_ms measured at the capture
// site (NOT NULL by construction), so the design's "add a three-state
// companion IF a NULL-producing path exists" resolves to: no companion. The
// answer is in the record, not a comment drift.
//
// # Unregistered roots
//
// The rollup chain is step → component_root → component → project. A step
// whose root maps to no registered component still gets its detail rows —
// the loop tier is authoritative for what happened — and the rollup bump is
// skipped: a sum cannot be attributed to a component that does not exist.
// Silently? No: the bump count is returned to the caller-free insert path
// only as a completeness fact of the transaction, and TestRecordXWithAnUnregisteredRootKeepsTheDetail
// pins the detail survives half of that sentence.

// TelemetryCategories is the closed set (TL6): four, no fifth. A turn row's
// category is which structurally-distinct append produced it; the two
// context-side values exist in the same closed vocabulary because the CHECK
// is the DB's word that nothing else may ever be spelled into this column.
const (
	TelemetryCategorySystemPrompt    = "system_prompt"
	TelemetryCategoryInjectedContext = "injected_context"
	TelemetryCategoryAssistantOutput = "assistant_output"
	TelemetryCategoryToolResult      = "tool_result"
)

// TelemetrySums is the rollup shape every tier carries identically (TL9):
// counts and totals ONLY — no per-path, no per-command detail, or the rollup
// would grow with the same shape as the detail it exists to outlive (TL8).
type TelemetrySums struct {
	LoopsRun              int     `json:"loopsRun"`
	WallMsTotal           int64   `json:"wallMsTotal"`
	InputTokensTotal      int64   `json:"inputTokensTotal"`
	OutputTokensTotal     int64   `json:"outputTokensTotal"`
	CacheReadTokensTotal  int64   `json:"cacheReadTokensTotal"`
	CacheWriteTokensTotal int64   `json:"cacheWriteTokensTotal"`
	ReasoningTokensTotal  int64   `json:"reasoningTokensTotal"`
	CostUSDTotal          float64 `json:"costUsdTotal"`
	// CostState is 'known' only while EVERY contributing model call was
	// 'known'; one partial/unknown contributor floors the tier (D9/P32).
	CostState           string `json:"costState"`
	FilesTouchedTotal   int    `json:"filesTouchedTotal"`
	FilesReadTotal      int    `json:"filesReadTotal"`
	FilesWrittenTotal   int    `json:"filesWrittenTotal"`
	LinesAddedTotal     int    `json:"linesAddedTotal"`
	LinesRemovedTotal   int    `json:"linesRemovedTotal"`
	CommandsRunTotal    int    `json:"commandsRunTotal"`
	CommandsFailedTotal int    `json:"commandsFailedTotal"`
	CommandWallMsTotal  int64  `json:"commandWallMsTotal"`
}

// ComponentTelemetry is one component's row. Exists separates "zero because
// nothing ran" from "no row yet" — the rollup tables create their row at the
// first contributing write, never before. ChildCount is the node's non-archived
// children (CT7: a node's row is its own loops plus its children's), so a
// caller reading one row can tell a project (children > 0) from a leaf.
type ComponentTelemetry struct {
	ComponentID string `json:"componentId"`
	TelemetrySums
	ChildCount int    `json:"childCount"`
	UpdatedAt  string `json:"updatedAt"`
	Exists     bool   `json:"exists"`
}

// WorkspaceTelemetry is THE row — one store, one row (TL1). No row yet is the
// legitimate zero-loops-ever state: Exists=false with all-zero sums, never an
// error and never a fabricated timestamp (test map #7).
type WorkspaceTelemetry struct {
	TelemetrySums
	UpdatedAt string `json:"updatedAt"`
	Exists    bool   `json:"exists"`
}

// StepTurn is one queryable per-model-call row (D1), and one tool_result
// append row (TL6). Token columns stay *int: NULL means "the provider did
// not report it", never 0 (M9).
type StepTurn struct {
	ID               int64    `json:"id"`
	StepID           int64    `json:"stepId"`
	Iteration        int      `json:"iteration"`
	StartedAt        string   `json:"startedAt"`
	EndedAt          string   `json:"endedAt"`
	WallMs           int64    `json:"wallMs"`
	InputTokens      *int     `json:"inputTokens,omitempty"`
	OutputTokens     *int     `json:"outputTokens,omitempty"`
	CacheReadTokens  *int     `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int     `json:"cacheWriteTokens,omitempty"`
	ReasoningTokens  *int     `json:"reasoningTokens,omitempty"`
	CostUSD          *float64 `json:"costUsd,omitempty"`
	CostState        string   `json:"costState"`
	Route            string   `json:"route"`
	Category         string   `json:"category"`
}

// StepCompaction is CompactionRecord's queryable second home (D3/TL7). The
// blob inside the step's checkpoint stays byte-identical; this is additive.
type StepCompaction struct {
	ID             int64    `json:"id"`
	StepID         int64    `json:"stepId"`
	StartIdx       int      `json:"startIdx"`
	EndIdx         int      `json:"endIdx"`
	EstimateBefore int      `json:"estimateBefore"`
	EstimateAfter  int      `json:"estimateAfter"`
	SummaryLen     int      `json:"summaryLen"`
	InputTokens    *int     `json:"inputTokens,omitempty"`
	OutputTokens   *int     `json:"outputTokens,omitempty"`
	CostUSD        *float64 `json:"costUsd,omitempty"`
	CostState      string   `json:"costState"`
	OccurredAt     string   `json:"occurredAt"`
}

// StepFileActivity is one loop-tier file touch (D5). Line deltas are nullable:
// a read has neither; a write may not know (binary file).
type StepFileActivity struct {
	ID           int64  `json:"id"`
	StepID       int64  `json:"stepId"`
	Path         string `json:"path"`
	Action       string `json:"action"`
	LinesAdded   *int   `json:"linesAdded,omitempty"`
	LinesRemoved *int   `json:"linesRemoved,omitempty"`
	OccurredAt   string `json:"occurredAt"`
}

// StepCommandActivity is one loop-tier command (D6). A NULL exit_code is
// "ended on a signal", not zero.
type StepCommandActivity struct {
	ID         int64  `json:"id"`
	StepID     int64  `json:"stepId"`
	Command    string `json:"command"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	WallMs     int64  `json:"wallMs"`
	OccurredAt string `json:"occurredAt"`
}

// floorCostState is D9's rule in one place: 'known' survives only while every
// contributor was 'known'; any 'partial' floors to 'partial'; any 'unknown'
// floors to 'unknown'. One helper, called identically at every tier —
// reimplementing it twice is how two tiers start disagreeing.
func floorCostState(current, contributed string) string {
	rank := func(s string) int {
		switch s {
		case "partial":
			return 1
		case "unknown":
			return 2
		default: // 'known' — and the empty contributed state means
			// "this row is not a spend event", which the CALLER encodes
			// by not calling floor at all; a model row with no reported
			// cost arrives as 'unknown' from normalizeCostState.
			return 0
		}
	}
	if rank(contributed) > rank(current) {
		return contributed
	}
	return current
}

// normalizeCostState maps model.Cost.State onto the column vocabulary,
// keeping M9: an unset state is 'unknown', never 'known' by silence.
func normalizeCostState(c model.Cost) string {
	switch string(c.State) {
	case "known", "partial", "unknown":
		return string(c.State)
	default:
		return "unknown"
	}
}

// costUSD answers the row's number in the same three states: a 'known' cost
// carries USD; a 'partial' cost carries USDAtLeast — the FLOOR, named as one
// by the state beside it, never presented as a total; 'unknown' carries nil.
func costUSD(c model.Cost) *float64 {
	switch normalizeCostState(c) {
	case "known":
		v := c.USD
		return &v
	case "partial":
		v := c.USDAtLeast
		return &v
	default:
		return nil
	}
}

// telemetryDelta is one detail write's contribution to every rollup tier.
// The same delta lands on component, project and workspace (TL2) — one
// number, three homes, one transaction.
type telemetryDelta struct {
	wallMs           int64
	inputTokens      int64
	outputTokens     int64
	cacheReadTokens  int64
	cacheWriteTokens int64
	reasoningTokens  int64
	costUSD          float64
	costContributes  bool   // only model-call rows speak to cost_state
	costState        string // normalized; meaningful when costContributes
	filesTouched     int
	filesRead        int
	filesWritten     int
	linesAdded       int
	linesRemoved     int
	commandsRun      int
	commandsFailed   int
	commandWallMs    int64
	loopsRun         int
}

// resolveTelemetryScope answers TL2's chain for one step: the component its
// loop's root maps to, and the tree PARENT that component has ("" for a root).
// found=false is the unregistered-root case — the detail row still writes, the
// bump cannot be attributed to a component that does not exist. The parent is
// the CT7 rollup target: a child's loop lands on the child and on the parent
// (once each); a root with no parent lands only on itself and the workspace.
func (s *Store) resolveTelemetryScope(stepID int64) (componentID, parentID string, found bool, err error) {
	row := s.r.QueryRow(`
		SELECT c.id, COALESCE(c.parent_id,'') FROM step s
		JOIN component_root cr ON cr.component_root = s.component_root
		JOIN component c ON c.id = cr.component_id
		WHERE s.id = ?`, stepID)
	if err = row.Scan(&componentID, &parentID); err != nil {
		if err == sql.ErrNoRows {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("store: resolve telemetry scope for step %d: %w", stepID, err)
	}
	return componentID, parentID, true, nil
}

// bumpTelemetryRollups writes one delta to all three tiers in the CALLER'S
// transaction (TL2's rule: a function that writes a detail row without the
// bump in the same statement is a function with a bug). The singleton
// workspace row is keyed by the schema's own CHECK (id = 1) — a second row
// is structurally impossible, not merely unidiomatic.
//
// cost_state is read-then-floored inside this same single-writer transaction
// (the writer pool is one connection, Q7) rather than floored in SQL: the
// three-way lattice has one definition — floorCostState — and SQL CASE would
// be a second one.
// rollupAdditiveColumns is the ONE list of the eighteen carried numbers in
// every rollup tier (design TL1) — the upsert's column list, its DO UPDATE
// sets, and the move's subtraction are all GENERATED from it (T7), because
// a subtract that forgot a column would silently leak that total out of the
// source project on every component move. Order is load-bearing: the delta
// values below are read position-for-position against it.
var rollupAdditiveColumns = []string{
	"loops_run", "wall_ms_total",
	"input_tokens_total", "output_tokens_total", "cache_read_tokens_total",
	"cache_write_tokens_total", "reasoning_tokens_total",
	"cost_usd_total",
	"files_touched_total", "files_read_total", "files_written_total",
	"lines_added_total", "lines_removed_total",
	"commands_run_total", "commands_failed_total", "command_wall_ms_total",
}

// rollupDeltaValues reads a delta in rollupAdditiveColumns' exact order.
func rollupDeltaValues(d telemetryDelta) []any {
	return []any{d.loopsRun, d.wallMs,
		d.inputTokens, d.outputTokens, d.cacheReadTokens, d.cacheWriteTokens, d.reasoningTokens,
		d.costUSD,
		d.filesTouched, d.filesRead, d.filesWritten, d.linesAdded, d.linesRemoved,
		d.commandsRun, d.commandsFailed, d.commandWallMs}
}

// telemetryTierUpsert adds one delta to one tier's row, creating it when
// absent. `contributed` is the spend-event flag: only spend contributions
// floor the row's cost_state through the lattice (floorCostState, the one
// definition, applied in Go); a fresh row from non-spend writes opens
// 'known'. The move (T7) passes false — a transfer carries numbers, a move
// recomputes the STATE from members right after, so flooring here would be
// a state decision the member set is about to answer better.
func telemetryTierUpsert(tx *sql.Tx, table, pkCol, pk string, d telemetryDelta, now string, contributed bool) error {
	var current string
	q := fmt.Sprintf(`SELECT cost_state FROM %s WHERE %s = ?`, table, pkCol)
	err := tx.QueryRow(q, pk).Scan(&current)
	exists := err == nil
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("store: read %s cost_state: %w", table, err)
	}
	next := current
	if contributed {
		if !exists {
			next = d.costState // a fresh row's state is its first contributor
		} else {
			next = floorCostState(current, d.costState)
		}
	} else if !exists {
		next = "known" // a zero row created by a non-spend write is not a claim about cost
	}
	cols := append([]string{pkCol}, rollupAdditiveColumns[:8]...)
	cols = append(cols, "cost_state")
	cols = append(cols, rollupAdditiveColumns[8:]...)
	cols = append(cols, "updated_at")
	sets := make([]string, 0, len(rollupAdditiveColumns))
	for _, c := range rollupAdditiveColumns {
		sets = append(sets, fmt.Sprintf("%s = %s + excluded.%s", c, c, c))
	}
	stmt := fmt.Sprintf(`
		INSERT INTO %s (%s)
		VALUES (%s)
		ON CONFLICT(%s) DO UPDATE SET
			%s,
			cost_state = ?,
			updated_at = excluded.updated_at`,
		table, strings.Join(cols, ","), strings.TrimSuffix(strings.Repeat("?,", len(cols)), ","),
		pkCol, strings.Join(sets, ","))
	// The DO UPDATE's cost_state = ? rides AFTER the 19 VALUES placeholders.
	args := make([]any, 0, 20)
	args = append(args, pk)
	args = append(args, rollupDeltaValues(d)[:8]...)
	args = append(args, next)
	args = append(args, rollupDeltaValues(d)[8:]...)
	args = append(args, now, next)
	if _, err := tx.Exec(stmt, args...); err != nil {
		return fmt.Errorf("store: bump %s: %w", table, err)
	}
	return nil
}

// bumpTelemetryRollups is the CT7 bump: a loop lands on its OWN component and —
// when it is a child — its tree parent, each ONCE, and on the singleton
// workspace row (keyed by the schema's CHECK id = 1). A root with no parent
// lands only on itself and the workspace, so a root with its own repository
// and children is bumped once per event, never twice.
func bumpTelemetryRollups(tx *sql.Tx, s *Store, componentID, parentID string, d telemetryDelta, now string) error {
	if err := telemetryTierUpsert(tx, "component_telemetry", "component_id", componentID, d, now, d.costContributes); err != nil {
		return err
	}
	// The parent (a root) carries the child's rollup too — once. Skip when the
	// node is a root (no parent) or when parent == component (a self-parent is
	// impossible, but a guard keeps a defect a named fact, not a double count).
	if parentID != "" && parentID != componentID {
		if err := telemetryTierUpsert(tx, "component_telemetry", "component_id", parentID, d, now, d.costContributes); err != nil {
			return err
		}
	}
	if err := telemetryTierUpsert(tx, "workspace_telemetry", "id", "1", d, now, d.costContributes); err != nil {
		return err
	}
	return nil
}

// transferRollups is CT7's tree-move transfer (replaces transferProjectRollups):
// the moving node's OWN current totals leave its former parent's rollup and
// join its new parent's, inside the caller's transaction — the same tx that
// flips the node's parent_id, so the totals can never be counted by two
// parents (or none) across a crash. The node's own row does not move: it is
// keyed by component_id and rides the move unchanged (the Do-NOT: the node's
// totals ARE the transfer's source of truth). An empty from or to means the
// other side is a root (no parent) — that side is skipped. Workspace totals
// are untouched — a move rearranges attribution, not truth.
//
// cost_state is RECOMPUTED at both parents from their CURRENT member set
// AFTER the caller's parent_id flip (recomputeCostState) — not floored along
// the way: a move is the one moment where a state is recomputed rather than
// carried, because the member set it depends on just changed.
func (s *Store) transferRollups(tx *sql.Tx, child, fromParent, toParent, now string) error {
	var d telemetryDelta
	var state string
	err := tx.QueryRow(`SELECT loops_run, wall_ms_total,
			input_tokens_total, output_tokens_total, cache_read_tokens_total,
			cache_write_tokens_total, reasoning_tokens_total,
			cost_usd_total,
			files_touched_total, files_read_total, files_written_total,
			lines_added_total, lines_removed_total,
			commands_run_total, commands_failed_total, command_wall_ms_total,
			cost_state
		FROM component_telemetry WHERE component_id = ?`, child).
		Scan(&d.loopsRun, &d.wallMs,
			&d.inputTokens, &d.outputTokens, &d.cacheReadTokens,
			&d.cacheWriteTokens, &d.reasoningTokens,
			&d.costUSD,
			&d.filesTouched, &d.filesRead, &d.filesWritten,
			&d.linesAdded, &d.linesRemoved,
			&d.commandsRun, &d.commandsFailed, &d.commandWallMs,
			&state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // the node never produced telemetry: nothing to transfer, not an error
	}
	if err != nil {
		return fmt.Errorf("store: read the telemetry of component %s for its move: %w", child, err)
	}
	if len(rollupDeltaValues(d)) != len(rollupAdditiveColumns) {
		return fmt.Errorf("store: telemetry delta and column list disagree — %d values for %d columns",
			len(rollupDeltaValues(d)), len(rollupAdditiveColumns))
	}
	// Source: the node's former parent's row MUST exist — T1's bump-on-write
	// created it together with the component row. Its absence is a defect.
	if fromParent != "" {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM component_telemetry WHERE component_id = ?`, fromParent).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("store: component %s carries telemetry but parent %q has no rollup row — the bump-on-write invariant has been broken", child, fromParent)
			}
			return fmt.Errorf("store: check the source parent %s of a telemetry move: %w", fromParent, err)
		}
		sets := make([]string, 0, len(rollupAdditiveColumns))
		for _, c := range rollupAdditiveColumns {
			sets = append(sets, fmt.Sprintf("%s = %s - ?", c, c))
		}
		args := rollupDeltaValues(d)
		args = append(args, now, fromParent)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE component_telemetry SET %s, updated_at = ? WHERE component_id = ?`,
			strings.Join(sets, ",")), args...); err != nil {
			return fmt.Errorf("store: subtract the rollups of parent %s on the move of %s: %w", fromParent, child, err)
		}
	}
	// Destination: the same upsert the live writes use (one SQL truth), with
	// the state question deferred to the recompute below.
	if toParent != "" {
		if err := telemetryTierUpsert(tx, "component_telemetry", "component_id", toParent, d, now, false); err != nil {
			return fmt.Errorf("store: add the rollups to parent %s on the move of %s: %w", toParent, child, err)
		}
	}
	// Recompute the affected parents' cost_state from the CURRENT member set
	// (the caller already flipped parent_id). A node that leaves a parent may
	// return it to 'known'; one that joins may floor it to 'partial'.
	if fromParent != "" {
		if err := recomputeCostState(tx, fromParent, now); err != nil {
			return err
		}
	}
	if toParent != "" {
		if err := recomputeCostState(tx, toParent, now); err != nil {
			return err
		}
	}
	return nil
}

// recomputeCostState folds the lattice over a node's OWN cost_state and its
// CURRENT non-archived children's cost_states (the move has already
// re-attributed them — this reads the NEW member set). No telemetry row for
// the node: the UPDATE touches nothing — not an error.
//
// The node's OWN cost_state is recomputed from its OWN spend detail rows
// (ownCostState), NOT read from the row: the stored cost_state is the floored
// EFFECTIVE value (own floored with children), and a floor cannot be un-floored
// — a partial child that left would otherwise stay 'partial' forever, so an
// incremental floor could never return a source to 'known' (the very move this
// recompute exists to answer). The own-only fold is invariant under a child
// move, so re-folding it with the NEW member set gives the true new state.
func recomputeCostState(tx *sql.Tx, id, now string) error {
	own, err := ownCostState(tx, id)
	if err != nil {
		return err
	}
	state := own
	rows, err := tx.Query(`SELECT ct.cost_state FROM component_telemetry ct
		JOIN component c ON c.id = ct.component_id
		WHERE c.parent_id = ? AND c.archived_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("store: list the telemetry members of component %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cs string
		if err := rows.Scan(&cs); err != nil {
			return fmt.Errorf("store: read a member cost state of component %s: %w", id, err)
		}
		state = floorCostState(state, cs)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: scan the telemetry members of component %s: %w", id, err)
	}
	if _, err := tx.Exec(`UPDATE component_telemetry SET cost_state = ?, updated_at = ? WHERE component_id = ?`,
		state, now, id); err != nil {
		return fmt.Errorf("store: restate the cost state of component %s: %w", id, err)
	}
	return nil
}

// ownCostState folds the lattice over a node's OWN spend detail rows — its own
// model calls, the only rows that floor a cost_state (assistant-output turns
// and compactions; TL7's "a compaction IS a model call"). It is the base a
// recompute folds children onto, and it is invariant under a child move.
// 'known' while the node has no own spend claim — the fresh-row rule
// (telemetryTierUpsert: a non-spend write opens 'known'), so a node whose only
// contributions came from a now-gone child returns to 'known' once re-folded.
func ownCostState(tx *sql.Tx, id string) (string, error) {
	state := "known"
	rows, err := tx.Query(`
		SELECT st.cost_state FROM step_turn st
		JOIN step s ON s.id = st.step_id
		JOIN component_root cr ON cr.component_root = s.component_root
		JOIN component c ON c.id = cr.component_id
		WHERE c.id = ? AND st.category = 'assistant_output'
		UNION ALL
		SELECT sc.cost_state FROM step_compaction sc
		JOIN step s ON s.id = sc.step_id
		JOIN component_root cr ON cr.component_root = s.component_root
		JOIN component c ON c.id = cr.component_id
		WHERE c.id = ?`, id, id)
	if err != nil {
		return "", fmt.Errorf("store: list the own spend rows of component %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cs string
		if err := rows.Scan(&cs); err != nil {
			return "", fmt.Errorf("store: read an own spend row cost state of component %s: %w", id, err)
		}
		state = floorCostState(state, cs)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("store: scan the own spend rows of component %s: %w", id, err)
	}
	return state, nil
}

// turnDelta maps one model call's usage/cost into a rollup contribution.
// Only non-nil provider counts add anything — a nil count adds ZERO to the
// sum column while the DETAIL row keeps NULL, which is the difference
// between "reported nothing" and "reported nothing" being counted as a zero
// (M9 says the sum may not pretend; the detail row says what actually came).
func turnDelta(usage model.Usage, cost model.Cost, wallMs int64, isModelCall bool) telemetryDelta {
	d := telemetryDelta{wallMs: wallMs}
	if usage.InputTokens != nil {
		d.inputTokens = int64(*usage.InputTokens)
	}
	if usage.OutputTokens != nil {
		d.outputTokens = int64(*usage.OutputTokens)
	}
	if usage.CacheReadTokens != nil {
		d.cacheReadTokens = int64(*usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != nil {
		d.cacheWriteTokens = int64(*usage.CacheWriteTokens)
	}
	if usage.ReasoningTokens != nil {
		d.reasoningTokens = int64(*usage.ReasoningTokens)
	}
	if isModelCall {
		d.costContributes = true
		d.costState = normalizeCostState(cost)
		if u := costUSD(cost); u != nil {
			d.costUSD = *u
		}
	}
	return d
}

// RecordStepTurn writes one per-turn row and bumps all three rollups in the
// same transaction (D1/TL2). startedAt/endedAt are stamped from the store's
// own clock (the sink call site is one statement wide); startedAt is endedAt
// minus the measured wall time — a duration the RUNTIME measured, dates the
// STORE answers, from the one clock both doors read.
//
// The category is validated at the door rather than trusting the CHECK to
// answer with a raw constraint error: a capture point that spelled a
// category wrong is a defect naming itself.
func (s *Store) RecordStepTurn(stepID int64, iteration int, usage model.Usage, cost model.Cost, wallMs int64, route, category string) error {
	switch category {
	case TelemetryCategorySystemPrompt, TelemetryCategoryInjectedContext,
		TelemetryCategoryAssistantOutput, TelemetryCategoryToolResult:
	default:
		return fmt.Errorf("store: step_turn category %q is not one of the four closed values (system_prompt, injected_context, assistant_output, tool_result)", category)
	}
	if iteration < 0 {
		return fmt.Errorf("store: step %d turn iteration must not be negative, got %d", stepID, iteration)
	}
	if wallMs < 0 {
		return fmt.Errorf("store: step %d turn wall_ms must not be negative, got %d", stepID, wallMs)
	}
	ended := s.Now()
	started := s.isoBefore(wallMs)
	usd := costUSD(cost)
	state := normalizeCostState(cost)
	// tool_result/section-shaped rows are not spend events: their cost
	// columns stay NULL/'unknown' and they contribute nothing to the tier.
	isModelCall := category == TelemetryCategoryAssistantOutput

	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: record turn for step %d: %w", stepID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO step_turn (step_id, iteration, started_at, ended_at, wall_ms,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
			cost_usd, cost_state, route, category)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		stepID, iteration, started, ended, wallMs,
		usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens,
		usage.CacheWriteTokens, usage.ReasoningTokens,
		usd, state, route, category); err != nil {
		return fmt.Errorf("store: insert step_turn for step %d: %w", stepID, err)
	}

	componentID, parentID, found, err := s.resolveTelemetryScope(stepID)
	if err != nil {
		return err
	}
	if found {
		if err := bumpTelemetryRollups(tx, s, componentID, parentID,
			turnDelta(usage, cost, wallMs, isModelCall), ended); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit turn for step %d: %w", stepID, err)
	}
	return nil
}

// RecordStepCompaction writes one compaction row and bumps all three rollups
// in the same transaction (D3/TL7). The summary text is NOT stored here —
// the blob keeps it verbatim and the row carries its length; a second copy
// of the summary would be a second source of truth for the same prose.
func (s *Store) RecordStepCompaction(stepID int64, startIdx, endIdx, estimateBefore, estimateAfter, summaryLen int, usage model.Usage, cost model.Cost) error {
	if estimateBefore < 0 || estimateAfter < 0 || summaryLen < 0 || endIdx < startIdx {
		return fmt.Errorf("store: compaction for step %d is malformed (start %d end %d before %d after %d summary %d)", stepID, startIdx, endIdx, estimateBefore, estimateAfter, summaryLen)
	}
	now := s.Now()
	usd := costUSD(cost)
	state := normalizeCostState(cost)

	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: record compaction for step %d: %w", stepID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO step_compaction (step_id, start_idx, end_idx, estimate_before, estimate_after,
			summary_len, input_tokens, output_tokens, cost_usd, cost_state, occurred_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		stepID, startIdx, endIdx, estimateBefore, estimateAfter, summaryLen,
		usage.InputTokens, usage.OutputTokens, usd, state, now); err != nil {
		return fmt.Errorf("store: insert step_compaction for step %d: %w", stepID, err)
	}

	componentID, parentID, found, err := s.resolveTelemetryScope(stepID)
	if err != nil {
		return err
	}
	if found {
		// A compaction IS a model call (TL7): it contributes cost like any
		// turn. Its wall time is not in the design's row shape, so it adds
		// no wall_ms_total — recorded in the slice Notes as the one honest
		// gap TL4 left between per-turn time (measured) and compaction time
		// (unmeasured today).
		d := turnDelta(usage, cost, 0, true)
		if err := bumpTelemetryRollups(tx, s, componentID, parentID, d, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit compaction for step %d: %w", stepID, err)
	}
	return nil
}

// RecordFileActivity writes one loop-tier file touch and bumps the tiers with
// COUNTS AND TOTALS ONLY (D5/TL8) — the path survives at the loop tier, never
// past it.
func (s *Store) RecordFileActivity(stepID int64, path, action string, linesAdded, linesRemoved *int) error {
	if path == "" {
		return fmt.Errorf("store: file activity for step %d needs a path", stepID)
	}
	if action != "read" && action != "write" {
		return fmt.Errorf("store: file activity action %q is not read or write (step %d, path %q)", action, stepID, path)
	}
	now := s.Now()
	d := telemetryDelta{filesTouched: 1}
	switch action {
	case "read":
		d.filesRead = 1
	case "write":
		d.filesWritten = 1
	}
	if linesAdded != nil {
		d.linesAdded = *linesAdded
	}
	if linesRemoved != nil {
		d.linesRemoved = *linesRemoved
	}

	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: record file activity for step %d: %w", stepID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO step_file_activity (step_id, path, action, lines_added, lines_removed, occurred_at)
		VALUES (?,?,?,?,?,?)`,
		stepID, path, action, linesAdded, linesRemoved, now); err != nil {
		return fmt.Errorf("store: insert file activity for step %d: %w", stepID, err)
	}

	componentID, parentID, found, err := s.resolveTelemetryScope(stepID)
	if err != nil {
		return err
	}
	if found {
		if err := bumpTelemetryRollups(tx, s, componentID, parentID, d, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit file activity for step %d: %w", stepID, err)
	}
	return nil
}

// RecordCommandActivity writes one loop-tier command and bumps the tiers
// (D6/TL8). exitCode nil means the process ended on a signal — the failure
// counts; the number is simply not there. A successful (exit 0) command
// counts too: D6 is "how many commands ran", not "how many failed".
func (s *Store) RecordCommandActivity(stepID int64, command string, exitCode *int, wallMs int64) error {
	if command == "" {
		return fmt.Errorf("store: command activity for step %d needs the command", stepID)
	}
	if wallMs < 0 {
		return fmt.Errorf("store: command activity for step %d has a negative wall_ms", stepID)
	}
	now := s.Now()
	d := telemetryDelta{commandsRun: 1, commandWallMs: wallMs}
	if exitCode == nil || *exitCode != 0 {
		d.commandsFailed = 1
	}

	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: record command activity for step %d: %w", stepID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		INSERT INTO step_command_activity (step_id, command, exit_code, wall_ms, occurred_at)
		VALUES (?,?,?,?,?)`,
		stepID, command, exitCode, wallMs, now); err != nil {
		return fmt.Errorf("store: insert command activity for step %d: %w", stepID, err)
	}

	componentID, parentID, found, err := s.resolveTelemetryScope(stepID)
	if err != nil {
		return err
	}
	if found {
		if err := bumpTelemetryRollups(tx, s, componentID, parentID, d, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit command activity for step %d: %w", stepID, err)
	}
	return nil
}

// ComponentTelemetry reads one component's row verbatim (TL10). A component
// that has never produced telemetry is ErrNotFound — the READ answers "no
// row", the caller decides whether that is a zero or a refusal; the socket
// refuses by naming the id (T8's TB2-style rule).
func (s *Store) ComponentTelemetry(id string) (ComponentTelemetry, error) {
	var out ComponentTelemetry
	row := s.r.QueryRow(`SELECT `+telemetrySumsColumns+`, updated_at FROM component_telemetry WHERE component_id = ?`, id)
	var err error
	out.TelemetrySums, out.UpdatedAt, err = scanTelemetrySums(row)
	if err == sql.ErrNoRows {
		return ComponentTelemetry{}, fmt.Errorf("store: no telemetry row for component %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return ComponentTelemetry{}, fmt.Errorf("store: read component telemetry %q: %w", id, err)
	}
	out.ComponentID = id
	out.Exists = true
	// ChildCount: the node's non-archived children (CT7: a node's row is its
	// own loops plus its children's, so a project's row carries the children).
	if err := s.r.QueryRow(`SELECT count(*) FROM component ch WHERE ch.parent_id = ? AND ch.archived_at IS NULL`, id).Scan(&out.ChildCount); err != nil {
		return ComponentTelemetry{}, fmt.Errorf("store: read component telemetry %q child count: %w", id, err)
	}
	return out, nil
}

// WorkspaceTelemetry reads THE row. Its absence is the legitimate
// zero-loops-ever state, answered as an all-zero Exists=false — never an
// error (test map #7: three-state honesty at the top tier includes "nothing
// has ever run here" being an answer, not a fault).
func (s *Store) WorkspaceTelemetry() (WorkspaceTelemetry, error) {
	var out WorkspaceTelemetry
	row := s.r.QueryRow(`SELECT ` + telemetrySumsColumns + `, updated_at FROM workspace_telemetry WHERE id = 1`)
	var err error
	out.TelemetrySums, out.UpdatedAt, err = scanTelemetrySums(row)
	if err == sql.ErrNoRows {
		return WorkspaceTelemetry{}, nil // all-zero, Exists=false
	}
	if err != nil {
		return WorkspaceTelemetry{}, fmt.Errorf("store: read workspace telemetry: %w", err)
	}
	out.Exists = true
	return out, nil
}

const telemetrySumsColumns = `loops_run, wall_ms_total,
	input_tokens_total, output_tokens_total, cache_read_tokens_total,
	cache_write_tokens_total, reasoning_tokens_total,
	cost_usd_total, cost_state,
	files_touched_total, files_read_total, files_written_total,
	lines_added_total, lines_removed_total,
	commands_run_total, commands_failed_total, command_wall_ms_total`

type rowScanner interface{ Scan(dest ...any) error }

func scanTelemetrySums(row rowScanner) (TelemetrySums, string, error) {
	var t TelemetrySums
	var updated string
	err := row.Scan(&t.LoopsRun, &t.WallMsTotal,
		&t.InputTokensTotal, &t.OutputTokensTotal, &t.CacheReadTokensTotal,
		&t.CacheWriteTokensTotal, &t.ReasoningTokensTotal,
		&t.CostUSDTotal, &t.CostState,
		&t.FilesTouchedTotal, &t.FilesReadTotal, &t.FilesWrittenTotal,
		&t.LinesAddedTotal, &t.LinesRemovedTotal,
		&t.CommandsRunTotal, &t.CommandsFailedTotal, &t.CommandWallMsTotal,
		&updated)
	return t, updated, err
}

// StepTurns pages the loop tier's per-turn rows (T8/telemetry.loop's detail).
// ORDER BY id is the emission order the transcript keeps; `limit` bounds the
// page the way every other list door in this project bounds one.
func (s *Store) StepTurns(stepID int64, limit, offset int) ([]StepTurn, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.r.Query(`
		SELECT id, step_id, iteration, started_at, ended_at, wall_ms,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
			cost_usd, cost_state, route, category
		FROM step_turn WHERE step_id = ? ORDER BY id LIMIT ? OFFSET ?`, stepID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: read turns of step %d: %w", stepID, err)
	}
	defer rows.Close()
	var out []StepTurn
	for rows.Next() {
		var t StepTurn
		if err := rows.Scan(&t.ID, &t.StepID, &t.Iteration, &t.StartedAt, &t.EndedAt, &t.WallMs,
			&t.InputTokens, &t.OutputTokens, &t.CacheReadTokens, &t.CacheWriteTokens, &t.ReasoningTokens,
			&t.CostUSD, &t.CostState, &t.Route, &t.Category); err != nil {
			return nil, fmt.Errorf("store: scan turn of step %d: %w", stepID, err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// StepCompactions reads a step's compaction rows (D3 — queryable, joinable
// across a step's history through step_id).
func (s *Store) StepCompactions(stepID int64) ([]StepCompaction, error) {
	rows, err := s.r.Query(`
		SELECT id, step_id, start_idx, end_idx, estimate_before, estimate_after,
			summary_len, input_tokens, output_tokens, cost_usd, cost_state, occurred_at
		FROM step_compaction WHERE step_id = ? ORDER BY id`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: read compactions of step %d: %w", stepID, err)
	}
	defer rows.Close()
	var out []StepCompaction
	for rows.Next() {
		var c StepCompaction
		if err := rows.Scan(&c.ID, &c.StepID, &c.StartIdx, &c.EndIdx, &c.EstimateBefore, &c.EstimateAfter,
			&c.SummaryLen, &c.InputTokens, &c.OutputTokens, &c.CostUSD, &c.CostState, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: scan compaction of step %d: %w", stepID, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// StepFileActivities reads a step's file-touch rows (D5's loop tier — the
// only home where a path survives).
func (s *Store) StepFileActivities(stepID int64) ([]StepFileActivity, error) {
	rows, err := s.r.Query(`
		SELECT id, step_id, path, action, lines_added, lines_removed, occurred_at
		FROM step_file_activity WHERE step_id = ? ORDER BY id`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: read file activity of step %d: %w", stepID, err)
	}
	defer rows.Close()
	var out []StepFileActivity
	for rows.Next() {
		var f StepFileActivity
		if err := rows.Scan(&f.ID, &f.StepID, &f.Path, &f.Action, &f.LinesAdded, &f.LinesRemoved, &f.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: scan file activity of step %d: %w", stepID, err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// StepCommandActivities reads a step's command rows (D6's loop tier).
func (s *Store) StepCommandActivities(stepID int64) ([]StepCommandActivity, error) {
	rows, err := s.r.Query(`
		SELECT id, step_id, command, exit_code, wall_ms, occurred_at
		FROM step_command_activity WHERE step_id = ? ORDER BY id`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: read command activity of step %d: %w", stepID, err)
	}
	defer rows.Close()
	var out []StepCommandActivity
	for rows.Next() {
		var c StepCommandActivity
		if err := rows.Scan(&c.ID, &c.StepID, &c.Command, &c.ExitCode, &c.WallMs, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: scan command activity of step %d: %w", stepID, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CumulativeTokensTotal is TL11's SECOND number: the sum every step_turn row
// of this step adds up to — read from the rows, never a second in-memory
// accumulator (T3's Do-NOT names this file as its home precisely because
// LoopState.Usage already IS the running total; a third copy in a struct
// field is how three numbers start disagreeing).
func (s *Store) CumulativeTokensTotal(stepID int64) (int64, error) {
	var total sql.NullInt64
	err := s.r.QueryRow(`
		SELECT SUM(COALESCE(input_tokens,0) + COALESCE(output_tokens,0)
			+ COALESCE(cache_read_tokens,0) + COALESCE(cache_write_tokens,0)
			+ COALESCE(reasoning_tokens,0))
		FROM step_turn WHERE step_id = ?`, stepID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("store: cumulative tokens of step %d: %w", stepID, err)
	}
	if !total.Valid {
		return 0, nil // the step with no turns yet: zero is the sum of nothing
	}
	return total.Int64, nil
}

// StepContextManifest answers telemetry.loop's composition side (D4): the
// frozen manifest JSON already persisted at the step's row — sections with
// their categories and sizes ride the checkpoint the runtime already writes
// (runtime.ContextManifest carries them; this door hands the bytes over and
// internal/protocol decodes through runtime, which owns the type).
func (s *Store) StepContextManifest(stepID int64) (string, bool, error) {
	var raw sql.NullString
	err := s.r.QueryRow(`SELECT context_manifest FROM step WHERE id = ?`, stepID).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", false, fmt.Errorf("store: step %d not found for its manifest: %w", stepID, ErrNotFound)
	}
	if err != nil {
		return "", false, fmt.Errorf("store: read manifest of step %d: %w", stepID, err)
	}
	return raw.String, raw.Valid && raw.String != "", nil
}

// isoBefore stamps "what date were we at `wallMs` ago" from the STORE's own
// clock — the same clock Now() answers with, so a turn's started_at and
// ended_at can never straddle two time sources.
func (s *Store) isoBefore(wallMs int64) string {
	return isoUTC(s.clock().Add(-time.Duration(wallMs) * time.Millisecond))
}

// resolveTelemetryScopeByRoot answers the same chain as
// resolveTelemetryScope but from a root path rather than a step — the one
// caller today is CreateLoop, which is a root's row, not a step's. The parent
// is the node's tree parent ("" for a root).
func resolveTelemetryScopeByRoot(tx *sql.Tx, componentRoot string) (componentID, parentID string, found bool) {
	err := tx.QueryRow(`
		SELECT c.id, COALESCE(c.parent_id,'') FROM component_root cr
		JOIN component c ON c.id = cr.component_id
		WHERE cr.component_root = ?`, componentRoot).Scan(&componentID, &parentID)
	return componentID, parentID, err == nil
}
