// mergerule.go — S57 T8 (VG18): the six merge-rule methods — the daemon's
// surface for the rule that governs a component's merges: the four reads
// (get, preview, changes, vocabulary) and the two human-door writes
// (set, clear). The two merge-plane READS (merge.events, merge.watches)
// ride the same seam — their views and handlers live in
// merge_events.go, their registration here — because the rule is the
// thing the events record and the watches enforce; three seams would
// split one plane (the T8 Notes record the ruling).
//
// The seam is the daemon's MergeRuleStore (internal/daemon/
// mergerule_service.go). A *MergeRuleRefusal is the engine's refusal as
// data (DB19): the closed-set kind in Data.Kind, the person's sentence
// in Data.What, one code for the family (ErrMergeRuleRefused, T0's
// -31871). preview is the read-only dry evaluation the design promises:
// against the forge's LIVE facts when a PR is named (what would the gate
// say now, with every refusal sentence and the autonomy floor result),
// and honestly partial when it is not — the rule, its source and the
// floor, plus the sentence that says the gate's question has no PR to
// ask about.
//
// The writes stamp the actor from the CONNECTION (Table.ActorFor, the
// kernel peer credential), never from params — rejectGateIdentityKeys
// refuses an identity key in the wire, and TestNoMethodAcceptsAnActorParam
// pins the family. A person at the socket may set ANY rule — the
// restrictive-only tightening is the PROPOSAL path's law (VG17: agents
// can only tighten), never the human door's.
//
// The method name constants live HERE, beside their family, and not in
// method.go (the SIZE1 ruling, verify.go's header).

package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The eight `mergerule.*`/`merge.*` method names live in method.go's S57
// block — protogen enumerates that file alone, and the app's contract is
// what T11's panel reads. The rule algebra and the seam stay here.

// --- the seam ----------------------------------------------------------------

// MergeRuleStore is the daemon's one merge-plane seam: the rule's six
// doors plus the two merge-plane reads (events, watches). The wire views
// are this package's types; a *MergeRuleRefusal is the engine's refusal
// rendered as data (DB19), never an error.
type MergeRuleStore interface {
	// Get answers mergerule.get: the component's OWN row when
	// effective is false (source "none" when it has no row — an honest
	// absence, not an error), or the EFFECTIVE rule (own, else the
	// root's, else the baseline — the store's resolution) with its
	// source, last change (who and when, from the audit) and the
	// autonomy floor result, each missing ingredient named.
	Get(ctx context.Context, componentID string, effective bool) (MergeRuleGetResult, error)
	// Preview answers mergerule.preview: the rule (effective resolution,
	// source and revision), the autonomy floor, and — when pr is given —
	// the read-only dry evaluation over the forge's LIVE facts for that
	// PR: allowed, or every refusal sentence, with the evidence.
	// Without a PR the verdict is nil and CannotSay carries the honest
	// sentence (the gate's question has no PR to ask about).
	Preview(ctx context.Context, componentID string, pr int) (MergeRulePreviewResult, error)
	// Changes answers mergerule.changes: the rule's audit trail, newest
	// first, capped — revision, op, who, when, and the before/after
	// images as raw JSON.
	Changes(ctx context.Context, componentID string, limit int) (MergeRuleChangesResult, error)
	// Vocabulary answers mergerule.vocabulary: the closed sets the rule
	// fields draw from (require_dod, checks_policy, no_checks,
	// review_mode, method), the closed refusal codes, the floor codes,
	// and the baseline rule — the set the client renders from, so no
	// client types a set twice.
	Vocabulary(ctx context.Context) (MergeRuleVocabularyResult, error)
	// Set answers mergerule.set: the full rule on the component (a patch
	// shape would leave "which half changed" to the reader — the wire
	// carries the whole rule, and the store's no-op check keeps a
	// re-set of the standing rule quiet).
	Set(ctx context.Context, actor Actor, in MergeRuleSetInput) (MergeRuleSetResult, *MergeRuleRefusal, error)
	// Clear answers mergerule.clear: the row is deleted (the component
	// returns to its root's rule, or the baseline), the revisions
	// survive in the audit.
	Clear(ctx context.Context, actor Actor, componentID string) (MergeRuleSetResult, *MergeRuleRefusal, error)
	// Events answers merge.events: the merge-event trail (the merges
	// that happened and the gate's account of each), scoped by
	// component, loop (which the daemon resolves to the loop's own
	// root — a loop names both halves of the scope) and capped.
	Events(ctx context.Context, f MergeEventsQuery) (MergeEventsResult, error)
	// Watches answers merge.watches: the standing merge watches (the
	// merges the gate is still checking), optionally by state.
	Watches(ctx context.Context, f MergeWatchesQuery) (MergeWatchesResult, error)
}

