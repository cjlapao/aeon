package protocol

// S26 T5 — the `roadmap.*` socket methods. The same named departure from
// PL15 that CG9 granted the queue applies verbatim here (brief §5's D5):
// roadmap items are ordinary project content at the same trust tier as
// `loop.start`'s own params — planning content is not a guard — so the
// socket WRITES them, and every handler marshals around the SAME
// internal/roadmap calls the CLI dials. "CLI vs socket" is a transport
// choice, never two authoring paths, which is why there is no validation
// logic in this file beyond parameter shape: the door and the database
// CHECK are the two truths, and neither rides twice.
//
// The daemon-first rule (P34) is what the extra verbs ARE: `add/list/
// show/cancel/estimate` are the brief's five, and `priority/ideate/plan/
// materialize` are the machine's remaining doors — without them the
// eight-status state machine exists but nothing on the app can walk it,
// and "a capability that exists only on the daemon's inner path is not
// accepted as finished". Ideation and planning are daemon actions (the
// dispatch files own them, T3); materialization is R4's queue-or-direct
// decision (T4). All three ride Table seams the daemon fills, exactly as
// QueueAdvance does — nil seam answers ErrStoreUnavailable, loud.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/roadmap"
)

// --- the names ---------------------------------------------------------------
//
// These ride here with their handlers (queue.go's rule): one file owns the
// verb, its name, its params and its handler — nothing to forget to update
// twice.

const (
	MethodRoadmapAdd         MethodName = "roadmap.add"
	MethodRoadmapList        MethodName = "roadmap.list"
	MethodRoadmapShow        MethodName = "roadmap.show"
	MethodRoadmapCancel      MethodName = "roadmap.cancel"
	MethodRoadmapEstimate    MethodName = "roadmap.estimate"
	MethodRoadmapPriority    MethodName = "roadmap.priority"
	MethodRoadmapIdeate      MethodName = "roadmap.ideate"
	MethodRoadmapPlan        MethodName = "roadmap.plan"
	MethodRoadmapMaterialize MethodName = "roadmap.materialize"
	// MethodRoadmapHealth is S50 T3 (RM6): the honest answer about
	// the two blueprint settings — name, state, never a gate.
	MethodRoadmapHealth MethodName = "roadmap.health"
	// MethodRoadmapProposeIdea is S50 T5's ONE roadmap agent door (RM8):
	// it files an INERT proposal of kind roadmap_idea (the fifteenth kind —
	// an ask that one IDEA be added) through the records service. It is
	// registered CONDITIONALLY, on the records seam — nil seam ⇒ ABSENT,
	// never registered-and-refusing — and its actor is the STEP's, stamped
	// from the step context, never a param (OL3). It is a write by nature
	// and never a door: the eight person doors above stay the only ones
	// that move a row, and this one enforces nothing until a person
	// commits what it files (TD2).
	MethodRoadmapProposeIdea MethodName = "roadmap.proposeIdea"
	// MethodRoadmapDepend and MethodRoadmapUndepend are S27 T3: the two
	// graph doors. An edge is INTENT — the row pairs live in
	// roadmap_dependency, authored through the store's one write path,
	// which holds the same-root law, the self/duplicate refusals and the
	// cycle walk. Readiness is never a field these doors move: the graph
	// is read at every attempt (D4: decided now, never cached), so
	// `depend` can only ever BLOCK a pick and `undepend` free it.
	MethodRoadmapDepend   MethodName = "roadmap.depend"
	MethodRoadmapUndepend MethodName = "roadmap.undepend"
)

// ErrRoadmapRefused is the one roadmap-flavoured error: a state refusal
// with its reason carried in `what` (T2's discipline — a refusal names the
// reason AND the thing refused). Not-found rides it too: the socket does
// not answer a missing row with a silence.
var ErrRoadmapRefused = &Error{
	Code:    -31900,
	Message: "the roadmap refused the act",
	Data: Data{
		What:  "the item's status, its priority, or the act itself broke a roadmap law (R1's table, D2, D3, D4)",
		Means: "nothing moved — the row keeps the state it had",
		Todo:  "read the reason in `what`, then roadmap.list to see the board as the daemon sees it",
		Fatal: false,
	},
}

