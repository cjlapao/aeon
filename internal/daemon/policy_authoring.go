package daemon

// policy_authoring.go — S45 T4 (PM1): the daemon's authoring door.
//
// # What this file is
//
// The implementation behind the socket's three human-door methods
// (add, remove and the corpus seed verb — protocol's
// PolicyAuthoring seam): THIS file is the ONLY daemon file that calls the
// policy write boundary's Add/Remove (the sanctioned entry in the cmd/aeon
// PL15 walk's `allowedBoundary`, citing S45 T4/PM1), and the only door that
// reaches the corpus seed engine from the socket besides seed.go's own
// corpusSeedDoor. Every other daemon file reaches policy only as a reader
// (the gate, the detail reads) — a second daemon caller of Add/Remove is
// exactly what TestAuthoringServiceIsTheOnlyDaemonCallerOfBoundaryAddRemove
// fails the build for.
//
// # One implementation, two doors (PM2, PM4)
//
// The boundary is the SAME *policy.Store over the SAME newPolicyRepo bridge
// the gate and the reads use (daemon.go wires this beside them) — the row
// the socket stores is byte-for-byte the row the CLI's door stores, because
// both pass through the one Add. The service validates NOTHING twice:
// the params arrive from protocol (which owns only their SHAPE — an
// unparseable notAfter is a -32602 there), and the closed vocabulary, the
// identifier gate, PL1 and the per-type specs run exactly once, inside
// boundary.Add, whose *Reject travels verbatim through
// ErrPolicyAuthoringRefused (no second validator drifted, because there is
// no second validator). The authoring-time warning sentences ride from
// internal/desk (warn.go) — the same bytes the CLI prints, one definition.
//
// # The audit (PM8)
//
// One policy_change row per SUCCESSFUL write, beside the write it records
// (`op='add'`/`'remove'`, ids-only fixed-template summaries — SEC-033),
// stamped with the peer actor protocol stamped from the connection (never a
// param). A refused write audits nothing (the row never existed); a dry-run
// seed audits nothing (a read is not an event); the seed run's audit is
// written by T12's OWN door (policyseed.ApplyCorpus writes the one per-run
// `op='seed'` row through PolicySeedAuditTx) — this file does not append a
// second one. An audit-row failure after a successful boundary write is
// SPOKEN in the log, never hidden and never a reason to call a stored rule
// a failed write (the gate's rule for its own refusal rows, carried); the
// gap is reported by the reconciliation read `policy.changes` runs (T4
// Behaviour 6), so "written, unaudited" is a state the audit names.
//
// # The seed door is the ENGINE door (T12)
//
// The corpus seed door reaches the corpus through policyseed.ApplyCorpus — the
// same engine path `seed.apply`, `aeon seed apply --set policy.corpus` and
// a committed `seed_corpus` proposal use (PM15): per-agent filtering, the
// unknown-agent refusal naming them, the actor rule (a system actor is D4
// in a costume and is refused before any write), idempotence, and the
// per-run audit all belong to that door. This service adds no policy rows,
// no audit rows and no RecordAuthored calls of its own — the applier pairs
// every seeded row with its authored event, and a second writer beside it
// would be the two-row seed T12's Notes exist to forbid.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/policyseed"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// authoringService is the protocol.PolicyAuthoring the socket dispatches to.
// (The task's field sketch named a desk.Registry; T5's commit door is the
// Registry's caller — none of T4's three verbs dispatches a proposal effect,
// and the seed path goes to the engine per T12, so the fields below are the
// ones this door actually holds. Recorded as a deviation in T4's Notes.)
type authoringService struct {
	boundary *policy.Store
	store    *store.Store
	engine   *seed.Engine
	log      Logger
	now      func() time.Time
}