// MergeRuleRefusal is a refused merge-rule act as data (DB19): Kind is
// the store's closed refusal vocabulary (the machine can branch on it),
// Detail the person's sentence, printed verbatim.
type MergeRuleRefusal struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// --- the seam's inputs ---------------------------------------------------------

// MergeRuleSetInput is the full rule the person sets: every behavioural
// field, no server-managed field. The daemon validates the closed sets
// before it reaches the store (the store's own closed sets are the
// backstop; the refusal names the value it refused either way).
type MergeRuleSetInput struct {
	ComponentID            string
	Enabled                bool
	RequireLocal           bool
	RequireDoD             string
	RequiredChecks         []string
	ChecksPolicy           string
	NoChecks               string
	ReviewMode             string
	AeonReviewAgents       []string
	RequireThreadsResolved bool
	Method                 string
}

type MergeEventsQuery struct {
	ComponentID string
	LoopSlug    string
	Limit       int
}

type MergeWatchesQuery struct {
	State string
}

// --- the wire views ------------------------------------------------------------

// MergeRuleWire is the rule's behavioural fields — the shape the person
// sets and the shape every read carries. The server-managed fields
// (componentId, revision, source, updatedAt) are NOT here: they are the
// ledger's, and a param that names them is refused (the set handler
// checks the raw envelope).
type MergeRuleWire struct {
	Enabled                bool     `json:"enabled"`
	RequireLocal           bool     `json:"requireLocal"`
	RequireDoD             string   `json:"requireDod"`
	RequiredChecks         []string `json:"requiredChecks"`
	ChecksPolicy           string   `json:"checksPolicy"`
	NoChecks               string   `json:"noChecks"`
	ReviewMode             string   `json:"reviewMode"`
	AeonReviewAgents       []string `json:"aeonReviewAgents"`
	RequireThreadsResolved bool     `json:"requireThreadsResolved"`
	Method                 string   `json:"method"`
}

