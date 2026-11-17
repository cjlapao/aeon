// Package mergerule is the pure half of S57's merge gate: the merge rule's
// record type (VG10), the evaluation (VG11) — one pure function, no I/O —
// and the autonomy floor (VG12).
//
// # Why pure
//
// The decision "may this PR merge, and on what evidence" is the whole
// argument of the slice, and a decision that reads the clock, the store or
// the network cannot be pinned by a test and cannot be reasoned about.
// `Evaluate` takes everything it may look at as arguments and returns a
// discriminated verdict. The store (which imports this package, never the
// reverse — TestMergeRulePackageIsPure pins the direction) records rules,
// DoD items and merge events; the action package gathers facts from the
// forge (T6); T6 calls this function. Nothing here touches disk, network
// or time.
//
// # The baseline is the contract
//
// `Baseline()` returns exactly today's mergeGate behaviour (VG11): forge
// review, every reported check green, a missing checks API passes as
// not-configured, unresolved threads refuse, squash. The T1 gate goldens
// (internal/action/testdata/gate_golden) are the byte-level contract, and
// `TestBaselineEvaluateEqualsTodaysGate` drives every golden fact set
// through `Evaluate(Baseline(), f)` and compares the refusal sentence and
// the evidence map byte for byte. A rule row with all database defaults
// IS the baseline (the migration's defaults are the values below), and a
// test proves `Evaluate(defaultRow, f) == Evaluate(Baseline(), f)` over a
// fact grid.

package mergerule

import (
	"github.com/cjlapao/aeon/internal/forge"
)

// --- the rule (VG10) --------------------------------------------------------

// Rule is one row of merge_rule, decoded. The behavioural fields are the
// ones Evaluate reads; ComponentID, Revision and Source are identity and
// provenance (the read side stamps Source own|root|baseline).
type Rule struct {
	// ComponentID is the node the row belongs to (empty for Baseline()).
	ComponentID string

	// Enabled is the row's switch: a disabled row is evaluated as the
	// baseline (the rule's fields simply do not apply — turning a rule
	// off returns the tree to today's gate, which is what "off" means).
	Enabled bool

	// RequireLocal demands a PASS verify run on the PR head SHA (VG10):
	// a run for another head SHA is stale evidence and does not count.
	RequireLocal bool

	// RequireDoD is 'none' | 'all_met'.
	RequireDoD string

	// RequiredChecks names the checks the rule cares about (the forge's
	// check names, verbatim). Both policies require each named check to
	// be reported and green; reported_green ADDS "every reported check
	// must be green".
	RequiredChecks []string

	// ChecksPolicy is 'reported_green' (every reported check must be
	// success — today's behaviour) | 'required_only' (only the named
	// ones; an unrelated red is the forge's business, not the gate's).
	ChecksPolicy string

	// NoChecks is 'allow' | 'refuse': what a merge with ZERO reported
	// checks is. 'allow' is the baseline (absence of evidence is not
	// evidence of red — L34); 'refuse' says the gate needs a signal.
	NoChecks string

	// ReviewMode is 'forge' (a forge approval — the baseline) |
	// 'aeon' (the loop's review step finished ok for this head SHA) |
	// 'either' | 'both'. Aeon's own review counts only if the rule says
	// so (AG4); the baseline is forge.
	ReviewMode string

	// AeonReviewAgents names the agents whose review-step verdict
	// satisfies the aeon half. The default list is the one the loop
	// seeds (code-reviewer); the field is declarative in v1.
	AeonReviewAgents []string

	// RequireThreadsResolved demands the forge report review threads AND
	// report them all resolved. In forge review mode the baseline
	// already honours unresolved threads (today's gate); this flag adds
	// the refusal when the forge CANNOT report them — unverifiable is
	// not green right before something irreversible (VG8's note).
	RequireThreadsResolved bool

	// Method is 'squash' | 'merge' | 'rebase' — the merge kind T6 asks
	// the forge for when a rule row (any row) applies. Neutral for the
	// evaluation.
	Method string

	// Revision is the row's ledger number (the event's MAX, never
	// resetting). Zero for the baseline (the baseline has no row).
	Revision int

	// Source is 'own' | 'root' | 'baseline' — the read side's answer to
	// "where did this rule come from". Baseline() is 'baseline'.
	Source string

	// UpdatedAt is the row's clock reading (ISO, the store's Now).
	// Storage bookkeeping: the evaluator never reads it, and it is
	// identity for Tightens (a change of the stamp is not a change of
	// the authority). Empty for the baseline (the baseline has no row).
	UpdatedAt string
}

