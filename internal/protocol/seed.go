package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/seed"
)

// seed.go — S43-extra T6 (SD10): the four seed.* methods (status/plan/apply/
// restore). They are the human door over the one seed engine (SD10): status
// and plan are READS (zero writes), apply and restore are WRITES that take the
// actor from the CONNECTION (the daemon's ActorFor seam), never from params.
//
// Registered CONDITIONALLY by RegisterSeed, beside the settings block (V13: a
// nil Seed leaves the four ABSENT, not registered-and-refusing — the S22
// search.query and S44 settings precedents). None is Debug, and none is
// projected onto the narrow surface (the S44 CS9 / S43 guards keep that true).
//
// The seam is the daemon's ONE *seed.Engine (one engine is handed to both the
// startup inert apply and this door, P34). *seed.Engine already satisfies
// SeedService field-for-field, so the daemon adapter is a nil guard, not a
// translation (the settingsService convention). The wire types below are THIS
// package's field-for-field twins of the engine's view types (seed.SetStatus
// et al.), so the handler maps engine → twin on the way out and a second copy
// of the fields cannot drift. The handler maps the engine's untyped refusals
// to the protocol's two codes VERBATIM (the engine's own sentence, via
// WithWhat), and the one unattributed-seed refusal is held at the actor seam
// (ErrSeedNoActor), never reached by a handler.

// --- the seam ---------------------------------------------------------------

// SeedService is the seed accessor the four methods read and write through. It
// is the read/write half of the seed engine (the *seed.Engine a daemon holds
// satisfies it directly, so the daemon adapter is a nil guard, not a
// translation). It returns the engine's own view types; the handler maps them
// to THIS package's wire twins below. A nil seam (a storeless daemon, V13)
// leaves the four methods absent: RegisterSeed is a no-op, so a seedless build
// never answers a seed door at all, rather than answering a refusal.
type SeedService interface {
	// Status answers status: every set (every set when setID is ""), the
	// available/applied versions and the per-record state, read from the
	// ledger and the live fingerprints. It never writes (SD10).
	Status(ctx context.Context, setID string) ([]seed.SetStatus, error)
	// Plan answers plan: the decisions apply would make and any refusal (a
	// downgrade), filtered to keys (all when nil). It never writes (SD10).
	Plan(ctx context.Context, setID string, keys []string) (seed.SetPlan, error)
	// Apply answers apply: the explicit human door (via=explicit). It may
	// apply a guard set (none exist in this slice; S45 declares the first).
	// The actor comes from the connection, not params.
	Apply(ctx context.Context, setID string, keys []string, by seed.Actor, via seed.Path) (seed.ApplyResult, error)
	// Restore answers restore: re-applies a removable record (removed,
	// diverged, retired) over its live row. The actor comes from the
	// connection, not params.
	Restore(ctx context.Context, setID, key string, by seed.Actor) (seed.ApplyResult, error)
}

// --- the wire types ---------------------------------------------------------
//
// These are the field-for-field twins of the engine's view types, the only
// layer that may know both. They carry only what a client reads; the engine's
// Op/State/Class are spoken as their string values on the wire.

// SeedSetStatus is the per-set status, the wire twin of seed.SetStatus.
type SeedSetStatus struct {
	ID               string             `json:"id"`
	Class            string             `json:"class"` // "inert" | "guard"
	AvailableVersion int                `json:"availableVersion"`
	AppliedVersion   int                `json:"appliedVersion"`
	Records          []SeedRecordStatus `json:"records"`
}

// SeedRecordStatus is one record's status, the wire twin of
// seed.RecordStatus.
type SeedRecordStatus struct {
	Key              string `json:"key"`
	State            string `json:"state"` // absent | applied | removed | diverged | retired | failed
	AvailableVersion int    `json:"availableVersion"`
	AppliedVersion   int    `json:"appliedVersion"`
	Reason           string `json:"reason"`
}