// MergeRuleView is the rule as a read carries it: the wire's fields plus
// the ledger's own (revision, source, the row's stamp) and the component
// the view is about (the store always answers for the component asked
// about, even when the rule came from the root or the baseline).
type MergeRuleView struct {
	ComponentID string `json:"componentId"`
	MergeRuleWire
	Revision  int    `json:"revision"`
	Source    string `json:"source"` // "own" | "root" | "baseline"
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// FloorView is the autonomy floor's answer: ok, and each missing
// ingredient with its code (the machine half) and its sentence (the
// human half — printed verbatim, the checklists render it).
type FloorView struct {
	OK      bool           `json:"ok"`
	Missing []FloorMissing `json:"missing"`
}

type FloorMissing struct {
	Code     string `json:"code"`
	Sentence string `json:"sentence"`
}

// VerdictView is the evaluation's answer as the wire carries it:
// allowed, every refusal (code + the sentence, verbatim), and the
// evidence map — the gate's account of what it saw, the same keys the
// merge gate's answer carries, so the preview and the gate never drift.
type VerdictView struct {
	Allowed  bool              `json:"allowed"`
	Refusals []RefusalView     `json:"refusals"`
	Evidence map[string]string `json:"evidence"`
}

type RefusalView struct {
	Code     string `json:"code"`
	Sentence string `json:"sentence"`
}

// MergeRuleChangeView is one row of the rule's audit: the revision
// (the ledger's number, never resetting), the op (set|clear), who, when,
// the summary, and the before/after images as RAW JSON strings (the
// reader parses; the wire does not promise a shape the ledger may
// reshape).
type MergeRuleChangeView struct {
	ComponentID string  `json:"componentId"`
	Revision    int     `json:"revision"`
	Op          string  `json:"op"`
	ActorKind   string  `json:"actorKind"`
	ActorID     string  `json:"actorId"`
	Summary     string  `json:"summary"`
	Before      *string `json:"before,omitempty"`
	After       *string `json:"after,omitempty"`
	At          string  `json:"at"`
}

type MergeRuleGetResult struct {
	ComponentID string `json:"componentId"`
	// Source is "own" | "root" | "baseline" for an effective read, and
	// "own" | "none" for a specific one (the component has no row).
	Source     string               `json:"source"`
	Rule       *MergeRuleView       `json:"rule,omitempty"`
	LastChange *MergeRuleChangeView `json:"lastChange,omitempty"`
	// Floor is present on effective reads only: the floor the rule would
	// run under, each missing ingredient named.
	Floor *FloorView `json:"floor,omitempty"`
}

type MergeRulePreviewResult struct {
	ComponentID string         `json:"componentId"`
	Source      string         `json:"source"`
	Rule        *MergeRuleView `json:"rule,omitempty"`
	Floor       *FloorView     `json:"floor,omitempty"`
	// Verdict is the dry evaluation over the PR's live facts — nil when
	// no PR was named, and CannotSay then carries the honest sentence.
	Verdict   *VerdictView `json:"verdict,omitempty"`
	CannotSay string       `json:"cannotSay,omitempty"`
}

type MergeRuleChangesResult struct {
	Changes []MergeRuleChangeView `json:"changes"`
}

// MergeRuleVocabularyResult is the set the client renders from: the five
// closed sets the rule fields draw from, the closed refusal codes and
// floor codes (the machine halves), and the baseline rule — the rule
// every component starts from, in code, never in rows (VG21).
type MergeRuleVocabularyResult struct {
	RequireDoD   []string      `json:"requireDod"`
	ChecksPolicy []string      `json:"checksPolicy"`
	NoChecks     []string      `json:"noChecks"`
	ReviewMode   []string      `json:"reviewMode"`
	Method       []string      `json:"method"`
	RefusalCodes []string      `json:"refusalCodes"`
	FloorCodes   []string      `json:"floorCodes"`
	Baseline     MergeRuleWire `json:"baseline"`
}

type MergeRuleSetResult struct {
	Rule MergeRuleView `json:"rule"`
}

// MergeEventView is one merge event as the wire carries it: the merge
// that happened (PR, SHA, the method the rule asked for) and the gate's
// account (the evidence, the rule revision it judged under, the run the
// rule measured, who recorded it).
type MergeEventView struct {
	ID            string  `json:"id"`
	LoopSlug      string  `json:"loopSlug"`
	ComponentRoot string  `json:"componentRoot"`
	ComponentID   *string `json:"componentId,omitempty"`
	PR            int     `json:"pr"`
	MergeSHA      string  `json:"mergeSha"`
	Method        *string `json:"method,omitempty"`
	BaseRef       *string `json:"baseRef,omitempty"`
	HeadSHA       *string `json:"headSha,omitempty"`
	RuleRevision  *int    `json:"ruleRevision,omitempty"`
	Evidence      string  `json:"evidence"`
	VerifyRunID   *string `json:"verifyRunId,omitempty"`
	MergedAt      string  `json:"mergedAt"`
	ActorKind     *string `json:"actorKind,omitempty"`
	ActorID       *string `json:"actorId,omitempty"`
}

// MergeWatchView is one standing merge watch: the merge it covers (the
// event's join: root, loop, PR, the rule revision, the run), the state
// (the closed five), how long it has been checking, and its last words.
type MergeWatchView struct {
	MergeEventID  string  `json:"mergeEventId"`
	ComponentID   string  `json:"componentId"`
	BaseRef       string  `json:"baseRef"`
	SHA           string  `json:"sha"`
	State         string  `json:"state"`
	Attempts      int     `json:"attempts"`
	NextCheckAt   string  `json:"nextCheckAt"`
	LastCheckedAt *string `json:"lastCheckedAt,omitempty"`
	Detail        string  `json:"detail"`
	CreatedAt     string  `json:"createdAt"`
	ComponentRoot string  `json:"componentRoot"`
	LoopSlug      string  `json:"loopSlug"`
	PR            int     `json:"pr"`
	RuleRevision  *int    `json:"ruleRevision,omitempty"`
	VerifyRunID   *string `json:"verifyRunId,omitempty"`
}

type MergeEventsResult struct {
	Events []MergeEventView `json:"events"`
}

type MergeWatchesResult struct {
	Watches []MergeWatchView `json:"watches"`
}

// --- the params ------------------------------------------------------------------

type MergeRuleGetParams struct {
	ComponentID string `json:"componentId"`
	Effective   *bool  `json:"effective"`
}

type MergeRulePreviewParams struct {
	ComponentID string `json:"componentId"`
	Pr          int    `json:"pr"`
}

type MergeRuleChangesParams struct {
	ComponentID string `json:"componentId"`
	Limit       int    `json:"limit"`
}

// MergeRuleSetParams: the whole rule, as a raw envelope. The handler
// strict-decodes it (unknown field refused) and refuses the
// server-managed keys by name — a param that names the ledger is a
// param that forges it.
type MergeRuleSetParams struct {
	ComponentID string          `json:"componentId"`
	Rule        json.RawMessage `json:"rule"`
}

type MergeRuleClearParams struct {
	ComponentID string `json:"componentId"`
}

type MergeEventsParams struct {
	ComponentID *string `json:"componentId"`
	Loop        string  `json:"loop"`
	Limit       int     `json:"limit"`
}

type MergeWatchesParams struct {
	State string `json:"state"`
}

// --- the refusal rendering ----------------------------------------------------------

func mergeRuleRefusedError(verb string, r *MergeRuleRefusal) *Error {
	e := ErrMergeRuleRefused.WithWhat("merge rule %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	return e
}

func (t *Table) mergeRuleSeam() error {
	if t.MergeRule == nil {
		return ErrStoreUnavailable.WithWhat("mergerule.* needs the merge-rule seam, and this build did not link it — the merge-rule surface is absent for the same reason (V13)")
	}
	return nil
}

// ruleManagedKeys are the ledger's fields: a set param that names one is
// refused, because the revision is the ledger's number, the source the
// store's resolution, and the stamp the store's clock — a caller that
// names them is a caller that forges the ledger.
var ruleManagedKeys = []string{"componentId", "revision", "source", "updatedAt"}

func rejectRuleManagedKeys(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return ErrBadParams{Err: fmt.Errorf("mergerule.set's rule is the rule — it may not be absent")}
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return ErrBadParams{Err: fmt.Errorf("mergerule.set's rule is a JSON object of the rule's fields, not %s", strings.TrimSpace(string(raw)))}
	}
	for _, key := range ruleManagedKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf("mergerule.set's rule names %q — that field is the ledger's, not the caller's", key)}
		}
	}
	return nil
}

