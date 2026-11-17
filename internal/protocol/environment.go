package protocol

// environment.go — S59 T6 (EV2, EV16, EV17, EV18): the environment doors.
//
// Thirteen methods, one seam. The six reads (list, show, effective, vocabulary,
// changes, provisions) and the seven human-door writes (create, update,
// archive, restore, bind, unbind, teardown) are registered together over the
// daemon's ONE environment service, or not at all: a nil seam is the
// configuration speaking and the methods are ABSENT, never registered-and-
// refusing (V13, the RegisterBackup/RegisterSeed/RegisterVerify shape). All
// thirteen are `RequiresInitialize` and none is `Debug`.
//
// Two rules the wire shape carries, not a convention:
//
//   - No identity in params. `actor`, `origin`, `legacy` and (for environments)
//     `source` are refused from the raw key bag before the lenient decode — the
//     actor is the CONNECTION's person (`Table.ActorFor`, the kernel's peer
//     credential), the origin is the seed's or the door's, and the legacy flag
//     belongs to the migration. A param naming one would let a caller forge
//     provenance (EV17).
//   - No value, ever. There is no field anywhere in this file that could hold a
//     secret value, and `secretref.go` is held to the same shape by a reflection
//     guard (`TestNoEnvironmentParamCarriesAValue`, EV7): a leak is not
//     something a forgotten call site can do because there is nowhere to put one.
//
// The refusals are the plane's one voice, `ErrEnvironmentRefused` (what/why/do,
// DB19): the daemon service fills `what` with the sentence its validator or the
// store actually spoke, so nothing paraphrases a refusal between the caller and
// the truth.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// --- the method names -------------------------------------------------------------

const (
	// The six reads. `effective` is the one a person reads before they trust a
	// loop: what a step on that component would actually run in, as names.
	MethodEnvironmentList       MethodName = "environment.list"
	MethodEnvironmentShow       MethodName = "environment.show"
	MethodEnvironmentEffective  MethodName = "environment.effective"
	MethodEnvironmentVocabulary MethodName = "environment.vocabulary"
	MethodEnvironmentChanges    MethodName = "environment.changes"
	MethodEnvironmentProvisions MethodName = "environment.provisions"

	// The seven human-door writes (EV16): socket only, absent from the narrow
	// projection, the agent services, the tool registry and the records catalog
	// — the four caller-classes that are not the person.
	MethodEnvironmentCreate   MethodName = "environment.create"
	MethodEnvironmentUpdate   MethodName = "environment.update"
	MethodEnvironmentArchive  MethodName = "environment.archive"
	MethodEnvironmentRestore  MethodName = "environment.restore"
	MethodEnvironmentBind     MethodName = "environment.bind"
	MethodEnvironmentUnbind   MethodName = "environment.unbind"
	MethodEnvironmentTeardown MethodName = "environment.teardown"

	// MethodEnvironmentProposeRestrict (S59 T7) is the plane's ONE agent door
	// and the only `environment.*` name an agent may reach: it files the INERT
	// environment_restrict proposal a person commits (EV16 — strength goes up
	// by a person alone). It is named here, beside its siblings, so one grep
	// over this block lists every door the plane has; its registration lives in
	// environment_propose.go because it is conditional on the records service,
	// and its guard is cmd/aeon/environment_propose_guard_test.go. It is NOT in
	// environmentAllMethods() — that list is the twenty HUMAN doors, and this
	// one is answered by the records service, not the environment seam.
	MethodEnvironmentProposeRestrict MethodName = "environment.proposeRestrict"
)

// --- the params -------------------------------------------------------------------

