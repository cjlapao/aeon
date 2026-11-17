package protocol

// ════════════════════════════════════════════════════════════════════════════
// The authoring doors (S45 T4, PM1): add, remove and seedCorpus —
// the human door over the socket (the method-name literals themselves live
// only in method.go's sanctioned S45 authoring block; this file spells the
// verbs undotted, which cmd/aeon's amended PL15 walk pins).
//
// # What this file is
//
// PM1's ruling made executable for policy: the three authoring verbs ride the
// socket as the human surface — filesystem permissions on a 0600 endpoint
// plus peer credentials, no claim or role in the path (P26). The methods are
// registered CONDITIONALLY (V13, the RegisterSeed pattern): a table without
// the `PolicyAuthoring` seam has NONE of the three — absent, not
// registered-and-refusing. The daemon wires the seam (internal/daemon/
// policy_authoring.go) whenever a store exists, and the CLI stays a secondary
// client of the same door (P34/PM4).
//
// # The actor is the kernel's peer, never a param (PM7.3, PM8)
//
// `Actor` arrives as the interface's first argument, filled by this file from
// `Table.ActorFor` — the daemon stamps it from the connection's peer
// credential. Params carry no identity: `decodeParams` tolerates unknown
// fields package-wide (T3's forward-compatibility rule, pinned by
// TestNothingCallsDisallowUnknownFields), so the identity keys — `actor`,
// `status`, `createdBy`, `acceptedBy` — are refused BY NAME through the
// package's own stand-in for strict decoding (the proposals_agent_import
// pattern): params carrying any of them answer the shape error (-32602) and
// nothing is written. A caller that names its own identity is refused, not
// believed. A peer that cannot be resolved (the daemon's uid 0) is refused
// with ErrPolicyNoActor — never a placeholder stamp (T0 item 3.4).
//
// # No second validator, one boundary round-trip (PM4, PM11)
//
// The handlers validate NOTHING policy-shaped. The service behind the seam
// (internal/daemon/policy_authoring.go) presents every row to THE write
// boundary — `Store.Add`/`Store.Remove` — exactly once, and PL1's refusal,
// the closed sets and the per-type spec refusals arrive VERBATIM through
// ErrPolicyAuthoringRefused's `what` (policyRefusal's rule, carried onto
// writes). A `before`/`after` InForceAnswer.Render() pair rides every
// successful write result un-reformatted (D14, D9 — this file assembles
// nothing); a refused write has no result.
//
// # What the methods are NOT
//
// Not on the narrow HTTP/MCP surface, not agent services, not tools, not the
// records catalog (PM10/T6's negatives; nothing here regresses that). Not
// Debug: product surface, like every policy method before them. Not cached,
// not queued, not debounced (PM11): the next policy.why after a successful
// add reflects it, through the store, with no pause.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// PolicyAuthoring is the package-local seam onto the daemon's authoring
// service (the ProposalStore pattern, S35 T3 — no internal/store import here,
// no protocol import there). The actor argument is filled by THIS package
// from the connection (Table.ActorFor), never from params; a service that
// trusted anything else would be trusting a caller to name its own identity.
//
// The three methods answer the three verbs; their refusals travel as typed
// *Errors (ErrPolicyAuthoringRefused carries the boundary's sentence
// verbatim), and a plain error is infrastructure (Dispatch's internal error,
// the proposals.go rule) — never a refusal.
type PolicyAuthoring interface {
	Add(ctx context.Context, actor Actor, p PolicyAddParams) (PolicyAddResult, error)
	Remove(ctx context.Context, actor Actor, id string) (PolicyRemoveResult, error)
	SeedCorpus(ctx context.Context, actor Actor, p SeedCorpusParams) (SeedCorpusResult, error)
}

// --- the wire shapes ---------------------------------------------------------

