package protocol

// mergequeue.go — S60 T8 (MQ13, MQ17, P4, P34): the merge-queue doors.
//
// Ten methods, one seam. The four reads (list, show, status, vocabulary)
// and the six human-door writes (enqueue, dequeue, reorder, retry, land,
// settings) register together over the daemon's ONE queue door service, or
// not at all: a nil seam is the configuration speaking and the methods are
// ABSENT, never registered-and-refusing (V13, the RegisterEnvironment/
// RegisterVerify shape). All ten are `RequiresInitialize` and none is
// `Debug`.
//
// Two rules the wire shape carries, not a convention:
//
//   - No identity in params, and no state in params. `actor`, `status`,
//     `reason` and the stamped columns (`landedSha`, `verifiedSha`, the
//     timestamps, `enqueuedBy`) are refused from the raw key bag before the
//     lenient decode: the actor is the CONNECTION's person (`Table.ActorFor`,
//     the kernel's peer credential — {user, <uid>}), and the status is the
//     driver's — a queue row whose status a caller could type is a queue
//     where anyone could type `merged` (MQ13). `attended` is the one
//     exception and it belongs here by G14's own list: append-to-the-queue
//     is one of the three doors that DECLARES attendedness.
//   - Land binds to what the caller SAW. `land` carries the digest the
//     `show` answer computed from the row's head SHA and position: a land
//     for an item whose head moved since the person read it refuses, because
//     the thing they approved is not the thing that would merge.
//
// The refusals are the plane's voice: `ErrMergeQueueRefused` carries the
// service's own sentence in `what` (DB19, nothing paraphrases a refusal);
// `ErrMergeQueueNotFound` names the id that answered nothing;
// `ErrMergeQueueBaseUnknown` is the enqueue whose base is not a branch of
// the named repository; `ErrMergeQueueConflictState` is the write aimed at
// an item whose state does not carry that door.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// --- the method names -------------------------------------------------------------

const (
	// The four reads. `status` is the per-repository view (settings, counts,
	// lane heads); `show` is one item with its ledger; `list` is the queue
	// across the machine (or one root); `vocabulary` is the closed sets the
	// screens render from, derived from the driver's own tables, never a
	// hand-copied list (MQ17).
	MethodMergeQueueList       MethodName = "mergequeue.list"
	MethodMergeQueueShow       MethodName = "mergequeue.show"
	MethodMergeQueueStatus     MethodName = "mergequeue.status"
	MethodMergeQueueVocabulary MethodName = "mergequeue.vocabulary"

	// The six human-door writes (MQ13): socket only, absent from the narrow
	// projection, the agent services, the tool registry and the records
	// catalog — the four caller-classes that are not the person. (T9 adds
	// the plane's ONE agent proposal twin, mergequeue.proposeEnqueue; it is
	// named beside its siblings when it lands, registration and all.)
	MethodMergeQueueEnqueue  MethodName = "mergequeue.enqueue"
	MethodMergeQueueDequeue  MethodName = "mergequeue.dequeue"
	MethodMergeQueueReorder  MethodName = "mergequeue.reorder"
	MethodMergeQueueRetry    MethodName = "mergequeue.retry"
	MethodMergeQueueLand     MethodName = "mergequeue.land"
	MethodMergeQueueSettings MethodName = "mergequeue.settings"
)

// mergequeueMethodNames is the plane's exact ten.
func mergequeueMethodNames() []MethodName {
	return []MethodName{
		MethodMergeQueueList, MethodMergeQueueShow, MethodMergeQueueStatus, MethodMergeQueueVocabulary,
		MethodMergeQueueEnqueue, MethodMergeQueueDequeue, MethodMergeQueueReorder,
		MethodMergeQueueRetry, MethodMergeQueueLand, MethodMergeQueueSettings,
	}
}

