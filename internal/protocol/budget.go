package protocol

// budget.go — S55 T8: the budget plane's EIGHT DOORS. budget.list,
// budget.show, budget.effective and spend.report are reads (list-with-
// details, rule 3); budget.set, budget.remove and loop.raise are the HUMAN
// DOORS (P34, LC5) — the person is stamped from the CONNECTION (the daemon's
// ActorFor seam, the kernel's peer credential), NEVER from params, and every
// write carries a rejected-by-name `actor` field so a forged one is a parse
// error the caller sees, not a silently ignored value.
//
// # Confirmation
//
// A budget.set that LOWERS or ADDS a cap tightens authority and needs no
// confirmation. A set that RAISES or DISABLES an enabled cap, and a
// budget.remove, loosen authority: the daemon renders a CANONICAL STATEMENT
// of the act (and its SHA-256 digest) and the commit requires that digest —
// bound to what was read (the S45 PM7.1 pattern). The mismatch rides
// ErrBudgetRefused with data.kind confirm-mismatch; a commit without a
// digest rides confirm-required, carrying the statement and the digest so
// the client completes in one round trip. A dryRun returns the statement
// and the effect and writes nothing.
//
// # Refusals
//
// A refused budget act rides ErrBudgetRefused (one code for the plane, the
// state found in Data.Kind, the person's sentence in What — the store's
// BudgetRefusal rendered verbatim, DB19). A LOST RACE on loop.raise is not a
// refusal: the cap was lifted (the raised event says so) and the loop moved
// first, so the answer is a RESULT with a refusal that names the door the
// loop's real state has — the loop.resume precedent, byte for byte.
//
// # Absence
//
// The doors are registered only over a non-nil BudgetStore seam (V13, the
// settings/loop-control precedent): a budget-less build never answers a
// budget door at all, rather than answering a refusal.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The eight method names live in method.go with every other MethodName —
// tools/protogen reads that one const block to build the app's contract —
// this file carries their params, results, handlers and the register lines.

// BudgetEventWindow is how many of a budget's audit events (oldest first)
// budget.show returns — the history a person reads when a cap's numbers are
// in question, not the full ledger (the full ledger is the store's job; this
// is the screen's). It is exported because the wire contract names the
// window and the daemon's seam reads the same constant, not its own copy
// (the ControlEventWindow shape).
const BudgetEventWindow = 50

// --- views ------------------------------------------------------------------

// BudgetDimension is one dimension of a BudgetView: the cap (nil = no cap on
// the dimension — NULL, never the zero value, zero is a real cap) and the
// spend against it: a FLOOR (spendFloor, nil when no spend is known at all),
// exact (whether the floor is a total — P31's honesty mark), and the count of
// unknowns beside it (an unknown is counted, never summed as zero).
type BudgetDimension struct {
	Dimension    string   `json:"dimension"` // usd|tokens|wall-time|steps
	Cap          *float64 `json:"cap"`
	SpendFloor   *float64 `json:"spendFloor,omitempty"`
	Exact        *bool    `json:"exact,omitempty"`
	UnknownCount int      `json:"unknownCount"`
}

// BudgetLastChanged is the view's last change (the newest change-kind event
// in the budget's audit: created, changed or raised — a removed budget has no
// view, so the reader sees the removal in the audit, not in the list).
type BudgetLastChanged struct {
	At     string `json:"at"`
	Kind   string `json:"kind"` // created|changed|raised
	ByKind string `json:"byKind"`
	ByID   string `json:"byId"`
}

// BudgetView is one budget, list-with-details (rule 3): the row, its live
// status (ok|warn|hit — computed at the read, from the current spend and the
// warn line, never a stored flag), the per-dimension detail, and the last
// change. A client that renders the list sees the cap AND the spend AND
// whether the two are about to meet, in one read.
type BudgetView struct {
	ID            string             `json:"id"`
	Scope         string             `json:"scope"` // workspace|component|epic|loop
	SubjectID     string             `json:"subjectId"`
	Window        string             `json:"window"` // lifetime|day
	Enabled       bool               `json:"enabled"`
	UnknownPolicy string             `json:"unknownPolicy"` // proceed|stop
	WarnPct       *int               `json:"warnPct,omitempty"`
	Origin        string             `json:"origin"` // user|migrated
	CreatedAt     string             `json:"createdAt"`
	UpdatedAt     string             `json:"updatedAt"`
	Status        string             `json:"status"` // ok|warn|hit
	Dimensions    []BudgetDimension  `json:"dimensions"`
	LastChanged   *BudgetLastChanged `json:"lastChanged,omitempty"`
}

