package protocol

// ════════════════════════════════════════════════════════════════════════════
// The authoring audit read (S45 T3, PM8): `policy.changes` — who widened or
// narrowed a guard, when, from which door.
//
// # What this file is
//
// One read-only method over the append-only `policy_change` table:
// `policy.changes {policyId?, limit?}` answers `{changes, statement?,
// unauditedPolicyIds}`. The audit rows are written by the authoring doors
// (T4/T5) beside the writes they record; this door only reads them — zero
// writes (D9: a read is not an event, and the audit is downstream of every
// decision, never upstream of one — PM11/TD2, pinned store-side by
// TestEnforcementNeverReadsPolicyChange).
//
// It reaches the table through the package-local `PolicyChangeStore` seam,
// the proposalStoreAdapter pattern (S35 T3): the wire view is THIS
// package's, the row shape is the store's, and the daemon's adapter is the
// one layer that knows both. The seam has no writer — an audit this door
// could edit would be the audit that isn't (PM8).
//
// # Three states, three answers (the audit's DEF-041 honesty)
//
//   - the audit could not be read → the typed refusal (-31841), loudly;
//   - the table provably holds NO rows → the changes list plus the
//     statement "no authoring changes recorded." — the statement belongs to
//     THAT answer alone (Count says it, not "my filter matched nothing");
//   - the table holds rows and the filter matched none → an empty list and
//     NO statement: the filter's emptiness is the filter's answer, and
//     printing the empty-table sentence here would claim "nobody has ever
//     authored policy" beside a table full of authoring (PolicyListFilterEmptyStatement's
//     discipline, carried onto the audit by omission — silence is the
//     filter's honest rendering, the statement is the table's).
//
// `unauditedPolicyIds` is T4's reconciliation read (which stored policies
// carry no audit row — the recoverable gap of a write-then-audit crash,
// never reportable as "nothing happened"). The field is ALWAYS present —
// `[]` until T4 wires the reconciliation — because a field that vanishes
// when empty is the field a client forgets to check.
//
// # The params, and who validates them
//
// `policyId` passes the boundary's own identifier gate
// (policy.CheckIdentifier — SEC-046's delegation, the same call the other
// policy reads make; a second looser validator here is how invisible names
// reach an operator's console). `limit` is passed through UNTOUCHED: the
// clamp [1,500]/default 50 is the store accessor's contract (one
// definition, two doors — restating it here would be a second number free
// to drift).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// PolicyChangesEmptyStatement is the EMPTY TABLE's sentence — pinned byte
// for byte where the wire shape is defined (the PolicyListEmptyStatement
// discipline): T4's CLI twin (T8's `aeon policy changes`) prints the
// identical line, and a test pins the two surfaces. It rides only the
// Count-proved zero; a filtered-empty read prints nothing (see the header's
// three states).
const PolicyChangesEmptyStatement = "no authoring changes recorded."

