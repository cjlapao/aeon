package seed

import (
	"context"
	"fmt"
	"time"
)

// LedgerRow is the engine's view of a seed_ledger row (the seam over the
// store's SeedLedgerRow; the daemon adapts it in T6). It is the engine's
// memory of what was applied for a record and at which version.
type LedgerRow struct {
	SetID          string
	RecordKey      string
	AppliedVersion int
	AppliedHash    string
	State          string
	AppliedAt      string
	AppliedBy      string
}

// Event is the engine's view of a seed_event row (the seam over the store's
// SeedEvent; the daemon adapts it in T6). It is one append-only audit row.
type Event struct {
	SetID       string
	RecordKey   string
	Op          string
	FromVersion *int
	ToVersion   *int
	Reason      string
	Actor       string
	OccurredAt  string
}

// Actor is who made an apply or restore (SD10): the peer uid on the socket,
// the system at startup, or the cli. An unattributed seed is refused.
type Actor struct {
	Kind string // "system" | "user" | "cli"
	ID   string // the uid / "startup" / "cli"
}

// String is the audit's actor string (e.g. "system:startup").
func (a Actor) String() string {
	if a.ID == "" {
		return a.Kind
	}
	return a.Kind + ":" + a.ID
}

// Path is how an apply was reached: at startup (only inert) or explicitly
// (the human door; inert or guard). A startup path may not reach a guard
// set (SD5) — the class gate.
type Path string

const (
	// PathStartup is the daemon-start path: only inert sets, actor
	// system:startup. It may not reach a guard set.
	PathStartup Path = "startup"
	// PathExplicit is the human-door path (seed.apply over the socket or
	// CLI): inert or guard.
	PathExplicit Path = "explicit"
)

// Repo is the engine's seam over the store (implemented by the daemon in
// T6 over internal/store). It reads the ledger and the audit and gives the
// engine a transaction over which a domain row and its ledger/audit rows
// commit together (SD3). It names no store or sql type — internal/seed
// imports the standard library only.
type Repo interface {
	// Ledger reads the ledger rows for a set (every set when setID is "").
	Ledger(ctx context.Context, setID string) ([]LedgerRow, error)
	// LedgerGet reads one ledger row; false if none.
	LedgerGet(ctx context.Context, setID, key string) (LedgerRow, bool, error)
	// Events reads the audit rows for a set and key, newest first.
	Events(ctx context.Context, setID, key string, limit int) ([]Event, error)
	// WithTx opens one transaction and runs fn over it, committing on
	// success, rolling back on error. It is the shared transaction a domain
	// row and its ledger/audit rows commit on.
	WithTx(ctx context.Context, fn func(tx Tx) error) error
	// PutLedger writes one ledger row on the transaction (upsert).
	PutLedger(tx Tx, row LedgerRow) error
	// InsertEvent appends one audit row on the transaction.
	InsertEvent(tx Tx, ev Event) error
}

// Options is the engine's construction: the registry (the declared sets),
// the appliers (one per set) and the clock (injected, so the engine holds
// no time and is deterministic in tests).
type Options struct {
	Registry *Registry
	Appliers map[string]Applier
	Now      func() time.Time
}

// Engine is the join of the pure plan (T1) and the storage (T2): it reads
// the ledger and live fingerprints, plans, and applies each record
// atomically with its ledger and audit rows, enforces the inert/guard
// boundary (SD5), never clobbers an operator's choice (SD6), and reports
// status. It holds no global state, no goroutine and no timer; the clock is
// injected.
type Engine struct {
	repo     Repo
	reg      *Registry
	appliers map[string]Applier
	now      func() time.Time
}