// EnvironmentListParams is environment.list's request. Every filter is
// optional; an empty filter is "everything not archived", never "nothing".
type EnvironmentListParams struct {
	Backend         string `json:"backend,omitempty"`
	Origin          string `json:"origin,omitempty"`
	IncludeArchived bool   `json:"includeArchived,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

// EnvironmentShowParams is environment.show's request: one environment by id
// or by slug. Both empty is a refusal; both set must agree.
type EnvironmentShowParams struct {
	ID   string `json:"id,omitempty"`
	Slug string `json:"slug,omitempty"`
}

// EnvironmentEffectiveParams is environment.effective's request. ComponentID
// names the component whose next step is being asked about; empty asks for the
// workspace default.
//
// There is deliberately NO attended field. Attendedness decides EV6's note —
// whether a legacy binding inherits the daemon's credentials — and G14's law
// (internal/loop/allowoutward_test.go, TestQ4Recorded) is that it is declared
// by a caller at exactly three doors: loop.start, the queue's append, the
// roadmap's dispatch. A preview door that accepted `attended` would be a fourth
// place to set it, and the one thing a preview must never be able to do is let
// the person at the keyboard choose the answer that shows them more privilege
// than the spawn will give. The daemon derives it (below) and answers it in the
// VIEW, where it is a statement about a loop and not a permission.
//
// A client that still sends `attended` is ignored, not obeyed: the field is
// gone from the wire shape, and the conformance fixture for this door carries
// no attended key in its request.
type EnvironmentEffectiveParams struct {
	ComponentID string `json:"componentId,omitempty"`
}

// EnvironmentChangesParams is environment.changes' request — the append-only
// audit (EV17). Subject is optional: an empty filter is the whole log, and the
// answer distinguishes "no rows" from "no such subject" in its own words.
type EnvironmentChangesParams struct {
	SubjectKind string `json:"subjectKind,omitempty"`
	SubjectID   string `json:"subjectId,omitempty"`
	Action      string `json:"action,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// EnvironmentProvisionsParams is environment.provisions' request: the
// container/vm ledger, optionally narrowed to one state.
type EnvironmentProvisionsParams struct {
	State string `json:"state,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// EnvironmentCreateParams is environment.create's request. Spec is the
// environment's YAML (or JSON — the same parser) as the person wrote it; the
// daemon validates it through the one validator (`execenv.Validate`) and
// stores the CANONICAL JSON, so what a later read returns is what the
// validator approved, byte-stable (EV4). Origin and legacy are not here: a
// door cannot author a system row (EV19) or resurrect the inherit row.
type EnvironmentCreateParams struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Backend     string `json:"backend"`
	Spec        string `json:"spec"`
}

// EnvironmentUpdateParams is environment.update's request: a new spec for an
// existing environment, written only if the row still carries
// `ExpectedRevision` (the CAS the whole plane writes with — a stale revision
// refuses rather than clobbering a bind somebody else just tightened).
type EnvironmentUpdateParams struct {
	ID               string `json:"id"`
	Spec             string `json:"spec"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// EnvironmentIDParams is environment.archive's and environment.restore's
// request: one environment by id or slug.
type EnvironmentIDParams struct {
	ID   string `json:"id,omitempty"`
	Slug string `json:"slug,omitempty"`
}

// EnvironmentBindParams is environment.bind's request. Scope is "workspace" or
// "component"; a component scope names ComponentID and a workspace scope must
// not. DryRun answers the daemon's own statement of what would change and
// writes nothing — the statement is the same text the applied bind answers
// (EV18, `execenv.DescribeBind`), which is what makes the preview worth
// reading.
type EnvironmentBindParams struct {
	Scope         string `json:"scope"`
	ComponentID   string `json:"componentId,omitempty"`
	EnvironmentID string `json:"environmentId"`
	DryRun        bool   `json:"dryRun,omitempty"`
}

// EnvironmentUnbindParams is environment.unbind's request. Only a component
// can be unbound: the workspace binding is the floor the seed set laid (EV19)
// and unbinding it would hand every unbound loop the legacy inherit, so the
// door refuses that ask with a sentence rather than doing it.
type EnvironmentUnbindParams struct {
	ComponentID string `json:"componentId"`
}

// ProvisionIDParams is environment.teardown's request: one provision row whose
// backend container must be removed.
type ProvisionIDParams struct {
	ProvisionID string `json:"provisionId"`
}

// --- the views --------------------------------------------------------------------

// EnvironmentView is one environment as a person reads it: the row's own
// fields, the backend's capability chips, the validator's warnings (D8's
// floating tag lives here, never as a refusal), the secret references it
// injects BY NAME, and where it is bound. There is no field for a value: the
// spec is names and literals only (EV17), and a secret reference is a pointer
// to a source, not a credential.
type EnvironmentView struct {
	ID             string          `json:"id"`
	Slug           string          `json:"slug"`
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	Backend        string          `json:"backend"`
	Origin         string          `json:"origin"`
	Legacy         bool            `json:"legacy"`
	Revision       int64           `json:"revision"`
	Spec           json.RawMessage `json:"spec"`
	CreatedAt      string          `json:"createdAt"`
	UpdatedAt      string          `json:"updatedAt"`
	ArchivedAt     *string         `json:"archivedAt,omitempty"`
	Capabilities   []string        `json:"capabilities"`
	Warnings       []string        `json:"warnings,omitempty"`
	SecretRefNames []string        `json:"secretRefNames,omitempty"`
	Bindings       []BindingView   `json:"bindings,omitempty"`
}

// BindingView is one binding row: which scope is pinned to which environment.
type BindingView struct {
	ScopeKind     string `json:"scopeKind"`
	ComponentID   string `json:"componentId,omitempty"`
	EnvironmentID string `json:"environmentId"`
	BoundAt       string `json:"boundAt"`
}

// VariableView is one variable a step would see: its name and WHERE it comes
// from, never its value (EV5, EV7). `source` is one of the closed verbs the
// daemon speaks — "allowlist", "set", "injected:<sourceKind>".
type VariableView struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// EnvironmentEffectiveView is environment.effective's answer: what a step on that
// component would actually run in. Names, sources, capabilities and the
// downgrade note — no value appears anywhere in this type, and a scanner test
// (T6's `TestEffectiveCarriesNamesAndSourcesNeverValues`) proves it on the
// wire rather than trusting the field list.
type EnvironmentEffectiveView struct {
	ComponentID     string `json:"componentId,omitempty"`
	ScopeKind       string `json:"scopeKind"`
	EnvironmentID   string `json:"environmentId,omitempty"`
	EnvironmentSlug string `json:"environmentSlug,omitempty"`
	Backend         string `json:"backend"`
	Legacy          bool   `json:"legacy"`
	// Attends is the loop's attendedness, ANSWERED by the daemon and never
	// accepted from a caller (see EnvironmentEffectiveParams and G14). It is
	// spelled Attends, not Attended, on purpose: internal/loop's
	// TestQ4Recorded pins attendedness as caller-settable at exactly three
	// doors by looking for that field name beside that json tag, and a view
	// field must not read like a fourth door. The wire name stays "attended".
	Attends        bool           `json:"attended"`
	Capabilities   []string       `json:"capabilities"`
	Variables      []VariableView `json:"variables"`
	SecretRefNames []string       `json:"secretRefNames,omitempty"`
	DowngradeNote  string         `json:"downgradeNote,omitempty"`
	Honesty        string         `json:"honesty"`
}

// EnvironmentChangeView is one audit row (EV17): who, when, what they did to
// which record, and the spec that resulted. `specAfter` is names and literals
// only — no value can be in it because none is accepted (EV7).
type EnvironmentChangeView struct {
	ID          int64           `json:"id"`
	At          string          `json:"at"`
	ActorKind   string          `json:"actorKind"`
	ActorID     string          `json:"actorId"`
	SubjectKind string          `json:"subjectKind"`
	SubjectID   string          `json:"subjectId"`
	Action      string          `json:"action"`
	Revision    *int64          `json:"revision,omitempty"`
	SpecAfter   json.RawMessage `json:"specAfter,omitempty"`
	Summary     string          `json:"summary"`
}

// ProvisionView is one row of the provision ledger. `lastError` is the
// backend's own sentence, kept for the person to read; it never carries a
// secret because the command lines that produced it were redacted at capture.
type ProvisionView struct {
	ID            string  `json:"id"`
	EnvironmentID string  `json:"environmentId"`
	ComponentRoot string  `json:"componentRoot"`
	LoopSlug      string  `json:"loopSlug,omitempty"`
	Backend       string  `json:"backend"`
	Handle        string  `json:"handle,omitempty"`
	Owner         string  `json:"owner,omitempty"`
	State         string  `json:"state"`
	CreatedAt     string  `json:"createdAt"`
	ReadyAt       *string `json:"readyAt,omitempty"`
	RemovedAt     *string `json:"removedAt,omitempty"`
	LastError     *string `json:"lastError,omitempty"`
}

// EnvironmentVocabularyView is environment.vocabulary's answer: everything a
// client would otherwise retype or guess — the backends and what each one
// honestly does, the default allowlist, the deny list and its patterns, the
// egress modes, the source kinds, and the two honesty lines. The daemon fills
// it from `execenv` (one definition of each), and the panel renders it
// verbatim instead of paraphrasing a capability list (T11).
type EnvironmentVocabularyView struct {
	Backends         []BackendView `json:"backends"`
	DefaultAllowlist []string      `json:"defaultAllowlist"`
	// The deny list in the shapes it actually has: a name a person is refused
	// for is answered by the rule that refused it, not by one merged list a
	// client would have to re-partition to explain.
	DenyWords         []string `json:"denyWords"`
	DenyPrefixes      []string `json:"denyPrefixes"`
	DenyExact         []string `json:"denyExact"`
	DenyLoader        []string `json:"denyLoader"`
	DenyExempt        []string `json:"denyExempt,omitempty"`
	DenySetOnly       []string `json:"denySetOnly,omitempty"`
	ProxyWarnPatterns []string `json:"proxyWarnPatterns"`
	EgressModes       []string `json:"egressModes"`
	SourceKinds       []string `json:"sourceKinds"`
	PullPolicies      []string `json:"pullPolicies"`
	HomeModes         []string `json:"homeModes"`
	Honesty           string   `json:"honesty"`
	RedactionLimits   string   `json:"redactionLimits"`

	// The limitations the platform knows about, said in the daemon's words,
	// with whether the daemon asserts each is true RIGHT NOW (EV15). The panel
	// prints these verbatim and holds no status of its own, so a defect that
	// gets fixed stops being announced the day it is fixed instead of living on
	// in a client's string literal.
	KnownLimits []KnownLimitView `json:"knownLimits,omitempty"`

	// The provision sweep interval (EV12), from the DECLARED setting
	// (S44's execenv.provisionIdleTTL) rather than a client's guess. `…From`
	// carries the three states — "setting", "declared-default",
	// "unreadable-default" — because a panel printing "24h" cannot otherwise
	// tell a person "that is what is set" from "that is what runs because the
	// row could not be read" (P3). `…Note` is the sentence explaining the
	// third state, verbatim from the daemon.
	ProvisionIdleTTL        string `json:"provisionIdleTtl"`
	ProvisionIdleTTLSeconds int64  `json:"provisionIdleTtlSeconds"`
	ProvisionIdleTTLFrom    string `json:"provisionIdleTtlFrom"`
	ProvisionIdleTTLNote    string `json:"provisionIdleTtlNote,omitempty"`
	ProvisionIdleTTLKey     string `json:"provisionIdleTtlKey"`
}

// KnownLimitView is one entry of the `knownLimits` list: the daemon's key, the
// daemon's sentence, and whether the daemon says it is true today.
type KnownLimitView struct {
	Key      string `json:"key"`
	Sentence string `json:"sentence"`
	Active   bool   `json:"active"`
}

// BackendView is one backend with its capabilities and its strength — the
// number `execenv.BackendStrength` spoke, so a panel can order the list the
// same way `Restricts` decides.
type BackendView struct {
	Name         string   `json:"name"`
	Strength     int      `json:"strength"`
	Capabilities []string `json:"capabilities"`
}

// BindResult is environment.bind's answer, dry-run or not: the daemon's own
// statement (one formatter, `execenv.DescribeBind` — the dry-run answer and the
// applied answer differ ONLY by the statement's own dry-run clause), whether
// anything was written, and the binding that now exists (nil on a dry run).
type BindResult struct {
	Statement string       `json:"statement"`
	DryRun    bool         `json:"dryRun"`
	Changed   bool         `json:"changed"`
	Binding   *BindingView `json:"binding,omitempty"`
}

// UnbindResult is environment.unbind's answer.
type UnbindResult struct {
	Unbound bool `json:"unbound"`
}

// --- the seam ---------------------------------------------------------------------

// EnvironmentService is the daemon's ONE environment service
// (`internal/daemon/environment.go`): the only caller of the store's
// environment writes, and the only place `execenv`'s validator is consulted
// for a wire write. Refusals come back as `ErrEnvironmentRefused` with the
// sentence that was actually spoken in `data.what` (DB19); a non-nil plain
// error is an infrastructure failure the door cannot name any better.
//
// Reads take no actor: they are the person's own view and stamp nothing.
// Every write takes the actor the CONNECTION resolved, never a params field.
type EnvironmentService interface {
	List(ctx context.Context, p EnvironmentListParams) ([]EnvironmentView, error)
	Show(ctx context.Context, p EnvironmentShowParams) (EnvironmentView, error)
	Effective(ctx context.Context, p EnvironmentEffectiveParams) (EnvironmentEffectiveView, error)
	Vocabulary(ctx context.Context) (EnvironmentVocabularyView, error)
	Changes(ctx context.Context, p EnvironmentChangesParams) ([]EnvironmentChangeView, error)
	Provisions(ctx context.Context, p EnvironmentProvisionsParams) ([]ProvisionView, error)

	Create(ctx context.Context, actor Actor, p EnvironmentCreateParams) (EnvironmentView, error)
	Update(ctx context.Context, actor Actor, p EnvironmentUpdateParams) (EnvironmentView, error)
	Archive(ctx context.Context, actor Actor, p EnvironmentIDParams) (EnvironmentView, error)
	Restore(ctx context.Context, actor Actor, p EnvironmentIDParams) (EnvironmentView, error)
	Bind(ctx context.Context, actor Actor, p EnvironmentBindParams) (BindResult, error)
	Unbind(ctx context.Context, actor Actor, p EnvironmentUnbindParams) (UnbindResult, error)
	Teardown(ctx context.Context, actor Actor, p ProvisionIDParams) (ProvisionView, error)
}

// --- registration -----------------------------------------------------------------

// RegisterEnvironment registers the thirteen environment doors over a non-nil
// seam. Nil means the build has no environment service wired and the methods
// are ABSENT (V13) — the dispatcher answers `CodeMethodNotFound`, which is the
// truth, rather than registering thirteen doors that can only refuse. Returns
// whether they are in the table so the daemon's wiring is asserted, not trusted.
func (t *Table) RegisterEnvironment() bool {
	if t.Environment == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodEnvironmentList, RequiresInitialize: true, Handle: t.handleEnvironmentList},
		{Name: MethodEnvironmentShow, RequiresInitialize: true, Handle: t.handleEnvironmentShow},
		{Name: MethodEnvironmentEffective, RequiresInitialize: true, Handle: t.handleEnvironmentEffective},
		{Name: MethodEnvironmentVocabulary, RequiresInitialize: true, Handle: t.handleEnvironmentVocabulary},
		{Name: MethodEnvironmentChanges, RequiresInitialize: true, Handle: t.handleEnvironmentChanges},
		{Name: MethodEnvironmentProvisions, RequiresInitialize: true, Handle: t.handleEnvironmentProvisions},
		{Name: MethodEnvironmentCreate, RequiresInitialize: true, Handle: t.handleEnvironmentCreate},
		{Name: MethodEnvironmentUpdate, RequiresInitialize: true, Handle: t.handleEnvironmentUpdate},
		{Name: MethodEnvironmentArchive, RequiresInitialize: true, Handle: t.handleEnvironmentArchive},
		{Name: MethodEnvironmentRestore, RequiresInitialize: true, Handle: t.handleEnvironmentRestore},
		{Name: MethodEnvironmentBind, RequiresInitialize: true, Handle: t.handleEnvironmentBind},
		{Name: MethodEnvironmentUnbind, RequiresInitialize: true, Handle: t.handleEnvironmentUnbind},
		{Name: MethodEnvironmentTeardown, RequiresInitialize: true, Handle: t.handleEnvironmentTeardown},
	} {
		t.register(m)
	}
	return true
}