// PolicyTargetP is the wire twin of policy.Target: {kind, id}. Whether a kind
// or an id MEANS anything is the boundary's own target-parse judgement
// (ParseTarget, fail-closed) — reached by the service inside the single
// boundary round-trip, never pre-checked here (no second validator).
type PolicyTargetP struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// PolicyAddParams is the add door's params — the CLI verb's flags made wire
// (the parity table's `policy add` row). `source` is accepted (the CLI
// exposes --source) and PL1 is enforced exactly once, at the boundary, on
// every door: an `allow` with a non-operator source is refused there with
// PL1's own sentence carried verbatim. `source` omitted defaults to
// "operator" server-side — the CLI's default, applied by the service so both
// doors state the same default. `notAfter` is RFC3339 or null; null (and
// omission) means NO expiry, never a far-future sentinel (PL7).
//
// It carries NO actor, NO status, NO createdBy: those keys, if they arrive,
// are refused as a shape error (see authoringIdentityKeys).
type PolicyAddParams struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Target   PolicyTargetP   `json:"target"`
	Action   string          `json:"action"`
	Spec     json.RawMessage `json:"spec"`
	Source   string          `json:"source"`
	NotAfter *string         `json:"notAfter"`
}

// InForceView is one target's in-force answer, carried VERBATIM from
// InForceAnswer.Render() (D14): the three-state rows are the same
// PolicyInForceRow shape policy.inForce renders — one sentence per rule, the
// state name beside it, nothing collapsed and nothing re-assembled (D9).
// Answers is [] (never null) when the target has no rules: "nothing in
// force" is an answer, null is a shrug.
type InForceView struct {
	Target  string             `json:"target"`
	Answers []PolicyInForceRow `json:"answers"`
}

// PolicyAddResult is the add door's answer: the row as policy.get would return
// it, the warnings the CLI prints on stderr (carried as data for every
// surface — DEF-038's unmatchable-target line and SEC-014's broad-allow
// line, each VERBATIM), and the in-force sentence set for the target before
// and after the write (PM7.2's confirmation made wire).
type PolicyAddResult struct {
	Policy   PolicyDetail `json:"policy"`
	Warnings []string     `json:"warnings"`
	Before   InForceView  `json:"before"`
	After    InForceView  `json:"after"`
}

// PolicyRemoveParams is the remove door's params: the one id.
type PolicyRemoveParams struct {
	ID string `json:"id"`
}

// PolicyRemoveResult is the remove door's answer. Removed:false with a nil
// Policy is DB19's idempotent no-op (removing an absent id is not an error
// and owes no audit row); the row as it was travels beside a real removal so
// the operator sees WHAT they deleted after it is gone (the evidence the CLI
// verb only prints and forgets). Before/After name the removed row's target
// and are absent (empty) for the no-op — there is no target to answer for.
type PolicyRemoveResult struct {
	Removed bool          `json:"removed"`
	Before  InForceView   `json:"before"`
	After   InForceView   `json:"after"`
	Policy  *PolicyDetail `json:"policy"`
}

// SeedCorpusParams is the seedCorpus door's params: which agents (empty spans
// the whole shipped corpus) and whether this run writes (false) or only
// counts (true). A dry run is a READ: it writes nothing and audits nothing
// (Behaviour 4 — no policy_change row, no seed_event, no RecordAuthored).
type SeedCorpusParams struct {
	Agents []string `json:"agents"`
	DryRun bool     `json:"dryRun"`
}

// SeedAgentCount is one named agent's share of a seed run — the per-agent
// counts the CLI verb prints (T1's goldens) made wire. Seeded counts rows
// the run wrote; Unchanged counts what it left alone (already applied,
// diverged, removed, or conflict-skipped); the two add up to the agent's
// corpus rules, so a count never silently omits what was skipped.
type SeedAgentCount struct {
	Agent     string `json:"agent"`
	Seeded    int    `json:"seeded"`
	Unchanged int    `json:"unchanged"`
}

// SeedCorpusResult is the seedCorpus door's answer: the per-agent lines (when
// agents named them), the totals, and whether the run wrote. A second real
// run of the same corpus reports Unchanged and audited nothing new
// (idempotence, D4's convergence).
type SeedCorpusResult struct {
	PerAgent  []SeedAgentCount `json:"perAgent"`
	Seeded    int              `json:"seeded"`
	Unchanged int              `json:"unchanged"`
	DryRun    bool             `json:"dryRun"`
}

// --- registration -------------------------------------------------------------

