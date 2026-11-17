// Package mission is `mission.status`'s one evaluation (S58 T6, SV11): the
// overview a person reads per root component — what is running, waiting,
// blocked and idle; what needs you; what it costs against its cap; what
// comes next; when anything last moved.
//
// # What it is not
//
// It is not a judgment. There is no composite health score, no traffic
// light, no "at risk" (the reflection test pins that the result has no such
// field): an overview that invents a number nobody can trace has replaced
// four facts with one lie waiting to happen. Every field is a read of a row
// that already holds the truth — the loop table for the counts, the
// attention core for needs-you (the SAME evaluation inbox.summary answers
// with, injected, so "two truths" is structurally impossible and a test
// asserts the equality), the CT7 telemetry rollup for spend, and two sealed
// seams for the rest.
//
// # The seams
//
// A cap (`BudgetReader`) and a next-ready (`NextReady`) arrive from S55/S27
// through the daemon's registrars. Until they do, the honest answer is
// `{state:"not-wired"}` — never a zero, never an empty, because a zero for
// an unwired group reads as "checked, nothing found" (the Do-NOT list).
//
// Imports: the standard library, `internal/store` (the read half) and
// `internal/attention` (the scope type). Not `internal/protocol` — the
// protocol file shapes these values for the wire; the evaluation does not
// know the wire exists.
package mission

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Reader is the store surface Evaluate needs — the three reads of
// mission_read.go plus the CT7 component rollup. The daemon passes its
// *store.Store; a test passes a hand-built table.
type Reader interface {
	LoopStatusCountsForScope(roots []string) (map[string]int, error)
	LastActivityForScope(roots []string) (string, error)
	ComponentTelemetry(id string) (store.ComponentTelemetry, error)
}

// NeedsYou is the attention summary carried as mission's own shape (the
// package does not import protocol; the daemon maps both sides of the
// equivalence the consistency test pins).
type NeedsYou struct {
	Total       int            `json:"total"`
	BySeverity  map[string]int `json:"bySeverity"`
	Acked       int            `json:"acked"`
	OldestSince string         `json:"oldestSince,omitempty"`
	Sources     []SourceState  `json:"sources"`
}

// SourceState mirrors the inbox source census (SV1) — the same three states,
// because "nothing needs you" must be auditable here exactly as it is there.
type SourceState struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// CapInForce is what a wired BudgetReader names: the cap in force over a
// root's subtree, in words the caller will print (S55 owns the arithmetic
// and the sentence; mission only relays).
type CapInForce struct {
	Summary  string  `json:"summary"`
	LimitUSD float64 `json:"limitUsd"`
	Period   string  `json:"period"`
}

// CapReader is the S55 seam: cap, present?, error. The bool is "a cap is in
// force" — false with a nil error means the subtree has no cap, which the
// result prints as `{state:"none"}`, distinct from the seam's own absence
// (`not-wired`) exactly because the two read differently.
type CapReader func(ctx context.Context, rootID string) (CapInForce, bool, error)

// NextReadyInfo names what would run next under a root (the S27 queue's
// answer, relayed in the queue's words).
type NextReadyInfo struct {
	LoopSlug string `json:"loopSlug"`
	Phase    string `json:"phase"`
	Reason   string `json:"reason"`
}

// NextReader is the S27 seam: next ready, present?, error.
type NextReader func(ctx context.Context, rootID string) (NextReadyInfo, bool, error)

// NeedsYouFunc is the injected attention evaluation. The daemon injects a
// closure over the SAME InboxService that answers `inbox.summary` — not a
// re-implementation, so `TestMissionNeedsYouEqualsInboxSummary` holds by
// construction and fails only if someone forks the truth.
type NeedsYouFunc func(ctx context.Context, entry Entry) (NeedsYou, error)

// Deps is the assembled evaluator: the store reads, the one attention
// injection, and the two seams (nil ⇒ not-wired, the honest default).
type Deps struct {
	Store     Reader
	NeedsYou  NeedsYouFunc
	Budget    CapReader
	NextReady NextReader
}

// Loops counts loop rows by their stored status over a root's tree. Four
// named live counters, the loop_control vocabulary exactly; terminal rows
// (done, aborted — the statuses the engine ends loops on) are deliberately
// NOT counted, so no `terminal` total is invented from half-truths.
type Loops struct {
	Running int `json:"running"`
	Waiting int `json:"waiting"`
	Blocked int `json:"blocked"`
	Idle    int `json:"idle"`
}

// Spend is the root's CT7 rollup — the root's row IS the subtree total
// (CT7's invariant; recomputed on moves), which is what makes the per-root
// presentation honest without a second aggregation. `USDAtLeast` carries the
// P32 floor: when CostState is `partial` or `unknown` the number is a floor
// and the surface must say so; nothing here rounds it into an exact claim.
type Spend struct {
	State      string  `json:"state"` // "reporting" while the rollup is readable
	USDAtLeast float64 `json:"usdAtLeast"`
	CostState  string  `json:"costState"` // known|partial|unknown
}