// --- the handlers --------------------------------------------------------------------

func (t *Table) handleMergeRuleGet(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.mergeRuleSeam(); err != nil {
		return nil, err
	}
	var p MergeRuleGetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergerule.get needs the component's id")}
	}
	effective := false
	if p.Effective != nil {
		effective = *p.Effective
	}
	res, err := t.MergeRule.Get(ctx, p.ComponentID, effective)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleMergeRulePreview(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.mergeRuleSeam(); err != nil {
		return nil, err
	}
	var p MergeRulePreviewParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergerule.preview needs the component's id")}
	}
	if p.Pr < 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("mergerule.preview's pr is a PR number, not %d", p.Pr)}
	}
	res, err := t.MergeRule.Preview(ctx, p.ComponentID, p.Pr)
	if err != nil {
		return nil, err
	}
	if res.Verdict != nil {
		if res.Verdict.Refusals == nil {
			res.Verdict.Refusals = []RefusalView{}
		}
	}
	return res, nil
}

func (t *Table) handleMergeRuleChanges(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.mergeRuleSeam(); err != nil {
		return nil, err
	}
	var p MergeRuleChangesParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergerule.changes needs the component's id")}
	}
	if p.Limit < 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("mergerule.changes's limit is a count, not %d", p.Limit)}
	}
	res, err := t.MergeRule.Changes(ctx, p.ComponentID, p.Limit)
	if err != nil {
		return nil, err
	}
	if res.Changes == nil {
		res.Changes = []MergeRuleChangeView{}
	}
	return res, nil
}