// --- the door-shared rules --------------------------------------------------------

// environmentIdentityKeys names the keys the environment params may never
// carry: the actor is the door's (the connection's person), the origin is the
// seed's, the legacy flag is the migration's, and `source` on an ENVIRONMENT is
// the secret reference's own field — an environment names references, it does
// not carry sources (EV7).
var environmentIdentityKeys = []string{"actor", "actorKind", "origin", "legacy", "source", "sourceKind", "revision", "createdAt", "updatedAt", "createdBy", "updatedBy"}

// rejectEnvironmentIdentityKeys checks the raw key BAG before the lenient
// struct decode (the policy-authoring shape: additive keys are tolerated,
// identity keys never are).
func rejectEnvironmentIdentityKeys(method string, raw json.RawMessage) error {
	return rejectEnvironmentKeys(method, raw, environmentIdentityKeys)
}

// environmentReadIdentityKeys is the reads' half of the same door, and the one
// key the writes refuse that a read accepts is `origin`: `environment.list`
// filters on it, because the seed's rows and a person's rows are different
// things to look at and the store's own filter offers it. Everything else a
// write may not name, a read may not name either — a smuggled actor on a READ
// still forges the identity the plane's reads answer as, and `source` on an
// environment is the reference plane's field (EV7).
var environmentReadIdentityKeys = []string{
	"actor", "actorKind", "legacy", "source", "sourceKind",
	"revision", "createdAt", "updatedAt", "createdBy", "updatedBy",
}