// PolicyChangeView is one authoring-audit row on the wire — the columns of
// policy_change, field for field (the ProposalRow mirror convention):
// policyId is absent where the table holds NULL (seed names many ids,
// reject names none); proposalId is absent where no proposal was decided
// (NULL is not 0 — a 0 would name a row that does not exist). summary is
// the fixed-template sentence the writer built from ids (SEC-033: never
// free text), printed as it is — this file assembles nothing.
type PolicyChangeView struct {
	ID         int64  `json:"id"`
	Op         string `json:"op"`
	PolicyID   string `json:"policyId,omitempty"`
	ProposalID *int64 `json:"proposalId,omitempty"`
	Summary    string `json:"summary"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

// PolicyChangesParams are `policy.changes`' params. The ZERO request — no
// params, `null`, or `{}` — reads the whole audit newest-first at the
// accessor's default depth.
type PolicyChangesParams struct {
	// PolicyID filters to one rule's trail; "" spans every rule.
	PolicyID string `json:"policyId,omitempty"`
	// Limit passes through to the store accessor, which owns the clamp
	// [1,500] and the default 50. This handler does not restate either.
	Limit int `json:"limit,omitempty"`
}

// PolicyChangesResult is `policy.changes`' answer envelope.
type PolicyChangesResult struct {
	// Changes is newest first (the accessor's ORDER BY id DESC); [] when
	// nothing matched, never null — the same contract PolicyEventsResult
	// holds its rows to.
	Changes []PolicyChangeView `json:"changes"`
	// Statement is the EMPTY TABLE's own sentence, present only when the
	// audit provably holds no rows (the header's three states; omitempty
	// because its absence IS the other answer).
	Statement string `json:"statement,omitempty"`
	// UnauditedPolicyIDs names stored policies whose authoring audit row is
	// missing (the crash gap between a write and its audit — DEF-041's
	// third state). Always present; [] until T4 wires the reconciliation
	// read (before that read exists, [] states "this door has not looked",
	// which T4's reconciliation turns into the measured answer — and it is
	// why the field has no omitempty).
	UnauditedPolicyIDs []string `json:"unauditedPolicyIds"`
}

// PolicyChangeStore is the package-local seam onto the authoring audit —
// the ProposalStore pattern (S35 T3): internal/store is not banned from
// this package, but the seam keeps the wire types here and the adaptation
// in internal/daemon. READS ONLY, by shape and by right: the audit is
// append-only by trigger (PM8), and a seam with a write method would be
// that trigger, bypassed in Go, by a door that was never meant to have it.
// The daemon adapts *store.Store's PolicyChangeList/PolicyChangeCount.
type PolicyChangeStore interface {
	PolicyChangeList(ctx context.Context, policyID string, limit int) ([]PolicyChangeView, error)
	PolicyChangeCount(ctx context.Context) (int, error)
}

// requirePolicyChanges answers the one wiring question the read faces
// first, with the framework's own store-absence code (requirePolicyStore's
// idiom, carried unchanged): a storeless daemon says so, rather than
// dereferencing or pretending the audit is empty.
func (t *Table) requirePolicyChanges() (PolicyChangeStore, error) {
	if t.PolicyChanges == nil {
		return nil, ErrStoreUnavailable
	}
	return t.PolicyChanges, nil
}

func (t *Table) handlePolicyChanges(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	seam, err := t.requirePolicyChanges()
	if err != nil {
		return nil, err
	}
	var p PolicyChangesParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}

	// The id is an identifier that reaches the operator's console rendered
	// (SEC-046): the boundary's own gate, one delegated call — an id that
	// could never have been authored cannot be the filter's subject either.
	if p.PolicyID != "" {
		if rej := policy.CheckIdentifier("policyId", p.PolicyID); rej != nil {
			return nil, policyRefusal(rej)
		}
	}

	// The filtered read first (it is the answer), then the whole-table
	// Count (it is the answer ABOUT the answer: which empty this is). Two
	// bounded reads; no cache between them and the store (PM11: the audit
	// is re-read per call, the way the gate re-reads the set per decision).
	rows, err := seam.PolicyChangeList(ctx, p.PolicyID, p.Limit)
	if err != nil {
		return nil, policyChangesFault(err)
	}
	total, err := seam.PolicyChangeCount(ctx)
	if err != nil {
		return nil, policyChangesFault(err)
	}

	res := PolicyChangesResult{
		Changes: rows,
		// The always-present two: [] is an answer, null is a shrug.
		UnauditedPolicyIDs: []string{},
	}
	if res.Changes == nil {
		res.Changes = []PolicyChangeView{}
	}
	if total == 0 {
		res.Statement = PolicyChangesEmptyStatement
	}
	// S45 T4 (Behaviour 6): the reconciliation. A crash (or a lost audit
	// write) between the boundary and its audit row leaves a stored policy
	// the audit never names — the gap T3's field exists to report rather
	// than hide, and the read below is the door's own DEF-041 third state.
	unaudited, rerr := t.unauditedPolicyIDs(ctx, seam, total)
	if rerr != nil {
		return nil, rerr
	}
	res.UnauditedPolicyIDs = unaudited
	return res, nil
}

// policyChangeFullScanCap is the whole-table scan bound — the store
// accessor's own clamp maximum, named once here rather than a fourth number
// free to drift (the handler passes limits through untouched; this is the
// reconciliation's scan width, not a user-facing limit).
const policyChangeFullScanCap = 500

// unauditedPolicyIDs implements the crash-gap read (T4 Behaviour 6) with ONE
// narrow, honest rule, stated where the field is defined:
//
//	A policy row is unaudited iff its provenance is 'user' (an operator's
//	add — the door that owes exactly one policy_change row referencing its
//	id) AND no policy_change row — any op — names its policy_id.
//
// Why 'user' only: a seeded (origin 'system') row is audited by the corpus
// door's ONE per-run seed row (S45 T12: policyseed.ApplyCorpus writes it
// through PolicySeedAuditTx), which names the RUN, not each of the 367 ids
// — per-id absence there is not a crash gap, it is that row's shape, and
// calling it "unaudited" would make every seeded corpus look like a
// corruption. Rows removed and re-added keep whichever audit names their
// current id.
//
// Completeness is proven, not assumed (T3's three-state discipline): when
// the whole audit fits one bounded scan (Count ≤ policyChangeFullScanCap)
// the scan's policy_id set is the WHOLE set, and membership is exact. When
// it does not, the reconciliation degrades to a per-row existence probe
// (PolicyChangeList(id, 1) per candidate — the accessor's indexed point
// read), so a large audit never shrinks the answer to the comfortable []
// that would lie. Candidates are read from the boundary's own fail-closed
// ListDetailed — a row that refuses its own decode refuses this read loudly
// (policy.get's rule; a reconciliation that drops bad rows drops exactly
// the rows an operator needs named).
//
// A daemon without the policy set (nil PolicyStore) cannot reconcile: [] is
// then the stated "this door has not looked", exactly as before T4 wired
// the read (the field's documented pre-state, kept honest for the one
// configuration where it remains true).
func (t *Table) unauditedPolicyIDs(ctx context.Context, seam PolicyChangeStore, total int) ([]string, *Error) {
	return UnauditedPolicyIDs(ctx, t.PolicyStore, seam, total, now())
}

// UnauditedPolicyIDs is the crash-gap reconciliation exported (S45 T8): the
// CLI's offline direct door runs THE SAME reconciliation the socket handler
// runs — one implementation, two doors (PM4) — over its own boundary and
// its own shim onto the audit, rather than a third copy of the rule. The
// doc above the method is the claim; this function is that claim's body.
func UnauditedPolicyIDs(ctx context.Context, ps *policy.Store, seam PolicyChangeStore, total int, instant time.Time) ([]string, *Error) {
	if ps == nil {
		return []string{}, nil
	}
	listed, rej := ps.ListDetailed(ctx, instant)
	if rej != nil {
		return nil, policyRefusal(rej)
	}
	var candidates []string
	for _, l := range listed {
		if l.Origin == "user" {
			candidates = append(candidates, l.ID)
		}
	}
	if len(candidates) == 0 {
		return []string{}, nil
	}

	if total <= policyChangeFullScanCap {
		// One bounded scan spans the whole audit: membership is exact.
		all, err := seam.PolicyChangeList(ctx, "", policyChangeFullScanCap)
		if err != nil {
			return nil, policyChangesFault(err)
		}
		named := make(map[string]bool, len(all))
		for _, c := range all {
			if c.PolicyID != "" {
				named[c.PolicyID] = true
			}
		}
		var out []string
		for _, id := range candidates {
			if !named[id] {
				out = append(out, id)
			}
		}
		if out == nil {
			out = []string{}
		}
		return out, nil
	}

	// The audit outgrew one scan: probe existence per candidate instead of
	// pretending the window is the table. Each probe is the accessor's
	// indexed single-row read; the candidate set is the operator's own rows,
	// which the audit was built to be small about.
	var out []string
	for _, id := range candidates {
		hits, err := seam.PolicyChangeList(ctx, id, 1)
		if err != nil {
			return nil, policyChangesFault(err)
		}
		if len(hits) == 0 {
			out = append(out, id)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// policyChangesFault is the three-state refusal: a fault READING the audit
// is neither a refusal (nothing was declined) nor an empty audit (nothing
// was proven absent) — collapsing it into either is the confident wrong
// answer the audit exists to make impossible, and the empty-table sentence
// would then be printed by the door that failed to read. The store's own
// sentence travels verbatim in `what` (the boundary-refusal rule, carried
// from policyRefusal: the layer below named the fault; this file adds no
// interpretation).
func policyChangesFault(err error) *Error {
	return ErrPolicyChangesUnavailable.WithWhat(
		"the policy.changes read could not reach the authoring audit: %s", err.Error())
}