// SeedDecision is one apply decision, the wire twin of seed.Decision.
type SeedDecision struct {
	Key         string `json:"key"`
	Op          string `json:"op"` // apply | update | skip | restore | retire | adopt
	State       string `json:"state"`
	FromVersion int    `json:"fromVersion"`
	ToVersion   int    `json:"toVersion"`
	Reason      string `json:"reason"`
}

// SeedRefusal is a plan-level refusal (a downgrade), the wire twin of
// seed.Refusal. It fails the whole apply before any write (SD6, SD10).
type SeedRefusal struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// SeedSetPlan is a set's plan, the wire twin of seed.SetPlan.
type SeedSetPlan struct {
	SetID     string         `json:"setId"`
	Decisions []SeedDecision `json:"decisions"`
	Refusal   SeedRefusal    `json:"refusal"`
}

// SeedRecordResult is one record's apply result, the wire twin of
// seed.RecordResult.
type SeedRecordResult struct {
	Key         string `json:"key"`
	Op          string `json:"op"`
	FromVersion int    `json:"fromVersion"`
	ToVersion   int    `json:"toVersion"`
	Reason      string `json:"reason"`
}

// SeedApplyOutcome is the per-record apply/restore outcome, the wire twin of
// seed.ApplyResult. One definition, shared by the apply and restore results —
// a second copy of the fields would be a second truth.
type SeedApplyOutcome struct {
	SetID   string              `json:"setId"`
	Records []SeedRecordResult `json:"records"`
}

// --- params and results -----------------------------------------------------

// SeedStatusParams is status's params: the set to scope the read to. Absent
// (nil) means every set. A read; it carries no actor (SD10).
type SeedStatusParams struct {
	Set *string `json:"set,omitempty"`
}

// SeedStatusResult is status's result: every set's status.
type SeedStatusResult struct {
	Sets []SeedSetStatus `json:"sets"`
}

// SeedPlanParams is plan's params: the set and the keys to scope the plan to.
// Absent keys means every record. A read; it carries no actor (SD10).
type SeedPlanParams struct {
	Set  string   `json:"set"`
	Keys []string `json:"keys,omitempty"`
}

// SeedPlanResult is plan's result: the plan apply would make.
type SeedPlanResult struct {
	Plan SeedSetPlan `json:"plan"`
}

// SeedApplyParams is apply's params: the set and the keys to scope the apply
// to. It carries NO actor — the actor comes from the connection (the daemon's
// ActorFor seam), never from params (a params actor would let a caller forge
// the audit identity of a write).
type SeedApplyParams struct {
	Set  string   `json:"set"`
	Keys []string `json:"keys,omitempty"`
}

// SeedApplyResult is apply's result: the per-record apply outcome and whether
// the human-door confirmation is required. The daemon does not pretend a
// socket can prove a human is present (SD11), so RequiresConfirmation is
// always false here — the confirmation is a UI/CLI concern (S45 PM7's stance).
type SeedApplyResult struct {
	Apply              SeedApplyOutcome `json:"apply"`
	RequiresConfirmation bool           `json:"requiresConfirmation"`
}

// SeedRestoreParams is restore's params: the set and the one record key. It
// carries NO actor — the actor comes from the connection (the daemon's
// ActorFor seam), never from params.
type SeedRestoreParams struct {
	Set string `json:"set"`
	Key string `json:"key"`
}

// SeedRestoreResult is restore's result: the per-record restore outcome.
type SeedRestoreResult struct {
	Apply SeedApplyOutcome `json:"apply"`
}

// --- registration -----------------------------------------------------------