// The closed vocabularies, named so a typo is a refusal with the value in
// it, not a constraint violation naming a column (the store mirrors
// each of these in the migration's CHECKs — the backstop the store is
// bypassed).
const (
	RequireDoDNone   = "none"
	RequireDoDAllMet = "all_met"
	ChecksReported   = "reported_green"
	ChecksRequired   = "required_only"
	NoChecksAllow    = "allow"
	NoChecksRefuse   = "refuse"
	ReviewModeForge  = "forge"
	ReviewModeAeon   = "aeon"
	ReviewModeEither = "either"
	ReviewModeBoth   = "both"
	MethodSquash     = "squash"
	MethodMerge      = "merge"
	MethodRebase     = "rebase"
)

// Baseline is exactly today's mergeGate behaviour (VG11): forge review,
// reported_green, no_checks=allow, no local/DoD/threads requirement,
// squash. Its values are the migration's DEFAULTS — a row with all
// defaults is the baseline wearing a row.
func Baseline() Rule {
	return Rule{
		Enabled:                true,
		RequireLocal:           false,
		RequireDoD:             RequireDoDNone,
		RequiredChecks:         nil,
		ChecksPolicy:           ChecksReported,
		NoChecks:               NoChecksAllow,
		ReviewMode:             ReviewModeForge,
		AeonReviewAgents:       []string{"code-reviewer"},
		RequireThreadsResolved: false,
		Method:                 MethodSquash,
		Revision:               0,
		Source:                 "baseline",
	}
}