// NewEngine builds an engine over a repo and options, refusing a registry
// set with no applier or a class/applier mismatch (rules 6, 7). A set the
// engine cannot apply is worse than none.
func NewEngine(repo Repo, opt Options) (*Engine, error) {
	if repo == nil {
		return nil, fmt.Errorf("seed: a nil repo was given to the engine")
	}
	if opt.Registry == nil {
		return nil, fmt.Errorf("seed: a nil registry was given to the engine")
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	for _, s := range opt.Registry.All() {
		a, ok := opt.Appliers[s.ID]
		if !ok {
			return nil, fmt.Errorf("seed: set %q has no applier", s.ID)
		}
		if s.Class == ClassInert && a.GuardAffecting() {
			return nil, fmt.Errorf("seed: set %q is inert but its applier is guard-affecting (SD5)", s.ID)
		}
	}
	return &Engine{
		repo:     repo,
		reg:      opt.Registry,
		appliers: opt.Appliers,
		now:      opt.Now,
	}, nil
}

// Status returns, for a set (every set when setID is ""), the available
// version, the applied version and the per-record state, read from the
// ledger and the live fingerprints (SD10). It never writes.
func (e *Engine) Status(ctx context.Context, setID string) ([]SetStatus, error) {
	sets := e.affectedSets(setID)
	var out []SetStatus
	for _, s := range sets {
		a, ok := e.appliers[s.ID]
		if !ok {
			continue
		}
		st, err := e.statusOne(ctx, s, a)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func (e *Engine) statusOne(ctx context.Context, s Set, a Applier) (SetStatus, error) {
	// One read of the set's source for the whole operation (S72): Records is
	// a source function, not a cached list, and a domain's source rebuilds
	// and canonicalises every record on each call. Reading it once here and
	// indexing by key is what keeps status O(records) instead of O(records²).
	sn := newSetSnapshot(s)
	ledger, err := e.readLedger(ctx, s.ID)
	if err != nil {
		return SetStatus{}, err
	}
	live, err := e.readLive(ctx, a, sn)
	if err != nil {
		return SetStatus{}, err
	}
	plan := planRecords(sn.records, ledger, live)
	st := SetStatus{
		ID:               s.ID,
		Class:            s.Class,
		AvailableVersion: s.Version,
		AppliedVersion:   maxAppliedVersion(ledger),
	}
	for _, d := range plan.Decisions {
		ls := ledger[d.Key]
		// A status reports what a record IS, not what a planned write would
		// make it. A skip decision already carries the current state
		// (removed / diverged / up-to-date-applied / conflict-absent), so it
		// passes through; a WRITE decision (apply / update / adopt / restore
		// / retire) carries its POST state — leaking that into the display
		// made a never-applied set report "applied" for every record (the
		// S45 T11 walk measured it: `seed.status` said policy.corpus applied
		// 367 while the ledger held no policy.corpus row and the policy
		// table held no corpus row). The ledger's own state is the truth
		// under a planned write; a missing ledger row is absent, never the
		// zero value (planRecord's normalisation, same rule here).
		state := d.State
		if d.Op != OpSkip {
			state = ls.State
			if state == "" {
				state = StateAbsent
			}
		}
		st.Records = append(st.Records, RecordStatus{
			Key:              d.Key,
			State:            state,
			AvailableVersion: sn.version(d.Key),
			AppliedVersion:   ls.Version,
			Reason:           d.Reason,
		})
	}
	return st, nil
}

// Plan returns the plan (the decisions and any refusal) a set would make,
// filtered to keys (all when nil). It never writes and is identical to what
// Apply would do on the same state (SD10).
func (e *Engine) Plan(ctx context.Context, setID string, keys []string) (SetPlan, error) {
	s, ok := e.reg.Lookup(setID)
	if !ok {
		return SetPlan{}, fmt.Errorf("seed: set %q is not declared", setID)
	}
	a, ok := e.appliers[s.ID]
	if !ok {
		return SetPlan{}, fmt.Errorf("seed: set %q has no applier", setID)
	}
	ledger, err := e.readLedger(ctx, s.ID)
	if err != nil {
		return SetPlan{}, err
	}
	sn := newSetSnapshot(s)
	live, err := e.readLive(ctx, a, sn)
	if err != nil {
		return SetPlan{}, err
	}
	res := planRecords(sn.records, ledger, live)
	plan := SetPlan{
		SetID:     s.ID,
		Decisions: filterDecisions(res.Decisions, keys),
		Refusal:   res.Refusal,
	}
	return plan, nil
}

// Apply applies a set's plan (filtered to keys) — per record, in key order,
// each in its own transaction (the domain row, its ledger row and its audit
// row commit together, SD3). A failure rolls back that record only and
// marks it failed in a second transaction, so one bad record never blocks
// the set and is visible in status (SD10). A plan Refusal (a downgrade)
// fails the whole apply before any write. via=startup may not reach a guard
// set (SD5, the class gate). A skip writes an audit event only when the
// state is news, so a no-change re-apply writes zero rows (idempotent).
func (e *Engine) Apply(ctx context.Context, setID string, keys []string, by Actor, via Path) (ApplyResult, error) {
	if by.ID == "" && by.Kind == "" {
		return ApplyResult{}, fmt.Errorf("seed: a seed may not be unattributed — an actor is required")
	}
	s, ok := e.reg.Lookup(setID)
	if !ok {
		return ApplyResult{}, fmt.Errorf("seed: set %q is not declared", setID)
	}
	if s.Class == ClassGuard && via == PathStartup {
		// The class gate (SD5, the headline invariant): a startup path
		// may not reach a guard set. There is no code path from
		// ApplyInert to a guard set; this refusal is its teeth.
		return ApplyResult{}, fmt.Errorf("seed: set %q is a guard set and cannot be applied at startup — the human door only (SD5)", setID)
	}
	a, ok := e.appliers[s.ID]
	if !ok {
		return ApplyResult{}, fmt.Errorf("seed: set %q has no applier", setID)
	}
	ledger, err := e.readLedger(ctx, s.ID)
	if err != nil {
		return ApplyResult{}, err
	}
	// One source read for the whole apply: the plan, every record written and
	// every ledger hash below come from the same bytes (snapshot.go).
	sn := newSetSnapshot(s)
	live, err := e.readLive(ctx, a, sn)
	if err != nil {
		return ApplyResult{}, err
	}
	res := planRecords(sn.records, ledger, live)
	// A Refusal fails the whole apply before any write.
	if res.Refusal.Key != "" {
		return ApplyResult{}, fmt.Errorf("seed: apply of %q refused: %s", setID, res.Refusal.Reason)
	}
	result := ApplyResult{SetID: s.ID}
	for _, d := range filterDecisions(res.Decisions, keys) {
		if err := e.applyDecision(ctx, sn, a, d, ledger, by); err != nil {
			return ApplyResult{}, err
		}
		result.Records = append(result.Records, d.result())
	}
	return result, nil
}

// applyDecision applies one record's decision, each in its own transaction.
// A write failure rolls back that record only and marks it failed in a
// second transaction (the failed state is visible in status).
func (e *Engine) applyDecision(ctx context.Context, sn setSnapshot, a Applier, d Decision, ledger map[string]LedgerState, by Actor) error {
	switch d.Op {
	case OpApply, OpUpdate, OpAdopt:
		adopt := d.Op == OpAdopt
		rec := sn.record(d.Key)
		err := e.repo.WithTx(ctx, func(tx Tx) error {
			if err := a.Upsert(tx, rec, adopt); err != nil {
				return err
			}
			if err := e.repo.PutLedger(tx, LedgerRow{
				SetID:          sn.id(),
				RecordKey:      d.Key,
				AppliedVersion: d.ToVersion,
				AppliedHash:    Hash(rec.Content),
				State:          "applied",
				AppliedAt:      e.now().UTC().Format(time.RFC3339),
				AppliedBy:      by.String(),
			}); err != nil {
				return err
			}
			return e.repo.InsertEvent(tx, Event{
				SetID:       sn.id(),
				RecordKey:   d.Key,
				Op:          string(d.Op),
				FromVersion: ptr(d.FromVersion),
				ToVersion:   ptr(d.ToVersion),
				Reason:      d.Reason,
				Actor:       by.String(),
				OccurredAt:  e.now().UTC().Format(time.RFC3339),
			})
		})
		if err != nil {
			// A failure rolls back that record only and marks it failed
			// in a second transaction, so the failure is visible. The
			// ledger records the last-known-good version and hash; a
			// never-applied record has none (version 0, no hash), and the
			// ledger's CHECKs (T2) refuse applied_version 0 and a hash that
			// is not 64 chars. So when there is no prior applied version,
			// the attempted version and its content hash stand in — they
			// are what a successful apply would have written, always
			// CHECK-valid — and the state "failed" is what names the truth.
			appliedVersion := ledger[d.Key].Version
			appliedHash := ledger[d.Key].Hash
			if appliedVersion == 0 {
				appliedVersion = d.ToVersion
				appliedHash = Hash(rec.Content)
			}
			_ = e.repo.WithTx(ctx, func(tx Tx) error {
				if err := e.repo.PutLedger(tx, LedgerRow{
					SetID:          sn.id(),
					RecordKey:      d.Key,
					AppliedVersion: appliedVersion,
					AppliedHash:    appliedHash,
					State:          "failed",
					AppliedAt:      e.now().UTC().Format(time.RFC3339),
					AppliedBy:      by.String(),
				}); err != nil {
					return err
				}
				return e.repo.InsertEvent(tx, Event{
					SetID:      sn.id(),
					RecordKey:  d.Key,
					Op:         "skip",
					Reason:     "failed — " + truncate(err.Error(), 512),
					Actor:      by.String(),
					OccurredAt: e.now().UTC().Format(time.RFC3339),
				})
			})
			return nil // the record is failed, not an engine error
		}
		return nil
	case OpRetire:
		return e.repo.WithTx(ctx, func(tx Tx) error {
			if err := a.Retire(tx, d.Key); err != nil {
				return err
			}
			ls := ledger[d.Key]
			if err := e.repo.PutLedger(tx, LedgerRow{
				SetID:          sn.id(),
				RecordKey:      d.Key,
				AppliedVersion: ls.Version,
				AppliedHash:    ls.Hash,
				State:          "retired",
				AppliedAt:      e.now().UTC().Format(time.RFC3339),
				AppliedBy:      by.String(),
			}); err != nil {
				return err
			}
			return e.repo.InsertEvent(tx, Event{
				SetID:      sn.id(),
				RecordKey:  d.Key,
				Op:         "retire",
				Reason:     d.Reason,
				Actor:      by.String(),
				OccurredAt: e.now().UTC().Format(time.RFC3339),
			})
		})
	case OpSkip:
		return e.applySkip(ctx, sn, d, ledger, by)
	default:
		return nil
	}
}

// applySkip writes an audit event only when the state is news (first time
// a diverged/removed is seen, or the state changed) — a no-change re-apply
// writes zero rows, so idempotence is observable (SD10).
func (e *Engine) applySkip(ctx context.Context, sn setSnapshot, d Decision, ledger map[string]LedgerState, by Actor) error {
	cur := ledger[d.Key]
	if cur.State == "" {
		cur.State = StateAbsent
	}
	if d.State == cur.State {
		return nil // the state is not news: no write (idempotent)
	}
	ls := ledger[d.Key]
	return e.repo.WithTx(ctx, func(tx Tx) error {
		if err := e.repo.PutLedger(tx, LedgerRow{
			SetID:          sn.id(),
			RecordKey:      d.Key,
			AppliedVersion: ls.Version,
			AppliedHash:    ls.Hash,
			State:          string(d.State),
			AppliedAt:      e.now().UTC().Format(time.RFC3339),
			AppliedBy:      by.String(),
		}); err != nil {
			return err
		}
		return e.repo.InsertEvent(tx, Event{
			SetID:      sn.id(),
			RecordKey:  d.Key,
			Op:         "skip",
			Reason:     d.Reason,
			Actor:      by.String(),
			OccurredAt: e.now().UTC().Format(time.RFC3339),
		})
	})
}

// Restore re-applies a record in a removable state (removed, diverged,
// retired) over its live row (SD6): the explicit consent restore is. It
// applies PlanRestore's decision and writes op=restore. Restoring an
// up-to-date record is a no-op with a reason.
func (e *Engine) Restore(ctx context.Context, setID, key string, by Actor) (ApplyResult, error) {
	if by.ID == "" && by.Kind == "" {
		return ApplyResult{}, fmt.Errorf("seed: a restore may not be unattributed — an actor is required")
	}
	s, ok := e.reg.Lookup(setID)
	if !ok {
		return ApplyResult{}, fmt.Errorf("seed: set %q is not declared", setID)
	}
	a, ok := e.appliers[s.ID]
	if !ok {
		return ApplyResult{}, fmt.Errorf("seed: set %q has no applier", setID)
	}
	ledger, err := e.readLedger(ctx, s.ID)
	if err != nil {
		return ApplyResult{}, err
	}
	sn := newSetSnapshot(s)
	live, err := e.readLive(ctx, a, sn)
	if err != nil {
		return ApplyResult{}, err
	}
	rec, inSet := sn.lookup(key)
	d := planRestoreRecord(key, rec, inSet, ledger[key], live[key])
	result := ApplyResult{SetID: s.ID}
	if d.Op == OpSkip {
		result.Records = append(result.Records, RecordResult{Key: key, Op: OpSkip, Reason: d.Reason})
		return result, nil
	}
	if d.Op == OpRestore {
		err := e.repo.WithTx(ctx, func(tx Tx) error {
			if err := a.Upsert(tx, rec, false); err != nil {
				return err
			}
			if err := e.repo.PutLedger(tx, LedgerRow{
				SetID:          s.ID,
				RecordKey:      key,
				AppliedVersion: d.ToVersion,
				AppliedHash:    Hash(rec.Content),
				State:          "applied",
				AppliedAt:      e.now().UTC().Format(time.RFC3339),
				AppliedBy:      by.String(),
			}); err != nil {
				return err
			}
			return e.repo.InsertEvent(tx, Event{
				SetID:       s.ID,
				RecordKey:   key,
				Op:          "restore",
				FromVersion: ptr(d.FromVersion),
				ToVersion:   ptr(d.ToVersion),
				Reason:      d.Reason,
				Actor:       by.String(),
				OccurredAt:  e.now().UTC().Format(time.RFC3339),
			})
		})
		if err != nil {
			return ApplyResult{}, err
		}
		result.Records = append(result.Records, RecordResult{Key: key, Op: OpRestore, FromVersion: d.FromVersion, ToVersion: d.ToVersion, Reason: d.Reason})
	}
	return result, nil
}

// ApplyInert is the startup path: it applies only the inert sets in the
// registry, with via=startup and actor system:startup. There is no code
// path from ApplyInert to a guard set (SD5) — a guard set would be refused
// by the class gate in Apply.
func (e *Engine) ApplyInert(ctx context.Context, by Actor) ([]ApplyResult, error) {
	by = Actor{Kind: "system", ID: "startup"}
	var out []ApplyResult
	for _, s := range e.reg.All() {
		if s.Class != ClassInert && s.Class != ClassDefault {
			continue // guard sets are never applied at startup
		}
		r, err := e.Apply(ctx, s.ID, nil, by, PathStartup)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// --- helpers ---

func (e *Engine) affectedSets(setID string) []Set {
	if setID != "" {
		s, ok := e.reg.Lookup(setID)
		if !ok {
			return nil
		}
		return []Set{s}
	}
	return e.reg.All()
}

func (e *Engine) readLedger(ctx context.Context, setID string) (map[string]LedgerState, error) {
	rows, err := e.repo.Ledger(ctx, setID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]LedgerState)
	for _, r := range rows {
		if setID != "" && r.SetID != setID {
			continue
		}
		out[r.RecordKey] = LedgerState{Version: r.AppliedVersion, Hash: r.AppliedHash, State: stateFrom(r.State)}
	}
	return out, nil
}

// readLive fingerprints the live row of every record in the snapshot. It
// reads the snapshot, not the source: the loop runs once per record and a
// source read per record would make the whole thing quadratic (snapshot.go).
func (e *Engine) readLive(ctx context.Context, a Applier, sn setSnapshot) (map[string]LiveState, error) {
	out := map[string]LiveState{}
	err := e.repo.WithTx(ctx, func(tx Tx) error {
		for _, r := range sn.records {
			hash, present, err := a.Fingerprint(tx, r.Key)
			if err != nil {
				return err
			}
			adoptable := false
			if present {
				adoptable, err = a.Adoptable(tx, r.Key)
				if err != nil {
					return err
				}
			}
			out[r.Key] = LiveState{Hash: hash, Present: present, Adoptable: adoptable}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func filterDecisions(ds []Decision, keys []string) []Decision {
	if len(keys) == 0 {
		return ds
	}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	var out []Decision
	for _, d := range ds {
		if want[d.Key] {
			out = append(out, d)
		}
	}
	return out
}

func maxAppliedVersion(ledger map[string]LedgerState) int {
	max := 0
	for _, ls := range ledger {
		if ls.Version > max {
			max = ls.Version
		}
	}
	return max
}

func stateFrom(s string) State {
	if s == "" {
		return StateAbsent
	}
	return State(s)
}

func ptr(i int) *int { p := i; return &p }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
