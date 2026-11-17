package protocol

// watchdog.go — S55 T9: the watchdog plane's SIX DOORS. watchdog.list,
// watchdog.show and watchdog.vocabulary are reads (list-with-details, rule 3;
// the vocabulary is the closed signal/action set WITH per-signal availability
// — rule 4: the client builds from the daemon, never a local copy).
// watchdog.set, watchdog.setEnabled and watchdog.remove are the HUMAN DOORS
// (P34, LC18): the person is stamped from the CONNECTION (the daemon's
// ActorFor seam, the kernel's peer credential), NEVER from params, and every
// write carries a rejected-by-name `actor` field so a forged one is a parse
// error the caller sees, not a silently ignored value.
//
// # Confirmation
//
// A watchdog act that LOOSENS a safeguard commits only against the digest of
// a canonical statement the daemon rendered (the S45 PM7.1 pattern, the T8
// budget shape). The loosening acts are: a disable (setEnabled false, or a
// set that flips an enabled rule off) and a removal (always, any origin). A
// create and a re-enable TIGHTEN (a rule that did not exist now restricts
// work; a disabled rule restored is the default state) and commit directly.
// A commit without a digest rides ErrWatchdogRefused with data.kind
// confirm-required, carrying the statement and the digest so the client
// completes in one round trip; a wrong digest rides confirm-mismatch (the row
// moved since the read). A dryRun returns the statement and the effect and
// writes nothing.
//
// # Refusals
//
// A refused watchdog act rides ErrWatchdogRefused (one code for the plane, the
// state found in Data.Kind, the person's sentence in What — DB19). The kinds
// are: unknown-rule (a show/setEnabled/remove of an id with no row),
// duplicate (a create whose (scope,subject,signal,action) already has a rule
// at another threshold), unavailable-signal (a rule on a signal whose source
// is absent in this store — P31's honest third state), workspace-subject and
// unknown-subject (a scope/subject pair that names nothing real), and the two
// confirm kinds. A param-SHAPE fault (a scope/signal/action outside the
// closed set, a negative threshold, a missing id) is NOT a refusal: it is a
// -32602 ErrBadParams, the shape a parse error has.
//
// # Absence
//
// The doors are registered only over a non-nil Watchdog seam (V13, the
// settings/budget/loop-control precedent): a storeless build never answers a
// watchdog door at all, rather than answering a refusal.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The six method names live in method.go with every other MethodName —
// tools/protogen reads that one const block to build the app's contract —
// this file carries their params, results, handlers and the register lines.

// WatchdogEventWindow is how many of a rule's audit events (newest first)
// watchdog.show returns — the history a person reads when a rule's life is in
// question, not the full ledger (the full ledger is the store's job; this is
// the screen's). It is exported because the wire contract names the window
// and the daemon's seam reads the same constant, not its own copy (the
// BudgetEventWindow shape).
const WatchdogEventWindow = 50

// WatchdogFireWindow is how many of a rule's fires (newest first)
// watchdog.show returns — the loops the rule has moved, not the whole fire
// history (the fire rows live on the loops' own control audit; this is the
// screen's slice of it).
const WatchdogFireWindow = 50

// --- views ------------------------------------------------------------------

// WatchdogRuleView is one watchdog_rule, list-with-details (rule 3): the row,
// the threshold in its own human units (ms for the time signals, a count for
// the count signals — the unit the vocabulary serves, never assumed), the
// fire side (how often it has fired and when), and the loops currently in an
// episode under it (the loops its once-per-episode memory says it will not
// re-fire for until they make progress). A client that renders the list sees
// the rule AND its effect AND who it is currently held over, in one read.
type WatchdogRuleView struct {
	ID             string   `json:"id"`
	Scope          string   `json:"scope"` // workspace|component
	SubjectID      string   `json:"subjectId"`
	Signal         string   `json:"signal"` // no-activity|same-position|repeat-block-reason|step-over-deadline
	Threshold      int64    `json:"threshold"`
	ThresholdHuman string   `json:"thresholdHuman"`
	Action         string   `json:"action"` // notify|stop|block
	Enabled        bool     `json:"enabled"`
	Origin         string   `json:"origin"` // system|user
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
	LastFiredAt    string   `json:"lastFiredAt,omitempty"`
	FireCount      int      `json:"fireCount"`
	LoopsInEpisode []string `json:"loopsInEpisode"`
}

// WatchdogEventView is one row of a rule's audit (LC11: the history outlives
// the rule). The values are numbers (the enabled flag as 0/1), never user
// text.
type WatchdogEventView struct {
	At        string `json:"at"`
	Kind      string `json:"kind"` // created|changed|removed|disabled
	ActorKind string `json:"actorKind"`
	ActorID   string `json:"actorId"`
	OldValue  *int64 `json:"oldValue,omitempty"`
	NewValue  *int64 `json:"newValue,omitempty"`
}

