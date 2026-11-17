package protocol

// ════════════════════════════════════════════════════════════════════════════
// The policy reads (S10, T13): the app's window onto policy state.
//
// # What this file is, and what it refuses to be
//
// Four read-only methods — list, why, inForce, events — exist **for the app
// only** (ruling R4). They read the SAME boundary the enforcement gate reads
// (Table.PolicyStore is the *policy.Store the daemon wires over the same
// RepoFuncs bridge), so the app is never shown a set the runtime is not
// deciding with, and they PRINT what that boundary answers:
//
//   - policy.list's rows are what List re-decoded, row by row, through
//     DecodePolicy — the fail-closed read side, kept because a second read
//     path here is a path that drops rows quietly (and dropping a row can
//     drop a Deny);
//   - policy.why prints Verdict.Sentence() verbatim and branches on the
//     Unimplemented FIELD (SEC-033): the app must branch on a field, never
//     on whether the sentence starts with "refused: ", because "nobody can
//     answer yet" and "a rule refused you" are different answers that share
//     that prefix;
//   - policy.inForce carries InForceAnswer.Render() verbatim, un-collapsed —
//     all three states, three distinct sentences (D14);
//   - policy.events carries the rows plus the depth/cap/oldest triple the
//     retention report owes (D15), capDays derived from PolicyEventAgeCap
//     itself so the number on the wire can never disagree with what
//     TrimEvents enforces.
//
// Re-validating the layer beneath is not this file's job (each surface
// re-renders, never re-validates — T12's rule, carried up). What IS its job
// is validating its OWN params (SEC-T6-005): the shape via decodeParams
// (-32602), and the caller-supplied names through policy.CheckIdentifier —
// the boundary's own identifier gate, exported for exactly this delegation
// (SEC-046). These strings are third-party text from a socket that reaches
// an operator's console through every rendered sentence and render, so they
// pass through the same alphabet parse the write side obeys; a second,
// looser validator here is how BiDi controls and zero-width names slip into
// a render.
//
// # Zero writes (D9)
//
// None of the four touches the recorder. policy.why in particular runs
// List → Evaluate → Sentence and nothing else — a refusal by `why` is not a
// refusal that happened, and an audit row for a hypothetical is the record
// that lies (the CLI holds the same line at T12; policy_test.go pins this
// handler with a spy at zero writes).
//
// # Refusals travel verbatim
//
// A *Reject from the boundary becomes ErrPolicyReadRefused with the
// boundary's own sentence carried in `what` UNTOUCHED — this file adds no
// interpretation, because the boundary already named the fault (and the
// app shows the user what the boundary said).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/policy"
)

// PolicyListEmptyStatement is D4's sentence for an empty policy set, pinned
// byte for byte where the wire shape is defined. The CLI prints the same
// sentence (cmd/aeon's emptyPolicyListLine); a test pins the two against
// each other so the two surfaces cannot drift into two different claims
// about the same empty table. A guard whose empty state reads as "fine" is
// the guard that fails open in the reader's head — so the empty list carries
// this statement, and it is the list's own field, not an error.
const PolicyListEmptyStatement = "no policies. default is DENY (PL5) — nothing is granted until something grants it."

// defaultPolicyEventsLimit mirrors the CLI's `--depth 20`: the socket and
// the terminal answer the same browse with the same window. A caller who
// wants another depth says so with `limit`; a limit of zero arrives here as
// the default, not as ReportEvents' refusal — the boundary still refuses a
// literal zero-limit read, which a socket caller can no longer express.
const defaultPolicyEventsLimit = 20

// requirePolicyStore answers the one wiring question every read faces first:
// does this daemon have a policy store at all. Nil is the pre-S10 and
// storeless shape, and every handler says so with the framework's own
// store-absence code rather than dereferencing (memory.go's requireMemory
// idiom, carried here unchanged).
func (t *Table) requirePolicyStore() (*policy.Store, error) {
	if t.PolicyStore == nil {
		return nil, ErrStoreUnavailable
	}
	return t.PolicyStore, nil
}

// policyRefusal carries the boundary's refusal across the wire VERBATIM: the
// `what` is the boundary's own sentence, because the boundary already named
// the fault and anything this file added would be an interpretation wearing
// the boundary's authority.
func policyRefusal(rej *policy.Reject) *Error {
	return ErrPolicyReadRefused.WithWhat("%s", rej.Reason)
}

// --- policy.list -------------------------------------------------------------

