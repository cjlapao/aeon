package protocol

// autonomy.go — S56 T6 (AL11): the socket's human door for the autonomy
// authority. Three writes (grant/deny/revoke) and a read set (list/show/
// vocabulary/effective/pinned/changes/uses) on the person's 0600 + peer-
// credential surface. Registered CONDITIONALLY by RegisterAutonomy (V13: a
// nil Autonomy leaves all ten ABSENT, not registered-and-refusing — the
// settings and seed precedents). The writes stamp the actor from the
// CONNECTION's peer, never params, and refuse a kind that is not `user`
// (AL11 lock 2; the store's actor check is lock 3, the schema CHECK the
// backstop). None is Debug, and none is projected onto the narrow surface
// (AL17; T7's write-side guards keep that true).
//
// The seam (AutonomyService) is the store + the pure internal/autonomy core
// bridged by the daemon — the ONLY layer that may know both. The wire types
// here are THIS package's field-for-field twins of the pure package's, the
// SettingsService convention: the handler maps, it never re-derives. The
// pure package imports nothing this package needs, and this package imports
// nothing the pure package needs back, so the boundary guard stays quiet.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// --- the origin ---------------------------------------------------------------
//
// Mirrors internal/autonomy.Origin (the closed set a start names, AL7). The
// protocol restates it rather than importing the pure package: the wire
// vocabulary is protocol's own, exactly as the settings twins restate
// config's closed sets — a mirror a test can check, not a second truth.

const (
	OriginSchedule  = "schedule"
	OriginWebhook   = "webhook"
	OriginEvent     = "event"
	OriginScheduler = "scheduler"
)

// autonomyOrigins is the closed set `autonomy.effective`'s origin param may
// name; anything else is a bad param, not a default.
var autonomyOrigins = map[string]bool{
	OriginSchedule:  true,
	OriginWebhook:   true,
	OriginEvent:     true,
	OriginScheduler: true,
}

// autonomyStates is the closed polarity a grant may have (AL3), restated
// from the pure package for the `autonomy.list` state filter.
var autonomyStates = map[string]bool{"allow": true, "deny": true}

// --- the seam ---------------------------------------------------------------

// AutonomyService is the accessor the ten autonomy.* methods read and write
// through. The daemon's adapter (internal/daemon/autonomy.go) is the only
// layer that may know both this package's wire twins and the pure
// internal/autonomy core; it maps store rows → GrantView and pure
// refusals → AutonomyRefusal on the way out. A nil seam (a storeless daemon,
// V13) leaves the ten methods ABSENT: RegisterAutonomy is a no-op, so the
// no-store build never answers an autonomy door at all, rather than
// answering a refusal — mirroring Settings and Seed.
type AutonomyService interface {
	// List answers list: the grants on a root ("" = every root), optionally
	// filtered by polarity; revoked rows are excluded unless includeInactive.
	List(ctx context.Context, rootID, state string, includeInactive bool) ([]GrantView, error)
	// Show answers show: one grant's view, its LAST change row (nil if the
	// audit holds none), the count of begun uses, and the running loops
	// pinned with it. An unknown id is a refusal, never an empty success.
	Show(ctx context.Context, id string) (GrantView, *ChangeView, int, []string, *AutonomyRefusal, error)
	// Vocabulary answers vocabulary: the closed descriptor set a form reads
	// and never re-types (AL15).
	Vocabulary() (VocabularyView, error)
	// Effective answers effective: per registry action, the verdict an
	// unattended loop started NOW on the root would get, from the SAME pure
	// Plan the start path pins with (AL14) — zero seams, so the view says
	// "could not be asked" rather than a confident pass.
	Effective(ctx context.Context, rootID, origin string, labels []string) (EffectiveView, *AutonomyRefusal, error)
	// Pinned answers pinned: the loop's pin snapshot, each pinned grant's
	// LIVE status, and the verdict per registry action. A loop with no pin
	// row is {pinned:false, statement}, not an error; an unknown loop is.
	Pinned(ctx context.Context, loopID string) (PinnedView, *AutonomyRefusal, error)
	// Changes answers changes: the append-only audit rows (the store's
	// order, oldest first), filtered by root and/or grant.
	Changes(ctx context.Context, rootID, grantID string, limit int) ([]ChangeView, error)
	// Uses answers uses: the use-log rows with begun/outcome PAIRED (an
	// unpaired begun is outcome "unknown — the process stopped before the
	// result was recorded"), the caller's limit applied as the newest N.
	Uses(ctx context.Context, rootID, grantID, loopID string, limit int) ([]UseView, error)
	// Grant answers grant: create an allow on a live root, stamped by the
	// connection's actor. dryRun validates everything and returns the
	// confirmation with View nil and Changed false, writing nothing.
	Grant(ctx context.Context, actor Actor, in AutonomyGrantRequest) (AutonomyWriteOutcome, *AutonomyRefusal, error)
	// Deny answers deny: create a veto on a root or child, same door.
	Deny(ctx context.Context, actor Actor, in AutonomyDenyRequest) (AutonomyWriteOutcome, *AutonomyRefusal, error)
	// Revoke answers revoke: set the revoked trio once, return the affected
	// running loops. Already revoked is a no-op success (Changed false,
	// Affected empty), writing nothing.
	Revoke(ctx context.Context, actor Actor, in AutonomyRevokeRequest) (AutonomyWriteOutcome, *AutonomyRefusal, error)
}