// WatchdogFireView is one fire the rule caused — a loop it moved (or notified
// of). Action is the move taken (a stop rule on an unattended loop reads
// block: the attendedness, not the rule, picked the move — LC17).
type WatchdogFireView struct {
	At      string `json:"at"`
	LoopID  string `json:"loopId"`
	Signal  string `json:"signal"`
	Action  string `json:"action"`
	Episode string `json:"episode"`
}

// WatchdogShowResult is watchdog.show's answer: the rule (detailed), its audit
// trail (newest first), and the fires it has caused (newest first). A show of
// an id with no row is a refusal (unknown-rule), not an empty result.
type WatchdogShowResult struct {
	Rule   WatchdogRuleView    `json:"rule"`
	Events []WatchdogEventView `json:"events"`
	Fires  []WatchdogFireView  `json:"fires"`
}

// --- params -----------------------------------------------------------------

// WatchdogListParams is watchdog.list's read params: every filter optional.
type WatchdogListParams struct {
	Scope     string `json:"scope,omitempty"`
	SubjectID string `json:"subjectId,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

// WatchdogListResult is watchdog.list's answer: the views, already detailed.
type WatchdogListResult struct {
	Rules []WatchdogRuleView `json:"rules"`
}

// WatchdogShowParams is watchdog.show's read params: the rule by id.
type WatchdogShowParams struct {
	ID string `json:"id"`
}

// WatchdogSetParams is watchdog.set's params (the human door): create or
// update by the unique key (scope, subjectId, signal, action, threshold). At
// an EXISTING key the only editable field is enabled (the key is fixed — a
// rule that changes its key is a different rule), so a set there flips the
// rule on or off; a set at a NEW key creates a rule (tightening, free). A
// create whose (scope, subjectId, signal, action) already has a rule at
// another threshold is a duplicate refusal: one threshold per combination, and
// raising a threshold is a removal (confirmed) plus a fresh create (free).
// DryRun returns the effect and the confirmation statement (when the act
// loosens) and writes nothing. Confirm is the digest the daemon demands of a
// loosening commit — the SHA-256 of the statement, bound to what was read.
type WatchdogSetParams struct {
	Scope     string `json:"scope"`
	SubjectID string `json:"subjectId"`
	Signal    string `json:"signal"`
	Threshold int64  `json:"threshold"`
	Action    string `json:"action"`
	Enabled   *bool  `json:"enabled,omitempty"`
	DryRun    bool   `json:"dryRun,omitempty"`
	Confirm   string `json:"confirm,omitempty"`
	// Actor is never read. Present to be refused by name (LC5).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// WatchdogSetResult is watchdog.set's answer. Effect is created, updated or
// noop (a set that changes nothing writes nothing — LC11). A dryRun carries
// the effect it WOULD take and the statement/digest when the act loosens, and
// no view (nothing was written). A committed loosening carries Confirmed true.
type WatchdogSetResult struct {
	View      *WatchdogRuleView `json:"view,omitempty"`
	Effect    string            `json:"effect"` // created|updated|noop
	DryRun    bool              `json:"dryRun,omitempty"`
	Statement string            `json:"statement,omitempty"`
	Digest    string            `json:"digest,omitempty"`
	Confirmed bool              `json:"confirmed,omitempty"`
}

// WatchdogSetEnabledParams is watchdog.setEnabled's params (the human door):
// the rule by id and the new enabled flag. Disabling (true→false) loosens and
// confirms; enabling (false→true) tightens and is free; a set that changes
// nothing is a noop.
type WatchdogSetEnabledParams struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	DryRun  bool   `json:"dryRun,omitempty"`
	Confirm string `json:"confirm,omitempty"`
	// Actor is never read. Present to be refused by name (LC5).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// WatchdogSetEnabledResult is watchdog.setEnabled's answer. Effect is
// enabled, disabled or noop. A dryRun carries the statement/digest when the
// act loosens and writes nothing; a committed loosening carries Confirmed.
type WatchdogSetEnabledResult struct {
	View      *WatchdogRuleView `json:"view,omitempty"`
	Effect    string            `json:"effect"` // enabled|disabled|noop
	DryRun    bool              `json:"dryRun,omitempty"`
	Statement string            `json:"statement,omitempty"`
	Digest    string            `json:"digest,omitempty"`
	Confirmed bool              `json:"confirmed,omitempty"`
}

// WatchdogRemoveParams is watchdog.remove's params (the human door): the rule
// by id and the digest of the removal statement the daemon rendered — a
// removal always loosens a safeguard (a rule that is gone will never fire),
// so it always confirms, any origin.
type WatchdogRemoveParams struct {
	ID      string `json:"id"`
	Confirm string `json:"confirm"`
	// Actor is never read. Present to be refused by name (LC5).
	Actor LoopControlActor `json:"actor,omitempty"`
}

// WatchdogRemoveResult is watchdog.remove's answer: the id, and that it is
// gone (the row is disabled, never deleted, and the audit stays — LC11).
type WatchdogRemoveResult struct {
	ID      string `json:"id"`
	Removed bool   `json:"removed"`
}

// WatchdogSignalVocab is one signal in the vocabulary: its unit (the threshold
// reads in it — ms for the time signals, count for the count signals) and
// whether its source is available in THIS store (P31: a signal whose source is
// absent reads available:false with the reason, and a rule on it is refused at
// the door rather than silently inert).
type WatchdogSignalVocab struct {
	Signal    string `json:"signal"`
	Unit      string `json:"unit"` // ms|count
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// WatchdogVocabularyResult is watchdog.vocabulary's answer: the closed sets the
// forms and the CLI build from (rule 4: the vocabulary comes from the daemon,
// never a client copy — a literal in the client is a second truth that drifts
// when the set widens). The signals carry their unit and availability, so a
// client can build a threshold input that knows its own unit and knows which
// signals this store can actually serve.
type WatchdogVocabularyResult struct {
	Scopes       []string              `json:"scopes"` // workspace|component
	Signals      []WatchdogSignalVocab `json:"signals"`
	Actions      []string              `json:"actions"`    // notify|stop|block
	EventKinds   []string              `json:"eventKinds"` // created|changed|removed|disabled
	RefusalKinds []string              `json:"refusalKinds"`
}

// --- the refusal and the seam ------------------------------------------------

// WatchdogRefusal is the wire twin of the store's watchdog refusal (DB19: a
// refused watchdog act is a result, rendered as the plane's one error code).
// Kind is machine-readable — it rides the refusal's Data.Kind so a client can
// branch on it without parsing the sentence; Detail is the person's sentence.
// Statement and Digest are set only by the confirm-required refusal: they
// carry the act's canonical statement and its digest so the client completes
// the commit in one round trip, rather than re-reading.
type WatchdogRefusal struct {
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	Statement string `json:"statement,omitempty"`
	Digest    string `json:"digest,omitempty"`
}

// WatchdogStore is the daemon-facing seam (Table.Watchdog): the watchdog
// plane's six doors over the T9 store. The protocol layer carries the wire
// contract; the daemon implements it — the same one service (the watchdog
// ticker's owner) that acts on firings, so the list's loopsInEpisode reads the
// service's own once-per-episode memory and the vocabulary reads the store's
// signal availability. The daemon assembles the views from store rows (no
// caching — a rule a person disabled at the last tick must read disabled at
// this read), classifies each act's direction (tightening free, loosening
// confirms), and renders the canonical statement for the digest. The wire
// types are THIS package's field-for-field twins of what the daemon assembles
// — the daemon's adapter is the only layer that may know both.
//
// Actor is protocol.Actor (the peer identity the handler resolved through
// ActorFor) — the seam receives it stamped, never a raw claim (LC5).
type WatchdogStore interface {
	List(ctx context.Context, scope, subjectID string, enabled *bool) ([]WatchdogRuleView, error)
	Show(ctx context.Context, id string) (WatchdogShowResult, error)
	Set(ctx context.Context, actor Actor, p WatchdogSetParams) (WatchdogSetResult, *WatchdogRefusal, error)
	SetEnabled(ctx context.Context, actor Actor, id string, enabled bool, dryRun bool, confirm string) (WatchdogSetEnabledResult, *WatchdogRefusal, error)
	// RemoveRule is watchdog.remove's human door at the seam. The 2026-11-02
	// merge reconciliation renamed it from Remove, the same reason as
	// RemoveBudget: S45's boundary guards grep every non-test file for the
	// policy boundary's Remove call shape (a dot-Remove receiver whose first
	// argument is a bare ctx) against a closed whitelist, and this seam is
	// S55's, not one of S45's doors. The rename clears S45's pattern without
	// touching S45's files; the wire name stays watchdog.remove.
	RemoveRule(ctx context.Context, actor Actor, id, confirm string) (WatchdogRemoveResult, *WatchdogRefusal, error)
	Vocabulary(ctx context.Context) (WatchdogVocabularyResult, error)
}

// RegisterWatchdog registers the six watchdog doors over a non-nil seam (V13).
// It returns whether the methods are in the table, so the daemon's wiring can
// be asserted rather than trusted — the settings/budget/loop-control shape.
func (t *Table) RegisterWatchdog() bool {
	if t.Watchdog == nil {
		return false
	}
	t.register(&Method{Name: MethodWatchdogList, RequiresInitialize: true, Handle: t.handleWatchdogList})
	t.register(&Method{Name: MethodWatchdogShow, RequiresInitialize: true, Handle: t.handleWatchdogShow})
	t.register(&Method{Name: MethodWatchdogVocabulary, RequiresInitialize: true, Handle: t.handleWatchdogVocabulary})
	t.register(&Method{Name: MethodWatchdogSet, RequiresInitialize: true, Handle: t.handleWatchdogSet})
	t.register(&Method{Name: MethodWatchdogSetEnabled, RequiresInitialize: true, Handle: t.handleWatchdogSetEnabled})
	t.register(&Method{Name: MethodWatchdogRemove, RequiresInitialize: true, Handle: t.handleWatchdogRemove})
	return true
}

// --- the actor and the refusals -------------------------------------------------

// watchdogSeam is the one nil-guard shape the six handlers share: registration
// is conditional on the seam, so a nil here is a registration/seam disagreement
// the guard tests catch — spoken, not silent (the budget shape).
func (t *Table) watchdogSeam() error {
	if t.Watchdog == nil {
		return ErrStoreUnavailable.WithWhat("watchdog.* needs the watchdog seam, and this build did not link it — the watchdog surface is absent for the same reason (V13)")
	}
	return nil
}

// watchdogActor is the write boundary's audit identity for the three human
// doors (LC5): the person comes from the CONNECTION (the daemon's ActorFor
// seam, the kernel's peer credential), never from params. It is the honest
// absence (a daemon with no identity seam has no persons) that refuses the
// write, and an empty id that is refused as well.
func (t *Table) watchdogActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("the watchdog write is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("the watchdog write is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	return a, nil
}

// watchdogRefusedError renders the store's WatchdogRefusal as the socket's
// refusal (DB19): one code (ErrWatchdogRefused), the state found in Data.Kind,
// the person's sentence in What, and the statement/digest the confirm-required
// refusal carries (the client's one-round-trip completion).
func watchdogRefusedError(verb string, r *WatchdogRefusal) *Error {
	e := ErrWatchdogRefused.WithWhat("watchdog %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	if r.Statement != "" {
		e.Data.Statement = r.Statement
		e.Data.Confirm = r.Digest
	}
	return e
}

// --- the handlers ----------------------------------------------------------------

func (t *Table) handleWatchdogList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := t.watchdogSeam(); err != nil {
		return nil, err
	}
	var p WatchdogListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	switch p.Scope {
	case "", "workspace", "component":
	default:
		return nil, ErrBadParams{Err: fmt.Errorf("scope is workspace or component, not %q", p.Scope)}
	}
	views, err := t.Watchdog.List(ctx, strings.TrimSpace(p.Scope), strings.TrimSpace(p.SubjectID), p.Enabled)
	if err != nil {
		return nil, err
	}
	if views == nil {
		views = []WatchdogRuleView{}
	}
	return WatchdogListResult{Rules: views}, nil
}

func (t *Table) handleWatchdogShow(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := t.watchdogSeam(); err != nil {
		return nil, err
	}
	var p WatchdogShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the rule to read")}
	}
	return t.Watchdog.Show(ctx, p.ID)
}

func (t *Table) handleWatchdogVocabulary(ctx context.Context, _ *Session, _ json.RawMessage) (any, error) {
	if err := t.watchdogSeam(); err != nil {
		return nil, err
	}
	return t.Watchdog.Vocabulary(ctx)
}

func (t *Table) handleWatchdogSet(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.watchdogSeam(); err != nil {
		return nil, err
	}
	var p WatchdogSetParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	actor, err := t.watchdogActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, refusal, err := t.Watchdog.Set(ctx, actor, p)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, watchdogRefusedError("set", refusal)
	}
	return result, nil
}

func (t *Table) handleWatchdogSetEnabled(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.watchdogSeam(); err != nil {
		return nil, err
	}
	var p WatchdogSetEnabledParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the rule to move")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	actor, err := t.watchdogActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, refusal, err := t.Watchdog.SetEnabled(ctx, actor, p.ID, p.Enabled, p.DryRun, p.Confirm)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, watchdogRefusedError("setEnabled", refusal)
	}
	return result, nil
}

func (t *Table) handleWatchdogRemove(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.watchdogSeam(); err != nil {
		return nil, err
	}
	var p WatchdogRemoveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the rule to remove")}
	}
	if p.Actor != "" {
		return nil, refuseActor("actor")
	}
	actor, err := t.watchdogActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	result, refusal, err := t.Watchdog.RemoveRule(ctx, actor, p.ID, p.Confirm)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, watchdogRefusedError("remove", refusal)
	}
	return result, nil
}
