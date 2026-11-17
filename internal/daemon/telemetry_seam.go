package daemon

import (
	"context"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// S33 T2 — the production TelemetrySink: the one concrete adapter the
// runtime's capture seam ever sees.
//
// It lives HERE because internal/daemon already imports both sides freely —
// internal/runtime knows a recorder exists, internal/store writes rows, and
// this file is the thin seam between them, the same shape
// daemon/policy_gate.go takes for the gate (I3: the runtime never learns
// what a rollup row is). It is wired at every production construction site
// of RunOpts (the answer to plan.md's Q2): internal/protocol/step.go's two
// RunOpts literals (via Table.Telemetry, which daemon.New sets) and
// internal/daemon/recover.go's recovery literal. A storeless daemon gets
// nil — the feature is OFF, I2 — exactly as the graph service absentees work.
//
// The adapter is deliberately THIN: each method is one store call plus the
// two conversions that belong to this side of the boundary —
// CompactionRecord's unroll (the store must not import runtime) and the
// section/turn category already carried as a string the store's own CHECK
// re-validates. Any logic that grew here would be a second definition of
// the rollup discipline, and internal/store owns it (TL2, pinned by T1's
// tests).
//
// # Capture failures
//
// The runtime treats a sink error as an accounting hole, never a step
// failure (T2's comment on TelemetrySink). The store's errors therefore
// travel up but stop at the runtime's call sites, where they will be spoken
// (T3) — this adapter adds no retrying, no swallowing, and no second
// write. A partial capture is an honest gap in an account; a swallowed one
// is a silent one.

// telemetrySink is the one production implementation. It is a *store.Store
// seen through the runtime's interface — nothing else.
type telemetrySink struct {
	s *store.Store
}

// newTelemetrySink answers the adapter for a possibly-nil store: nil in,
// nil out — so `Telemetry: newTelemetrySink(d.store)` at a construction
// site states the I2 fact (storeless daemon, feature off) without a second
// nil branch at the site.
func newTelemetrySink(s *store.Store) runtime.TelemetrySink {
	if s == nil {
		return nil
	}
	return &telemetrySink{s: s}
}

func (t *telemetrySink) RecordTurn(ctx context.Context, stepID int64, iteration int, usage model.Usage, cost model.Cost, wallMs int64, route string, category runtime.SectionCategory) error {
	_ = ctx // the store's writer pool serializes; no per-call context exists at that layer (Q7).
	return t.s.RecordStepTurn(stepID, iteration, usage, cost, wallMs, route, string(category))
}

func (t *telemetrySink) RecordCompaction(ctx context.Context, stepID int64, rec runtime.CompactionRecord) error {
	_ = ctx
	// The unroll that belongs to this side of the seam (I1): the store takes
	// the record's FIELDS; the summary text stays in the checkpoint blob —
	// the row carries its LENGTH (TL7), never a second copy of the prose.
	if err := t.s.RecordStepCompaction(stepID, rec.Start, rec.End, rec.EstimateBefore, rec.EstimateAfter, len(rec.Summary), rec.Usage, rec.Cost); err != nil {
		return err
	}
	// The durable event row — TL7's correction of the brief's "live-only"
	// complaint, and the third AppendEvent call site in this codebase (the
	// store's own verb and the memory door are the two prior). The kind is a
	// DURABLE-STORE kind, deliberately NOT registered in protocol's
	// notification Kinds() (that list is the live stream's contract and
	// demands notification fixtures; this row is for `history` and the
	// restart, exactly like the debug hammer's kinds). The event carries the
	// measurement, never the summary prose — same one-copy rule as the row.
	step, err := t.s.ReadStep(stepID)
	if err != nil {
		return err
	}
	_, err = t.s.AppendEvent(store.NewEvent{
		LoopSlug:      step.LoopSlug,
		ComponentRoot: step.ComponentRoot,
		Kind:          "step.compacted",
		Payload: map[string]any{
			"stepId":         stepID,
			"start":          rec.Start,
			"end":            rec.End,
			"estimateBefore": rec.EstimateBefore,
			"estimateAfter":  rec.EstimateAfter,
			"summaryChars":   len(rec.Summary),
		},
	})
	return err
}

func (t *telemetrySink) RecordFileActivity(ctx context.Context, stepID int64, path, action string, linesAdded, linesRemoved *int) error {
	_ = ctx
	return t.s.RecordFileActivity(stepID, path, action, linesAdded, linesRemoved)
}

func (t *telemetrySink) RecordCommandActivity(ctx context.Context, stepID int64, command string, exitCode *int, wallMs int64) error {
	_ = ctx
	return t.s.RecordCommandActivity(stepID, command, exitCode, wallMs)
}
