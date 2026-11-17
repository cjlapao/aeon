package roadmap

// ════════════════════════════════════════════════════════════════════════════
// Audited door writes — S50 T1 (RM2): every production roadmap write names
// its actor and appends its roadmap_change row in the SAME transaction as
// the write.
//
// The rule the shapes encode:
//
//   - ONE audit row per write, fixed-template summary (ids, statuses and
//     enums — never caller text; SEC-033 kept). The templates live HERE,
//     the only place allowed to phrase an audit line.
//   - NO audit for a no-op: the CAS loser commits nothing and is audited
//     nothing.
//   - Actor is mandatory and lawful: user | cli | system with a named id.
//     An agent actor is refused with its own sentence — an agent never
//     writes the roadmap (RM4); the socket layer refuses an unresolvable
//     peer with ErrRoadmapNoActor BEFORE arriving, and this door refuses
//     anything malformed anyway (no placeholder actors exist on this path).
//   - The transition machine is not re-implemented here: audited moves run
//     through transitionCore, the one machine, with the audited write
//     injected as the claim step.
//
// The plain door methods (Create, Transition, Cancel, SetEstimate,
// SetPriority, SetArtifacts, RecordLoop) stay for direct-store callers and
// S26's own tests; EVERY production call site — the four socket handlers,
// the dispatch file, the reactor, triage's create — moved to the audited
// half below (the wrapper list is recorded in the task Notes).
// ════════════════════════════════════════════════════════════════════════════

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Actor is the roadmap audit's author vocabulary (store.RoadmapActor):
// user | cli | system plus a non-blank id. Aliased so door callers write
// roadmap.Actor and the vocabulary stays the store's one definition.
type Actor = store.RoadmapActor

// SystemReactor is the actor the daemon-driven writes carry — the loop
// reactor landing a brief or a plan, marking an item done, recording a
// materialization link, and the triage landing. RM2's point in a name: a
// person can see WHAT MOVED ON ITS OWN.
var SystemReactor = Actor{Kind: "system", ID: "reactor"}

// actorRefusal is the door's pre-SQL gate on an audit author. The two
// sentences are distinct on purpose: an AGENT authoring roadmap rows is a
// policy refusal (RM4), and an ANONYMOUS author is the audit gap the whole
// table exists to close.
func actorRefusal(a Actor) *Refusal {
	if a.Kind == "agent" {
		return &Refusal{Reason: "an agent never writes the roadmap (RM4) — dispatch and every state move are a person's act; agents read and propose only"}
	}
	if !a.Valid() {
		return &Refusal{Reason: fmt.Sprintf("the roadmap write carries no lawful actor (kind %q, id %q) — the audit names WHO wrote it; user, cli or system with a named id, never a placeholder", a.Kind, a.ID)}
	}
	return nil
}

// auditSummaries are the fixed templates, the only phrasing an audit row
// ever carries (the add row is composed inside the store, where the id is
// born). Ids, statuses and enums, never a word of user text.
// moveArrow is the join in the move sentences below; MoveTimes finds them by it, so it lives once.
const moveArrow = " → "

func summaryMove(id int64, from, to Status) string {
	return fmt.Sprintf("moved item %d %s"+moveArrow+"%s", id, from, to)
}
func summaryMoveLanded(id int64, from, to Status, what string) string {
	return fmt.Sprintf("moved item %d %s"+moveArrow+"%s (%s landed)", id, from, to, what)
}
func summaryEstimate(id int64) string {
	return fmt.Sprintf("estimated item %d (range with its basis recorded)", id)
}
func summaryPriority(id int64, state PriorityState) string {
	return fmt.Sprintf("set priority on item %d to %s", id, state)
}
func summaryLoopRecorded(id int64, slug string) string {
	return fmt.Sprintf("recorded item %d on loop %s", id, slug)
}

