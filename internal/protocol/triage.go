package protocol

// S29 T7 — the `triage.*` socket methods. The same named departure from PL15
// that CG9 granted the queue and roadmap applies verbatim here (P34): bugs
// are ordinary project content at the same trust tier as loop.start's own
// params, so the socket WRITES them, and every handler marshals around the
// SAME internal/triage calls the CLI dials. "CLI vs socket" is a transport
// choice, never two authoring paths, which is why there is no validation
// logic in this file beyond parameter shape: the door and the database
// CHECK are the two truths, and neither rides twice.
//
// The daemon-first rule (P34) is what the extra verbs ARE: file/list/show/
// set-priority/confirm-duplicate are the brief's surface, and validate/
// handoff/suggest are the machine's remaining doors — without them the
// four-state machine and the S26 hand-off exist but nothing on the app can
// walk them, and "a capability that exists only on the daemon's inner path
// is not accepted as finished". validate and handoff ride Table seams the
// daemon fills (initTriage); suggest computes the embedding the triage door
// itself must never reach a model to get. A nil seam answers
// ErrStoreUnavailable, loud.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/triage"
)

// --- the names ---------------------------------------------------------------
//
// These ride here with their handlers (queue.go's rule): one file owns the
// verb, its name, its params and its handler — nothing to forget to update
// twice.

const (
	MethodTriageFile             MethodName = "triage.file"
	MethodTriageList             MethodName = "triage.list"
	MethodTriageShow             MethodName = "triage.show"
	MethodTriageSetPriority      MethodName = "triage.set-priority"
	MethodTriageConfirmDuplicate MethodName = "triage.confirm-duplicate"
	MethodTriageValidate         MethodName = "triage.validate"
	MethodTriageHandoff          MethodName = "triage.handoff"
	MethodTriageSuggest          MethodName = "triage.suggest-duplicates"
	// S51 T2 (TM5/TM7): the management reads and the title/body edit. The
	// constants ride the ONE vocabulary block beside the eight S29 verbs —
	// a method name lives exactly where the plane's method names live.
	MethodTriageUpdate     MethodName = "triage.update"
	MethodTriageBoard      MethodName = "triage.board"
	MethodTriageHistory    MethodName = "triage.history"
	MethodTriageVocabulary MethodName = "triage.vocabulary"
	// MethodTriageHealth is S51 T3 (TM4): the honest answer about
	// triage.validationBlueprint's standing — a read, never a gate.
	MethodTriageHealth MethodName = "triage.health"
)

// --- params ------------------------------------------------------------------