// newAuthoringService builds the door over the daemon's store. nil store ⇒
// nil service ⇒ the three methods stay ABSENT on the table (V13 — absent,
// not registered-and-refusing; RegisterPolicyAuthoring is the gate). The
// seed engine may be nil (a seedless mount): add/remove do not need it and
// the seed door then refuses by name rather than half-seeding.
func newAuthoringService(s *store.Store, e *seed.Engine, log Logger) protocol.PolicyAuthoring {
	if s == nil {
		return nil
	}
	if log == nil {
		log = discardLogger{}
	}
	return &authoringService{
		boundary: policy.NewStore(newPolicyRepo(s)),
		store:    s,
		engine:   e,
		log:      log,
		now:      time.Now,
	}
}

// --- the add door ---------------------------------------------------------------

// Add runs Behaviour 1's order: build the row (pure shaping — the closed
// sets are the boundary's step 1, re-run inside Add exactly once, never
// pre-checked here) → capture the target's in-force BEFORE (PM7.2: a write
// that cannot show what it widened is refused before anything is written)
// → THE boundary (one round-trip; its *Reject, verbatim, is the whole
// refusal) → AFTER-state → the warnings the CLI speaks (desk, one
// definition) → exactly one audit row → the row as policy.get would return
// it. No cache, no queue: the next policy.why reads the store and flips
// (PM11, N8).
func (a *authoringService) Add(ctx context.Context, actor protocol.Actor, p protocol.PolicyAddParams) (protocol.PolicyAddResult, error) {
	pol, bad := policyFromAddParams(p)
	if bad != nil {
		return protocol.PolicyAddResult{}, bad
	}
	before, badView := a.inForceView(ctx, pol.Target)
	if badView != nil {
		// The before-state is part of the confirmation, not garnish: an add
		// whose effect the store cannot answer for is refused BEFORE the
		// write (a half answer about a rule is how a confident wrong answer
		// is built — policy.get's rule, carried onto the write).
		return protocol.PolicyAddResult{}, badView
	}

	at := a.now()
	if rej := a.boundary.Add(ctx, pol, at); rej != nil {
		// PL1's sentence, the closed-set refusal, the identifier gate, the
		// per-type spec refusal — whichever the boundary judged, it named
		// itself, and `what` is its own words (policyRefusal's rule, carried
		// onto writes). NOTHING was stored and NO audit row is owed.
		return protocol.PolicyAddResult{}, protocol.ErrPolicyAuthoringRefused.WithWhat("%s", rej.Reason)
	}

	after, badAfter := a.inForceView(ctx, pol.Target)
	if badAfter != nil {
		// The rule stands — the write committed and the audit will follow;
		// the answer that cannot say the AFTER sentence says plainly which
		// half happened instead of claiming a clean confirmation (the
		// ApplyCorpus audit-failure sentence's discipline, carried).
		return protocol.PolicyAddResult{}, fmt.Errorf(
			"daemon: policy %q was stored, but its after-state could not be read (%s); the rule stands — policy.inForce answers afresh",
			pol.ID, badAfter.Message)
	}

	warnings := authoringWarnings(pol)

	// The audit row: one per accepted write, ids only, fixed template
	// (SEC-033 — the row says WHAT happened built from the closed vocabulary
	// and gated identifiers; the boundary already refused anything else).
	a.audit(ctx, actor, "add", pol.ID, fmt.Sprintf("added %s %s for %s", pol.Type, pol.Action, pol.Target.String()))

	// The row as policy.get would return it — read back through the SAME
	// fail-closed path, so the authoring answer and the read answer are one
	// definition (D9), never a shape the write assembled for itself.
	detail, err := a.detailFor(ctx, pol.ID)
	if err != nil {
		return protocol.PolicyAddResult{}, err
	}
	return protocol.PolicyAddResult{
		Policy:   detail,
		Warnings: warnings,
		Before:   before,
		After:    after,
	}, nil
}