// Cap answers one of: not-wired (no seam), none (seam says no cap), in-force
// (the seam's own words).
type Cap struct {
	State    string  `json:"state"`
	Summary  string  `json:"summary,omitempty"`
	LimitUSD float64 `json:"limitUsd,omitempty"`
	Period   string  `json:"period,omitempty"`
}

// NextReady answers not-wired | ready (with the queue's words) | none.
type NextReady struct {
	State    string `json:"state"`
	LoopSlug string `json:"loopSlug,omitempty"`
	Phase    string `json:"phase,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// ComponentStatus is one entry of the overview: the root named, its five
// facts, and the instant of last loop movement under it.
type ComponentStatus struct {
	RootID         string    `json:"rootId"`
	Name           string    `json:"name"`
	Loops          Loops     `json:"loops"`
	NeedsYou       NeedsYou  `json:"needsYou"`
	Spend          Spend     `json:"spend"`
	Cap            Cap       `json:"cap"`
	NextReady      NextReady `json:"nextReady"`
	LastActivityAt string    `json:"lastActivityAt,omitempty"`
}

// Entry names one root to evaluate: the component's id (for the telemetry
// rollup and the seams' scope argument) and ALL its paths (for the store
// scope filter — a component may carry secondary paths, and every loop
// under any of them, or under `path/%`, belongs to its tree).
type Entry struct {
	RootID string
	Paths  []string
	Name   string
}

// Result is the overview the method answers with: one ComponentStatus per
// evaluated root, in the order given.
type Result struct {
	Components []ComponentStatus `json:"components"`
}

// Evaluate reads the five facts for every entry. Errors are loud: an
// overview built on half-read facts is worse than no overview, so each read
// failing aborts the call rather than zeroing its group.
func Evaluate(ctx context.Context, deps Deps, entries []Entry) (Result, error) {
	if deps.Store == nil || deps.NeedsYou == nil {
		return Result{}, fmt.Errorf("mission: Evaluate needs the store reads and the attention injection")
	}
	out := Result{Components: make([]ComponentStatus, 0, len(entries))}
	for _, e := range entries {
		cs := ComponentStatus{RootID: e.RootID, Name: e.Name}
		counts, err := deps.Store.LoopStatusCountsForScope(e.Paths)
		if err != nil {
			return Result{}, fmt.Errorf("mission: loop counts for %s: %w", e.RootID, err)
		}
		cs.Loops = Loops{
			Running: counts["running"],
			Waiting: counts["waiting"],
			Blocked: counts["blocked"],
			Idle:    counts["idle"],
		}
		nu, err := deps.NeedsYou(ctx, e)
		if err != nil {
			return Result{}, fmt.Errorf("mission: needs-you for %s: %w", e.RootID, err)
		}
		cs.NeedsYou = nu
		tel, err := deps.Store.ComponentTelemetry(e.RootID)
		if err != nil {
			return Result{}, fmt.Errorf("mission: spend for %s: %w", e.RootID, err)
		}
		cs.Spend = Spend{State: "reporting", USDAtLeast: tel.CostUSDTotal, CostState: tel.CostState}
		if !tel.Exists {
			// The legitimate no-rollup-yet state (TL1: Exists=false with
			// zero sums is not an error). Cost is then UNKNOWN at a floor
			// of zero — never an exact "0.00 spent" claim.
			cs.Spend.CostState = "unknown"
		}
		last, err := deps.Store.LastActivityForScope(e.Paths)
		if err != nil {
			return Result{}, fmt.Errorf("mission: last activity for %s: %w", e.RootID, err)
		}
		cs.LastActivityAt = last
		if deps.Budget == nil {
			cs.Cap = Cap{State: "not-wired"}
		} else {
			cap_, ok, err := deps.Budget(ctx, e.RootID)
			if err != nil {
				return Result{}, fmt.Errorf("mission: cap for %s: %w", e.RootID, err)
			}
			if !ok {
				cs.Cap = Cap{State: "none"}
			} else {
				cs.Cap = Cap{State: "in-force", Summary: cap_.Summary, LimitUSD: cap_.LimitUSD, Period: cap_.Period}
			}
		}
		if deps.NextReady == nil {
			cs.NextReady = NextReady{State: "not-wired"}
		} else {
			nr, ok, err := deps.NextReady(ctx, e.RootID)
			if err != nil {
				return Result{}, fmt.Errorf("mission: next ready for %s: %w", e.RootID, err)
			}
			if !ok {
				cs.NextReady = NextReady{State: "none"}
			} else {
				cs.NextReady = NextReady{State: "ready", LoopSlug: nr.LoopSlug, Phase: nr.Phase, Reason: nr.Reason}
			}
		}
		out.Components = append(out.Components, cs)
	}
	return out, nil
}