func roadmapRefusal(reason string) *Error {
	return ErrRoadmapRefused.WithWhat("%s", reason)
}

// ErrRoadmapNoActor is S50 T1 (RM2): the roadmap writes are a person's
// door, the actor is the CONNECTION's person (the daemon's ActorFor seam,
// the kernel's uid — never a param), and an unresolvable peer refuses
// BEFORE anything is read, let alone written (the blueprint/mcp shape).
// Nothing half-stamped and nothing anonymous ever reaches the audit.
var ErrRoadmapNoActor = &Error{
	Code:    -31931,
	Message: "the roadmap write could not name its author",
	Data: Data{
		What:  "the roadmap write is a person's door and no identity is available on this connection — the audit row must name WHO wrote it (RM2), and an anonymous write is the gap this table exists to close",
		Means: "nothing moved — the item keeps the state it had and no audit row was written",
		Todo:  "connect as the person who owns the change (the endpoint is 0600, peer-credentialed); an agent never writes the roadmap (RM4) — it reads and proposes only",
		Fatal: false,
	},
}

// roadmapActor stamps the write's author from the CONNECTION (the daemon's
// ActorFor seam), never from params. The honest absence — no identity
// seam, a blank id, or the uid 0 a connection the transport never recorded
// reads as (the blueprint shape: uid 0 is refused, never stamped as the
// person "0") — refuses with ErrRoadmapNoActor before anything is read
// (RM2). The kind is the roadmap audit's own vocabulary (roadmap.Actor =
// store.RoadmapActor): user | cli | system; a stamped socket peer is the
// person, "user".
func (t *Table) roadmapActor(ctx context.Context, sess *Session) (roadmap.Actor, error) {
	if t.ActorFor == nil {
		return roadmap.Actor{}, ErrRoadmapNoActor.WithWhat("the roadmap write is a person's door and no identity is available on this connection — the daemon stamped none")
	}
	a := t.ActorFor(ctx, sess)
	id := strings.TrimSpace(a.ID)
	if id == "" || id == "0" {
		return roadmap.Actor{}, ErrRoadmapNoActor.WithWhat("the roadmap write is a person's door and the peer is unresolvable (uid %q) — the actor is the connection's person, never a guess", a.ID)
	}
	return roadmap.Actor{Kind: "user", ID: id}, nil
}

// roadmapIdentityKeys names the keys a roadmap WRITE call may never carry:
// the actor is stamped by the door that holds the connection (the backup
// shape), and a param naming it would let a caller forge the audit identity
// of the write (RM2's wire-side twin).
var roadmapIdentityKeys = []string{"actor", "actorKind", "who"}

// rejectRoadmapIdentityKeys checks the raw key bag BEFORE the lenient
// struct decode (decodeParams is deliberately lenient — unknown keys are
// ignored — so the refusal lives here or nowhere).
func rejectRoadmapIdentityKeys(method MethodName, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range roadmapIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf("%s refuses the param %q — the actor is stamped from the connection, never carried in params (RM2)", method, key)}
		}
	}
	return nil
}

// --- params ------------------------------------------------------------------

// RoadmapAddParams is D5's honest create: provenance is REQUIRED on the
// wire (the column has no default at the database on purpose) and
// priority rides its three-state name — never a number, never blank
// (D2). An absent `priorityState` is the honest 'unknown', said in its
// own word, not a collapse.
type RoadmapAddParams struct {
	ComponentRoot string `json:"componentRoot"`
	Title         string `json:"title"`
	Epic          string `json:"epic,omitempty"`
	PriorityState string `json:"priorityState,omitempty"`
	PriorityLabel string `json:"priorityLabel,omitempty"`
	EstimateMin   *int   `json:"estimateMin,omitempty"`
	EstimateMax   *int   `json:"estimateMax,omitempty"`
	EstimateBasis string `json:"estimateBasis,omitempty"`
	Provenance    string `json:"provenance"`
}