func (t *Table) handleMergeRuleVocabulary(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.mergeRuleSeam(); err != nil {
		return nil, err
	}
	_ = params // the closed sets have no params: they are the daemon's own
	res, err := t.MergeRule.Vocabulary(ctx)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleMergeRuleSet(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.mergeRuleSeam(); err != nil {
		return nil, err
	}
	if err := rejectGateIdentityKeys(string(MethodMergeRuleSet), params); err != nil {
		return nil, err
	}
	var p MergeRuleSetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergerule.set needs the component's id")}
	}
	if err := rejectRuleManagedKeys(p.Rule); err != nil {
		return nil, err
	}
	var wire MergeRuleWire
	if err := json.Unmarshal(p.Rule, &wire); err != nil {
		return nil, ErrBadParams{Err: fmt.Errorf("mergerule.set's rule does not decode: %v", err)}
	}
	actor, err := t.gateActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, refusal, err := t.MergeRule.Set(ctx, actor, MergeRuleSetInput{
		ComponentID:            p.ComponentID,
		Enabled:                wire.Enabled,
		RequireLocal:           wire.RequireLocal,
		RequireDoD:             wire.RequireDoD,
		RequiredChecks:         wire.RequiredChecks,
		ChecksPolicy:           wire.ChecksPolicy,
		NoChecks:               wire.NoChecks,
		ReviewMode:             wire.ReviewMode,
		AeonReviewAgents:       wire.AeonReviewAgents,
		RequireThreadsResolved: wire.RequireThreadsResolved,
		Method:                 wire.Method,
	})
	if refusal != nil {
		return nil, mergeRuleRefusedError("set", refusal)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleMergeRuleClear(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.mergeRuleSeam(); err != nil {
		return nil, err
	}
	if err := rejectGateIdentityKeys(string(MethodMergeRuleClear), params); err != nil {
		return nil, err
	}
	var p MergeRuleClearParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergerule.clear needs the component's id")}
	}
	actor, err := t.gateActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, refusal, err := t.MergeRule.Clear(ctx, actor, p.ComponentID)
	if refusal != nil {
		return nil, mergeRuleRefusedError("clear", refusal)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (t *Table) handleMergeEvents(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.mergeRuleSeam(); err != nil {
		return nil, err
	}
	var p MergeEventsParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Limit < 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("merge.events's limit is a count, not %d", p.Limit)}
	}
	componentID := ""
	if p.ComponentID != nil {
		componentID = *p.ComponentID
	}
	res, err := t.MergeRule.Events(ctx, MergeEventsQuery{
		ComponentID: componentID,
		LoopSlug:    p.Loop,
		Limit:       p.Limit,
	})
	if err != nil {
		return nil, err
	}
	if res.Events == nil {
		res.Events = []MergeEventView{}
	}
	return res, nil
}

func (t *Table) handleMergeWatches(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.mergeRuleSeam(); err != nil {
		return nil, err
	}
	var p MergeWatchesParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	res, err := t.MergeRule.Watches(ctx, MergeWatchesQuery{State: p.State})
	if err != nil {
		return nil, err
	}
	if res.Watches == nil {
		res.Watches = []MergeWatchView{}
	}
	return res, nil
}

// RegisterMergeRule conditionally registers the merge plane: the rule's
// six doors plus the two merge reads, over the ONE seam (V13: a nil
// seam leaves all eight ABSENT — the daemon reports the plane as
// unavailable, not as a set of registered doors that all refuse).
func (t *Table) RegisterMergeRule() bool {
	if t.MergeRule == nil {
		return false
	}
	t.register(&Method{Name: MethodMergeRuleGet, RequiresInitialize: true, Handle: t.handleMergeRuleGet})
	t.register(&Method{Name: MethodMergeRulePreview, RequiresInitialize: true, Handle: t.handleMergeRulePreview})
	t.register(&Method{Name: MethodMergeRuleChanges, RequiresInitialize: true, Handle: t.handleMergeRuleChanges})
	t.register(&Method{Name: MethodMergeRuleVocabulary, RequiresInitialize: true, Handle: t.handleMergeRuleVocabulary})
	t.register(&Method{Name: MethodMergeRuleSet, RequiresInitialize: true, Handle: t.handleMergeRuleSet})
	t.register(&Method{Name: MethodMergeRuleClear, RequiresInitialize: true, Handle: t.handleMergeRuleClear})
	t.register(&Method{Name: MethodMergeEvents, RequiresInitialize: true, Handle: t.handleMergeEvents})
	t.register(&Method{Name: MethodMergeWatches, RequiresInitialize: true, Handle: t.handleMergeWatches})
	return true
}