// RegisterSeed registers the four seed.* methods over the wired seam. It rides
// with the wiring rather than NewTable because the seam is assigned after
// construction (the daemon's own order, the S33 telemetry / S44 settings
// precedent), and a registration that ran inside NewTable would only ever see
// nil. The nil seam is the V13 configuration speaking — every seedless build
// arrives here with nil and the four methods stay absent (not
// registered-and-refusing); that is the designed outcome, not a silent
// failure. Returns whether the methods are in the table, so the daemon's
// wiring can be asserted rather than trusted.
func (t *Table) RegisterSeed() bool {
	if t.Seed == nil {
		return false
	}
	t.register(&Method{Name: MethodSeedStatus, RequiresInitialize: true, Handle: t.handleSeedStatus})
	t.register(&Method{Name: MethodSeedPlan, RequiresInitialize: true, Handle: t.handleSeedPlan})
	t.register(&Method{Name: MethodSeedApply, RequiresInitialize: true, Handle: t.handleSeedApply})
	t.register(&Method{Name: MethodSeedRestore, RequiresInitialize: true, Handle: t.handleSeedRestore})
	return true
}

// --- the actor --------------------------------------------------------------

// seedActor is the write boundary's audit identity (SD10): the actor comes
// from the CONNECTION (the daemon's ActorFor seam), never from params — a
// params actor would let a caller forge the audit identity of a write to
// their own rules. It is the honest absence (a storeless daemon has no
// persons) that refuses a write, and an empty id that is refused as well.
func (t *Table) seedActor(ctx context.Context, sess *Session) (seed.Actor, error) {
	if t.ActorFor == nil {
		return seed.Actor{}, ErrSeedNoActor
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return seed.Actor{}, ErrSeedNoActor
	}
	return seed.Actor{Kind: a.Kind, ID: a.ID}, nil
}

// --- the handlers -----------------------------------------------------------

func (t *Table) handleSeedStatus(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	// Unreachable in any built configuration — registration is conditional on
	// this same field. The guard is the tripwire that says registration and
	// the seam disagreed, spoken rather than silent (the search.query shape).
	if t.Seed == nil {
		return nil, ErrStoreUnavailable.WithWhat("seed.* needs the seed engine, and this build did not link it — the seed surface is absent for the same reason (V13)")
	}
	var p SeedStatusParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	states, err := t.Seed.Status(ctx, setOf(p.Set))
	if err != nil {
		return nil, t.seedError(err)
	}
	return SeedStatusView(states), nil
}

func (t *Table) handleSeedPlan(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Seed == nil {
		return nil, ErrStoreUnavailable.WithWhat("seed.* needs the seed engine, and this build did not link it — the seed surface is absent for the same reason (V13)")
	}
	var p SeedPlanParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Set) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("set is required — name the seed set to plan")}
	}
	plan, err := t.Seed.Plan(ctx, p.Set, p.Keys)
	if err != nil {
		return nil, t.seedError(err)
	}
	return SeedPlanResult{Plan: seedSetPlanView(plan)}, nil
}

func (t *Table) handleSeedApply(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Seed == nil {
		return nil, ErrStoreUnavailable.WithWhat("seed.* needs the seed engine, and this build did not link it — the seed surface is absent for the same reason (V13)")
	}
	var p SeedApplyParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Set) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("set is required — name the seed set to apply")}
	}
	actor, err := t.seedActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, err := t.Seed.Apply(ctx, p.Set, p.Keys, actor, seed.PathExplicit)
	if err != nil {
		return nil, t.seedError(err)
	}
	// The daemon does not confirm: a socket cannot prove a human is present
	// (SD11). The confirmation is a UI/CLI concern; the apply is the explicit
	// door, and the audit stamps the peer.
	return SeedApplyResult{Apply: seedApplyOutcomeView(result), RequiresConfirmation: false}, nil
}