// rejectEnvironmentReadKeys runs the reads' rejection, and it runs FIRST in
// every handler — before the semantic checks — so a refusal names the key it
// refused rather than arriving as "you did not give me an id".
func rejectEnvironmentReadKeys(method string, raw json.RawMessage) error {
	return rejectEnvironmentKeys(method, raw, environmentReadIdentityKeys)
}

// rejectEnvironmentKeys is the raw-key-bag check both halves share: it reads
// the keys and nothing else, so a value never has to be interpreted (or
// logged) to decide whether it was allowed to arrive.
func rejectEnvironmentKeys(method string, raw json.RawMessage, keys []string) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range keys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the caller names what to act on, never who acted, when it happened, which origin it carries, or where a value lives (S59 EV7, EV17)",
				method, key)}
		}
	}
	return nil
}

// environmentActor stamps the write's author from the CONNECTION, never from
// params. The honest absence — no identity seam, a blank id, or the uid 0 the
// transport never recorded — refuses BEFORE anything is read, let alone
// written, with the plane's one voice (EV17).
func (t *Table) environmentActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrEnvironmentRefused.WithWhat("an environment write is a person's door and no identity is available on this connection — the daemon stamped none")
	}
	a := t.ActorFor(ctx, sess)
	id := strings.TrimSpace(a.ID)
	if id == "" || id == "0" {
		return Actor{}, ErrEnvironmentRefused.WithWhat("an environment write is a person's door and the peer is unresolvable (uid %q) — the actor is the connection's person, never a guess", a.ID)
	}
	return Actor{Kind: "user", ID: id}, nil
}

