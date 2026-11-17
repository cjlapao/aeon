package protocol

// telemetry.go — S33 T8: the four read-only methods over the telemetry
// tiers (design TL10, rule TL11). Public like policy.* and S34's reads:
// these are PRODUCT reads the app and any initialized caller invoke, not
// diagnostics, so none of them carries Debug.
//
// # Why READS ONLY
//
// There is no write method on this surface and none may be added: telemetry
// is a byproduct of work already done (S33 brief §0 — the name is the S31
// tab's, the data never leaves the store), and a door that could write it
// would let a caller fabricate the very numbers the slice exists to make
// honest. The detail rows are written at their capture points inside the
// runtime's seams (T2–T6); this file is the window, not the door.
//
// # Why the wire types are LOCAL (the correction made at T9)
//
// T8 first drafted these handlers returning the store's own structs. The
// daemon's own architecture guard (TestTheFourPackagesCannotSeeEachOther)
// refused: `protocol` imports no store — I1/T11's seam, the one claim a
// reader cannot check by reading one file. So these are the wire's own
// types, field-for-field twins of what the store writes (json tags
// included), and the daemon-side adapter — the only layer allowed to know
// both packages — maps between them. The drift that freedom opens is
// closed by the conformance fixtures: every twin's shape is pinned against
// the same bytes the store produces, so a field renamed on one side fails
// the round-trip guard rather than disagreeing quietly in production.
// (store says the same thing about its manifest: "anything crossing the
// protocol uses these bytes.")
//
// # The wire keeps three states exactly as the store said them (TL11)
//
// A nullable token count stays null — "not reported", never 0; a cost of
// state partial or unknown travels as it is, with the tier's cost_state
// beside the sum so a reader can never mistake "USD at least" for an exact
// number. currentContextTokens and cumulativeTokensTotal are TWO fields
// forever (D2): collapsing them "to simplify the wire" would delete the
// distinction between what the next call would carry and what the loop has
// ever spent — the Do-NOT this file exists to obey.
//
// # Unknown ids refuse, naming the subject (TB2's rule, applied here)
//
// An unknown componentId or projectId gets ErrNoTelemetry with the id in
// the sentence — never an empty-everything result that a caller could
// mistake for "this project has done nothing" when the truth is "there is
// no such project here". The workspace singleton has no id to mistype, so
// its absence travels as data: the row's exists flag, the same shape T1
// gave the store (DB19: no rows is an answer, not a failure).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// ErrNoTelemetryRow is what the wired seam returns when the id names
// nothing here. It is declared HERE (not translated from a store error) so
// the handler can recognise the refusal without importing the package it
// must not see; the daemon-side adapter maps store.ErrNotFound onto it.
var ErrNoTelemetryRow = errors.New("telemetry: the named subject is absent")

// TelemetrySums is the wire twin of the store's sums struct — the sixteen
// totals every tier carries. Field names and json tags are the store's,
// verbatim: one fact, one spelling, pinned by the conformance fixtures.
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

// ComponentTelemetry is one component tier row on the wire.
type ComponentTelemetry struct {
	ComponentID string `json:"componentId"`
	TelemetrySums
	UpdatedAt string `json:"updatedAt"`
	Exists    bool   `json:"exists"`
}

// ProjectTelemetry is one GROUP tier row (TL1's S38 correction: the brief's
// "project" is `component`; this is the group above it).
type ProjectTelemetry struct {
	ProjectID string `json:"projectId"`
	TelemetrySums
	UpdatedAt string `json:"updatedAt"`
	Exists    bool   `json:"exists"`
}

// WorkspaceTelemetry is THE row — one store, one row (TL1). No row yet is
// the legitimate zero-loops-ever state: Exists=false with all-zero sums,
// never an error and never a fabricated timestamp (test map #7).
type WorkspaceTelemetry struct {
	TelemetrySums
	UpdatedAt string `json:"updatedAt"`
	Exists    bool   `json:"exists"`
}

// TelemetryStep is the step's row as the wire carries it: identity and
// state, cost reassembled with its state word, and the frozen context
// manifest as RAW BYTES — store's own rule for anything crossing the
// protocol ("the view for readers in this package, the bytes for everyone
// who owns the shape"). The store's narrow ContextManifest VIEW does not
// ride: it exists to narrow fields the store itself admits narrowing, and
// the wire carries bytes instead.
type TelemetryStep struct {
	ID              int64      `json:"id"`
	LoopSlug        string     `json:"loopSlug"`
	ComponentRoot   string     `json:"componentRoot"`
	TaskID          string     `json:"taskid,omitempty"`
	Phase           string     `json:"phase"`
	Agent           string     `json:"agent"`
	Attempt         int        `json:"attempt"`
	Status          string     `json:"status"`
	StartedAt       string     `json:"startedAt"`
	AgentReturnedAt string     `json:"agentReturnedAt,omitempty"`
	EndedAt         string     `json:"endedAt,omitempty"`
	AgentMS         *int       `json:"agentMs,omitempty"`
	WallMS          *int       `json:"wallMs,omitempty"`
	Outcome         string     `json:"outcome,omitempty"`
	EndedBy         string     `json:"endedBy"`
	Source          string     `json:"source"`
	Cost            model.Cost `json:"cost"`
	// ContextManifestRaw is the column, byte for byte — nil when none was
	// frozen, which is every step until the runtime assembles one.
	ContextManifestRaw json.RawMessage `json:"contextManifestRaw,omitempty"`
}