// RegisterPolicyAuthoring registers the three authoring doors when (and only
// when) the seam is wired — the RegisterSeed pattern (V13): a nil seam is the
// configuration speaking, and the methods are ABSENT, not
// registered-and-refusing. RequiresInitialize true (the table is not complete
// before the handshake, and an authoring write on an unnegotiated connection
// is unthinkable); Debug false (product surface — the narrow projection's
// exclusion of Debug must never be what keeps these off HTTP; PM10's
// negative guards own that absence). Returns whether the doors are in the
// table, so the daemon's wiring is asserted, not trusted.
func (t *Table) RegisterPolicyAuthoring() bool {
	if t.PolicyAuthoring == nil {
		return false
	}
	t.register(&Method{Name: MethodPolicyAdd, RequiresInitialize: true, Handle: t.handlePolicyAdd})
	t.register(&Method{Name: MethodPolicyRemove, RequiresInitialize: true, Handle: t.handlePolicyRemove})
	t.register(&Method{Name: MethodPolicySeedCorpus, RequiresInitialize: true, Handle: t.handlePolicySeedCorpus})
	return true
}

// --- the actor ------------------------------------------------------------------

// authoringIdentityKeys are the params keys that would let a CALLER name its
// own identity or the row's authorship. The package tolerates unknown fields
// everywhere (T3's additive-evolution rule — see handshake.go's decodeParams
// and TestNothingCallsDisallowUnknownFields), so strictness here is the
// proposals_agent_import stand-in: the params decode leniently and the key
// BAG is checked; any bag carrying one of these keys is refused the shape
// error, naming the key. A future additive key is tolerated; an identity key
// never is.
var authoringIdentityKeys = []string{"actor", "status", "createdBy", "acceptedBy"}

// rejectIdentityKeys reads the params as a bag of keys and refuses any bag
// that carries an identity key. The refusal is ErrBadParams — the shape of
// params is a protocol-level fact (JSON-RPC's -32602), and "you named your
// own identity" is exactly a wrong-shaped request.
func rejectIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope with its own error, in its own words
	}
	for _, key := range authoringIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the actor and the authorship are stamped by the daemon from the connection, never named by the caller (PM7.3/PM8)",
				method, key)}
		}
	}
	return nil
}

// authoringActor stamps who pressed the button from the CONNECTION (PM7.3,
// PM8; the seedActor shape carried onto policy). Three refusals, all
// ErrPolicyNoActor — the honest absence that stops a write:
//
//   - no ActorFor seam wired (a table built without the daemon's stamping):
//     no persons, no authoring;
//   - an empty id: nothing to attribute the write to;
//   - id "0": the daemon resolves an unattributable peer to uid 0 (T0 item
//     3.4 — d.peer's answer for an unresolvable connection). Stamping
//     authoring rows "user 0" would dress the audit in a real-looking
//     identity that belongs to no verified peer, so the door REFUSES
//     (PM7.3's own words). Root at the socket reaches the same refusal and
//     the CLI door remains — recorded in T4's Notes.
func (t *Table) authoringActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil || sess == nil {
		return Actor{}, ErrPolicyNoActor
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" || a.ID == "0" {
		return Actor{}, ErrPolicyNoActor
	}
	return a, nil
}

// --- the handlers -----------------------------------------------------------------

// requirePolicyAuthoring is the tripwire that registration and the seam
// disagreed (the unreachable-in-built-configurations guard, search.query's
// shape): absent is the designed outcome (V13), and if a handler is ever
// reached without its seam it says so rather than dereferencing nil.
func (t *Table) requirePolicyAuthoring() (PolicyAuthoring, error) {
	if t.PolicyAuthoring == nil {
		return nil, ErrStoreUnavailable.WithWhat(
			"the policy authoring doors need the daemon's authoring service, and this build did not wire it — the three methods are absent for the same reason (V13)")
	}
	return t.PolicyAuthoring, nil
}

func (t *Table) handlePolicyAdd(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	svc, err := t.requirePolicyAuthoring()
	if err != nil {
		return nil, err
	}
	// Behaviour 1's order: the actor resolves FIRST (a write is attributed
	// before it is even parsed), then the strict params shape, then the
	// boundary judgement (inside the service, once).
	actor, err := t.authoringActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	if err := rejectIdentityKeys(string(MethodPolicyAdd), params); err != nil {
		return nil, err
	}
	var p PolicyAddParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// `notAfter` is RFC3339-or-null (PL7): the SHAPE is checked here — a
	// string that is not an instant is the params' fault, -32602 — and the
	// boundary re-owns the value it stores. null and omission are NO
	// expiry, never a sentinel.
	if p.NotAfter != nil {
		if _, perr := parseAuthoringInstant("notAfter", *p.NotAfter); perr != nil {
			return nil, perr
		}
	}
	return svc.Add(ctx, actor, p)
}