// AutonomyGrantRequest is the seam input for grant (the handler has already
// parsed expiresAt and applied the conditions defaulting). Conditions is
// the WHOLE object the request carries — absent became {RequireBudget:true}
// in the handler, so the seam sees one shape.
type AutonomyGrantRequest struct {
	NodeID     string
	Subject    string // a registry action name, or the one class shorthand
	Conditions AutonomyConditions
	ExpiresAt  *time.Time
	Note       string
	DryRun     bool
}

// AutonomyDenyRequest is the seam input for deny. A veto carries no
// conditions and no expiry — the params have no fields for them.
type AutonomyDenyRequest struct {
	NodeID  string
	Subject string
	Note    string
	DryRun  bool
}

// AutonomyRevokeRequest is the seam input for revoke.
type AutonomyRevokeRequest struct {
	ID     string
	Reason string
	DryRun bool
}

// AutonomyWriteOutcome is what a write seam call returns on admission. View
// is nil on a dry run (nothing was written, so there is no row); Affected is
// non-nil (empty when nothing is pinned); Changed is false on a dry run and
// on a no-op revoke; ComponentRoot is the workspace root the audit event is
// filed under ("" when the node names none).
type AutonomyWriteOutcome struct {
	View          *GrantView
	Confirmation  string
	Affected      []string
	Changed       bool
	ComponentRoot string
}

// AutonomyRefusal is the seam's discriminated refusal (DB19): a refused
// autonomy write names the rule it broke (Code, machine-closed) and the
// thing it broke it on (Sentence, the human half, verbatim from the pure
// validator or the store's actor door). A nil *AutonomyRefusal means
// admitted. The handler renders the Sentence into ErrAutonomyRefused's
// `what` unchanged — a re-worded refusal is a second truth.
type AutonomyRefusal struct {
	Code     string
	Sentence string
}

// --- the wire types ---------------------------------------------------------

// SubjectView is a grant's subject as a person reads it (AL2): an action by
// name, or the one class shorthand.
type SubjectView struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// AutonomyConditions is the closed, typed condition set (AL4), the wire
// twin of the pure package's Conditions — same field names and JSON tags,
// so the store row's canonical JSON and the wire are one shape. The grant
// params carry it as a POINTER: an absent key is the whole-object default
// ({RequireBudget:true}), a present object (even {}) is the caller's values
// verbatim — a distinction a plain struct cannot hold.
type AutonomyConditions struct {
	RequireGates  bool     `json:"requireGates"`
	RequireBudget bool     `json:"requireBudget"`
	Labels        []string `json:"labels,omitempty"`
	MaxPerDay     int      `json:"maxPerDay,omitempty"`
}

// RevokedView is the revoked trio a view carries (null when not revoked).
// By is rendered "kind:id".
type RevokedView struct {
	At     string `json:"at"`
	By     string `json:"by"`
	Reason string `json:"reason"`
}