type RoadmapListParams struct {
	ComponentRoot string `json:"componentRoot"`
	Epic          string `json:"epic,omitempty"`
	Status        string `json:"status,omitempty"`
	PriorityState string `json:"priorityState,omitempty"`
}

type RoadmapShowParams struct {
	ID int64 `json:"id"`
}

type RoadmapCancelParams struct {
	ID int64 `json:"id"`
}

// RoadmapDependParams names BOTH ends of one edge. There is no batch
// shape on purpose: an edge is one decision about one pair, and the
// refusal that names a cycle names the pair that would have closed it.
type RoadmapDependParams struct {
	ID        int64 `json:"id"`
	DependsOn int64 `json:"dependsOn"`
}

// RoadmapUndependParams removes exactly one edge. It never cascades,
// never reverses, and never touches a loop — the edge recorded intent,
// and removing it changes only what the NEXT readiness read sees.
type RoadmapUndependParams struct {
	ID        int64 `json:"id"`
	DependsOn int64 `json:"dependsOn"`
}

// RoadmapEstimateParams carries the whole range with its basis or nothing:
// the three arrive together (D3's never-a-bare-figure is the door's
// shape-check too, and the database CHECK is the backstop).
type RoadmapEstimateParams struct {
	ID            int64  `json:"id"`
	EstimateMin   int    `json:"estimateMin"`
	EstimateMax   int    `json:"estimateMax"`
	EstimateBasis string `json:"estimateBasis"`
}

// RoadmapPriorityParams is ONE decision (D2): state and label travel
// together; a move to 'unknown' with a label is refused at the door,
// never precedence-resolved into "clear the label".
type RoadmapPriorityParams struct {
	ID            int64  `json:"id"`
	PriorityState string `json:"priorityState"`
	PriorityLabel string `json:"priorityLabel,omitempty"`
}

// RoadmapIdeateParams starts the ideation loop (D1's ordinary loop, T3).
// `blueprint` names an EXISTING blueprint that already declares
// produces: brief.md — the daemon validates the declaration and refuses
// loudly otherwise; ideation ends at the engine's existing check, which
// only exists if the blueprint carries the declaration.
type RoadmapIdeateParams struct {
	ID            int64  `json:"id"`
	WorkspaceRoot string `json:"workspaceRoot"`
	Blueprint     string `json:"blueprint,omitempty"`
	Attended      *bool  `json:"attended,omitempty"`
}

// RoadmapPlanParams dispatches the project-planner (S13's existing
// handler, D9) against the brief-agreed item.
type RoadmapPlanParams struct {
	ID            int64  `json:"id"`
	WorkspaceRoot string `json:"workspaceRoot"`
}

// RoadmapMaterializeParams is the materialization payload (T4): the
// same LoopStartParams-shaped facts a direct loop.start would carry,
// whether the item enters S24's queue or starts directly — the identical
// loop.start call is the shared tail of both branches (R4), so the shape
// is identical too.
type RoadmapMaterializeParams struct {
	ID            int64             `json:"id"`
	Blueprint     string            `json:"blueprint"`
	Goal          string            `json:"goal"`
	WorkspaceRoot string            `json:"workspaceRoot"`
	Tech          string            `json:"tech,omitempty"`
	With          map[string]string `json:"with,omitempty"`
	Attended      *bool             `json:"attended,omitempty"`
}

// --- views -------------------------------------------------------------------