func (t *Table) handlePolicyRemove(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	svc, err := t.requirePolicyAuthoring()
	if err != nil {
		return nil, err
	}
	actor, err := t.authoringActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	if err := rejectIdentityKeys(string(MethodPolicyRemove), params); err != nil {
		return nil, err
	}
	var p PolicyRemoveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return svc.Remove(ctx, actor, p.ID)
}

func (t *Table) handlePolicySeedCorpus(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	svc, err := t.requirePolicyAuthoring()
	if err != nil {
		return nil, err
	}
	actor, err := t.authoringActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	if err := rejectIdentityKeys(string(MethodPolicySeedCorpus), params); err != nil {
		return nil, err
	}
	var p SeedCorpusParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return svc.SeedCorpus(ctx, actor, p)
}

// parseAuthoringInstant is the params' instant gate for notAfter: RFC3339
// (nanoseconds accepted — the CLI's --not-after spelling), refused as the
// shape error when the string is not one. This is not validation of what a
// time MEANS (the boundary owns the row); it is the params' own hygiene,
// stated once here because the wire spells the field as a string.
func parseAuthoringInstant(field, s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, ErrBadParams{Err: fmt.Errorf(
			"%s is an empty string — an expiry is an RFC3339 instant or null (PL7: null means no expiry; \"\" means nothing and is refused, never defaulted)", field)}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, ErrBadParams{Err: fmt.Errorf(
			"%s %q is not an RFC3339 instant (like 2026-10-15T00:00:00Z) or null", field, s)}
	}
	return t, nil
}

// --- shared wire helpers for the authoring service ---------------------------------

// PolicyDetailFromListed builds the policy.get answer for one row the caller
// already read (the service holds the boundary handle; this keeps the DETAIL
// assembly — the view fields, the raw spec, the acceptance record, the
// authored-audit scan — in ONE definition, policy_detail.go's, so the
// authoring results state exactly what policy.get would state, never a
// near-copy). The acceptance and authored reads travel the boundary's own
// fail-closed readers; a refusal refuses the whole result (T2's rule).
func PolicyDetailFromListed(ctx context.Context, store *policy.Store, found policy.Listed) (PolicyDetail, *policy.Reject) {
	detail := PolicyDetail{
		PolicyView: PolicyView{
			ID:        found.ID,
			Type:      string(found.Type),
			Target:    found.Target.String(),
			Action:    string(found.Action),
			Source:    string(found.Source),
			NotAfter:  found.NotAfter,
			Describe:  policy.Describe(found.Policy),
			CreatedAt: found.CreatedAt,
			Origin:    found.Origin,
		},
		Spec: found.Spec,
	}
	if len(detail.Spec) == 0 {
		detail.Spec = json.RawMessage("{}")
	}
	if store == nil {
		// A hand-built Listed (a test's spy set) still gets the view; the
		// two informational records stay the stated absences ("" / false),
		// which is what "no records were read" honestly renders as.
		return detail, nil
	}
	acceptances, rej := store.AcceptancesFor(ctx, found.Target)
	if rej != nil {
		return PolicyDetail{}, rej
	}
	for _, a := range acceptances {
		if a.PolicyID == found.ID {
			detail.AcceptedBy = a.AcceptedBy
			detail.AcceptedAt = a.AcceptedAt
			break
		}
	}
	if found.Target.Kind == policy.AgentKind || found.Target.Kind == policy.LoopKind {
		_, events, rej := store.ReportEvents(ctx, found.Target.String(), policyAuthoredScanLimit)
		if rej != nil {
			return PolicyDetail{}, rej
		}
		for _, e := range events {
			if e.Outcome == "authored" && e.PolicyID == found.ID {
				detail.Authored = true
				break
			}
		}
	}
	return detail, nil
}

// InForceRowsFrom is the ONE rendering path onto the wire for an in-force
// answer set (D9: policy.inForce's rows and the authoring results'
// before/after are built by the same two lines, so a drift between them is
// a build failure, not a discovery). Render() rides VERBATIM; nothing here
// re-formats a sentence or collapses a state.
func InForceRowsFrom(answers []policy.InForceAnswer) []PolicyInForceRow {
	rows := make([]PolicyInForceRow, 0, len(answers))
	for _, a := range answers {
		rows = append(rows, PolicyInForceRow{
			PolicyID: a.Policy.ID,
			State:    string(a.State),
			Render:   a.Render(),
		})
	}
	return rows
}