// GrantView is the read shape of a grant row (AL10). Describe is the ONE
// fixed-template sentence rendered by the DAEMON (AL17's one formatter): no
// client re-words it, and a test greps the client surface for the template's
// distinctive phrase to keep it that way.
type GrantView struct {
	ID         string             `json:"id"`
	NodeID     string             `json:"nodeId"`
	RootID     string             `json:"rootId"`
	Subject    SubjectView        `json:"subject"`
	State      string             `json:"state"`
	Conditions AutonomyConditions `json:"conditions"`
	Note       string             `json:"note,omitempty"`
	// ExpiresAt is *string WITHOUT omitempty so null is the honest "no
	// expiry" (a bare absent field would read as unset).
	ExpiresAt  *string `json:"expiresAt"`
	CreatedAt  string  `json:"createdAt"`
	CreatedBy  Actor   `json:"createdBy"`
	ProposedBy string  `json:"proposedBy,omitempty"`
	// Revoked is null when the grant is live.
	Revoked  *RevokedView `json:"revoked"`
	Active   bool         `json:"active"`
	Describe string       `json:"describe"`
}

// EffectiveAction is one registry action's entry in the effective view: the
// verdict an unattended loop started now would get, why, the grant behind
// it, and the grant's action-time conditions (met / unmet / cannot-say —
// the three states a view that cannot show unknown will show wrong).
type EffectiveAction struct {
	Action     string             `json:"action"`
	Class      string             `json:"class"`
	Verdict    string             `json:"verdict"`
	Because    string             `json:"because"`
	GrantID    string             `json:"grantId,omitempty"`
	Conditions []EffectiveCond    `json:"conditions,omitempty"`
}

// EffectiveCond is one action-time condition's status in the effective view.
type EffectiveCond struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Sentence string `json:"sentence,omitempty"`
}

// EffectiveView is the answer to "what could an unattended loop on this
// root do, started now" (AL14): the per-action plan and the statement that
// says it plainly.
type EffectiveView struct {
	RootID      string            `json:"rootId"`
	Origin      string            `json:"origin"`
	EvaluatedAt string            `json:"evaluatedAt"`
	Actions     []EffectiveAction `json:"actions"`
	Statement   string            `json:"statement"`
}

// LiveStatus is a grant's live status as a person reads it (AL6/AL17): the
// closed trio "active" | "revoked" | "expired" and the sentence that says
// what it is — the SAME sentences the decision site speaks when it denies.
type LiveStatus struct {
	State    string `json:"state"`
	Sentence string `json:"sentence"`
}

// PinnedGrantView is one pin entry: the action the snapshot covers, the
// grant behind it, and the grant's live status.
type PinnedGrantView struct {
	GrantID string     `json:"grantId"`
	Subject string     `json:"subject"`
	Live    LiveStatus `json:"live"`
}

// PinnedAction is one registry action's verdict for a pinned loop (AL9's
// decision, rendered: what would happen at the next action).
type PinnedAction struct {
	Action  string `json:"action"`
	Verdict string `json:"verdict"`
	Because string `json:"because"`
}

// PinnedView is what a loop was pinned with, and whether it is still live
// (AL8). A loop with no pin row is {pinned:false, statement} — the honest
// "started with no autonomy", byte-identical to a no-grant loop.
type PinnedView struct {
	LoopID    string              `json:"loopId"`
	Pinned    bool                `json:"pinned"`
	RootID    string              `json:"rootId,omitempty"`
	Rev       int64               `json:"rev,omitempty"`
	Origin    string              `json:"origin,omitempty"`
	Labels    []string            `json:"labels,omitempty"`
	Grants    []PinnedGrantView   `json:"grants,omitempty"`
	Actions   []PinnedAction      `json:"actions,omitempty"`
	Statement string              `json:"statement,omitempty"`
}