// RoadmapItemView renders an item the honest way, every state named:
//
//   - `priorityState` is ALWAYS the word — "known", "assumed", or the
//     honest "unknown", never blank, never a number (D2, brief R2: the
//     sort column that sorts unknown last is the failure this field
//     exists to prevent). The label rides only when the state earns it.
//   - `estimate` is the range with its basis, all-present-or-all-absent
//     (D3, P32 — queue's size rendering discipline carried).
//   - briefPath/planPath are POINTERS, never content (D9).
//   - `loopSlug` is simply absent until materialization (D6: nothing is
//     read off what the item points at).
type RoadmapItemView struct {
	ID            int64   `json:"id"`
	ComponentRoot string  `json:"componentRoot"`
	Title         string  `json:"title"`
	Epic          string  `json:"epic,omitempty"`
	Status        string  `json:"status"`
	PriorityState string  `json:"priorityState"`
	PriorityLabel *string `json:"priorityLabel"`
	EstimateMin   *int    `json:"estimateMin"`
	EstimateMax   *int    `json:"estimateMax"`
	EstimateBasis *string `json:"estimateBasis"`
	Provenance    string  `json:"provenance"`
	BriefPath     string  `json:"briefPath,omitempty"`
	PlanPath      string  `json:"planPath,omitempty"`
	LoopSlug      string  `json:"loopSlug,omitempty"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
	// Additive: when the item first started running and when it finished (done, else cancelled), as
	// the append-only audit recorded the moves. omitempty — a time that has not happened is absent,
	// never a zero date, so an item that never ran answers with the bytes it always answered with.
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
	// S27 T3 (additive): the graph beside the row. `dependencies` lists
	// the DIRECT upstream items the readiness reads (id, current status,
	// title — the same title roadmap.show already returns, never an audit
	// summary, CN16); `dependents` counts the live items waiting on THIS
	// one — the quiet number before a cancel strands someone. Both are
	// omitempty so an edge-less item answers with the bytes it always
	// answered with: every pre-S27 fixture stays byte-equal, and the
	// absence is honest ("no edges"), never a hidden zero.
	Dependencies []RoadmapDepView `json:"dependencies,omitempty"`
	Dependents   int              `json:"dependents,omitempty"`
}

// RoadmapDepView is one edge's other end as the readiness sees it.
type RoadmapDepView struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
}

type RoadmapListResult struct {
	ComponentRoot string            `json:"componentRoot"`
	Items         []RoadmapItemView `json:"items"`
}

type RoadmapItemResult struct {
	Item RoadmapItemView `json:"item"`
}

func roadmapItemView(it roadmap.Item) RoadmapItemView {
	v := RoadmapItemView{
		ID: it.ID, ComponentRoot: it.ComponentRoot, Title: it.Title, Epic: it.Epic,
		Status: string(it.Status), PriorityState: string(it.Priority),
		Provenance: string(it.Provenance),
		BriefPath:  it.BriefPath, PlanPath: it.PlanPath, LoopSlug: it.LoopSlug,
		CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
	if it.PriorityLabel != "" {
		label := it.PriorityLabel
		v.PriorityLabel = &label
	}
	if it.Estimate != nil {
		v.EstimateMin = &it.Estimate.Min
		v.EstimateMax = &it.Estimate.Max
		b := it.Estimate.Basis
		v.EstimateBasis = &b
	}
	return v
}

// roadmapGraphFields fills the S27 T3 additive fields on an item view:
// the direct upstream edges readiness reads, and the count of live items
// waiting on this one. The reads go through the same facade the doors
// write through — one graph, one truth. A failed read is an ANSWERED
// error, never a silently absent field: a missing `dependencies` would
// otherwise read exactly like "no edges", and that is the silence this
// house does not ship.
func roadmapGraphFields(r *roadmap.Roadmap, it roadmap.Item) (RoadmapItemView, error) {
	v := roadmapItemView(it)
	started, finished, err := r.MoveTimes(context.Background(), it.ID)
	if err != nil {
		return v, err
	}
	v.StartedAt, v.FinishedAt = started, finished
	deps, err := r.Dependencies(it.ID)
	if err != nil {
		return v, err
	}
	for _, d := range deps {
		v.Dependencies = append(v.Dependencies, RoadmapDepView{ID: d.ID, Status: string(d.Status), Title: d.Title})
	}
	n, err := r.DependentsCount(it.ID)
	if err != nil {
		return v, err
	}
	v.Dependents = n
	return v, nil
}

// roadmapItemAnswer is the one shape every roadmap write answers with:
// the row it moved, with the graph beside it.
func roadmapItemAnswer(r *roadmap.Roadmap, it roadmap.Item) (any, error) {
	v, err := roadmapGraphFields(r, it)
	if err != nil {
		return nil, err
	}
	return RoadmapItemResult{Item: v}, nil
}

// --- registration (called from method.go's table assembly) -------------------

func (t *Table) registerRoadmap() {
	t.register(&Method{Name: MethodRoadmapAdd, RequiresInitialize: true, Handle: t.handleRoadmapAdd})
	t.register(&Method{Name: MethodRoadmapList, RequiresInitialize: true, Handle: t.handleRoadmapList})
	t.register(&Method{Name: MethodRoadmapShow, RequiresInitialize: true, Handle: t.handleRoadmapShow})
	t.register(&Method{Name: MethodRoadmapCancel, RequiresInitialize: true, Handle: t.handleRoadmapCancel})
	t.register(&Method{Name: MethodRoadmapEstimate, RequiresInitialize: true, Handle: t.handleRoadmapEstimate})
	t.register(&Method{Name: MethodRoadmapPriority, RequiresInitialize: true, Handle: t.handleRoadmapPriority})
	t.register(&Method{Name: MethodRoadmapIdeate, RequiresInitialize: true, Handle: t.handleRoadmapIdeate})
	t.register(&Method{Name: MethodRoadmapPlan, RequiresInitialize: true, Handle: t.handleRoadmapPlan})
	t.register(&Method{Name: MethodRoadmapMaterialize, RequiresInitialize: true, Handle: t.handleRoadmapMaterialize})
	t.register(&Method{Name: MethodRoadmapDepend, RequiresInitialize: true, Handle: t.handleRoadmapDepend})
	t.register(&Method{Name: MethodRoadmapUndepend, RequiresInitialize: true, Handle: t.handleRoadmapUndepend})
	// S50 T2: the four management doors ride the same gate — no seam, no
	// door answers (V13), and they are only ever registered here.
	t.registerRoadmapManage()
}

// --- handlers ----------------------------------------------------------------

func (t *Table) requireRoadmap() (*roadmap.Roadmap, error) {
	if t.Roadmap == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Roadmap, nil
}

func (t *Table) handleRoadmapAdd(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapIdentityKeys(MethodRoadmapAdd, raw); err != nil {
		return nil, err
	}
	var p RoadmapAddParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ComponentRoot == "" || p.Title == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("componentRoot and title are required — the forward Gantt renders named rows in one project")}
	}
	if p.Provenance == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("provenance is required — authored, triggered or triaged (D5); the column has no default on purpose, so the writer answers where the item came from")}
	}
	// D3's shape law is decided HERE, before the seam: a half estimate is a
	// shape fact, not a store fact — its refusal must not depend on whether
	// a daemon was wired (queue's precedent).
	if p.EstimateMin != nil || p.EstimateMax != nil || p.EstimateBasis != "" {
		if p.EstimateMin == nil || p.EstimateMax == nil || p.EstimateBasis == "" {
			return nil, ErrBadParams{Err: fmt.Errorf("an estimate is a range with its basis or no estimate at all (D3) — min, max and basis arrive together")}
		}
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	in := roadmap.NewItem{
		Title: p.Title, Epic: p.Epic,
		Priority: roadmap.PriorityState(p.PriorityState), PriorityLabel: p.PriorityLabel,
		Provenance: roadmap.Provenance(p.Provenance),
	}
	if p.EstimateMin != nil {
		in.Estimate = &roadmap.Estimate{Min: *p.EstimateMin, Max: *p.EstimateMax, Basis: p.EstimateBasis}
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	it, ref := r.CreateAudited(p.ComponentRoot, in, actor)
	if ref != nil {
		return nil, roadmapRefusal(ref.Reason)
	}
	return roadmapItemAnswer(r, it)
}

func (t *Table) handleRoadmapList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	// S50 T5 (rule 4): thin — decode, then the shared answer the step's
	// roadmap_list twin runs too. The rendering lives in roadmap_reads.go.
	var p RoadmapListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if err := roadmapListShape(p); err != nil { // shape before the seam
		return nil, err
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	return AnswerRoadmapList(r, p)
}

func (t *Table) handleRoadmapShow(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	// S50 T5 (rule 4): thin — the shared answer, beside the step's
	// roadmap_show twin (which gets the history page with it).
	var p RoadmapShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if err := roadmapShowShape(p); err != nil { // shape before the seam
		return nil, err
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	return AnswerRoadmapShow(r, p)
}

func (t *Table) handleRoadmapCancel(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapIdentityKeys(MethodRoadmapCancel, raw); err != nil {
		return nil, err
	}
	var p RoadmapCancelParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id")}
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	it, ref := r.CancelAudited(p.ID, actor)
	if ref != nil {
		return nil, roadmapRefusal(ref.Reason)
	}
	return roadmapItemAnswer(r, it)
}

func (t *Table) handleRoadmapDepend(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapIdentityKeys(MethodRoadmapDepend, raw); err != nil {
		return nil, err
	}
	var p RoadmapDependParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	// The positive-id law lives in the shared answer now — both doors
	// refuse the same question the same words (rule 4, S27 T3).
	return AnswerRoadmapDepend(r, actor, p)
}

// AnswerRoadmapDepend is the ONE road both doors travel (rule 4): the
// person's socket handler and the roadmap_depend tool arm of the records
// service call THIS, so the edge is authored, re-read and rendered in one
// place — an actor stamped by the caller's door (peer for the socket, the
// step's agent slug for the tool), the store's refusal sentences verbatim,
// and the item answered beside its graph. 2026-11-05 (S27 T3), following
// AnswerTriageFileAs's precedent — a write twin's parity is structural,
// never a copy of sentences.
// edgeRefusal is a roadmap refusal with the graph's own way-out sentence:
// every edge refusal ends where an edge can always be undone —
// roadmap.undepend with the same pair (S27 T3: the refusal that names the
// block without naming the exit is half an answer).
func edgeRefusal(reason string) *Error {
	e := roadmapRefusal(reason)
	e.Data.Todo = "if the edge must go, roadmap.undepend {id, dependsOn} removes exactly the named pair and nothing else; roadmap.board shows what waits on what"
	return e
}

func AnswerRoadmapDepend(r *roadmap.Roadmap, actor roadmap.Actor, p RoadmapDependParams) (any, error) {
	if p.ID <= 0 || p.DependsOn <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id and dependsOn are required and must both be positive row ids — an edge names two real items")}
	}
	if ref := r.Depend(actor, p.ID, p.DependsOn); ref != nil {
		return nil, edgeRefusal(ref.Reason)
	}
	it, ok, err := r.Show(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, edgeRefusal(fmt.Sprintf("roadmap item %d vanished under the edge — say so loudly", p.ID))
	}
	return roadmapItemAnswer(r, it)
}

func (t *Table) handleRoadmapUndepend(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapIdentityKeys(MethodRoadmapUndepend, raw); err != nil {
		return nil, err
	}
	var p RoadmapUndependParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	// The positive-id law lives in the shared answer now (rule 4, S27 T3).
	return AnswerRoadmapUndepend(r, actor, p)
}

// AnswerRoadmapUndepend is AnswerRoadmapDepend's twin — the ONE road the
// person's socket handler and the roadmap_undepend tool arm travel (S27 T3).
// Removing an edge touches the graph and nothing else: no cascade, no loop
// touched, no status moved, and the item answers beside its remaining graph.
func AnswerRoadmapUndepend(r *roadmap.Roadmap, actor roadmap.Actor, p RoadmapUndependParams) (any, error) {
	if p.ID <= 0 || p.DependsOn <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id and dependsOn are required and must both be positive row ids — an edge to remove is named by its pair")}
	}
	if ref := r.Undepend(actor, p.ID, p.DependsOn); ref != nil {
		return nil, edgeRefusal(ref.Reason)
	}
	it, ok, err := r.Show(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, edgeRefusal(fmt.Sprintf("roadmap item %d vanished under the removal — say so loudly", p.ID))
	}
	return roadmapItemAnswer(r, it)
}

func (t *Table) handleRoadmapEstimate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapIdentityKeys(MethodRoadmapEstimate, raw); err != nil {
		return nil, err
	}
	var p RoadmapEstimateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id")}
	}
	if p.EstimateBasis == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("estimateBasis is required — a range without its reason is the bare figure D3 refuses")}
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	it, ref := r.SetEstimateAudited(p.ID, p.EstimateMin, p.EstimateMax, p.EstimateBasis, actor)
	if ref != nil {
		return nil, roadmapRefusal(ref.Reason)
	}
	return roadmapItemAnswer(r, it)
}

func (t *Table) handleRoadmapPriority(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapIdentityKeys(MethodRoadmapPriority, raw); err != nil {
		return nil, err
	}
	var p RoadmapPriorityParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id")}
	}
	if p.PriorityState == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("priorityState is required — known, assumed, or the honest unknown (D2); an empty string is not a priority state, it is the collapse D2 forbids")}
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	it, ref := r.SetPriorityAudited(p.ID, roadmap.PriorityState(p.PriorityState), p.PriorityLabel, actor)
	if ref != nil {
		return nil, roadmapRefusal(ref.Reason)
	}
	return roadmapItemAnswer(r, it)
}

// --- the daemon-action verbs ---------------------------------------------------
//
// ideate/plan/materialize are the machine's remaining doors (P34): the
// state machine exists ONLY as a surface if the app can walk it. Each
// rides a Table seam the daemon fills from its dispatch files (T3/T4) —
// nil seam answers ErrStoreUnavailable, loud, never a silent no-op.

func (t *Table) handleRoadmapIdeate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapIdentityKeys(MethodRoadmapIdeate, raw); err != nil {
		return nil, err
	}
	var p RoadmapIdeateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 || p.WorkspaceRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id and workspaceRoot are required — ideation is an ordinary loop, and an ordinary loop needs a workspace")}
	}
	if t.RoadmapIdeate == nil {
		return nil, ErrStoreUnavailable
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, err := t.RoadmapIdeate(ctx, actor, p)
	if err != nil {
		return nil, asRoadmapRefusal(err)
	}
	return res, nil
}

func (t *Table) handleRoadmapPlan(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapIdentityKeys(MethodRoadmapPlan, raw); err != nil {
		return nil, err
	}
	var p RoadmapPlanParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 || p.WorkspaceRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id and workspaceRoot are required — the planner reads the brief and writes plan.md where the item lives")}
	}
	if t.RoadmapPlan == nil {
		return nil, ErrStoreUnavailable
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, err := t.RoadmapPlan(ctx, actor, p)
	if err != nil {
		return nil, asRoadmapRefusal(err)
	}
	return res, nil
}

func (t *Table) handleRoadmapMaterialize(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapIdentityKeys(MethodRoadmapMaterialize, raw); err != nil {
		return nil, err
	}
	var p RoadmapMaterializeParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 || p.Blueprint == "" || p.Goal == "" || p.WorkspaceRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id, blueprint, goal and workspaceRoot are required — materialization ends at one loop.start call carrying exactly these (R4)")}
	}
	if t.RoadmapMaterialize == nil {
		return nil, ErrStoreUnavailable
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, err := t.RoadmapMaterialize(ctx, actor, p)
	if err != nil {
		return nil, asRoadmapRefusal(err)
	}
	return res, nil
}

// asRoadmapRefusal keeps the daemon seam's own sentence verbatim when it
// is already an Error (a loader refusal rides out raw, queue's precedent)
// and wraps a plain refusal reason in the roadmap error otherwise.
func asRoadmapRefusal(err error) error {
	if ae, ok := err.(*Error); ok {
		return ae
	}
	return roadmapRefusal(err.Error())
}