// environmentSeam speaks when registration and the seam disagree: registration
// is conditional, so a nil here is a wiring bug, said out loud rather than
// answered with a nil dereference.
func (t *Table) environmentSeam() error {
	if t.Environment == nil {
		return ErrStoreUnavailable.WithWhat("the environment doors need the daemon's environment service, and this build did not link it — the surface is absent for the same reason (V13)")
	}
	return nil
}

// maxEnvironmentLimit bounds a listing. A client asking for "all" gets this
// and can page with the audit's own ids; an unbounded read of a table that
// outlives its records (EV17: no FK, history forever) is a memory answer to a
// listing question.
const maxEnvironmentLimit = 500

func refuseEnvironmentLimit(method string, limit int) error {
	if limit < 0 {
		return ErrBadParams{Err: fmt.Errorf("%s limit must not be negative", method)}
	}
	if limit > maxEnvironmentLimit {
		return ErrBadParams{Err: fmt.Errorf("%s limit is %d; the bound is %d — narrow the filter rather than the patience", method, limit, maxEnvironmentLimit)}
	}
	return nil
}

// --- the read handlers ------------------------------------------------------------

func (t *Table) handleEnvironmentList(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectEnvironmentReadKeys(string(MethodEnvironmentList), raw); err != nil {
		return nil, err
	}
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	var p EnvironmentListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if err := refuseEnvironmentLimit(string(MethodEnvironmentList), p.Limit); err != nil {
		return nil, err
	}
	return t.Environment.List(ctx, p)
}