// ChangeView is one audit row (AL8/AL10): the one-sentence Summary is a
// fixed template from ids and enums (SEC-033) — never the note or reason.
type ChangeView struct {
	ID         int64  `json:"id"`
	At         string `json:"at"`
	RootID     string `json:"rootId"`
	GrantID    string `json:"grantId"`
	ChangeKind string `json:"changeKind"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	ProposedBy string `json:"proposedBy,omitempty"`
	Summary    string `json:"summary"`
}

// UseView is one use-log row with its outcome PAIRED in: a begun row carries
// the outcome of the completed/failed row that closed it, or the fixed
// "unknown" sentence when the process stopped before recording one (the
// receipt-first shape made readable). A completed/failed row is an outcome,
// not a use — its outcome is null.
type UseView struct {
	ID       int64   `json:"id"`
	At       string  `json:"at"`
	LoopID   string  `json:"loopId"`
	GrantID  string  `json:"grantId"`
	GrantRev int64   `json:"grantRev"`
	Action   string  `json:"action"`
	Phase    string  `json:"phase"`
	BegunID  *int64  `json:"begunId"`
	Outcome  *string `json:"outcome"`
}

// The unpaired-begun outcome is one fixed sentence (AL10): a crash leaves a
// begun with no outcome, and the view says "effect may have happened"
// rather than hiding it.
const UseOutcomeUnknown = "unknown — the process stopped before the result was recorded"

// --- the vocabulary (AL15) ---------------------------------------------------

// VocabularySubject is one subject a form may offer: read from the action
// registry, never re-typed (AL2).
type VocabularySubject struct {
	Kind             string   `json:"kind"`
	Name             string   `json:"name"`
	Class            string   `json:"class"`
	Label            string   `json:"label,omitempty"`
	RequiresExpiry   bool     `json:"requiresExpiry"`
	ForcedConditions []string `json:"forcedConditions,omitempty"`
	Covers           []string `json:"covers,omitempty"`
}

// VocabularyCondition is one condition a form may render. Description is the
// plain-English help (the wire name; the pure package calls it Help).
type VocabularyCondition struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Evaluated   string `json:"evaluated"`
	Forced      bool   `json:"forced"`
}

// VocabularyLimits is the closed bounds a form and the validator share (AL15).
type VocabularyLimits struct {
	MaxMergeGrantDays int `json:"maxMergeGrantDays"`
	MaxLabels         int `json:"maxLabels"`
	MaxLabelLen       int `json:"maxLabelLen"`
	MaxNoteBytes      int `json:"maxNoteBytes"`
	MaxReasonBytes    int `json:"maxReasonBytes"`
}

// VocabularyOrigin is one start origin a form may name, with its help.
type VocabularyOrigin struct {
	Origin string `json:"origin"`
	Help   string `json:"help"`
}

// VocabularyView is everything a form needs so a client never types a closed
// set (AL15). The result is the view at top level — no wrapper.
type VocabularyView struct {
	Subjects   []VocabularySubject   `json:"subjects"`
	Conditions []VocabularyCondition `json:"conditions"`
	Limits     VocabularyLimits      `json:"limits"`
	Origins    []VocabularyOrigin    `json:"origins"`
	States     []string              `json:"states"`
}

// --- params and results -----------------------------------------------------

// AutonomyListParams is list's params: every filter optional ("" = all).
type AutonomyListParams struct {
	RootID          *string `json:"rootId,omitempty"`
	State           *string `json:"state,omitempty"`
	IncludeInactive *bool   `json:"includeInactive,omitempty"`
}

// AutonomyListResult is list's result.
type AutonomyListResult struct {
	Grants []GrantView `json:"grants"`
}

// AutonomyShowParams is show's params.
type AutonomyShowParams struct {
	ID string `json:"id"`
}

// AutonomyShowResult is show's result: the grant, its last change, the count
// of begun uses, and the running loops pinned with it.
type AutonomyShowResult struct {
	Grant       GrantView   `json:"grant"`
	LastChange  *ChangeView `json:"lastChange"`
	UsedCount   int         `json:"usedCount"`
	PinnedLoops []string    `json:"pinnedLoops"`
}

// AutonomyEffectiveParams is effective's params. Origin absent means
// "schedule" (a cosmetic difference in the plan, AL7); labels is the start's
// label set the pin-time conditions would match against.
type AutonomyEffectiveParams struct {
	RootID string   `json:"rootId"`
	Origin *string  `json:"origin,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

// AutonomyPinnedParams is pinned's params.
type AutonomyPinnedParams struct {
	LoopID string `json:"loopId"`
}

// AutonomyChangesParams is changes's params.
type AutonomyChangesParams struct {
	RootID  *string `json:"rootId,omitempty"`
	GrantID *string `json:"grantId,omitempty"`
	Limit   *int    `json:"limit,omitempty"`
}

// AutonomyChangesResult is changes's result (the store's order, oldest first).
type AutonomyChangesResult struct {
	Changes []ChangeView `json:"changes"`
}

// AutonomyUsesParams is uses's params.
type AutonomyUsesParams struct {
	RootID  *string `json:"rootId,omitempty"`
	GrantID *string `json:"grantId,omitempty"`
	LoopID  *string `json:"loopId,omitempty"`
	Limit   *int    `json:"limit,omitempty"`
}

// AutonomyUsesResult is uses's result, with outcomes paired in.
type AutonomyUsesResult struct {
	Uses []UseView `json:"uses"`
}

// AutonomyGrantParams is grant's params. Subject is a registry action name
// or the one class shorthand (vocabulary says which is which); conditions is
// a pointer for the absent-vs-present distinction (AL4's whole-object
// defaulting); expiresAt is RFC3339, absent = no expiry.
type AutonomyGrantParams struct {
	RootID     string              `json:"rootId"`
	Subject    string              `json:"subject"`
	Conditions *AutonomyConditions `json:"conditions,omitempty"`
	ExpiresAt  *string             `json:"expiresAt,omitempty"`
	Note       string              `json:"note,omitempty"`
	DryRun     bool                `json:"dryRun,omitempty"`
}

// AutonomyDenyParams is deny's params. NodeID (not rootId): a veto may sit
// on a root or a child (OD4).
type AutonomyDenyParams struct {
	NodeID  string `json:"nodeId"`
	Subject string `json:"subject"`
	Note    string `json:"note,omitempty"`
	DryRun  bool   `json:"dryRun,omitempty"`
}

// AutonomyRevokeParams is revoke's params. Reason is required — a revocation
// without one cannot be audited.
type AutonomyRevokeParams struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	DryRun bool   `json:"dryRun,omitempty"`
}