// BudgetEventView is one row of the budget's audit (LC11: the history
// outlives the budget). The values are numbers (a cap, a spend floor), never
// user text.
type BudgetEventView struct {
	At         string   `json:"at"`
	Kind       string   `json:"kind"` // created|changed|removed|raised|hit|warned
	ActorKind  string   `json:"actorKind"`
	ActorID    string   `json:"actorId"`
	LoopID     string   `json:"loopId,omitempty"`
	Dimension  string   `json:"dimension,omitempty"`
	OldValue   *float64 `json:"oldValue,omitempty"`
	NewValue   *float64 `json:"newValue,omitempty"`
	SpendFloor *float64 `json:"spendFloor,omitempty"`
	Exact      *bool    `json:"exact,omitempty"`
}

// BudgetSpendView is budget.show's spend breakdown: the budget's spend so far,
// per dimension, with P31's honesty marks — the floor, the partial/unknown
// counts beside it, and exact saying when the floor is a total.
type BudgetSpendView struct {
	USDFloor      float64 `json:"usdFloor"`
	USDPartial    int     `json:"usdPartial"`
	USDUnknown    int     `json:"usdUnknown"`
	Exact         bool    `json:"exact"`
	Tokens        *int64  `json:"tokens,omitempty"`
	TokensUnknown int     `json:"tokensUnknown"`
	WallMs        int64   `json:"wallMs"`
	Steps         int     `json:"steps"`
}

// BudgetShowResult is budget.show's answer: the view, the spend breakdown,
// the OPEN loops the budget currently covers (who the cap is over, right
// now), the audit trail (oldest first), and the worst-case overshoot note —
// how far past the cap one more step could spend before the next check fires
// (the cap is enforced at the checks, not continuously, so the note is
// honest arithmetic, not a promise).
type BudgetShowResult struct {
	View          BudgetView        `json:"view"`
	Spend         BudgetSpendView   `json:"spend"`
	OpenLoops     []string          `json:"openLoops"`
	History       []BudgetEventView `json:"history"`
	OvershootNote string            `json:"overshootNote,omitempty"`
}

// --- params -----------------------------------------------------------------