// --- the errors (S60's block, allocated by S60 T0, taken at T8) --------------------
//
// S60 owns -31952...-31955. T0 measured -31941...-31944 free at 56455a52,
// but between that measurement and this landing S51's triage block comment
// settled -31933...-31947 as ITS reserve ("the rest of the block is reserve
// for the slice's later refusal families") — and two slices reaching for one
// reserved span is exactly what the collision rule exists against (S58's
// precedent, sitting after S47's reservation). The block moved, measured
// free again: -31950/-31951 are S52's learnings pair, -31952...-31955 are
// taken here, and nothing in the tree uses them or -31956 upward.
var (
	// ErrMergeQueueRefused is the plane's one voice: the door understood the
	// request and declined. `what` carries the validator's, the service's or
	// the store's own sentence, never a paraphrase.
	ErrMergeQueueRefused = &Error{
		Code:    -31952,
		Message: "the merge-queue operation was refused",
		Data: Data{
			What:  "the plane understood the request and declined it — the what field is the driver's, the validator's or the store's own sentence",
			Means: "nothing changed: no queue row moved, no loop started, no event was written. A merge-queue write is a human door and the driver is the only mover of item states, so a refusal is the plane doing its job",
			Todo:  "do what the what says — re-read the item with mergequeue.show (its digest is the land's binding), fix the named setting, or accept that a landed or removed item has nothing left to do",
			Fatal: false,
		},
	}

	// ErrMergeQueueNotFound names the absence: an id no merge_queue_item row
	// answers, in the words of the id that missed. Never an empty row.
	ErrMergeQueueNotFound = &Error{
		Code:    -31953,
		Message: "no merge-queue item has that id",
		Data: Data{
			What:  "the id named by the request matches no queue row, open or terminal — the what field repeats the id that answered nothing",
			Means: "the item may never have enqueued, or its id was mistyped, or it belongs to another daemon's database. Nothing was read and nothing was written",
			Todo:  "run mergequeue.list (or mergequeue.status for one repository) and take the id from the answer",
			Fatal: false,
		},
	}

	// ErrMergeQueueBaseUnknown is the enqueue whose base_branch is not a
	// branch the named repository has. The queue keys per repository; a base
	// that does not exist cannot hold a lane (and the refusal never lists
	// the branches the repository DOES have — that is the component's read).
	ErrMergeQueueBaseUnknown = &Error{
		Code:    -31954,
		Message: "that base branch does not exist for that repository",
		Data: Data{
			What:  "the enqueue named a base_branch the component's repository does not carry as a branch",
			Means: "nothing enqueued. A queue lane is keyed (component_root, base_branch); a lane onto a branch nobody can check out would merge nothing into nowhere",
			Todo:  "name the repository's real base (mergequeue.status answers the lanes this repository already has), or create the base branch first",
			Fatal: false,
		},
	}

	// ErrMergeQueueConflictState is the write aimed at the wrong state:
	// reorder off a non-queued item, retry off a non-failed one, land off
	// anything but ready, dequeue of an already-terminal row.
	ErrMergeQueueConflictState = &Error{
		Code:    -31955,
		Message: "the item's state does not carry that door",
		Data: Data{
			What:  "the write named an item whose current status does not accept it — reorder belongs to queued rows, retry to failed ones, land to ready ones, dequeue to open ones (the what field names the state the row is actually in)",
			Means: "nothing changed. MQ5's edge table is the queue's shape and the driver alone moves rows along it; a door that worked from any state would be a second driver typing states",
			Todo:  "mergequeue.show the row and read its status and nextStep — the answer names the door that fits where the item actually is",
			Fatal: false,
		},
	}
)

// --- the params ---------------------------------------------------------------------

// MergeQueueListParams filters the listing. An empty root is "every
// repository", never "nothing"; IncludeTerminal brings the recent landed,
// failed and removed rows for the screens' histories.
type MergeQueueListParams struct {
	ComponentRoot   string `json:"componentRoot,omitempty"`
	IncludeTerminal bool   `json:"includeTerminal,omitempty"`
}

// MergeQueueShowParams is mergequeue.show: one item by id.
type MergeQueueShowParams struct {
	ID string `json:"id"`
}

// MergeQueueStatusParams is mergequeue.status: one repository's view. The
// root is required — the machine-wide view is mergequeue.list.
type MergeQueueStatusParams struct {
	ComponentRoot string `json:"componentRoot"`
}

// MergeQueueEnqueueParams is mergequeue.enqueue. HeadSha and BaseSha are
// the caller's git facts, verified against the repository before the row
// lands (an enqueue whose head is not a commit refuses). Attended is
// declared HERE, one of G14's three doors. Blueprint names the loop's
// blueprint ("" is the queue's own land-feature default).
type MergeQueueEnqueueParams struct {
	ComponentRoot string `json:"componentRoot"`
	BaseBranch    string `json:"baseBranch"`
	FeatureBranch string `json:"featureBranch"`
	HeadSha       string `json:"headSha"`
	BaseSha       string `json:"baseSha,omitempty"`
	Attended      bool   `json:"attended"`
	Blueprint     string `json:"blueprint,omitempty"`
}