func (t *Table) handleSeedRestore(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Seed == nil {
		return nil, ErrStoreUnavailable.WithWhat("seed.* needs the seed engine, and this build did not link it — the seed surface is absent for the same reason (V13)")
	}
	var p SeedRestoreParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Set) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("set is required — name the seed set to restore from")}
	}
	if strings.TrimSpace(p.Key) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("key is required — name the record to restore")}
	}
	actor, err := t.seedActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, err := t.Seed.Restore(ctx, p.Set, p.Key, actor)
	if err != nil {
		return nil, t.seedError(err)
	}
	return SeedRestoreResult{Apply: seedApplyOutcomeView(result)}, nil
}

// --- the mapping ------------------------------------------------------------

// setOf renders the optional set scope (absent = every set).
func setOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func seedSetStatusView(s seed.SetStatus) SeedSetStatus {
	out := SeedSetStatus{
		ID:               s.ID,
		Class:            string(s.Class),
		AvailableVersion: s.AvailableVersion,
		AppliedVersion:   s.AppliedVersion,
		Records:          make([]SeedRecordStatus, 0, len(s.Records)),
	}
	for _, r := range s.Records {
		out.Records = append(out.Records, SeedRecordStatus{
			Key:              r.Key,
			State:            string(r.State),
			AvailableVersion: r.AvailableVersion,
			AppliedVersion:   r.AppliedVersion,
			Reason:           r.Reason,
		})
	}
	return out
}

// SeedStatusView is the ONE mapping of the engine's status rows to the wire
// result. The socket's seed.status and the seed_status tool (a records
// catalog arm in the daemon) both call it over the same *seed.Engine, so the
// two doors cannot drift; the parity test pins it field for field (SD10). A
// second copy of this mapping would be a second truth.
func SeedStatusView(states []seed.SetStatus) SeedStatusResult {
	result := SeedStatusResult{Sets: make([]SeedSetStatus, 0, len(states))}
	for _, s := range states {
		result.Sets = append(result.Sets, seedSetStatusView(s))
	}
	return result
}

func seedSetPlanView(p seed.SetPlan) SeedSetPlan {
	out := SeedSetPlan{
		SetID:     p.SetID,
		Decisions: make([]SeedDecision, 0, len(p.Decisions)),
	}
	for _, d := range p.Decisions {
		out.Decisions = append(out.Decisions, SeedDecision{
			Key:         d.Key,
			Op:          string(d.Op),
			State:       string(d.State),
			FromVersion: d.FromVersion,
			ToVersion:   d.ToVersion,
			Reason:      d.Reason,
		})
	}
	out.Refusal = SeedRefusal{Key: p.Refusal.Key, Reason: p.Refusal.Reason}
	return out
}

func seedApplyOutcomeView(r seed.ApplyResult) SeedApplyOutcome {
	out := SeedApplyOutcome{
		SetID:   r.SetID,
		Records: make([]SeedRecordResult, 0, len(r.Records)),
	}
	for _, rec := range r.Records {
		out.Records = append(out.Records, SeedRecordResult{
			Key:         rec.Key,
			Op:          string(rec.Op),
			FromVersion: rec.FromVersion,
			ToVersion:   rec.ToVersion,
			Reason:      rec.Reason,
		})
	}
	return out
}

// seedError maps the engine's untyped refusals to the protocol's two codes,
// VERBATIM: the engine's own sentence (its What) travels unchanged, the
// handler only names the code. A plan refusal (a downgrade, an unattributed
// apply that reached the engine) is ErrSeedRefused; an unknown set is
// ErrSeedUnknownSet. Anything else is an infrastructure fault (a repo read
// that failed, P31) and passes through as-is — it keeps its own message and
// the transport's -32603, not a confident wrong Aeon code.
func (t *Table) seedError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "is not declared"):
		// The engine's sentence is verbatim — the handler adds no interpretation
		// (SD10: the engine is the source of truth for the refusal).
		return ErrSeedUnknownSet.WithWhat("%s", msg)
	case strings.Contains(msg, "refused"), strings.Contains(msg, "unattributed"):
		return ErrSeedRefused.WithWhat("%s", msg)
	default:
		return err
	}
}