func (t *Table) handleEnvironmentShow(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectEnvironmentReadKeys(string(MethodEnvironmentShow), raw); err != nil {
		return nil, err
	}
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	var p EnvironmentShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" && strings.TrimSpace(p.Slug) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("environment.show needs an id or a slug — there is no default environment")}
	}
	return t.Environment.Show(ctx, p)
}

func (t *Table) handleEnvironmentEffective(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectEnvironmentReadKeys(string(MethodEnvironmentEffective), raw); err != nil {
		return nil, err
	}
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	var p EnvironmentEffectiveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return t.Environment.Effective(ctx, p)
}

func (t *Table) handleEnvironmentVocabulary(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectEnvironmentReadKeys(string(MethodEnvironmentVocabulary), raw); err != nil {
		return nil, err
	}
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
		var bag map[string]json.RawMessage
		if err := json.Unmarshal(raw, &bag); err == nil && len(bag) > 0 {
			return nil, ErrBadParams{Err: fmt.Errorf("environment.vocabulary takes no params — the vocabulary is the plane's, not the caller's")}
		}
	}
	return t.Environment.Vocabulary(ctx)
}

func (t *Table) handleEnvironmentChanges(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectEnvironmentReadKeys(string(MethodEnvironmentChanges), raw); err != nil {
		return nil, err
	}
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	var p EnvironmentChangesParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if err := refuseEnvironmentLimit(string(MethodEnvironmentChanges), p.Limit); err != nil {
		return nil, err
	}
	return t.Environment.Changes(ctx, p)
}