// CreateAudited is Create with an actor: the item and its 'add' audit row
// commit in one transaction. Shape law and sentences are Create's — the
// audited twin runs through the same createRow machine.
func (r *Roadmap) CreateAudited(componentRoot string, in NewItem, a Actor) (Item, *Refusal) {
	if ref := actorRefusal(a); ref != nil {
		return Item{}, ref
	}
	row, ref := r.createRow(componentRoot, in)
	if ref != nil {
		return Item{}, ref
	}
	// The 'add' audit carries op and summary composed by the store itself
	// (the id does not exist until the INSERT lands; the template
	// "added item %d" is fixed and id-only, so the store owns this one
	// line — every other op's summary arrives composed by the door).
	created, err := r.s.CreateRoadmapItemAudited(row, store.RoadmapAudit{Actor: a})
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the create: %v", err)}
	}
	it, err := ItemFromRow(created)
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the row that came back is not a legal item: %v", err)}
	}
	return it, nil
}

// TransitionAudited is Transition through the one machine, with the move
// and its audit in one transaction. `op` names which verb the move serves
// — the dispatch's ideate moves with op "ideate", the human cancel with
// "cancel", the plain state moves with "transition", the materialization
// with "materialize". The summary is the fixed move template; op is the
// verb, never the sentence.
func (r *Roadmap) TransitionAudited(id int64, to Status, op string, a Actor) (Item, *Refusal) {
	if ref := actorRefusal(a); ref != nil {
		return Item{}, ref
	}
	if !validTransitionOp(op) {
		return Item{}, &Refusal{Reason: fmt.Sprintf("audit op %q is not one of the ops that move a status (transition, ideate, plan, materialize, cancel)", op)}
	}
	return r.transitionCore(id, to, func(from string) (bool, error) {
		return r.s.TransitionRoadmapItemAudited(id, from, string(to), store.RoadmapAudit{
			Op:      op,
			Summary: summaryMove(id, Status(from), to),
			Actor:   a,
		})
	})
}

// CancelAudited is Cancel with an actor and the 'cancel' op (RM2's list
// names cancel its own op — D7's never-delete decision is auditable as
// itself, not hidden inside a generic move).
func (r *Roadmap) CancelAudited(id int64, a Actor) (Item, *Refusal) {
	return r.TransitionAudited(id, StatusCancelled, "cancel", a)
}

// SetEstimateAudited writes the range and audits 'estimate' beside it, one
// transaction, one row (the no-op case — the store refusing the shape or
// the row being absent — earns nothing, per the shared refusal path).
func (r *Roadmap) SetEstimateAudited(id int64, min, max int, basis string, a Actor) (Item, *Refusal) {
	if ref := actorRefusal(a); ref != nil {
		return Item{}, ref
	}
	if basis == "" {
		return Item{}, &Refusal{Reason: "an estimate without its basis is not an estimate (D3) — the words are the point"}
	}
	if min > max {
		return Item{}, &Refusal{Reason: fmt.Sprintf("estimate min %d is above max %d — a range that contains nothing estimates nothing", min, max)}
	}
	if _, ok, err := r.Show(id); err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: read item %d: %v", id, err)}
	} else if !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap item %d does not exist", id)}
	}
	wrote, err := r.s.SetRoadmapItemEstimateAudited(id, min, max, basis, store.RoadmapAudit{
		Op: "estimate", Summary: summaryEstimate(id), Actor: a,
	})
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the estimate: %v", err)}
	}
	if !wrote {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap item %d moved or vanished under the estimate — nothing was written and nothing was audited", id), Moved: true}
	}
	fresh, ok, err := r.Show(id)
	if err != nil || !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the estimate landed but the row could not be re-read: %v", err)}
	}
	return fresh, nil
}

// SetPriorityAudited writes the pair and audits 'priority' beside it.
func (r *Roadmap) SetPriorityAudited(id int64, state PriorityState, label string, a Actor) (Item, *Refusal) {
	if ref := actorRefusal(a); ref != nil {
		return Item{}, ref
	}
	if !ValidPriorityState(state) {
		return Item{}, &Refusal{Reason: fmt.Sprintf("priority %q is not one of the three — known, assumed, unknown (D2; a number is never one)", state)}
	}
	if state == PriorityUnknown && label != "" {
		return Item{}, &Refusal{Reason: fmt.Sprintf("a priority label (%q) on an unknown priority contradicts itself (D2) — set the state or drop the label", label)}
	}
	if _, ok, err := r.Show(id); err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: read item %d: %v", id, err)}
	} else if !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap item %d does not exist", id)}
	}
	var lp *string
	if label != "" {
		lp = &label
	}
	wrote, err := r.s.SetRoadmapItemPriorityAudited(id, string(state), lp, store.RoadmapAudit{
		Op: "priority", Summary: summaryPriority(id, state), Actor: a,
	})
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the priority: %v", err)}
	}
	if !wrote {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap item %d moved or vanished under the priority — nothing was written and nothing was audited", id), Moved: true}
	}
	fresh, ok, err := r.Show(id)
	if err != nil || !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the priority landed but the row could not be re-read: %v", err)}
	}
	return fresh, nil
}