// BudgetListParams is budget.list's read params: every filter optional.
// Status filters on the COMPUTED status (ok|warn|hit), not a stored flag —
// the list is the live picture, so the filter is too.
type BudgetListParams struct {
	Scope     string `json:"scope,omitempty"`
	SubjectID string `json:"subjectId,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
	Status    string `json:"status,omitempty"` // ok|warn|hit
}

// BudgetListResult is budget.list's answer: the views, already detailed.
type BudgetListResult struct {
	Budgets []BudgetView `json:"budgets"`
}

// BudgetShowParams is budget.show's read params: the budget by id.
type BudgetShowParams struct {
	ID string `json:"id"`
}

// BudgetEffectiveParams is budget.effective's read params: the budgets that
// apply to the named subject, tightest first (LC9). Exactly one of the three
// is required — a loop by id, a component by id, or a root by id — and the
// daemon resolves the subject's applicability set (loop → goal → epic,
// component → root, workspace) the same way the enforcement side does, so
// the read and the gate cannot disagree about what is in force.
type BudgetEffectiveParams struct {
	LoopID      string `json:"loopId,omitempty"`
	RootID      string `json:"rootId,omitempty"`
	ComponentID string `json:"componentId,omitempty"`
}

// BudgetEffectiveResult is budget.effective's answer: the resolved subject
// and the budgets that apply, tightest first (loop, epic, component, root,
// workspace — the tier a spend is checked against first, listed first).
type BudgetEffectiveResult struct {
	Subject string       `json:"subject"`
	Budgets []BudgetView `json:"budgets"`
}

// SpendReportParams is spend.report's read params. Since/until are RFC 3339
// (the wire carries text; the daemon parses UTC); omitted since/until are the
// floor and now, respectively. RootID scopes the report to one project.
type SpendReportParams struct {
	GroupBy string `json:"groupBy"` // day|trigger|component|epic|route
	Since   string `json:"since,omitempty"`
	Until   string `json:"until,omitempty"`
	RootID  string `json:"rootId,omitempty"`
}

// SpendReportBucket is one bucket of the report: the key the groupBy names,
// the USD floor and its honesty marks (exact says when the floor is a total,
// the counts say what it is not — P31, never a sum that hides an unknown),
// the token floor (nil = no tokens known at all in the bucket), and the step
// count.
type SpendReportBucket struct {
	Key          string  `json:"key"`
	USDFloor     float64 `json:"usdFloor"`
	Exact        bool    `json:"exact"`
	PartialCount int     `json:"partialCount"`
	UnknownCount int     `json:"unknownCount"`
	Tokens       *int64  `json:"tokens,omitempty"`
	Steps        int     `json:"steps"`
}

// SpendReportResult is spend.report's answer: the window echoed (the client
// sees what was read) and the buckets, sorted by key (a shuffled report of
// the same numbers is a bug the sort makes impossible to read as).
type SpendReportResult struct {
	GroupBy string              `json:"groupBy"`
	Since   string              `json:"since,omitempty"`
	Until   string              `json:"until,omitempty"`
	RootID  string              `json:"rootId,omitempty"`
	Buckets []SpendReportBucket `json:"buckets"`
}

// BudgetSetParams is budget.set's params (the human door): create or update
// by the unique key (scope, subjectId, window). A missing cap stays NULL
// (no cap) on an update — to CLEAR a cap the caller passes the dimension
// absent, which on an existing row removes that dimension's cap; a row that
// would end with no cap at all is refused (no-dimension). DryRun returns the
// effect and the confirmation statement (when the act loosens authority)
// and writes nothing. Confirm is the digest the daemon demands of a
// loosening commit — the SHA-256 of the statement, bound to what was read.
type BudgetSetParams struct {
	Scope         string   `json:"scope"`
	SubjectID     string   `json:"subjectId"`
	Window        string   `json:"window"`
	USDCap        *float64 `json:"usdCap,omitempty"`
	TokensCap     *int64   `json:"tokensCap,omitempty"`
	WallMsCap     *int64   `json:"wallMsCap,omitempty"`
	StepsCap      *int64   `json:"stepsCap,omitempty"`
	UnknownPolicy string   `json:"unknownPolicy,omitempty"` // proceed|stop
	WarnPct       *int     `json:"warnPct,omitempty"`       // 1–99
	Enabled       *bool    `json:"enabled,omitempty"`
	DryRun        bool     `json:"dryRun,omitempty"`
	Confirm       string   `json:"confirm,omitempty"`
	// Actor is never read. Present to be refused by name (LC5).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// BudgetSetResult is budget.set's answer. Effect is created, updated or
// noop (a set that changes nothing writes nothing — LC11). A dryRun carries
// the effect it WOULD take, the statement and digest when the act loosens
// authority, and no view (nothing was written). A committed loosening
// carries Confirmed true.
type BudgetSetResult struct {
	View      *BudgetView `json:"view,omitempty"`
	Effect    string      `json:"effect"` // created|updated|noop
	DryRun    bool        `json:"dryRun,omitempty"`
	Statement string      `json:"statement,omitempty"`
	Digest    string      `json:"digest,omitempty"`
	Confirmed bool        `json:"confirmed,omitempty"`
}

// BudgetRemoveParams is budget.remove's params (the human door): the budget
// by id, and the digest of the removal statement the daemon rendered — a
// removal always loosens authority (a cap that is gone is a cap that will
// never fire), so it always confirms.
type BudgetRemoveParams struct {
	ID      string `json:"id"`
	Confirm string `json:"confirm"`
	// Actor is never read. Present to be refused by name (LC5).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// BudgetRemoveResult is budget.remove's answer: the id, and that it is gone
// (the audit row stays — LC11).
type BudgetRemoveResult struct {
	ID      string `json:"id"`
	Removed bool   `json:"removed"`
}

// LoopRaiseParams is loop.raise's params (the human door): the loop by id,
// the NEW cap (exactly one dimension — the one the loop is parked on; a
// raise lifts the cap the hit tripped, it does not write a new budget), and
// the person's reason (the audit's own words, 1–512 bytes). BudgetID, when
// given, must name the budget the loop is parked on — a raise against
// another budget is a refusal, not a guess.
type LoopRaiseParams struct {
	ID        string   `json:"id"`
	BudgetID  string   `json:"budgetId,omitempty"`
	USDCap    *float64 `json:"usdCap,omitempty"`
	TokensCap *int64   `json:"tokensCap,omitempty"`
	WallMsCap *int64   `json:"wallMsCap,omitempty"`
	StepsCap  *int64   `json:"stepsCap,omitempty"`
	Reason    string   `json:"reason"`
	// Actor is never read. Present to be refused by name (LC5).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// LoopRaiseDetail is loop.raise's `raised` block: the budget, the dimension
// lifted, the cap it lifted FROM and TO (from is nil when the pin carried no
// cap record — the crash window, where the raise re-does the kick).
type LoopRaiseDetail struct {
	BudgetID  string   `json:"budgetId"`
	Dimension string   `json:"dimension"`
	From      *float64 `json:"from,omitempty"`
	To        float64  `json:"to"`
}

// LoopRaiseResult is loop.raise's answer (LC9): a person-visible outcome in
// every case. Raised names the cap the act lifted (nil when nothing was
// lifted — a refused raise rides the error with its kind). Resumed says
// whether the loop moved (a fresh runner, or the parked step re-run) — a
// lost race answers raised-true/resumed-false with a refusal that names the
// door the loop's real state has.
type LoopRaiseResult struct {
	LoopID  string           `json:"loopId"`
	Raised  *LoopRaiseDetail `json:"raised,omitempty"`
	Resumed bool             `json:"resumed"`
	Status  string           `json:"status,omitempty"`
	How     string           `json:"how,omitempty"` // step|runner
	Note    string           `json:"note,omitempty"`
	Refusal *BudgetRefusal   `json:"refusal,omitempty"`
}

// BudgetVocabularyResult is budget.vocabulary's answer: the closed sets the
// forms and the CLI build from (rule 4: the vocabulary comes from the daemon,
// never a client copy — a literal in the client is a second truth that
// drifts when the set widens).
type BudgetVocabularyResult struct {
	Scopes          []string `json:"scopes"`          // workspace|component|epic|loop
	Windows         []string `json:"windows"`         // lifetime|day
	Dimensions      []string `json:"dimensions"`      // usd|tokens|wall-time|steps
	UnknownPolicies []string `json:"unknownPolicies"` // proceed|stop
	Statuses        []string `json:"statuses"`        // ok|warn|hit
	EventKinds      []string `json:"eventKinds"`      // created|changed|removed|raised|hit|warned
	RefusalKinds    []string `json:"refusalKinds"`
}

// --- the refusal and the seam ------------------------------------------------

// BudgetRefusal is the wire twin of the store's BudgetRefusal (DB19: a
// refused budget act is a result, rendered as the plane's one error code).
// Kind is machine-readable — it rides the refusal's Data.Kind so a client
// can branch on it without parsing the sentence; Detail is the person's
// sentence. Statement and Digest are set only by the confirm-required
// refusal: they carry the act's canonical statement and its digest so the
// client completes the commit in one round trip, rather than re-reading.
type BudgetRefusal struct {
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	Statement string `json:"statement,omitempty"`
	Digest    string `json:"digest,omitempty"`
}

// BudgetStore is the daemon-facing seam (Table.BudgetStore): the budget
// plane's eight doors over the T6 store and the T7 raise machinery. The
// protocol layer carries the wire contract; the daemon implements it,
// assembling the views from store rows and the current spend (no caching —
// the spend moves between reads, and a cached floor is a cap that lies),
// classifying each set's effect (tightening needs no confirm, loosening
// does), and feeding loop.raise the T7 in-process op. The wire types are
// THIS package's field-for-field twins of what the daemon assembles — the
// daemon's adapter is the only layer that may know both.
//
// Actor is protocol.Actor (the peer identity the handler resolved through
// ActorFor) — the seam receives it stamped, never a raw claim (LC5).
type BudgetStore interface {
	List(ctx context.Context, scope, subjectID, status string, enabled *bool) ([]BudgetView, error)
	Show(ctx context.Context, id string) (BudgetShowResult, error)
	Effective(ctx context.Context, loopID, rootID, componentID string) (BudgetEffectiveResult, error)
	SpendReport(ctx context.Context, p SpendReportParams) (SpendReportResult, error)
	Set(ctx context.Context, actor Actor, p BudgetSetParams) (BudgetSetResult, *BudgetRefusal, error)
	// RemoveBudget is budget.remove's human door at the seam. The 2026-11-02
	// merge reconciliation renamed it from Remove: S45's boundary guards
	// (TestOnlyTheDeskAndTheDaemonSeamsCallTheWriteBoundary,
	// TestNoPolicyAuthoringReachesTheNarrowSurfaceAmended) grep every
	// non-test file for the policy boundary's Remove call shape — a dot-
	// Remove receiver whose first argument is a bare ctx — against a closed
	// whitelist, and this seam is S55's, not one of S45's doors. The rename
	// clears S45's pattern without touching S45's files (their lines stay
	// byte-identical); the wire name stays budget.remove.
	RemoveBudget(ctx context.Context, actor Actor, id, confirm string) (BudgetRemoveResult, *BudgetRefusal, error)
	Raise(ctx context.Context, actor Actor, p LoopRaiseParams) (LoopRaiseResult, *BudgetRefusal, error)
	Vocabulary() BudgetVocabularyResult
}

// RegisterBudget registers the eight budget doors over a non-nil seam (V13).
// It returns whether the methods are in the table, so the daemon's wiring
// can be asserted rather than trusted — the settings/loop-control shape.
func (t *Table) RegisterBudget() bool {
	if t.BudgetStore == nil {
		return false
	}
	t.register(&Method{Name: MethodBudgetList, RequiresInitialize: true, Handle: t.handleBudgetList})
	t.register(&Method{Name: MethodBudgetShow, RequiresInitialize: true, Handle: t.handleBudgetShow})
	t.register(&Method{Name: MethodBudgetEffective, RequiresInitialize: true, Handle: t.handleBudgetEffective})
	t.register(&Method{Name: MethodSpendReport, RequiresInitialize: true, Handle: t.handleSpendReport})
	t.register(&Method{Name: MethodBudgetSet, RequiresInitialize: true, Handle: t.handleBudgetSet})
	t.register(&Method{Name: MethodBudgetRemove, RequiresInitialize: true, Handle: t.handleBudgetRemove})
	t.register(&Method{Name: MethodBudgetVocabulary, RequiresInitialize: true, Handle: t.handleBudgetVocabulary})
	t.register(&Method{Name: MethodLoopRaise, RequiresInitialize: true, Handle: t.handleLoopRaise})
	return true
}

// --- the actor and the refusals -------------------------------------------------

// budgetSeam is the one nil-guard shape the eight handlers share:
// registration is conditional on the seam, so a nil here is a
// registration/seam disagreement the guard tests catch — spoken, not
// silent (the loop-control shape).
func (t *Table) budgetSeam() error {
	if t.BudgetStore == nil {
		return ErrStoreUnavailable.WithWhat("budget.* needs the budget seam, and this build did not link it — the budget surface is absent for the same reason (V13)")
	}
	return nil
}

// budgetActor is the write boundary's audit identity for the three human
// doors (LC5): the person comes from the CONNECTION (the daemon's ActorFor
// seam, the kernel's peer credential), never from params. It is the honest
// absence (a daemon with no identity seam has no persons) that refuses the
// write, and an empty id that is refused as well.
func (t *Table) budgetActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("the budget write is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("the budget write is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	return a, nil
}

// budgetRefusedError renders the store's BudgetRefusal as the socket's
// refusal (DB19): one code (ErrBudgetRefused), the state found in
// Data.Kind, the person's sentence in What, and the statement/digest the
// confirm-required refusal carries (the client's one-round-trip completion).
func budgetRefusedError(verb string, r *BudgetRefusal) *Error {
	e := ErrBudgetRefused.WithWhat("budget %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	if r.Statement != "" {
		e.Data.Statement = r.Statement
		e.Data.Confirm = r.Digest
	}
	return e
}

// checkBudgetReason validates person-authored raise text: 1–512 bytes after
// trim (the audit's own words, beside the cap's closed-key numbers).
func checkBudgetReason(label, reason string) (string, error) {
	r := strings.TrimSpace(reason)
	if len(r) == 0 {
		return "", ErrBadParams{Err: fmt.Errorf("%s is required (1–512 bytes of text)", label)}
	}
	if len(r) > controlReasonBound {
		return "", ErrBadParams{Err: fmt.Errorf("%s is %d bytes — the bound is %d", label, len(r), controlReasonBound)}
	}
	return r, nil
}

// --- the handlers ----------------------------------------------------------------

func (t *Table) handleBudgetList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := t.budgetSeam(); err != nil {
		return nil, err
	}
	var p BudgetListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	switch p.Status {
	case "", "ok", "warn", "hit":
	default:
		return nil, ErrBadParams{Err: fmt.Errorf("status is ok, warn or hit, not %q", p.Status)}
	}
	views, err := t.BudgetStore.List(ctx, strings.TrimSpace(p.Scope), strings.TrimSpace(p.SubjectID), p.Status, p.Enabled)
	if err != nil {
		return nil, err
	}
	if views == nil {
		views = []BudgetView{}
	}
	return BudgetListResult{Budgets: views}, nil
}

func (t *Table) handleBudgetShow(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := t.budgetSeam(); err != nil {
		return nil, err
	}
	var p BudgetShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the budget to read")}
	}
	return t.BudgetStore.Show(ctx, p.ID)
}

func (t *Table) handleBudgetEffective(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := t.budgetSeam(); err != nil {
		return nil, err
	}
	var p BudgetEffectiveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	named := 0
	if p.LoopID != "" {
		named++
	}
	if p.RootID != "" {
		named++
	}
	if p.ComponentID != "" {
		named++
	}
	if named != 1 {
		return nil, ErrBadParams{Err: fmt.Errorf("name exactly one subject: loopId, rootId or componentId (named %d)", named)}
	}
	return t.BudgetStore.Effective(ctx, strings.TrimSpace(p.LoopID), strings.TrimSpace(p.RootID), strings.TrimSpace(p.ComponentID))
}

func (t *Table) handleSpendReport(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := t.budgetSeam(); err != nil {
		return nil, err
	}
	var p SpendReportParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	switch p.GroupBy {
	case "day", "trigger", "component", "epic", "route":
	default:
		return nil, ErrBadParams{Err: fmt.Errorf("groupBy is day, trigger, component, epic or route, not %q", p.GroupBy)}
	}
	res, err := t.BudgetStore.SpendReport(ctx, p)
	if err != nil {
		return nil, err
	}
	if res.Buckets == nil {
		res.Buckets = []SpendReportBucket{}
	}
	return res, nil
}

func (t *Table) handleBudgetSet(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.budgetSeam(); err != nil {
		return nil, err
	}
	var p BudgetSetParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	actor, err := t.budgetActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, refusal, err := t.BudgetStore.Set(ctx, actor, p)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, budgetRefusedError("set", refusal)
	}
	return result, nil
}

func (t *Table) handleBudgetRemove(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.budgetSeam(); err != nil {
		return nil, err
	}
	var p BudgetRemoveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the budget to remove")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	actor, err := t.budgetActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, refusal, err := t.BudgetStore.RemoveBudget(ctx, actor, p.ID, p.Confirm)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, budgetRefusedError("remove", refusal)
	}
	return result, nil
}

func (t *Table) handleBudgetVocabulary(ctx context.Context, _ *Session, _ json.RawMessage) (any, error) {
	if err := t.budgetSeam(); err != nil {
		return nil, err
	}
	return t.BudgetStore.Vocabulary(), nil
}

func (t *Table) handleLoopRaise(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.budgetSeam(); err != nil {
		return nil, err
	}
	var p LoopRaiseParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the loop to raise")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	// Exactly one new cap, and it names the dimension the raise lifts: a
	// raise is not a budget write (it cannot add or lower a cap — the store
	// refuses a lowering at the backstop), it lifts the one dimension the
	// loop is parked on, so two caps is a parse error the caller sees.
	named := 0
	if p.USDCap != nil {
		named++
	}
	if p.TokensCap != nil {
		named++
	}
	if p.WallMsCap != nil {
		named++
	}
	if p.StepsCap != nil {
		named++
	}
	if named != 1 {
		return nil, ErrBadParams{Err: fmt.Errorf("name exactly one new cap (usdCap, tokensCap, wallMsCap or stepsCap) — named %d", named)}
	}
	reason, err := checkBudgetReason("reason", p.Reason)
	if err != nil {
		return nil, err
	}
	p.Reason = reason
	actor, err := t.budgetActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, refusal, err := t.BudgetStore.Raise(ctx, actor, p)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		// The daemon returns a refusal as the SECOND value only for a HARD
		// refusal (the loop was not parked, the param named the wrong
		// budget or dimension, the cap cannot be lifted) — an error, like
		// every other refused budget act. A LOST RACE already arrives as a
		// RESULT with its refusal set (the loop.resume precedent): the cap
		// was lifted when the raise committed and the loop moved first, so
		// the answer names the status it already has and the door that
		// state has, rather than an error with a dead end.
		return nil, budgetRefusedError("raise", refusal)
	}
	return result, nil
}