// TriageFileParams is D7's honest create — S51 T1's shape: there is NO
// filedBy param (TM3): who filed the bug is DERIVED from the stamped actor
// — 'person' for a person at the socket, the agent's own slug for its twin
// — and a param carrying a filer is refused by name (the forged-identity
// vector this removal closes). The schema carries NO validationState (the
// store's own default is the backstop — a crafted call cannot pre-label a
// claim). A human path may supply a priority it has decided.
type TriageFileParams struct {
	ComponentRoot string `json:"componentRoot"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	PriorityState string `json:"priorityState,omitempty"`
	PriorityLabel string `json:"priorityLabel,omitempty"`
}

// TriageShowParams reads one bug by id.
type TriageShowParams struct {
	ID int64 `json:"id"`
}

// TriageListParams reads a root's bugs, optionally narrowed by validation or
// priority (empty means no filter on that column — never a collapse).
type TriageListParams struct {
	ComponentRoot   string `json:"componentRoot"`
	ValidationState string `json:"validationState,omitempty"`
	PriorityState   string `json:"priorityState,omitempty"`
}

// TriageSetPriorityParams sets state and label together (D2: one decision);
// a label on an unknown priority is the contradiction the CHECK refuses.
type TriageSetPriorityParams struct {
	ID            int64  `json:"id"`
	ComponentRoot string `json:"componentRoot"`
	State         string `json:"state"`
	Label         string `json:"label,omitempty"`
}

// TriageConfirmDuplicateParams is D5/N3's one write path: it sets
// duplicate_of only from a person-named confirmer (the door refuses a
// machine one).
type TriageConfirmDuplicateParams struct {
	ID            int64  `json:"id"`
	ComponentRoot string `json:"componentRoot"`
	Of            int64  `json:"of"`
	ConfirmedBy   string `json:"confirmedBy"`
}

// TriageValidateParams dispatches a validation attempt (T3): the daemon's
// action, an ordinary loop whose result moves the bug out of unvalidated.
type TriageValidateParams struct {
	ID int64 `json:"id"`
}

// TriageHandoffParams is D8's hand-off (T6): create an S26 roadmap item
// (provenance 'triaged') for a validated, prioritized bug, and kick its
// ideation.
type TriageHandoffParams struct {
	ID int64 `json:"id"`
}

// TriageSuggestParams is T4's read: nearest-neighbour duplicates for a bug
// within its own project. The daemon computes the embedding (the triage
// door never reaches a model); the result is a suggest-only list (D5), no
// side effect.
type TriageSuggestParams struct {
	ID    int64 `json:"id"`
	Limit int   `json:"limit,omitempty"`
}

// --- the error --------------------------------------------------------------

// ErrTriageRefused is the one triage-flavoured error: a state or write
// refusal with its reason carried in `what` (DB19's discipline — a refusal
// names the reason AND the thing refused). Not-found rides it too: the
// socket does not answer a missing row with a silence.
var ErrTriageRefused = &Error{
	Code:    -31910,
	Message: "triage refused the act",
	Data: Data{
		What:  "the bug's validation state, its priority, or the act itself broke a triage law (D1's table, D2, D5)",
		Means: "nothing moved — the bug keeps the state it had",
		Todo:  "read the reason in `what`, then triage.list to see the backlog as the daemon sees it",
		Fatal: false,
	},
}

func triageRefusal(reason string) *Error {
	return ErrTriageRefused.WithWhat("%s", reason)
}

// ErrTriageNoActor is S51 T1 (TM2/TM3): every triage WRITE now names its
// author from the CONNECTION — the daemon's ActorFor seam, the kernel's uid,
// never a param (the roadmap/backup/blueprint shape). An unresolvable peer
// refuses BEFORE anything is read, let alone written: nothing half-stamped
// and nothing anonymous ever reaches bug_change.
var ErrTriageNoActor = &Error{
	Code:    -31933,
	Message: "the triage write could not name its author",
	Data: Data{
		What:  "the triage write is a person's door and no identity is available on this connection — the audit row must name WHO wrote it (TM2), and an anonymous write is the gap that table exists to close",
		Means: "nothing moved — the bug keeps the state it had and no audit row was written",
		Todo:  "connect as the person who owns the change (the endpoint is 0600, peer-credentialed); an agent files through its own twin (S51 T5), never by claiming an identity here",
		Fatal: false,
	},
}

// ErrTriageUnknown is S51 T2 (TM5): the READS of this plane answer an id
// with no row with a NAME the caller can branch on — "no such bug" is not
// "no history", and an empty list must never stand in for a refusal to
// have looked. The door composes the sentence (one place); the handler
// maps it here (triageUnknownLike).
var ErrTriageUnknown = &Error{
	Code:    -31934,
	Message: "the triage read named no bug",
	Data: Data{
		What:  "the id in the request matches no bug row — the read refused to answer empty for a bug that never existed",
		Means: "nothing was read about a nonexistent bug — an EMPTY history is a real bug with no rows; this is a different fact, which is why it is a different error",
		Todo:  "list or show first and use an id the backlog actually holds",
		Fatal: false,
	},
}

// triageActor stamps the write's author from the CONNECTION (the daemon's
// ActorFor seam), never from params — the exact roadmapActor shape. The
// honest absence (no seam, a blank id, uid 0) refuses with
// ErrTriageNoActor before anything is read. The kind travels: a stamped
// socket peer is the person, "user"; triage is the one plane where an agent
// actor will become lawful (D7's filing, S51 T5's twin), so the kind is
// carried, not flattened.
func (t *Table) triageActor(ctx context.Context, sess *Session) (triage.Actor, error) {
	if t.ActorFor == nil {
		return triage.Actor{}, ErrTriageNoActor.WithWhat("the triage write is a person's door and no identity is available on this connection — the daemon stamped none")
	}
	a := t.ActorFor(ctx, sess)
	id := strings.TrimSpace(a.ID)
	if id == "" || id == "0" {
		return triage.Actor{}, ErrTriageNoActor.WithWhat("the triage write is a person's door and the peer is unresolvable (uid %q) — the actor is the connection's person, never a guess", a.ID)
	}
	kind := a.Kind
	if kind == "" {
		kind = "user"
	}
	switch kind {
	case "user", "cli", "agent":
	default:
		return triage.Actor{}, ErrTriageNoActor.WithWhat("the triage write carries actor kind %q, which no lawful triage author wears (person, cli, or the agent's own twin)", kind)
	}
	return triage.Actor{Kind: kind, ID: id}, nil
}

// triageIdentityKeys names the keys a triage WRITE call may never carry:
// the actor is stamped by the door that holds the connection (the roadmap
// shape), and a param naming it would let a caller forge the audit identity
// of the write. `filedBy` joined them at S51 T1 (TM3): who filed a bug is
// now DERIVED from the stamped actor — 'person' for the human paths, the
// agent's own slug for its twin — and a param carrying a filer is exactly
// the forged-identity vector the removal closes.
var triageIdentityKeys = []string{"actor", "actorKind", "filedBy"}

// rejectTriageIdentityKeys checks the raw key bag BEFORE the lenient struct
// decode (decodeParams is deliberately lenient — unknown keys are ignored —
// so the refusal lives here or nowhere).
func rejectTriageIdentityKeys(method MethodName, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range triageIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf("%s refuses the param %q — the actor is stamped from the connection and filed_by is derived from it, never carried in params (S51 TM3)", method, key)}
		}
	}
	return nil
}

// asTriageRefusal keeps the daemon seam's own sentence verbatim when it is
// already an Error (a loader refusal rides out raw, queue's precedent) and
// wraps a plain refusal reason in the triage error otherwise.
func asTriageRefusal(err error) error {
	if ae, ok := err.(*Error); ok {
		return ae
	}
	return triageRefusal(err.Error())
}

// --- registration -----------------------------------------------------------

func (t *Table) registerTriage() {
	t.register(&Method{Name: MethodTriageFile, RequiresInitialize: true, Handle: t.handleTriageFile})
	t.register(&Method{Name: MethodTriageList, RequiresInitialize: true, Handle: t.handleTriageList})
	t.register(&Method{Name: MethodTriageShow, RequiresInitialize: true, Handle: t.handleTriageShow})
	t.register(&Method{Name: MethodTriageSetPriority, RequiresInitialize: true, Handle: t.handleTriageSetPriority})
	t.register(&Method{Name: MethodTriageConfirmDuplicate, RequiresInitialize: true, Handle: t.handleTriageConfirmDuplicate})
	t.register(&Method{Name: MethodTriageValidate, RequiresInitialize: true, Handle: t.handleTriageValidate})
	t.register(&Method{Name: MethodTriageHandoff, RequiresInitialize: true, Handle: t.handleTriageHandoff})
	t.register(&Method{Name: MethodTriageSuggest, RequiresInitialize: true, Handle: t.handleTriageSuggest})
	// S51 T2 (TM7): the edit is a human-door WRITE (actor+audit; the agent
	// actor dies at the store's ValidFor, never at SQL); the three reads
	// ride the same registration door — none Debug, all RequiresInitialize.
	t.register(&Method{Name: MethodTriageUpdate, RequiresInitialize: true, Handle: t.handleTriageUpdate})
	t.register(&Method{Name: MethodTriageBoard, RequiresInitialize: true, Handle: t.handleTriageBoard})
	t.register(&Method{Name: MethodTriageHistory, RequiresInitialize: true, Handle: t.handleTriageHistory})
	t.register(&Method{Name: MethodTriageVocabulary, RequiresInitialize: true, Handle: t.handleTriageVocabulary})
	t.register(&Method{Name: MethodTriageHealth, RequiresInitialize: true, Handle: t.handleTriageHealth})
}

// --- handlers ---------------------------------------------------------------

func (t *Table) handleTriageFile(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Triage == nil {
		return nil, ErrStoreUnavailable
	}
	if err := rejectTriageIdentityKeys(MethodTriageFile, raw); err != nil {
		return nil, err
	}
	var p TriageFileParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ComponentRoot == "" || p.Title == "" || p.Body == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("componentRoot, title and body are required — the schema carries no validation state (the store's default is the backstop) and no filer (filed_by is derived from the stamped actor, S51 TM3)")}
	}
	actor, err := t.triageActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	b, ref := t.Triage.CreateAudited(p.ComponentRoot, newTriageBugFromParams(p, actor), actor)
	if ref != nil {
		return nil, triageRefusal(ref.Reason)
	}
	return toTriageView(b), nil
}

func (t *Table) handleTriageList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.Triage == nil {
		return nil, ErrStoreUnavailable
	}
	var p TriageListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ComponentRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("componentRoot is required — a bug list is scoped to one root")}
	}
	list, err := t.Triage.List(p.ComponentRoot, newTriageListFilter(p))
	if err != nil {
		return nil, asTriageRefusal(err)
	}
	views := make([]any, 0, len(list))
	for _, b := range list {
		views = append(views, toTriageView(b))
	}
	return views, nil
}

func (t *Table) handleTriageShow(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.Triage == nil {
		return nil, ErrStoreUnavailable
	}
	var p TriageShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	// S51 T5: the SAME shared answer the bug_show tool arm runs (rule 4 —
	// one rendering, two doors; the bytes below are unchanged).
	return AnswerTriageShow(t.Triage, p.ID)
}

func (t *Table) handleTriageSetPriority(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Triage == nil {
		return nil, ErrStoreUnavailable
	}
	if err := rejectTriageIdentityKeys(MethodTriageSetPriority, raw); err != nil {
		return nil, err
	}
	var p TriageSetPriorityParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 || p.ComponentRoot == "" || p.State == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id, componentRoot and state are required — priority rides its three-state name, never a number (D2)")}
	}
	actor, err := t.triageActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	b, ref := t.Triage.SetPriorityAudited(p.ID, p.ComponentRoot, triagePriorityState(p.State), p.Label, actor)
	if ref != nil {
		return nil, triageRefusal(ref.Reason)
	}
	return toTriageView(b), nil
}

func (t *Table) handleTriageConfirmDuplicate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.Triage == nil {
		return nil, ErrStoreUnavailable
	}
	if err := rejectTriageIdentityKeys(MethodTriageConfirmDuplicate, raw); err != nil {
		return nil, err
	}
	var p TriageConfirmDuplicateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 || p.ComponentRoot == "" || p.Of <= 0 || p.ConfirmedBy == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id, componentRoot, of and confirmedBy are required — a duplicate becomes real only from a person (D5/N3)")}
	}
	actor, err := t.triageActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	b, ref := t.Triage.ConfirmDuplicateAudited(p.ID, p.ComponentRoot, p.Of, p.ConfirmedBy, actor)
	if ref != nil {
		return nil, triageRefusal(ref.Reason)
	}
	return toTriageView(b), nil
}

func (t *Table) handleTriageValidate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectTriageIdentityKeys(MethodTriageValidate, raw); err != nil {
		return nil, err
	}
	var p TriageValidateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.TriageValidate == nil {
		return nil, ErrStoreUnavailable
	}
	actor, err := t.triageActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, err := t.TriageValidate(ctx, p)
	if err != nil {
		return nil, asTriageRefusal(err)
	}
	// TM2: the dispatch earns its one `validate` audit row — the ONE audit
	// that travels outside a write pair (starting an attempt moves no bug
	// row; the attempt is a loop and the embedding rides bug_vec). A failed
	// append is spoken, never swallowed: the loop runs, and the history that
	// must name this act says nothing — that gap is said out loud.
	if ref := t.Triage.AppendValidationAudit(p.ID, actor); ref != nil {
		return nil, triageRefusal(ref.Reason)
	}
	return res, nil
}

func (t *Table) handleTriageHandoff(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectTriageIdentityKeys(MethodTriageHandoff, raw); err != nil {
		return nil, err
	}
	var p TriageHandoffParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.TriageHandoff == nil {
		return nil, ErrStoreUnavailable
	}
	actor, err := t.triageActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, err := t.TriageHandoff(ctx, actor, p)
	if err != nil {
		return nil, asTriageRefusal(err)
	}
	return res, nil
}

func (t *Table) handleTriageSuggest(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p TriageSuggestParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.TriageSuggest == nil {
		return nil, ErrStoreUnavailable
	}
	res, err := t.TriageSuggest(ctx, p)
	if err != nil {
		return nil, asTriageRefusal(err)
	}
	return res, nil
}

// --- the view + conversions --------------------------------------------------
//
// TriageView is the ONE row→JSON shape the app sees: validation and priority
// as TWO separate fields (D2), never one combined badge (R4's guard the
// renderer obeys by having two fields to render). A bug with no duplicate_of
// or no roadmap_item_id renders the honest absence, not a zero.

type TriageView struct {
	ID            int64  `json:"id"`
	ComponentRoot string `json:"componentRoot"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	Validation    string `json:"validation"`
	PriorityState string `json:"priorityState"`
	PriorityLabel string `json:"priorityLabel,omitempty"`
	FiledBy       string `json:"filedBy"`
	DuplicateOf   *int64 `json:"duplicateOf,omitempty"`
	RoadmapItemID *int64 `json:"roadmapItemId,omitempty"`
	Validated     bool   `json:"validated"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

func toTriageView(b triage.Bug) *TriageView {
	return &TriageView{
		ID:            b.ID,
		ComponentRoot: b.ComponentRoot,
		Title:         b.Title,
		Body:          b.Body,
		Validation:    string(b.Validation),
		PriorityState: string(b.PriorityState),
		PriorityLabel: b.PriorityLabel,
		FiledBy:       b.FiledBy,
		DuplicateOf:   b.DuplicateOf,
		RoadmapItemID: b.RoadmapItemID,
		Validated:     b.Validated(),
		CreatedAt:     b.CreatedAt,
		UpdatedAt:     b.UpdatedAt,
	}
}

// newTriageBugFromParams shapes the door's NewBug from the wire params plus
// the DERIVED filer (S51 TM3): a person (user or cli at the socket, direct
// door alike) files as 'person'; an agent's twin files under its OWN stamped
// slug — never a name, never a caller's word. The stamping door has already
// refused the kinds that wear no lawful triage hat.
func newTriageBugFromParams(p TriageFileParams, actor triage.Actor) triage.NewBug {
	filedBy := "person"
	if actor.Kind == "agent" {
		filedBy = actor.ID
	}
	return triage.NewBug{
		Title:         p.Title,
		Body:          p.Body,
		PriorityState: triagePriorityState(p.PriorityState),
		PriorityLabel: p.PriorityLabel,
		FiledBy:       filedBy,
	}
}

func newTriageListFilter(p TriageListParams) triage.ListFilter {
	return triage.ListFilter{
		Validation: triageValidationState(p.ValidationState),
		Priority:   triagePriorityState(p.PriorityState),
	}
}

// triagePriorityState / triageValidationState map the wire string onto the
// door's vocabulary, leaving the empty value as the door's own honest
// default (unknown / no filter) — a number or an unknown word never crosses.
func triagePriorityState(s string) triage.PriorityState {
	if s == "" {
		return "" // the door defaults empty to unknown (D2's honest absence)
	}
	return triage.PriorityState(s)
}

func triageValidationState(s string) triage.ValidationState {
	if s == "" {
		return "" // no filter
	}
	return triage.ValidationState(s)
}