// AutonomyGrantResult is grant's result: the written grant (null on a dry
// run — nothing was written, so there is no row) and the daemon's own
// confirmation sentence.
type AutonomyGrantResult struct {
	Grant        *GrantView `json:"grant"`
	Confirmation string     `json:"confirmation"`
}

// AutonomyDenyResult is deny's result, the grant result's shape.
type AutonomyDenyResult struct {
	Grant        *GrantView `json:"grant"`
	Confirmation string     `json:"confirmation"`
}

// AutonomyRevokeResult is revoke's result: the revoked grant (null on a dry
// run), the confirmation, and the running loops the authority was pulled
// from (AL: the caller stops them through S55's loop.stop — AL does not
// stop loops itself, OD8).
type AutonomyRevokeResult struct {
	Grant         *GrantView `json:"grant"`
	Confirmation  string     `json:"confirmation"`
	AffectedLoops []string   `json:"affectedLoops"`
}

// --- the events -------------------------------------------------------------
//
// The four autonomy authority events (R5): granted/denied/revoked are
// published by the handlers AFTER the store commits (at-most-once, as
// task.changed); used is published by the loop's decision site after the use
// row is recorded. Payloads carry ids and enums only — the rows are the
// source of truth. No notify hint: the closed set in the class table is S58's
// to widen; AL publishes events only.

// AutonomyChangeEvent is the granted/denied/revoked payload: the grant id
// and the root it sits under. The subscriber reads the full row through
// autonomy.show / autonomy.changes; the event is the signal, not the record.
type AutonomyChangeEvent struct {
	GrantID string `json:"grantId"`
	RootID  string `json:"rootId"`

	componentRoot string
}

// EventComponentRoot marks the payload as rooted — a grant sits on a
// component, so the notification belongs to that workspace's root.
func (p AutonomyChangeEvent) EventComponentRoot() string { return p.componentRoot }

// AutonomyUsedEvent is the used payload: which loop took which action under
// which grant, and the phase (begun/completed/failed).
type AutonomyUsedEvent struct {
	LoopID  string `json:"loopId"`
	GrantID string `json:"grantId"`
	Action  string `json:"action"`
	Phase   string `json:"phase"`
	BegunID int64  `json:"begunId"`

	componentRoot string
}

// EventComponentRoot files the event under the loop's workspace root.
func (p AutonomyUsedEvent) EventComponentRoot() string { return p.componentRoot }

// UsedEventFor builds the used payload the loop's decision site publishes
// after a completed/failed use row is RECORDED (AL10: the event is the
// signal of a row that exists — a write that failed publishes nothing,
// because an event for a row that is not there is a second truth). The
// component root is the loop's OWN project root (S31 D1: a loop's events
// are appended with the loop row's real root, so a subscriber filtered by
// Filter{ComponentRoot} gets one project's uses and not the machine's),
// not the repository the loop acts on.
func UsedEventFor(loopID, grantID, action, phase string, begunID int64, componentRoot string) AutonomyUsedEvent {
	return AutonomyUsedEvent{LoopID: loopID, GrantID: grantID, Action: action, Phase: phase, BegunID: begunID, componentRoot: componentRoot}
}

// --- registration -----------------------------------------------------------

