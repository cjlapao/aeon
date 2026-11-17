package runtime

import (
	"context"

	"github.com/cjlapao/aeon/internal/model"
)

// S33 — the capture seam (design TL12).
//
// `internal/runtime` never imports `internal/store` — ReceiptWriter,
// CheckpointWriter, EventSink, SteerSource and PolicyGate are all
// package-local interfaces on LoopDeps/RunOpts, and TelemetrySink follows the
// same discipline: the runtime knows a recorder exists, not what a rollup
// row is. The concrete adapter over the store lives in internal/daemon,
// which already imports both sides freely.
//
// Nil means the feature is OFF — the exact I2 discipline Steers and Policy
// state verbatim: "every pre-existing caller runs exactly what it ran
// before", and no capture rule can surprise a caller that never asked for
// one. Every capture call site (T3/T4/T5/T6) nil-checks before calling, the
// same guard shape `d.Steers != nil` uses at RunLoop's turn boundaries.
//
// Capture failures never fail the step: what a step COST is observed, not
// transactional with what the step DOES. The call sites record the error
// where a watcher can see it and carry on — the transcript and the receipts
// are the facts that must not be lost, and losing a turn's row is a hole in
// an account, not a lost payment.

// SectionCategory names WHICH structurally-distinct piece of a step a
// captured fact came from (TL6, D4). Four values, closed — asserted by
// TestNoFifthCategoryExists here and by step_turn's CHECK in the store.
// `tool_schema` is deliberately ABSENT: TL6 measured that no provider's
// usage payload separates the tool definitions today, and a column nothing
// can ever fill is a lie that reads as a measurement.
//
// The type lives here rather than types.go's SectionSpec block because it is
// shared by BOTH hook points TL6 names: the two context-side values qualify
// a SectionSpec (T4), and the two transcript-side values tag the append
// points inside RunLoop's turn loop. One vocabulary, defined once, beside
// the interface that carries it.
type SectionCategory string

const (
	// CategorySystemPrompt and CategoryInjectedContext are the two a
	// SectionSpec may carry — the frozen opening context's own classification.
	CategorySystemPrompt    SectionCategory = "system_prompt"
	CategoryInjectedContext SectionCategory = "injected_context"
	// CategoryAssistantOutput and CategoryToolResult are structural positions
	// in the transcript — not a classification decision, a fact the code
	// already knows at the moment it appends (TL6: "no new classification
	// logic, just recording what the code already knows").
	CategoryAssistantOutput SectionCategory = "assistant_output"
	CategoryToolResult      SectionCategory = "tool_result"
)

// SectionCategories is the closed set, exported for the test that pins the
// set's size and for the store's CHECK to be provably the same vocabulary.
func SectionCategories() []string {
	return []string{
		string(CategorySystemPrompt), string(CategoryInjectedContext),
		string(CategoryAssistantOutput), string(CategoryToolResult),
	}
}

// TelemetrySink is the capture seam. Every method must be safe to call from
// the turn loop, and every implementation is free to be cheap or to no-op;
// the runtime's only contract with it is nil-means-off (I2) and
// failure-does-not-fail-the-step above.
//
// The signatures mirror the store's RecordX shapes (T1) across the package
// boundary — internal/daemon's adapter is a thin pass-through with the two
// conversions that belong to it: the CompactionRecord unroll (the store may
// not import this package) and the durable event for a compaction (the store
// may not know what an event kind is).
//
// Deviation from TL12's literal listing, recorded in T2's Notes: RecordTurn
// carries `route`. TL5's DDL makes route NOT NULL ("which route served this
// turn — routes can change mid-loop, S18"), and the only place that fact
// exists is beside the call — a row that cannot name its route after the
// fact would be an aggregate pretending to a detail it never captured.
type TelemetrySink interface {
	RecordTurn(ctx context.Context, stepID int64, iteration int, usage model.Usage, cost model.Cost, wallMs int64, route string, category SectionCategory) error
	RecordCompaction(ctx context.Context, stepID int64, rec CompactionRecord) error
	RecordFileActivity(ctx context.Context, stepID int64, path, action string, linesAdded, linesRemoved *int) error
	RecordCommandActivity(ctx context.Context, stepID int64, command string, exitCode *int, wallMs int64) error
}