// policyFromAddParams is the pure shaping of wire params into the boundary's
// input — string casts only, the CLI's own shape (`policy.PolicyType(*typ)`
// et al.), with the ONE server-side default the door owns: an omitted
// `source` is "operator", the CLI's default (the CLI's --source default is
// "operator"; the socket omits it, the same row is authored). Whether a
// value MEANS anything is Add's step 1 (the closed-set parsers re-run on
// hand-cast enums — SEC-015/SEC-007), never a pre-check here.
func policyFromAddParams(p protocol.PolicyAddParams) (policy.Policy, error) {
	var notAfter *time.Time
	if p.NotAfter != nil {
		t, err := time.Parse(time.RFC3339Nano, *p.NotAfter)
		if err != nil {
			// The socket already refused this shape (-32602 at decode);
			// a direct caller reaches the same refusal here rather than
			// storing a row with a time the wire could not have named.
			return policy.Policy{}, protocol.ErrBadParams{Err: fmt.Errorf(
				"notAfter %q is not an RFC3339 instant (like 2026-10-15T00:00:00Z) or null", *p.NotAfter)}
		}
		notAfter = &t
	}
	src := policy.Source(p.Source)
	if strings.TrimSpace(p.Source) == "" {
		src = policy.Operator // the CLI's default, stated once — the door does not guess a source
	}
	return policy.Policy{
		ID:       p.ID,
		Type:     policy.PolicyType(p.Type),
		Target:   policy.Target{Kind: policy.TargetKind(p.Target.Kind), ID: p.Target.ID},
		Action:   policy.Action(p.Action),
		Spec:     append(json.RawMessage(nil), p.Spec...),
		Source:   src,
		NotAfter: notAfter,
	}, nil
}

// authoringWarnings carries the two authoring-time operator signals the CLI
// speaks (broad-allow SEC-014 first — the CLI prints it before the write;
// unmatchable-target DEF-038 second — the CLI prints it after), VERBATIM
// from the desk's single definition. The socket carries them as DATA so
// every surface says what the CLI says (T8's parity: the app cannot lose a
// warning the terminal gives).
func authoringWarnings(p policy.Policy) []string {
	out := []string{}
	if warn, ok := desk.BroadAllowWarning(p.Action, p.Spec); ok {
		out = append(out, warn)
	}
	switch string(p.Target.Kind) {
	case string(policy.LoopKind), string(policy.LaneTypeKind), string(policy.MCPServerKind):
		out = append(out, desk.UnmatchableTargetWarn(string(p.Target.Kind)))
	}
	return out
}

// --- the remove door ---------------------------------------------------------------