// MergeQueueIDParams is dequeue's and retry's request: one item by id.
type MergeQueueIDParams struct {
	ID string `json:"id"`
}

// MergeQueueReorderParams moves one QUEUED item to sit directly before
// another ("" before-id means last). Anything else refuses.
type MergeQueueReorderParams struct {
	ID       string `json:"id"`
	BeforeID string `json:"beforeId,omitempty"`
}

// MergeQueueLandParams is mergequeue.land: the item, and the DIGEST the
// caller's mergequeue.show answer carried. The digest binds the approval to
// the head SHA and position the person actually saw; a moved item refuses
// and says so.
type MergeQueueLandParams struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// MergeQueueSettingsParams writes one repository's queue settings. Every
// present field must be a legal enum; a change that WIDENS authority (mode
// to auto, conflict policy to resolve) is applied only when Acknowledged
// carries the digest of the daemon's statement — which the refusal answers
// first. MaxCatchUps uses a pointer: 0 is a real number (no catch-ups).
type MergeQueueSettingsParams struct {
	ComponentRoot  string `json:"componentRoot"`
	Mode           string `json:"mode,omitempty"`
	ConflictPolicy string `json:"conflictPolicy,omitempty"`
	LandMethod     string `json:"landMethod,omitempty"`
	MaxCatchUps    *int   `json:"maxCatchUps,omitempty"`
	Acknowledged   string `json:"acknowledged,omitempty"`
}

// --- the views ----------------------------------------------------------------------

// StalenessView is the item's three-state read against its base: "current"
// (head and base as the queue last proved them), "behind" (base has moved
// under the item — the driver's catch-up counts against max_catchups), and
// "unknown" with the reason (no fetch has ever run for that repository in
// this daemon's life). Counts are behind/ahead commits when known.
type StalenessView struct {
	State  string `json:"state"`
	Behind *int64 `json:"behind,omitempty"`
	Ahead  *int64 `json:"ahead,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// EnqueuedByView names who appended the row: kind is user/cli/blueprint/
// daemon/agent, id is their name at the stamp.
type EnqueuedByView struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// ItemView is one queue row in the words the screens render. SEC-033: ids,
// enums, counts and PATHS — no diff, no hunk, no secret, anywhere in this
// shape. NextStep is the driver's own Describe sentence, byte for byte.
type ItemView struct {
	ID                string         `json:"id"`
	ComponentRoot     string         `json:"componentRoot"`
	Base              string         `json:"base"`
	Branch            string         `json:"branch"`
	Loop              string         `json:"loop,omitempty"`
	Status            string         `json:"status"`
	Reason            string         `json:"reason,omitempty"`
	Position          int64          `json:"position"`
	Attended          bool           `json:"attended"`
	HeadSha           string         `json:"headSha"`
	BaseSha           string         `json:"baseSha"`
	IntegrationBranch string         `json:"integrationBranch,omitempty"`
	IntegrationSha    string         `json:"integrationSha,omitempty"`
	PrNumber          *int           `json:"prNumber,omitempty"`
	VerifiedSha       string         `json:"verifiedSha,omitempty"`
	LandedSha         string         `json:"landedSha,omitempty"`
	LandingLoop       string         `json:"landingLoop,omitempty"`
	ResolveLoop       string         `json:"resolveLoop,omitempty"`
	CatchUps          int            `json:"catchUps"`
	EnqueuedBy        EnqueuedByView `json:"enqueuedBy"`
	Staleness         StalenessView  `json:"staleness"`
	NextStep          string         `json:"nextStep"`
	Digest            string         `json:"digest"`
	CreatedAt         string         `json:"createdAt"`
	UpdatedAt         string         `json:"updatedAt"`
}

// ConfigView is one repository's queue settings as rows answer them.
type ConfigView struct {
	ComponentRoot  string `json:"componentRoot"`
	Mode           string `json:"mode"`
	ConflictPolicy string `json:"conflictPolicy"`
	LandMethod     string `json:"landMethod"`
	MaxCatchUps    int    `json:"maxCatchUps"`
	UpdatedAt      string `json:"updatedAt"`
	UpdatedBy      string `json:"updatedBy"`
	Absent         bool   `json:"absent,omitempty"`
}

// TransitionView is one merge_queue_event row — the append-only ledger read
// back in order (SEC-033-clean by construction: the table holds enums, ids
// and counts only).
type TransitionView struct {
	Kind       string `json:"kind"`
	FromStatus string `json:"fromStatus,omitempty"`
	ToStatus   string `json:"toStatus,omitempty"`
	ReasonKind string `json:"reasonKind,omitempty"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	Detail     string `json:"detail,omitempty"`
	At         string `json:"at"`
}