// RegisterAutonomy registers the ten autonomy.* methods over the wired seam.
// It rides with the wiring rather than NewTable because the seam is assigned
// after construction (the settings precedent), and a registration that ran
// inside NewTable would only ever see nil. The nil seam is the V13
// configuration speaking — a no-store build arrives here with nil and the
// ten methods stay absent (not registered-and-refusing); that is the
// designed outcome, not a silent failure. Returns whether the methods are
// in the table, so the daemon's wiring can be asserted rather than trusted.
func (t *Table) RegisterAutonomy() bool {
	if t.Autonomy == nil {
		return false
	}
	t.register(&Method{Name: MethodAutonomyList, RequiresInitialize: true, Handle: t.handleAutonomyList})
	t.register(&Method{Name: MethodAutonomyShow, RequiresInitialize: true, Handle: t.handleAutonomyShow})
	t.register(&Method{Name: MethodAutonomyVocabulary, RequiresInitialize: true, Handle: t.handleAutonomyVocabulary})
	t.register(&Method{Name: MethodAutonomyEffective, RequiresInitialize: true, Handle: t.handleAutonomyEffective})
	t.register(&Method{Name: MethodAutonomyPinned, RequiresInitialize: true, Handle: t.handleAutonomyPinned})
	t.register(&Method{Name: MethodAutonomyChanges, RequiresInitialize: true, Handle: t.handleAutonomyChanges})
	t.register(&Method{Name: MethodAutonomyUses, RequiresInitialize: true, Handle: t.handleAutonomyUses})
	t.register(&Method{Name: MethodAutonomyGrant, RequiresInitialize: true, Handle: t.handleAutonomyGrant})
	t.register(&Method{Name: MethodAutonomyDeny, RequiresInitialize: true, Handle: t.handleAutonomyDeny})
	t.register(&Method{Name: MethodAutonomyRevoke, RequiresInitialize: true, Handle: t.handleAutonomyRevoke})
	return true
}

// --- the identity door ------------------------------------------------------
//
// AL11's three locks: (1) no agent tool twinned to a write (T7's canaries);
// (2) the socket refuses an actor whose kind is not `user` — the check
// below, which the daemon's ActorFor makes a test seam in production (it
// always answers kind "user"); (3) the store refuses before any SQL. A
// params smuggle of an identity is refused by checkAutonomyParams, because
// the actor is the KERNEL's, not a claim: a params actor would let a caller
// forge the audit identity of a write to their own rules.

// autonomyIdentityKeys are the keys a params object may NEVER name on an
// autonomy method. The rest of the protocol ignores unknown fields (T3) —
// this is the deliberate exception for exactly the five identity keys: an
// unknown FIELD is tolerated, an unknown ACTOR is not.
var autonomyIdentityKeys = []string{"actor", "by", "createdBy", "status", "source"}

// checkAutonomyParams refuses a params object that names an identity key.
// It reads the raw params as a key bag (the decodeAgentImportPayload
// pattern): decodeParams tolerates unknown fields, which is right for a
// field from the future and wrong for a claim of identity.
func checkAutonomyParams(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return ErrBadParams{Err: err}
	}
	for _, k := range autonomyIdentityKeys {
		if _, ok := bag[k]; ok {
			return ErrBadParams{Err: fmt.Errorf(
				"%q is not a parameter — the actor comes from the connection's peer credential, never from params (AL11)", k)}
		}
	}
	return nil
}

// autonomyActor resolves the write's audit identity from the CONNECTION
// (AL11 lock 2). A connection with no actor seam, an empty id, or a kind
// that is not `user` is refused with a typed sentence — a peer that cannot
// be resolved refuses, it is never a placeholder. The returned Actor is
// `user:<uid>`, the kernel's attestation.
func (t *Table) autonomyActor(ctx context.Context, sess *Session) (Actor, *AutonomyRefusal) {
	if t.ActorFor == nil {
		return Actor{}, &AutonomyRefusal{Code: "actor-unavailable",
			Sentence: "refused: an autonomy write needs a person on the connection, and this one carries no identity"}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, &AutonomyRefusal{Code: "actor-anonymous",
			Sentence: "refused: an autonomy write needs a named actor — an anonymous author cannot be audited (AU1)"}
	}
	if a.Kind != "user" {
		return Actor{}, &AutonomyRefusal{Code: "not-a-person",
			Sentence: fmt.Sprintf("refused: only a user may write autonomy — caller kind %q is not a person (AL11 lock 2)", a.Kind)}
	}
	return a, nil
}