// TurnRow is one queryable per-model-call row (D1) or tool_result append
// row (TL6). Token columns stay *int: NULL means "the provider did not
// report it", never 0 (M9).
type TurnRow struct {
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

// CompactionRow is CompactionRecord's queryable second home (D3/TL7); the
// blob inside the step's checkpoint stays byte-identical.
type CompactionRow struct {
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

// FileActivityRow is one loop-tier file touch (D5); line deltas are
// nullable — a read has neither, a write may not know.
type FileActivityRow struct {
	ID           int64  `json:"id"`
	StepID       int64  `json:"stepId"`
	Path         string `json:"path"`
	Action       string `json:"action"`
	LinesAdded   *int   `json:"linesAdded,omitempty"`
	LinesRemoved *int   `json:"linesRemoved,omitempty"`
	OccurredAt   string `json:"occurredAt"`
}

// CommandActivityRow is one loop-tier command (D6); a NULL exit code is
// "ended on a signal", not zero.
type CommandActivityRow struct {
	ID         int64  `json:"id"`
	StepID     int64  `json:"stepId"`
	Command    string `json:"command"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	WallMs     int64  `json:"wallMs"`
	OccurredAt string `json:"occurredAt"`
}

// TelemetryStore is this surface's local interface onto the telemetry
// reads — local types throughout, which is what keeps the seam holdable
// without an import (the S35 T3 ProposalStore pattern done right; T8's
// first draft claimed the store's own structs could ride, and the daemon's
// four-packages guard was the answer). LoopTelemetry is the one richer
// read: the loop tier is detail rows, not a single struct, and its
// assembly (transcript → EstimateTokens for the CURRENT number) rides the
// adapter in internal/daemon, the only layer allowed to know both runtime
// and store. A nil seam (storeless daemon) answers ErrStoreUnavailable
// below — the methods stay registered (V13's shape for store-backed
// reads, the same choice step.* made) because the storeless build is rare
// and the refusal is specific, not empty.
type TelemetryStore interface {
	ComponentTelemetry(componentID string) (ComponentTelemetry, error)
	ProjectTelemetry(projectID string) (ProjectTelemetry, error)
	WorkspaceTelemetry() (WorkspaceTelemetry, error)
	LoopTelemetry(stepID int64) (TelemetryLoopData, error)
}

// TelemetryLoopData is everything telemetry.loop answers with, assembled
// once by the daemon-side adapter. Every field is a store fact or derived
// from one; nothing here is computed twice.
type TelemetryLoopData struct {
	// Step anchors the read — id, loop, component root, state — so the
	// caller can tell WHICH loop this telemetry is.
	Step TelemetryStep `json:"step"`
	// Turns is every step_turn row (TL5's per-model-call rows AND the
	// tool_result rows, category-bearing): token nulls and per-row
	// cost/cost_state ride exactly as written.
	Turns []TurnRow `json:"turns"`
	// Compactions is the queryable second home of the loop's compaction
	// records (TL7): the blob in the checkpoint stays byte-identical.
	Compactions []CompactionRow `json:"compactions"`
	// FileActivities / CommandActivities are T6's activity rows — reads,
	// writes with line counts, commands with exit codes and wall time.
	// Absence of a row is absence of an event, never a zero.
	FileActivities    []FileActivityRow    `json:"fileActivities"`
	CommandActivities []CommandActivityRow `json:"commandActivities"`
	// CurrentContextTokens is D2's FIRST number, always present: an
	// EstimateTokens over the step's transcript rows — what the NEXT model
	// call would carry. An estimate, and named one (TL11).
	CurrentContextTokens int64 `json:"currentContextTokens"`
	// CumulativeTokensTotal is D2's SECOND number, always present: TL11's
	// definition verbatim — "the same number step_turn's rows sum to", the
	// running total across every TURN so far. The compaction row's usage
	// rides the rollup tiers (RecordStepCompaction bumps them) but not this
	// number, because it is not a step_turn row; TL11 chose the turn-sum
	// definition and this field honors it. Distinct from the current number by
	// construction — one is a projection of the wire, one is a history —
	// and they are never merged into one field.
	CumulativeTokensTotal int64 `json:"cumulativeTokensTotal"`
	// Composition is the step's recorded context manifest verbatim
	// (T1's StepContextManifest): the system-prompt char count and the
	// per-section {title, category, chars} rows. Nil means NOT RECORDED —
	// a step from before the composition was captured — and null is how
	// that reads on the wire, never an empty list pretending to be a
	// composition (P31).
	Composition json.RawMessage `json:"composition,omitempty"`
}

// TelemetryLoopParams names the ONE required argument: a step id. Not a
// loop slug — the loop tier lives per step (T1's rows are keyed by step_id),
// and refusing a non-positive id up front is the "required-argument,
// named-when-refused" rule (T5 §5), not a store round trip.
type TelemetryLoopParams struct {
	StepID int64 `json:"stepId"`
}

// TelemetryComponentParams requires the component id; the refusal names it.
type TelemetryComponentParams struct {
	ComponentID string `json:"componentId"`
}

// TelemetryProjectParams requires the GROUP id (S38's naming: the brief's
// "project" is component; this is the group above it).
type TelemetryProjectParams struct {
	ProjectID string `json:"projectId"`
}

// handleTelemetryLoop answers D2 with BOTH numbers, always both, plus every
// detail row the loop tier holds.
func (t *Table) handleTelemetryLoop(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	reads, err := t.requireTelemetryReads()
	if err != nil {
		return nil, err
	}
	var p TelemetryLoopParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.StepID <= 0 {
		return nil, ErrInvalidScope.WithWhat("telemetry.loop requires a step id greater than zero; none was given")
	}
	data, err := reads.LoopTelemetry(p.StepID)
	if err != nil {
		if errors.Is(err, ErrNoTelemetryRow) {
			return nil, ErrNoTelemetry.WithWhat("no step %d is in this store, so it has no telemetry — `aeon step list` names the steps that exist", p.StepID)
		}
		return nil, err
	}
	return data, nil
}

// handleTelemetryComponent is the sizing read TL10 names as the primary
// surface: a future agent (or the app's project tab) calling it gets the
// component tier verbatim — all sixteen totals, the floored cost_state,
// the updated_at.
func (t *Table) handleTelemetryComponent(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	reads, err := t.requireTelemetryReads()
	if err != nil {
		return nil, err
	}
	var p TelemetryComponentParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrInvalidScope.WithWhat("telemetry.component requires a component id; none was given")
	}
	row, err := reads.ComponentTelemetry(p.ComponentID)
	if err != nil {
		if errors.Is(err, ErrNoTelemetryRow) {
			return nil, ErrNoTelemetry.WithWhat("no telemetry row exists for component %q — either it has produced no counted work yet or no such component is in this store; `aeon component list` names what does", p.ComponentID)
		}
		return nil, err
	}
	return row, nil
}

// handleTelemetryProject is the same read at the group tier, TL1's S38
// correction honored: this "project" is the GROUP above components. The
// cost_state here is the one the store RECOMPUTES from members on every
// component move (T7) — the lattice visible at the wire.
func (t *Table) handleTelemetryProject(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	reads, err := t.requireTelemetryReads()
	if err != nil {
		return nil, err
	}
	var p TelemetryProjectParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ProjectID) == "" {
		return nil, ErrInvalidScope.WithWhat("telemetry.project requires a project (group) id; none was given")
	}
	row, err := reads.ProjectTelemetry(p.ProjectID)
	if err != nil {
		if errors.Is(err, ErrNoTelemetryRow) {
			return nil, ErrNoTelemetry.WithWhat("no telemetry row exists for project %q — either no component in it has produced counted work yet or no such group is in this store; `aeon project list` names what does", p.ProjectID)
		}
		return nil, err
	}
	return row, nil
}

// handleTelemetryWorkspace takes NO arguments — there is nothing to filter
// and no filter param may accrete here. The store's singleton row travels
// with its exists flag: a store that has recorded nothing answers exists
// false, which is data (DB19), not a failure and not a zero-filled lie.
func (t *Table) handleTelemetryWorkspace(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	reads, err := t.requireTelemetryReads()
	if err != nil {
		return nil, err
	}
	var p struct{}
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	row, err := reads.WorkspaceTelemetry()
	if err != nil {
		return nil, err
	}
	return row, nil
}

// requireTelemetryReads is the one seam check every handler makes before
// asking: a storeless daemon answers ErrStoreUnavailable — the absence is
// spoken, never a panic on a nil interface.
func (t *Table) requireTelemetryReads() (TelemetryStore, error) {
	if t.TelemetryReads == nil {
		return nil, ErrStoreUnavailable
	}
	return t.TelemetryReads, nil
}