func (t *Table) handleEnvironmentProvisions(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectEnvironmentReadKeys(string(MethodEnvironmentProvisions), raw); err != nil {
		return nil, err
	}
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	var p EnvironmentProvisionsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if err := refuseEnvironmentLimit(string(MethodEnvironmentProvisions), p.Limit); err != nil {
		return nil, err
	}
	return t.Environment.Provisions(ctx, p)
}

// --- the write handlers -----------------------------------------------------------

func (t *Table) handleEnvironmentCreate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	if err := rejectEnvironmentIdentityKeys(string(MethodEnvironmentCreate), raw); err != nil {
		return nil, err
	}
	var p EnvironmentCreateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Slug) == "" || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Backend) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("slug, name and backend are required — an environment with no backend is not an environment")}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.Environment.Create(ctx, actor, p)
}

func (t *Table) handleEnvironmentUpdate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	if err := rejectEnvironmentIdentityKeys(string(MethodEnvironmentUpdate), raw); err != nil {
		return nil, err
	}
	var p EnvironmentUpdateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("environment.update needs the id of the environment whose spec is being replaced")}
	}
	if p.ExpectedRevision < 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("expectedRevision must not be negative — it is the revision the caller read, and 0 is a real one")}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.Environment.Update(ctx, actor, p)
}

func (t *Table) handleEnvironmentArchive(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	if err := rejectEnvironmentIdentityKeys(string(MethodEnvironmentArchive), raw); err != nil {
		return nil, err
	}
	var p EnvironmentIDParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" && strings.TrimSpace(p.Slug) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("environment.archive needs an id or a slug — archiving every environment nobody named is not an act a person takes")}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.Environment.Archive(ctx, actor, p)
}

func (t *Table) handleEnvironmentRestore(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	if err := rejectEnvironmentIdentityKeys(string(MethodEnvironmentRestore), raw); err != nil {
		return nil, err
	}
	var p EnvironmentIDParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" && strings.TrimSpace(p.Slug) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("environment.restore needs an id or a slug — a restore is explicit, never a class of rows")}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.Environment.Restore(ctx, actor, p)
}

func (t *Table) handleEnvironmentBind(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	if err := rejectEnvironmentIdentityKeys(string(MethodEnvironmentBind), raw); err != nil {
		return nil, err
	}
	var p EnvironmentBindParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	switch p.Scope {
	case "component":
		if strings.TrimSpace(p.ComponentID) == "" {
			return nil, ErrBadParams{Err: fmt.Errorf("a component bind names a componentId — scope:component with no component is nothing to bind")}
		}
	case "workspace":
		if strings.TrimSpace(p.ComponentID) != "" {
			return nil, ErrBadParams{Err: fmt.Errorf("scope:workspace carries no componentId — the workspace binding is the floor, not one component's choice")}
		}
	default:
		return nil, ErrBadParams{Err: fmt.Errorf("scope %q is neither component nor workspace — the two scopes the plane has (EV14)", p.Scope)}
	}
	if strings.TrimSpace(p.EnvironmentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("environmentId is required — a bind with no environment is an unbind, and has its own method")}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.Environment.Bind(ctx, actor, p)
}

func (t *Table) handleEnvironmentUnbind(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	if err := rejectEnvironmentIdentityKeys(string(MethodEnvironmentUnbind), raw); err != nil {
		return nil, err
	}
	var p EnvironmentUnbindParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("environment.unbind names the component whose binding goes away — the workspace binding cannot be unbound, and naming no component is not the way to ask")}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.Environment.Unbind(ctx, actor, p)
}

func (t *Table) handleEnvironmentTeardown(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := t.environmentSeam(); err != nil {
		return nil, err
	}
	if err := rejectEnvironmentIdentityKeys(string(MethodEnvironmentTeardown), raw); err != nil {
		return nil, err
	}
	var p ProvisionIDParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ProvisionID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("provisionId is required — teardown removes ONE provision's container, never a class of them")}
	}
	actor, err := t.environmentActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.Environment.Teardown(ctx, actor, p)
}