// autonomyRefused renders the seam's refusal as the protocol's typed error:
// the code -31833, the sentence verbatim in `what` (%s — a sentence with a
// % in it must not be format-mangled).
func autonomyRefused(ref *AutonomyRefusal) error {
	return ErrAutonomyRefused.WithWhat("%s", ref.Sentence)
}

// autonomyTripwire is the unreachable guard: registration is conditional on
// this same field, so a handler that sees nil means registration and the
// seam disagreed — spoken, not silent (the settings shape, V13).
func (t *Table) autonomyTripwire() error {
	return ErrStoreUnavailable.WithWhat(
		"autonomy.* needs the autonomy store, and this build did not link it — the autonomy surface is absent for the same reason (V13)")
}

// --- the handlers -----------------------------------------------------------

func (t *Table) handleAutonomyList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p AutonomyListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	// A typo'd state would answer an empty list that reads as "no grants" —
	// a plausible wrong answer, so the closed set is checked here.
	state := ""
	if p.State != nil {
		state = *p.State
		if !autonomyStates[state] {
			return nil, ErrBadParams{Err: fmt.Errorf("state must be \"allow\" or \"deny\", got %q", state)}
		}
	}
	includeInactive := false
	if p.IncludeInactive != nil {
		includeInactive = *p.IncludeInactive
	}
	rootID := ""
	if p.RootID != nil {
		rootID = *p.RootID
	}
	grants, err := t.Autonomy.List(ctx, rootID, state, includeInactive)
	if err != nil {
		return nil, err
	}
	return AutonomyListResult{Grants: grants}, nil
}

func (t *Table) handleAutonomyShow(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p AutonomyShowParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the grant to read")}
	}
	view, lastChange, usedCount, pinnedLoops, ref, err := t.Autonomy.Show(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, autonomyRefused(ref)
	}
	return AutonomyShowResult{
		Grant:       view,
		LastChange:  lastChange,
		UsedCount:   usedCount,
		PinnedLoops: pinnedLoops,
	}, nil
}

func (t *Table) handleAutonomyVocabulary(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p struct{}
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	v, err := t.Autonomy.Vocabulary()
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (t *Table) handleAutonomyEffective(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p AutonomyEffectiveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.RootID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("rootId is required — name the root the unattended loop would start on")}
	}
	origin := OriginSchedule // the default a start without an origin is (cosmetic in the plan, AL7)
	if p.Origin != nil {
		origin = *p.Origin
		if !autonomyOrigins[origin] {
			return nil, ErrBadParams{Err: fmt.Errorf("origin must be one of %q, %q, %q, %q, got %q",
				OriginSchedule, OriginWebhook, OriginEvent, OriginScheduler, origin)}
		}
	}
	view, ref, err := t.Autonomy.Effective(ctx, p.RootID, origin, p.Labels)
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, autonomyRefused(ref)
	}
	return view, nil
}

func (t *Table) handleAutonomyPinned(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p AutonomyPinnedParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.LoopID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("loopId is required — name the loop to read the pin of")}
	}
	view, ref, err := t.Autonomy.Pinned(ctx, p.LoopID)
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, autonomyRefused(ref)
	}
	return view, nil
}

func (t *Table) handleAutonomyChanges(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p AutonomyChangesParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	limit := 0
	if p.Limit != nil {
		if *p.Limit < 0 {
			return nil, ErrBadParams{Err: fmt.Errorf("limit must be non-negative — an absent limit means every row")}
		}
		limit = *p.Limit
	}
	rootID := ""
	if p.RootID != nil {
		rootID = *p.RootID
	}
	grantID := ""
	if p.GrantID != nil {
		grantID = *p.GrantID
	}
	changes, err := t.Autonomy.Changes(ctx, rootID, grantID, limit)
	if err != nil {
		return nil, err
	}
	return AutonomyChangesResult{Changes: changes}, nil
}

func (t *Table) handleAutonomyUses(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p AutonomyUsesParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	limit := 0
	if p.Limit != nil {
		if *p.Limit < 0 {
			return nil, ErrBadParams{Err: fmt.Errorf("limit must be non-negative — an absent limit means every row")}
		}
		limit = *p.Limit
	}
	rootID := ""
	if p.RootID != nil {
		rootID = *p.RootID
	}
	grantID := ""
	if p.GrantID != nil {
		grantID = *p.GrantID
	}
	loopID := ""
	if p.LoopID != nil {
		loopID = *p.LoopID
	}
	uses, err := t.Autonomy.Uses(ctx, rootID, grantID, loopID, limit)
	if err != nil {
		return nil, err
	}
	return AutonomyUsesResult{Uses: uses}, nil
}