// LaneView is one base branch's head: the item the driver advances first.
type LaneView struct {
	Base     string `json:"base"`
	HeadItem string `json:"headItem"`
}

// StatusResult is mergequeue.status's answer: one repository's settings,
// open-row counts by state, the lane heads, and the open rows themselves.
type StatusResult struct {
	ComponentRoot string         `json:"componentRoot"`
	Config        ConfigView     `json:"config"`
	Counts        map[string]int `json:"counts"`
	Lanes         []LaneView     `json:"lanes"`
	Items         []ItemView     `json:"items"`
}

// ShowResult is one item, its ledger, and the integration references the
// row itself carries. Cost stays the loop plane's: the landing loop's
// spend rides loop.show, and a second copy of a ledger is a second truth.
type ShowResult struct {
	Item        ItemView         `json:"item"`
	Transitions []TransitionView `json:"transitions"`
	Worktree    string           `json:"worktree,omitempty"`
}

// StateView is one status with its meaning and the moves the driver's
// LEGAL table actually carries — legalNext is probed through the same
// mergequeue.Legal the driver moves by, so the screen cannot show an edge
// the driver would refuse (adding a status constant changes this answer
// with no handler edit; that is the point of deriving it).
type StateView struct {
	Name      string   `json:"name"`
	Meaning   string   `json:"meaning"`
	LegalNext []string `json:"legalNext"`
}

// MethodDoorView names a method and the kind of door it is.
type MethodDoorView struct {
	Name string `json:"name"`
	Door string `json:"door"` // "read" | "human"
}

// VocabularyResult is mergequeue.vocabulary: every closed set the plane
// speaks, derived from the driver's own tables.
type VocabularyResult struct {
	States           []StateView      `json:"states"`
	Reasons          []string         `json:"reasons"`
	Modes            []string         `json:"modes"`
	ConflictPolicies []string         `json:"conflictPolicies"`
	LandMethods      []string         `json:"landMethods"`
	MergeActors      []string         `json:"mergeActors"`
	Methods          []MethodDoorView `json:"methods"`
}

// --- the seam -------------------------------------------------------------------------

// MergeQueueService is the daemon's queue door. Reads take context and the
// filter; writes take the actor the CONNECTION resolved, never a params
// field (MQ13). Every write returns the row as the store just wrote it.
type MergeQueueService interface {
	List(ctx context.Context, p MergeQueueListParams) ([]ItemView, error)
	Show(ctx context.Context, p MergeQueueShowParams) (ShowResult, error)
	Status(ctx context.Context, p MergeQueueStatusParams) (StatusResult, error)
	Vocabulary(ctx context.Context) (VocabularyResult, error)

	Enqueue(ctx context.Context, actor Actor, p MergeQueueEnqueueParams) (ItemView, error)
	Dequeue(ctx context.Context, actor Actor, p MergeQueueIDParams) (ItemView, error)
	Reorder(ctx context.Context, actor Actor, p MergeQueueReorderParams) (ItemView, error)
	Retry(ctx context.Context, actor Actor, p MergeQueueIDParams) (ItemView, error)
	Land(ctx context.Context, actor Actor, p MergeQueueLandParams) (ItemView, error)
	Settings(ctx context.Context, actor Actor, p MergeQueueSettingsParams) (ConfigView, string, error)
}

// --- registration ---------------------------------------------------------------------