// PolicyListFilterEmptyStatement is the FILTER's answer (S45 T2, PM5): a
// filtered listing that matches nothing says the FILTER matched nothing —
// never D4's sentence, which is the corpus's answer about an empty table
// and would claim "nothing is granted" where rules exist and were simply
// not what was asked for. The two empties never read the same (D4's
// discipline extended by one sentence, and it is a const for the same
// reason D4's is: the CLI (T8) prints the identical line and a test pins
// the two surfaces).
const PolicyListFilterEmptyStatement = "no policies match this filter."

// PolicyListParams are policy.list's params (S45 T2, PM5). Every field is
// optional, and the ZERO request — no params, `null`, or `{}` — is EXACTLY
// the pre-S45 read: unfiltered, both empties as S10 left them (the additive
// fields below are the only difference an old client can see, and T3's rule
// is that it ignores them). The filters are ANDs, applied in the ONE place
// (policy.Filter, internal/policy), after the fail-closed re-decode.
//
// Names are checked as they are checked everywhere in this file: the
// closed-set values through the boundary's own parsers (a refusal names the
// value it refused and travels verbatim); the target through the boundary's
// own target parse, which makes an unparsable target a -32602 — it is the
// caller's shape error, not the boundary's fault.
type PolicyListParams struct {
	// Type keeps one policy type; "" (absent) means any.
	Type string `json:"type,omitempty"`
	// Target keeps one target in its CLI spelling — `agent:NAME`,
	// `loop:SLUG`, `lane-type:NAME`, `mcp:NAME`, or `all-agents`; ""
	// (absent) means any. Split on the FIRST colon and rebuilt by
	// policy.ParseTarget — the spelling `agent:agent:bob` is refused,
	// naming it.
	Target string `json:"target,omitempty"`
	// Source keeps one authoring source; "" (absent) means any.
	Source string `json:"source,omitempty"`
	// Action keeps one action; "" (absent) means any.
	Action string `json:"action,omitempty"`
	// IncludeExpired keeps rows the one expiry rule says have stopped
	// existing. ABSENT MEANS TRUE (D6/D8: an audit view does not hide
	// expiry — an expired deny is history an operator came to read); the
	// UI may default its own toggle and send false deliberately.
	IncludeExpired *bool `json:"includeExpired,omitempty"`
	// Origin keeps one provenance (S45 T12 additive, PM16): "system" for
	// rows the seed process wrote, "user" for operator-authored rows,
	// "import" reserved; "" (absent) means any. A value outside the three
	// is a -32602 naming it — the same delegation every filter value
	// practises (the boundary's ParseOrigin owns the vocabulary).
	Origin string `json:"origin,omitempty"`
}

// PolicyView is one stored policy as the read side renders it. There is no
// `spec` field: the spec is policy content the app has no reason to hold,
// and the read window renders identity, not payload (what a rule DOES is
// answered per-type by the surfaces that own those types).
type PolicyView struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Target   string     `json:"target"`
	Action   string     `json:"action"`
	Source   string     `json:"source"`
	NotAfter *time.Time `json:"notAfter"`
	// Describe is the policy package's ONE sentence for what this rule does
	// (S45 T2, PM5/D9): every surface prints it verbatim and assembles
	// nothing — a second renderer is a second truth. From a daemon that
	// knows the field it is never empty (an undescribable row renders the
	// fixed fallback sentence, so no surface ever shows a blank beside a
	// real rule); omitempty carries the OTHER absence — an older daemon,
	// or the pre-S45 fixture set — which is a meaningful fact (T3's
	// additive direction), not a gap.
	Describe string `json:"describe,omitempty"`
	// CreatedAt is the instant the row carries as `created_at`, rendered
	// byte for byte from the row (S45 T2 additive). It says when the rule
	// was WRITTEN — not when it was last consulted, which no column knows.
	// Omitted exactly as Describe is: absence means the renderer did not
	// have the field, not that the row has no written-at.
	CreatedAt string `json:"createdAt,omitempty"`
	// Origin is the row's PROVENANCE (S45 T12, PM15/PM16): 'system' for a
	// row the seed process wrote, 'user' for an operator's, 'import'
	// reserved. It is not policy content — it says where a rule came from,
	// never what it permits (PM11 keeps the enforcement path blind to it),
	// and a listing that can say "the machine seeded this" is what stops a
	// system rule from reading as an operator's. Omitted the way Describe
	// and CreatedAt are: absence means the rendering daemon did not know
	// the field, not that the row has no provenance.
	Origin string `json:"origin,omitempty"`
}

// PolicyListResult is policy.list's answer. Statement is non-nil EXACTLY
// when the set is empty (D4): the sentence is the answer to the empty case,
// not a caption on every case, and `policies` is always a present array —
// `[]` says "read and empty", a missing field says "this daemon cannot
// answer", and those are different facts. (S45 T2 adds WHICH sentence the
// empty carries — D4's for an unfiltered empty corpus, the filter's for a
// filtered-empty listing — and pins that only one is ever present.)
type PolicyListResult struct {
	Policies  []PolicyView `json:"policies"`
	Statement *string      `json:"statement,omitempty"`
}