func (t *Table) handleAutonomyGrant(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p AutonomyGrantParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.RootID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("rootId is required — an allow sits on a root component")}
	}
	if strings.TrimSpace(p.Subject) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("subject is required — name a registry action, or the outward class (autonomy.vocabulary lists them)")}
	}
	var expiresAt *time.Time
	if p.ExpiresAt != nil && strings.TrimSpace(*p.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, *p.ExpiresAt)
		if err != nil {
			return nil, ErrBadParams{Err: fmt.Errorf("expiresAt must be RFC3339 (e.g. 2026-12-01T00:00:00Z), got %q", *p.ExpiresAt)}
		}
		expiresAt = &parsed
	}
	// The conditions defaulting is WHOLE-OBJECT (AL4): an absent key is the
	// default set, a present object — even {} — is the caller's values
	// verbatim. The pointer is what holds the distinction.
	conds := AutonomyConditions{RequireBudget: true}
	if p.Conditions != nil {
		conds = *p.Conditions
	}
	actor, ref := t.autonomyActor(ctx, sess)
	if ref != nil {
		return nil, autonomyRefused(ref)
	}
	out, ref, err := t.Autonomy.Grant(ctx, actor, AutonomyGrantRequest{
		NodeID:     p.RootID,
		Subject:    p.Subject,
		Conditions: conds,
		ExpiresAt:  expiresAt,
		Note:       p.Note,
		DryRun:     p.DryRun,
	})
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, autonomyRefused(ref)
	}
	if out.Changed {
		t.publish(KindAutonomyGranted, AutonomyChangeEvent{
			GrantID:       out.View.ID,
			RootID:        out.View.RootID,
			componentRoot: out.ComponentRoot,
		})
	}
	return AutonomyGrantResult{Grant: out.View, Confirmation: out.Confirmation}, nil
}

func (t *Table) handleAutonomyDeny(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p AutonomyDenyParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.NodeID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("nodeId is required — a veto sits on a root or a child")}
	}
	if strings.TrimSpace(p.Subject) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("subject is required — name a registry action, or the outward class (autonomy.vocabulary lists them)")}
	}
	actor, ref := t.autonomyActor(ctx, sess)
	if ref != nil {
		return nil, autonomyRefused(ref)
	}
	out, ref, err := t.Autonomy.Deny(ctx, actor, AutonomyDenyRequest{
		NodeID:  p.NodeID,
		Subject: p.Subject,
		Note:    p.Note,
		DryRun:  p.DryRun,
	})
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, autonomyRefused(ref)
	}
	if out.Changed {
		t.publish(KindAutonomyDenied, AutonomyChangeEvent{
			GrantID:       out.View.ID,
			RootID:        out.View.RootID,
			componentRoot: out.ComponentRoot,
		})
	}
	return AutonomyDenyResult{Grant: out.View, Confirmation: out.Confirmation}, nil
}

func (t *Table) handleAutonomyRevoke(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Autonomy == nil {
		return nil, t.autonomyTripwire()
	}
	var p AutonomyRevokeParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := checkAutonomyParams(params); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the grant to revoke")}
	}
	if strings.TrimSpace(p.Reason) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("reason is required — a revocation without one cannot be audited")}
	}
	actor, ref := t.autonomyActor(ctx, sess)
	if ref != nil {
		return nil, autonomyRefused(ref)
	}
	out, ref, err := t.Autonomy.Revoke(ctx, actor, AutonomyRevokeRequest{
		ID:     p.ID,
		Reason: p.Reason,
		DryRun: p.DryRun,
	})
	if err != nil {
		return nil, err
	}
	if ref != nil {
		return nil, autonomyRefused(ref)
	}
	if out.Changed {
		t.publish(KindAutonomyRevoked, AutonomyChangeEvent{
			GrantID:       out.View.ID,
			RootID:        out.View.RootID,
			componentRoot: out.ComponentRoot,
		})
	}
	return AutonomyRevokeResult{Grant: out.View, Confirmation: out.Confirmation, AffectedLoops: out.Affected}, nil
}