// RegisterMergeQueue registers the ten queue doors over a non-nil seam. Nil
// means the build has no queue door service wired and the methods are
// ABSENT (V13) — the dispatcher answers CodeMethodNotFound, which is the
// truth, rather than registering ten doors that can only refuse. Returns
// whether they are in the table so the daemon's wiring is asserted, not
// trusted.
func (t *Table) RegisterMergeQueue() bool {
	if t.MergeQueue == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodMergeQueueList, RequiresInitialize: true, Handle: t.handleMergeQueueList},
		{Name: MethodMergeQueueShow, RequiresInitialize: true, Handle: t.handleMergeQueueShow},
		{Name: MethodMergeQueueStatus, RequiresInitialize: true, Handle: t.handleMergeQueueStatus},
		{Name: MethodMergeQueueVocabulary, RequiresInitialize: true, Handle: t.handleMergeQueueVocabulary},
		{Name: MethodMergeQueueEnqueue, RequiresInitialize: true, Handle: t.handleMergeQueueEnqueue},
		{Name: MethodMergeQueueDequeue, RequiresInitialize: true, Handle: t.handleMergeQueueDequeue},
		{Name: MethodMergeQueueReorder, RequiresInitialize: true, Handle: t.handleMergeQueueReorder},
		{Name: MethodMergeQueueRetry, RequiresInitialize: true, Handle: t.handleMergeQueueRetry},
		{Name: MethodMergeQueueLand, RequiresInitialize: true, Handle: t.handleMergeQueueLand},
		{Name: MethodMergeQueueSettings, RequiresInitialize: true, Handle: t.handleMergeQueueSettings},
	} {
		t.register(m)
	}
	return true
}

// --- the door-shared rules ---------------------------------------------------------------

// mergeQueueIdentityKeys names the keys no mergequeue param may carry: the
// actor is the door's (the connection's person), the status and reason are
// the driver's, and the stamped columns are the store's clock and evidence
// (MQ13, SEC-033's honesty clause).
var mergeQueueIdentityKeys = []string{
	"actor", "actorKind", "status", "reason", "reasonKind", "enqueuedBy",
	"landedSha", "landedSHA", "verifiedSha", "verifiedSHA", "catchUps",
	"createdAt", "updatedAt", "updatedBy", "position", "digestResult",
}

// rejectMergeQueueIdentityKeys checks the raw key BAG before the lenient
// struct decode (the policy-authoring/environment shape: additive keys are
// tolerated, identity and state keys never are). `position` is on the list
// because position is the queue's own assignment — reorder speaks by BEFORE
// id, never by a typed index.
func rejectMergeQueueIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range mergeQueueIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the caller names what to act on, never who acted, what state the row is in, or what evidence it carries (S60 MQ13)",
				method, key)}
		}
	}
	return nil
}

// mergeQueueActor stamps the write's author from the CONNECTION, never from
// params. The honest absence — no identity seam, a blank id, or the uid 0
// the transport never recorded — refuses BEFORE anything is read, let alone
// written, with the plane's one voice.
func (t *Table) mergeQueueActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrMergeQueueRefused.WithWhat("a merge-queue write is a person's door and no identity is available on this connection — the daemon stamped none")
	}
	a := t.ActorFor(ctx, sess)
	id := strings.TrimSpace(a.ID)
	if id == "" || id == "0" {
		return Actor{}, ErrMergeQueueRefused.WithWhat("a merge-queue write is a person's door and the peer is unresolvable (uid %q) — the actor is the connection's person, never a guess", a.ID)
	}
	return Actor{Kind: "user", ID: id}, nil
}

// mergeQueueSeam speaks when registration and the seam disagree:
// registration is conditional, so a nil here is a wiring bug, said out loud
// rather than answered with a nil dereference.
func (t *Table) mergeQueueSeam() error {
	if t.MergeQueue == nil {
		return ErrStoreUnavailable.WithWhat("the merge-queue doors need the daemon's queue door service, and this build did not link it — the surface is absent for the same reason (V13)")
	}
	return nil
}

// --- the handlers ------------------------------------------------------------------------

func (t *Table) handleMergeQueueList(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueList), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	var p MergeQueueListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return t.MergeQueue.List(ctx, p)
}

func (t *Table) handleMergeQueueShow(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueShow), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	var p MergeQueueShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergequeue.show needs an id — the queue has no default row")}
	}
	return t.MergeQueue.Show(ctx, p)
}