// LandBriefAudited is the reactor's brief landing: the pointer and the
// ideating → brief-agreed move in ONE transaction earning exactly ONE
// audit row (op "transition" — the move is what happened; the artifact is
// part of it). The transition machine still decides; a moved-under item
// lands nothing and is audited nothing.
func (r *Roadmap) LandBriefAudited(id int64, briefPath string, a Actor) (Item, *Refusal) {
	return r.landAudited(id, briefPath, "", StatusBriefAgreed, "brief", "transition", a)
}

// LandPlanAudited is the planner's landing: the plan pointer and the
// brief-agreed → planned move, one transaction, one 'plan' audit row.
func (r *Roadmap) LandPlanAudited(id int64, planPath string, a Actor) (Item, *Refusal) {
	return r.landAudited(id, "", planPath, StatusPlanned, "plan", "plan", a)
}

func (r *Roadmap) landAudited(id int64, briefPath, planPath string, to Status, what, op string, a Actor) (Item, *Refusal) {
	if ref := actorRefusal(a); ref != nil {
		return Item{}, ref
	}
	if briefPath == "" && planPath == "" {
		return Item{}, &Refusal{Reason: "a landing names its artifact — landing a status with no pointer is the half-write R3 refuses"}
	}
	var bp, pp *string
	if briefPath != "" {
		bp = &briefPath
	}
	if planPath != "" {
		pp = &planPath
	}
	return r.transitionCore(id, to, func(from string) (bool, error) {
		return r.s.RoadmapItemLandingAudited(id, bp, pp, from, string(to), store.RoadmapAudit{
			Op:      op,
			Summary: summaryMoveLanded(id, Status(from), to, what),
			Actor:   a,
		})
	})
}

// RecordLoopAudited records the materialization link and audits
// 'materialize' beside it — the reactor/queue path carries
// SystemReactor, the direct dispatch path may carry the person who asked.
func (r *Roadmap) RecordLoopAudited(id int64, componentRoot, loopSlug string, a Actor) (Item, *Refusal) {
	if ref := actorRefusal(a); ref != nil {
		return Item{}, ref
	}
	if loopSlug == "" {
		return Item{}, &Refusal{Reason: "a loop record names a loop — an empty slug is the sentinel the column forbids"}
	}
	if componentRoot == "" {
		return Item{}, &Refusal{Reason: "a loop record is scoped — naming a loop without the caller's root is the unscoped write the guard forbids"}
	}
	wrote, err := r.s.SetRoadmapItemLoopAudited(id, componentRoot, loopSlug, string(StatusRunning), store.RoadmapAudit{
		Op: "materialize", Summary: summaryLoopRecorded(id, loopSlug), Actor: a,
	})
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the loop record: %v", err)}
	}
	if !wrote {
		return Item{}, &Refusal{
			Reason: fmt.Sprintf("roadmap item %d is not `running` (or moved under the record) — a loop record lands only on the rung that produced the loop", id),
			Moved:  true,
		}
	}
	fresh, ok, err := r.Show(id)
	if err != nil || !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the loop record landed but the row could not be re-read: %v", err)}
	}
	if fresh.LoopSlug != loopSlug {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap item %d carries loop %q, not %q — the record did not land as written", id, fresh.LoopSlug, loopSlug)}
	}
	return fresh, nil
}

func validTransitionOp(op string) bool {
	switch op {
	case "transition", "ideate", "plan", "materialize", "cancel":
		return true
	}
	return false
}