func (t *Table) handlePolicyList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	store, err := t.requirePolicyStore()
	if err != nil {
		return nil, err
	}
	var p PolicyListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// S45 T7 (PM9): the answer moved VERBATIM to policy_reads.go's
	// PolicyListAnswer, shared with the records-catalog tool so the two
	// doors cannot drift into two implementations. Wire behaviour and the
	// refusal classes are exactly what they were; policy_test.go and the
	// conformance fixtures are the pins that say so.
	res, err := PolicyListAnswer(ctx, store, p, now())
	if err != nil {
		return nil, err
	}
	return res, nil
}

// --- policy.why --------------------------------------------------------------

// PolicyWhyParams is the question policy.why answers: who is asking under
// which identities, what they want to do, and as of when. `type` is the one
// required member — an explanation with no question is not an answer — and
// at is the RFC3339 instant the answer is dated (default now), the flag
// that makes the expiry boundary explorable without a daemon that waits.
type PolicyWhyParams struct {
	Type      string `json:"type"`
	Agent     string `json:"agent,omitempty"`
	Loop      string `json:"loop,omitempty"`
	LaneType  string `json:"laneType,omitempty"`
	MCPServer string `json:"mcp,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Command   string `json:"command,omitempty"`
	Route     string `json:"route,omitempty"`
	Effect    string `json:"effect,omitempty"`
	Cost      string `json:"cost,omitempty"`
	Skill     string `json:"skill,omitempty"`
	Trigger   string `json:"trigger,omitempty"`
	At        string `json:"at,omitempty"`
}

// PolicyWhyResult is the explanation: the boolean and the sentence, the
// sentence VERBATIM from Verdict.Sentence() (D9 — whole, never re-derived).
// Unimplemented is the field the app branches on (SEC-033): "nobody can
// answer this yet" (D10) is a third answer, and a client that reaches for
// strings.HasPrefix(sentence, "refused:") has just collapsed it into the
// denial it is not.
type PolicyWhyResult struct {
	Allowed       bool   `json:"allowed"`
	Sentence      string `json:"sentence"`
	Unimplemented bool   `json:"unimplemented,omitempty"`
	// Asked, DefaultDeny, By, ByTarget and Unmatchable are S45 T8's
	// additive half of the SEC-033 rule carried onto the CLI's second door:
	// the four verdict shapes (T11's "FOUR shapes, not three") and the two
	// operator notes this answer exists to let a door print travel as
	// FIELDS, so a door composes them from data — never by re-running the
	// engine or parsing prose. By/ByTarget name the TOOL-access rule that
	// refuses while the COMMAND question allows (the SEC-050 composition
	// pair, filled by PolicyWhyAnswer only when the whole documented pair
	// holds); Unmatchable says the question rode identity dimensions no
	// runtime decision carries (DEF-038). Absence (like every field here)
	// is the additive direction T3 permits, never a gap in an answer.
	Asked       bool   `json:"asked,omitempty"`
	DefaultDeny bool   `json:"defaultDeny,omitempty"`
	By          string `json:"by,omitempty"`
	ByTarget    string `json:"byTarget,omitempty"`
	Unmatchable bool   `json:"unmatchable,omitempty"`
}

func (t *Table) handlePolicyWhy(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	store, err := t.requirePolicyStore()
	if err != nil {
		return nil, err
	}
	var p PolicyWhyParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// S45 T7 (PM9): the whole answer — the clock-as-parameter rule (D5),
	// the closed-set and identifier delegations (SEC-046), List → Evaluate
	// → Sentence — moved VERBATIM to PolicyWhyAnswer and shared with the
	// records tool. `now()` stays the handler's, as it was.
	res, err := PolicyWhyAnswer(ctx, store, p, now(), config.PolicyUnmatchedAsks(ctx, t.Settings))
	if err != nil {
		return nil, err
	}
	return res, nil
}

// --- policy.inForce ----------------------------------------------------------

// PolicyInForceParams names ONE party: agent or loop, never both, never
// neither — "in force for whom" is the question and one answer is one party.
// That shape rule is the params' own hygiene; whether the NAME is a legal
// identifier is the boundary's judgement, reached by delegating to its gate.
type PolicyInForceParams struct {
	Agent string `json:"agent,omitempty"`
	Loop  string `json:"loop,omitempty"`
}

// PolicyInForceRow is one policy's answer for the target. Render is
// InForceAnswer.Render() VERBATIM — the three pinned sentences, un-collapsed
// (D14). Collapsing could-not-determine into either in-force line is the
// failure T11 exists to make unrepresentable, so the field carries the state
// name beside the sentence for the app to branch on without parsing prose
// (the same SEC-033 rule as policy.why's unimplemented flag).
type PolicyInForceRow struct {
	PolicyID string `json:"policyID"`
	State    string `json:"state"`
	Render   string `json:"render"`
	// Type is the row's policy type (S45 T8 additive): the CLI's `in-force`
	// column prints the type beside the id and the rendered sentence, and
	// the socket door must answer with the same bytes the direct door
	// prints (PM4). Omission is the additive direction (an older daemon, or
	// a before/after view whose door did not fill it), never a gap the
	// reader should read as "untyped".
	Type string `json:"type,omitempty"`
}

type PolicyInForceResult struct {
	Answers []PolicyInForceRow `json:"answers"`
}

func (t *Table) handlePolicyInForce(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	store, err := t.requirePolicyStore()
	if err != nil {
		return nil, err
	}
	var p PolicyInForceParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// S45 T7 (PM9): the whole answer moved VERBATIM to PolicyInForceAnswer
	// (policy_reads.go), shared with the records tool — same one party,
	// same three un-collapsed states, same Render() sentences (D14).
	res, err := PolicyInForceAnswer(ctx, store, p, now())
	if err != nil {
		return nil, err
	}
	return res, nil
}

// --- policy.events -----------------------------------------------------------

// PolicyEventsParams filters the audit window: whose rows ("" is every
// subject — how the log gets browsed) and how many. The filter is the whole
// question the answer answers: the depth/cap/oldest triple is read for THIS
// subject, so a filtered answer's footer is about the rows it shows (F-13).
type PolicyEventsParams struct {
	Subject string `json:"subject,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// PolicyEventsRow is one audit row as stored — values byte for byte, the
// way decodeEvent returned them (a read side that "fixes" a row is a record
// reader inventing what the row meant).
type PolicyEventsRow struct {
	// ID is the row's own stored id (S45 T8 additive): the CLI's `events`
	// line leads with it, so the socket door and the direct door must be
	// able to print the identical line — an answer that cannot say WHICH
	// row it listed is the gap T8's "if the socket cannot return something
	// the CLI printed, it is a defect in the daemon" rule names.
	ID       int64  `json:"id,omitempty"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	PolicyID string `json:"policyID"`
	Detail   string `json:"detail"`
	At       string `json:"at"`
}

// PolicyEventsResult carries the rows WITH the visibility triple (D15): a
// silently-trimming log is one that loses the row somebody came looking for
// and never says so, so the answer states its depth, the cap that trims it,
// and the oldest instant still retained. The depth and oldest are the
// LISTING's — read for the subject the caller asked about (F-13), so a
// `subject` nobody ever refused answers depth 0 instead of borrowing the
// whole table's number. Oldest is null exactly when the answer is EMPTY —
// the whole table when no subject was given, that subject's slice when one
// was — a stated fact, distinguishable from failure, which is a refusal.
type PolicyEventsResult struct {
	Rows    []PolicyEventsRow `json:"rows"`
	Depth   int               `json:"depth"`
	CapDays int               `json:"capDays"`
	Oldest  *string           `json:"oldest"`
}

func (t *Table) handlePolicyEvents(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	store, err := t.requirePolicyStore()
	if err != nil {
		return nil, err
	}
	var p PolicyEventsParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	limit := p.Limit
	if limit <= 0 {
		limit = defaultPolicyEventsLimit
	}

	// The subject arrives unvalidated from the socket and ReportEvents runs
	// it through parseSubject — the SAME alphabet parse the write side used
	// to record it (SEC-046 holds by delegation: one parse, no copy).
	report, rows, rej := store.ReportEvents(ctx, p.Subject, limit)
	if rej != nil {
		return nil, policyRefusal(rej)
	}

	out := make([]PolicyEventsRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, PolicyEventsRow{
			ID:       r.ID,
			Kind:     r.Outcome,
			Subject:  r.SubjectKind + ":" + r.SubjectID,
			PolicyID: r.PolicyID,
			Detail:   r.Detail,
			At:       r.At,
		})
	}
	res := PolicyEventsResult{
		Rows:  out,
		Depth: report.Depth,
		// Derived from the const every surface quotes (D15): a literal 90
		// here is a number that can drift from what TrimEvents enforces.
		CapDays: int(policy.PolicyEventAgeCap / (24 * time.Hour)),
	}
	if !report.Oldest.IsZero() {
		oldest := report.Oldest.Format(time.RFC3339)
		res.Oldest = &oldest
	}
	return res, nil
}