func (t *Table) handleMergeQueueStatus(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueStatus), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	var p MergeQueueStatusParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentRoot) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergequeue.status needs a componentRoot — the machine-wide view is mergequeue.list")}
	}
	return t.MergeQueue.Status(ctx, p)
}

func (t *Table) handleMergeQueueVocabulary(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueVocabulary), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
		var bag map[string]json.RawMessage
		if err := json.Unmarshal(raw, &bag); err == nil && len(bag) > 0 {
			return nil, ErrBadParams{Err: fmt.Errorf("mergequeue.vocabulary takes no params — the vocabulary is the plane's, not the caller's")}
		}
	}
	vocab, err := t.MergeQueue.Vocabulary(ctx)
	if err != nil {
		return nil, err
	}
	// The door half of the answer is derived HERE, from the one list the
	// registration itself walks — a method constant and its door kind can
	// only exist in this file once (MQ17's no-hand-copied-list rule).
	vocab.Methods = mergeQueueDoorViews()
	return vocab, nil
}

// mergeQueueReads names the four doors that carry no authority.
var mergeQueueReads = map[MethodName]bool{
	MethodMergeQueueList: true, MethodMergeQueueShow: true,
	MethodMergeQueueStatus: true, MethodMergeQueueVocabulary: true,
}

// mergeQueueDoorViews derives the methods table from mergequeueMethodNames
// — the same list RegisterMergeQueue walks — so the vocabulary answers the
// plane's real surface.
func mergeQueueDoorViews() []MethodDoorView {
	out := make([]MethodDoorView, 0, len(mergequeueMethodNames()))
	for _, n := range mergequeueMethodNames() {
		door := "human"
		if mergeQueueReads[n] {
			door = "read"
		}
		out = append(out, MethodDoorView{Name: string(n), Door: door})
	}
	return out
}

func (t *Table) handleMergeQueueEnqueue(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueEnqueue), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	actor, err := t.mergeQueueActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	var p MergeQueueEnqueueParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	return t.MergeQueue.Enqueue(ctx, actor, p)
}

func (t *Table) handleMergeQueueDequeue(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueDequeue), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	actor, err := t.mergeQueueActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	var p MergeQueueIDParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergequeue.dequeue needs an id")}
	}
	return t.MergeQueue.Dequeue(ctx, actor, p)
}

func (t *Table) handleMergeQueueReorder(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueReorder), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	actor, err := t.mergeQueueActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	var p MergeQueueReorderParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergequeue.reorder needs an id — position is moved by BEFORE-id, never typed")}
	}
	return t.MergeQueue.Reorder(ctx, actor, p)
}

func (t *Table) handleMergeQueueRetry(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueRetry), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	actor, err := t.mergeQueueActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	var p MergeQueueIDParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergequeue.retry needs an id")}
	}
	return t.MergeQueue.Retry(ctx, actor, p)
}

func (t *Table) handleMergeQueueLand(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueLand), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	actor, err := t.mergeQueueActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	var p MergeQueueLandParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Digest) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergequeue.land needs an id AND the digest from its mergequeue.show answer — the land is bound to the head the person saw, never to a hope")}
	}
	return t.MergeQueue.Land(ctx, actor, p)
}

func (t *Table) handleMergeQueueSettings(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectMergeQueueIdentityKeys(string(MethodMergeQueueSettings), raw); err != nil {
		return nil, err
	}
	if err := t.mergeQueueSeam(); err != nil {
		return nil, err
	}
	actor, err := t.mergeQueueActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	var p MergeQueueSettingsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ComponentRoot) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("mergequeue.settings needs a componentRoot — the settings are per repository")}
	}
	cfg, statement, err := t.MergeQueue.Settings(ctx, actor, p)
	if err != nil {
		return nil, err
	}
	return SettingsResult{Config: cfg, Statement: statement}, nil
}

// SettingsResult wraps the settings answer with the daemon's OWN sentence
// for the shape now in force (mergequeue.Statement, rendered verbatim —
// never a second formatter). An authority-widening write answers with this
// only after the caller acknowledged the statement's digest; the refusal
// that asks for the acknowledgement carries the same sentence in its `what`.
type SettingsResult struct {
	Config    ConfigView `json:"config"`
	Statement string     `json:"statement"`
}
