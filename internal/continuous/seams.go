package continuous

import (
	"context"
	"time"
)

// The seams CN24 defines: the pure core owns the RULES, the daemon owns
// every touch of the world. Each interface names its owning slice, and
// nothing below is implemented here — an implementation would be the
// second definition of something, and second definitions are how Aeon
// gets two answers to one question.

// Reader is the snapshot's whole provenance (owning slice: S27, adapter —
// it reads the store and composes the repository list). The tick hands
// `at` in: one instant names the read, the Plan, and every decision, and
// the Worker's Clock is the only producer of that instant — a reader
// that grabbed its own now could disagree with the tick that consumed it.
type Reader interface {
	// Snapshot reads the world once, as of `at`. Implementations must
	// read at that single instant and report it as Snapshot.Now — the
	// purity of Plan is only as honest as that instant.
	Snapshot(ctx context.Context, at time.Time) (Snapshot, error)
}

// Starter is the claim-then-start door (owning slice: S27 T5, over
// internal/loopengine): the adapter records the decision, claims FIRST,
// and starts under the scheduler origin (CN14/CN17), never the other way
// round. The whole ledger protocol — the claim row, the transitions, the
// stamps, the outcomes — is the adapter's, over the store's one write
// path; the core only says what a tick would do.
type Starter interface {
	// Record appends the decision row (deduplicated where the store's
	// words repeat) and answers its id — the id a claim must name.
	Record(ctx context.Context, s Snapshot, d Decision) (decisionID int64, err error)
	// Start performs the claim-then-start for one start decision, under
	// the per-repository lock, and reports the loop slug or the honest
	// reason it did not start. A refusal is FINAL (CN14): Start is never
	// called again for the same subject after one returns.
	Start(ctx context.Context, d Decision, s Snapshot, decisionID int64) (loopSlug string, err error)
}

// Reconciler is the crash-recovery pass (owning slice: S27 T5): open
// claims are the only trace of a tick that died mid-pick — the daemon
// runs Reconcile once after StartLoops, before Run (CN14), and never on
// a --no-recover boot (CN17).
type Reconciler interface {
	Reconcile(ctx context.Context) error
}

// Grants is S56's live check (owning slice: S56 — autonomy grants): the
// snapshot's GrantAbsent fact is its answer at snapshot time; a tick that
// means a start asks it again live (the floor is re-checked, the pin was
// taken at the start).
type Grants interface {
	// Absent answers whether the repository's agent has no grant for a
	// scheduler start — absent is a fact to state, never a default to
	// fill (V13).
	Absent(ctx context.Context, root string) (bool, error)
}

// Budgets is S55's check (owning slice: S55 — loop budgets), arriving on
// the snapshot as BudgetOK/BudgetReason so Plan stays pure.
type Budgets interface {
	// Exhausted answers for the repository: reason is carried into the
	// budget-exhausted sentence verbatim — it is S55's own words.
	Exhausted(ctx context.Context, root string) (ok bool, reason string, err error)
}

// Stopper is S55's loop.stop (owning slice: S55 — loop control): the
// scheduler stops NOTHING preemptively (CN9's "never preempt" law); the
// seam exists for the recovery path only, which is T5's, not Plan's.
type Stopper interface {
	Stop(ctx context.Context, loopSlug, reason string) error
}

// SettingsReader is S44's declared-settings read (owning slice: S44 —
// settings): the adapter resolves them once per snapshot into
// Snapshot.Settings with their sources; the core applies numbers and
// never reads a knob itself. (The spec's seam list names this one
// `Settings`; that word is the snapshot's struct, so the interface keeps
// the job-word — an interface named for what it reads.)
type SettingsReader interface {
	// Load resolves max_slots, max_starts_per_root_per_hour, aging_hours
	// (defaults 4, 6, 24) and the tick cadence at one instant.
	Load(ctx context.Context) (Settings, error)
}

// Logger is the worker's log surface — the daemon's Logger satisfies it,
// and the package never imports a logging library for one method.
type Logger interface {
	Infof(format string, args ...any)
}