// Remove runs Behaviour 3: capture the row AND the before-state (and the
// row-as-it-was detail, before the row is gone — the evidence the CLI verb
// prints and forgets), THEN the boundary, then the after-state and the one
// audit row. Removing an absent id is `removed:false`, no audit row, no
// error (DB19 — an idempotent delete is not a failure). Removing a SYSTEM
// row is allowed and the summary SAYS so (PM16: policy is the operator's to
// shape; the engine then reports the record `removed` and seed.apply will
// not resurrect it — that is T12's standing behaviour, this row records the
// choice).
func (a *authoringService) Remove(ctx context.Context, actor protocol.Actor, id string) (protocol.PolicyRemoveResult, error) {
	rows, rej := a.boundary.ListDetailed(ctx, a.now())
	if rej != nil {
		// The set cannot be read — fail closed: a remove whose target's
		// shape the store cannot confirm is not a remove to attempt.
		return protocol.PolicyRemoveResult{}, protocol.ErrPolicyAuthoringRefused.WithWhat(
			"the policy set could not be read to attempt the remove of %q: %s", id, rej.Reason)
	}
	var found *policy.Listed
	for i := range rows {
		if rows[i].ID == id {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		// DB19's idempotent no-op: no row, no state to name (the target was
		// never ours to answer for), no audit row, no error.
		return protocol.PolicyRemoveResult{
			Removed: false,
			Before:  protocol.InForceView{Answers: []protocol.PolicyInForceRow{}},
			After:   protocol.InForceView{Answers: []protocol.PolicyInForceRow{}},
			Policy:  nil,
		}, nil
	}

	// Capture BEFORE the write: the target's in-force set and the full
	// detail (acceptance and authored records included) as they stand with
	// the row still in it.
	before, badView := a.inForceView(ctx, found.Target)
	if badView != nil {
		return protocol.PolicyRemoveResult{}, badView
	}
	detail, drej := protocol.PolicyDetailFromListed(ctx, a.boundary, *found)
	if drej != nil {
		return protocol.PolicyRemoveResult{}, protocol.ErrPolicyAuthoringRefused.WithWhat(
			"the remove of %q was refused before the write: its detail could not be read (%s) — a removal whose evidence the store cannot capture is not a removal to attempt", id, drej.Reason)
	}

	existed, err := a.boundary.Remove(ctx, id)
	if err != nil {
		// Infrastructure (DB19's other half — this is not a refusal): the
		// transport's internal error, the store's own sentence inside.
		return protocol.PolicyRemoveResult{}, fmt.Errorf("remove policy %q: %w", id, err)
	}
	if !existed {
		// The row vanished between the read and the delete (another door
		// removed it first). Same honest answer as absent: removed:false,
		// no audit row — we removed nothing, and an audit row for someone
		// else's write is the audit that lies.
		return protocol.PolicyRemoveResult{
			Removed: false,
			Before:  protocol.InForceView{Answers: []protocol.PolicyInForceRow{}},
			After:   protocol.InForceView{Answers: []protocol.PolicyInForceRow{}},
			Policy:  nil,
		}, nil
	}

	after, badAfter := a.inForceView(ctx, found.Target)
	if badAfter != nil {
		return protocol.PolicyRemoveResult{}, fmt.Errorf(
			"daemon: policy %q was removed, but its after-state could not be read (%s); the absence stands — policy.inForce answers afresh",
			id, badAfter.Message)
	}

	summary := fmt.Sprintf("removed %s %s for %s", found.Type, found.Action, found.Target.String())
	if found.Origin == "system" {
		// PM16 said out of the audit row: an operator removed a SEEDED rule.
		// Fixed template — the origin value is the enumerated word, not free
		// text (SEC-033 keeps its rule even for a sentence worth having).
		summary += " (system rule — the operator's own removal; seed.apply will not resurrect it, PM16)"
	}
	a.audit(ctx, actor, "remove", id, summary)

	return protocol.PolicyRemoveResult{
		Removed: true,
		Before:  before,
		After:   after,
		Policy:  &detail,
	}, nil
}

// --- the corpus seed door -------------------------------------------------------------

// SeedCorpus is a THIN method over policyseed.ApplyCorpus (Behaviour 4 as
// T12 landed it): the engine path is the one path (idempotence, the
// divergence refusals, the removed-that-stay-removed, the unknown-agent
// refusal naming them, the actor rule, and the ONE per-run `op='seed'`
// audit row all belong to that door — this service appends nothing). A dry
// run Plans and writes nothing, audits nothing (a read). `PerAgent` lines
// ride for named agents; the totals always do.
func (a *authoringService) SeedCorpus(ctx context.Context, actor protocol.Actor, p protocol.SeedCorpusParams) (protocol.SeedCorpusResult, error) {
	if a.engine == nil {
		// Infrastructure absence, not a refusal: a seedless mount says so
		// (add/remove still work on the same door — the absence is the
		// engine's, not the store's).
		return protocol.SeedCorpusResult{}, fmt.Errorf(
			"daemon: the corpus seed door needs the seed engine and this daemon was mounted without one — the corpus is applied through the engine path (S45 T12), which this daemon does not hold")
	}
	run, err := policyseed.ApplyCorpus(ctx, a.engine, a.store, p.Agents, nil, p.DryRun,
		seed.Actor{Kind: actor.Kind, ID: actor.ID}, a.now())
	if err != nil {
		// The door's refusal (unknown agent named, unknown key named, the
		// costume actor named) travels VERBATIM — it named itself (the
		// boundary-refusal rule, carried onto the seed door).
		return protocol.SeedCorpusResult{}, protocol.ErrPolicyAuthoringRefused.WithWhat(
			"the policy.corpus seed door refused: %s", err.Error())
	}
	per := make([]protocol.SeedAgentCount, 0, len(run.Agents))
	for _, ag := range run.Agents {
		per = append(per, protocol.SeedAgentCount{Agent: ag.Agent, Seeded: ag.Seeded, Unchanged: ag.Unchanged})
	}
	sort.Slice(per, func(i, j int) bool { return per[i].Agent < per[j].Agent })
	return protocol.SeedCorpusResult{
		PerAgent:  per,
		Seeded:    run.Seeded,
		Unchanged: run.Unchanged,
		DryRun:    run.DryRun,
	}, nil
}

// --- shared parts --------------------------------------------------------------

// inForceView reads the target's in-force answer through the boundary's own
// reader and renders it via protocol's ONE row builder (InForceRowsFrom —
// Render() verbatim, D14). The target travels to ParseTarget inside the
// boundary (a bad kind/id is the boundary's refusal by name, not a
// pre-check here).
func (a *authoringService) inForceView(ctx context.Context, target policy.Target) (protocol.InForceView, *protocol.Error) {
	answers, rej := a.boundary.InForceFor(ctx, target, a.now())
	if rej != nil {
		return protocol.InForceView{}, protocol.ErrPolicyAuthoringRefused.WithWhat(
			"the in-force answer for %s could not be read: %s", target.String(), rej.Reason)
	}
	return protocol.InForceView{Target: target.String(), Answers: protocol.InForceRowsFrom(answers)}, nil
}

// detailFor re-reads one row through the fail-closed ListDetailed and
// builds its policy.get answer (protocol's shared builder — one definition
// of the DETAIL shape). A row that vanished or refuses between the write and
// the re-read is an honest infrastructure error: the write's own truth is
// already stated (the audit row records it; policy.changes reconciles).
func (a *authoringService) detailFor(ctx context.Context, id string) (protocol.PolicyDetail, error) {
	rows, rej := a.boundary.ListDetailed(ctx, a.now())
	if rej != nil {
		return protocol.PolicyDetail{}, fmt.Errorf(
			"daemon: the policy set could not be re-read after the write of %q (%s); policy.changes names what the audit holds", id, rej.Reason)
	}
	for i := range rows {
		if rows[i].ID == id {
			detail, drej := protocol.PolicyDetailFromListed(ctx, a.boundary, rows[i])
			if drej != nil {
				return protocol.PolicyDetail{}, fmt.Errorf(
					"daemon: policy %q was stored, but its detail could not be read (%s); the row stands", id, drej.Reason)
			}
			return detail, nil
		}
	}
	return protocol.PolicyDetail{}, fmt.Errorf(
		"daemon: policy %q was written but the fail-closed re-read does not carry it; the store is refusing to vouch for it — policy.changes names what the audit holds", id)
}

// audit appends the one policy_change row an authoring write owes (PM8).
// ActorKind/ActorID are the peer protocol stamped from the CONNECTION — the
// store's shape gate (checkPolicyChangeShape) refuses an empty actor as
// "the unsigned log entry PM8 exists to delete", and it should: a door that
// cannot say who pressed the button never reaches here (protocol's
// authoringActor refused first). A row that FAILS to append is SPOKEN, never
// fatal and never backfilled: the rule stands, the gap is named in the log,
// and `policy.changes`'s reconciliation (T4 Behaviour 6) reports it —
// "written, unaudited" is the state the audit is allowed to hold (the gate's
// rule for its own refusal rows, carried: enforcement never depends on the
// audit, in either direction).
func (a *authoringService) audit(ctx context.Context, actor protocol.Actor, op, policyID, summary string) {
	c := store.PolicyChange{
		Op:         op,
		PolicyID:   policyID,
		ProposalID: nil, // the direct human door decides no proposal (T5's commit door fills its own)
		Summary:    summary,
		ActorKind:  actor.Kind,
		ActorID:    actor.ID,
		OccurredAt: a.now().UTC().Format(time.RFC3339Nano),
	}
	if err := a.store.PolicyChangeInsert(ctx, c); err != nil {
		a.log.Infof("policy authoring: the %s of %s was NOT audited (%s); the write stands — policy.changes names the gap", op, policyID, err)
	}
}