// IsBaseline reports whether every behavioural field equals Baseline()'s
// (identity and provenance — ComponentID, Revision, Source — do not
// count). Evaluate routes a baseline-shaped rule through the baseline
// path, so a default row and no row are indistinguishable, which is the
// property the test proves.
func (r Rule) IsBaseline() bool {
	b := Baseline()
	return r.Enabled == b.Enabled &&
		r.RequireLocal == b.RequireLocal &&
		r.RequireDoD == b.RequireDoD &&
		sameStringSet(r.RequiredChecks, b.RequiredChecks) &&
		r.ChecksPolicy == b.ChecksPolicy &&
		r.NoChecks == b.NoChecks &&
		r.ReviewMode == b.ReviewMode &&
		sameStringSet(r.AeonReviewAgents, b.AeonReviewAgents) &&
		r.RequireThreadsResolved == b.RequireThreadsResolved &&
		r.Method == b.Method
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- the facts the evaluation may look at (VG11) ----------------------------

// ThreadFacts is the forge's thread report, in the three honest states:
// the server has no thread API (Available false), the server claimed one
// and refused/not-found the query (Readable false), or the read completed
// and Unresolved is its answer (possibly empty — "0 unresolved" is a
// report, not an absence).
type ThreadFacts struct {
	Available  bool
	Readable   bool
	Unresolved []forge.Thread
}

// Run is the verify-run view the evaluator needs (VG10): the run's own
// head SHA and state. T6 maps the store's VerifyRun onto it; the store's
// type never crosses into this package (no store import).
type Run struct {
	ID      string
	HeadSHA string
	// State is the run's aggregate: 'pass' | 'fail' | 'unverified'
	// (the store's closed set; 'skipped' is admitted for the runner's
	// states and reads as not-pass).
	State string
	// Reason is the run's one-line account (e.g. "unverified:
	// no-gates-configured") — the evidence, not a decision input.
	Reason string
}

// EffectiveGate is one gate of the effective set, as the evaluator and
// the floor need it: which gates exist, and which are REQUIRED. The
// store's type is wider (it carries the command); T6 maps the subset.
type EffectiveGate struct {
	ID          string
	Name        string
	Requirement string // 'required' | 'advisory'
	Enabled     bool
}

// EffectiveGates is the answer to "what runs here, and from where" (the
// VG3 effective set, in its pure shape). An empty set is honest: no gate
// declared anywhere up the tree.
type EffectiveGates struct {
	ComponentID string
	Source      string // 'own' | 'root' | ''
	SourceID    string
	Gates       []EffectiveGate
}

// RequiredCounts the enabled required gates (the floor's "at least one
// required gate" ingredient, VG12).
func (g EffectiveGates) RequiredCount() int {
	n := 0
	for _, e := range g.Gates {
		if e.Enabled && e.Requirement == "required" {
			n++
		}
	}
	return n
}

// Item is one DoD row as the evaluator reads it (VG13): its state and the
// evidence that state names. Text is the criterion's own words — the
// byte-equality the carry-over rule is about.
type Item struct {
	ID           string
	State        string // 'unchecked' | 'met' | 'unmet' | 'unverifiable'
	EvidenceKind string // '' | 'verify_run' | 'manual'
	EvidenceRef  string
	Text         string
}

// Facts is everything Evaluate may look at. The gate's decision is a
// function of these and the rule — nothing else (VG11).
//
// PR names the PR number in the baseline sentences (the goldens carry
// "PR #3" verbatim; the spec's Facts sketch omitted it, the goldens do
// not). HeadSHA is the PR head at merge time — the SHA every piece of
// evidence is measured against.
type Facts struct {
	PR int

	// Reviews is the forge's review list in server order (the latest is
	// the LAST element — today's gate's convention).
	Reviews []forge.Review

	// Checks is the forge's checks report for the head.
	//
	// ChecksReadable is the read's three states collapsed to the two the
	// evaluation needs: TRUE means the server claimed the capability and
	// the read completed — even an EMPTY report is a completed read
	// ("no checks reported" — the golden's not-found sentence); FALSE
	// means there was no read at all (the server reports no status API —
	// the threadless server's sentence). A transport failure never
	// reaches the evaluator: T6 refuses it while gathering, in the
	// action package, where the actual error is (the golden pins that
	// sentence there).
	Checks         forge.ChecksReport
	ChecksReadable bool

	// Threads is the forge's thread report (see ThreadFacts).
	Threads ThreadFacts

	// Gates is the component's effective verify-gate set (VG3) and
	// LatestRun the run require_local measures. A nil LatestRun is "no
	// run exists" — an honest absence, not an error.
	Gates     EffectiveGates
	LatestRun *Run

	// DoD is the loop's definition-of-done items (all briefs), and
	// AeonReview the engine's review token — "ok@<sha>" written by the
	// engine when the review step completes ok for a head SHA, never
	// model-supplied (VG9). Empty is "absent".
	DoD        []Item
	AeonReview string

	// HeadSHA is the PR head at merge time.
	HeadSHA string
}

// --- the verdict (VG11) ------------------------------------------------------

// Refusal is one reason the merge is refused: a code from the closed set
// below (the machine half) and the sentence (the human half — the
// surfaces print it verbatim).
type Refusal struct {
	Code     string
	Sentence string
}

// Verdict is the evaluation's answer: allowed, or the list of refusals
// (the baseline path produces at most one — today's gate stops at its
// first refusal; the extended path collects them all, in order).
//
// Evidence is the gate's account of what it saw — the baseline carries
// exactly today's five keys (review_state, review_by, review_at,
// checks, threads — the goldens pin the values byte for byte); a
// non-baseline rule ADDS rule_revision, verify_run_id, verify_head_sha,
// dod, checks_state and aeon_review (VG11).
type Verdict struct {
	Allowed  bool
	Refusals []Refusal
	Evidence map[string]string
}

// The closed refusal codes (PL3's shape: a value nobody compiled is a
// refusal nobody granted). One per branch of the evaluation.
const (
	CodeNoReview       = "no-review"
	CodeNotApproved    = "latest-not-approve"
	CodeOutstandingRC  = "outstanding-request-changes"
	CodeCheckNotGreen  = "check-not-green"
	CodeCheckMissing   = "check-missing"
	CodeCheckPending   = "check-pending"
	CodeCheckUnknown   = "check-unknown"
	CodeNoChecks       = "no-checks"
	CodeThreadsOpen    = "threads-unresolved"
	CodeThreadsMissing = "threads-unreportable"
	CodeLocalNotGreen  = "local-not-green"
	CodeEvidenceStale  = "evidence-stale"
	CodeDoDNotMet      = "dod-not-met"
	CodeDoDNone        = "dod-none"
	CodeAeonReviewMiss = "aeon-review-missing"
)

// AllRefusalCodes is the closed set, for the guards that count it.
func AllRefusalCodes() []string {
	return []string{
		CodeNoReview, CodeNotApproved, CodeOutstandingRC,
		CodeCheckNotGreen, CodeCheckMissing, CodeCheckPending, CodeCheckUnknown, CodeNoChecks,
		CodeThreadsOpen, CodeThreadsMissing,
		CodeLocalNotGreen, CodeEvidenceStale,
		CodeDoDNotMet, CodeDoDNone,
		CodeAeonReviewMiss,
	}
}

// Missing is one ingredient the autonomy floor found absent (VG12): the
// code (the machine half) and the sentence naming the field (the human
// half).
type Missing struct {
	Code     string
	Sentence string
}

// The closed floor codes.
const (
	FloorDisabled         = "disabled"
	FloorLocalNotRequired = "local-not-required"
	FloorNoRequiredGates  = "no-required-gates"
	FloorDoDNotRequired   = "dod-not-required"
	FloorChecksNotBound   = "checks-not-binding"
	FloorForgeOnly        = "forge-review-only"
)

// AllFloorCodes is the closed set, for the guards that count it.
func AllFloorCodes() []string {
	return []string{
		FloorDisabled, FloorLocalNotRequired, FloorNoRequiredGates,
		FloorDoDNotRequired, FloorChecksNotBound, FloorForgeOnly,
	}
}
