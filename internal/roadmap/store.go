package roadmap

import (
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

// Roadmap is the one door every other task calls. It wraps the store's
// roadmap methods the way internal/queue's Queue wraps its pair: the
// handle stays in internal/store, the decisions live here.
type Roadmap struct {
	s *store.Store
}

// New wires the roadmap to the store. One instance per daemon is the
// intent (the socket door and the dispatch files share it), though the
// type holds no state beyond the handle, so tests may open as many as
// they like.
func New(s *store.Store) *Roadmap { return &Roadmap{s: s} }

// Refusal is DB19 again (queue's shape carried): the discriminated answer
// to "may this move". OK with a refreshed item, or a Reason that names the
// thing refused and the alternative (house refusal rule), never a bare
// error for a refusal.
type Refusal struct {
	Reason string
	// Moved is true when the refusal is a RACE — the claimed-FROM update
	// found the row had changed status under us. The caller re-reads and
	// decides; it is not a policy refusal.
	Moved bool
	// Legal carries the table's targets from the status we tried to move
	// FROM, so a CLI can print "idea may go to cancelled, ideating".
	Legal []Status
}

// Error lets a Refusal satisfy error for callers that must thread one
// (the socket layer); it is not how callers are meant to branch.
func (r *Refusal) Error() string { return r.Reason }

// NewItem is what a person (CLI add, the socket verb, a trigger's
// produced idea, S29's hand-off) offers when creating. Provenance is
// mandatory and load-bearing: D5's honesty starts at whoever writes the
// row answering WHERE the item came from — the column has no default at
// the database precisely so this argument can never be skipped.
type NewItem struct {
	Title         string
	Epic          string
	Priority      PriorityState // empty means unknown — D2's honest absence
	PriorityLabel string        // must be empty when Priority is unknown (D2's contradiction)
	Estimate      *Estimate     // all three fields or none (D3)
	Provenance    Provenance
}

// Create adds one item, entering at `idea` (R1's start: created, ideation
// not started). The entry status is not an argument on purpose — letting
// a caller pick it would smuggle a half-run state (already-ideating with
// no loop, already-planned with no plan) in through the front door.
func (r *Roadmap) Create(componentRoot string, in NewItem) (Item, *Refusal) {
	row, ref := r.createRow(componentRoot, in)
	if ref != nil {
		return Item{}, ref
	}
	created, err := r.s.CreateRoadmapItem(row)
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the create: %v", err)}
	}
	it, err := ItemFromRow(created)
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the row that came back is not a legal item: %v", err)}
	}
	return it, nil
}

// createRow is Create's shape law and row build, extracted so the audited
// twin (S50 T1) validates through the SAME machine, not a copy: the
// priority/estimate/provenance refusals are one set of sentences no matter
// which door a create comes through.
func (r *Roadmap) createRow(componentRoot string, in NewItem) (store.NewRoadmapItemRow, *Refusal) {
	if componentRoot == "" {
		return store.NewRoadmapItemRow{}, &Refusal{Reason: "a roadmap item names the project root it plans"}
	}
	if in.Title == "" {
		return store.NewRoadmapItemRow{}, &Refusal{Reason: "a roadmap item has a title — the forward Gantt renders rows, and a row without a name is not a plan of intent"}
	}
	priority := in.Priority
	if priority == "" {
		priority = PriorityUnknown
	}
	if !ValidPriorityState(priority) {
		return store.NewRoadmapItemRow{}, &Refusal{Reason: fmt.Sprintf("priority %q is not one of the three — known, assumed, unknown (D2; a number is never one)", in.Priority)}
	}
	if priority == PriorityUnknown && in.PriorityLabel != "" {
		return store.NewRoadmapItemRow{}, &Refusal{Reason: fmt.Sprintf("a priority label (%q) on an unknown priority contradicts itself (D2) — set the state or drop the label", in.PriorityLabel)}
	}
	if !ValidProvenance(in.Provenance) {
		return store.NewRoadmapItemRow{}, &Refusal{Reason: fmt.Sprintf("provenance %q is not one of the three — authored, triggered, triaged (D5); the column has no default on purpose, so whoever writes the row answers where the item came from", in.Provenance)}
	}
	if in.Estimate != nil {
		if in.Estimate.Basis == "" {
			return store.NewRoadmapItemRow{}, &Refusal{Reason: "an estimate without its basis is not an estimate (D3) — a range with no reason is the bare figure D3 refuses"}
		}
		if in.Estimate.Min > in.Estimate.Max {
			return store.NewRoadmapItemRow{}, &Refusal{Reason: fmt.Sprintf("estimate min %d is above max %d — a range that contains nothing estimates nothing", in.Estimate.Min, in.Estimate.Max)}
		}
	}
	row := store.NewRoadmapItemRow{
		ComponentRoot: componentRoot,
		Title:         in.Title,
		PriorityState: string(priority),
		Provenance:    string(in.Provenance),
	}
	if in.Epic != "" {
		row.Epic = &in.Epic
	}
	if in.PriorityLabel != "" {
		row.PriorityLabel = &in.PriorityLabel
	}
	if in.Estimate != nil {
		row.EstimateMin = &in.Estimate.Min
		row.EstimateMax = &in.Estimate.Max
		row.EstimateBasis = &in.Estimate.Basis
	}
	return row, nil
}

// ListFilter narrows a read — T2's door is filterable by epic, priority
// and status; the empty string means no filter on that column.
type ListFilter struct {
	Epic     string
	Priority PriorityState
	Status   Status
}

// List is the roadmap's read in the forward Gantt's order (the store's
// CASE walk: the machine's rungs first, oldest within a rung). Terminal
// rows are INCLUDED — cancelled and done are the history D7 refuses to
// delete, and hiding them here would put the deletion back through the
// side door (queue's List carries the same rule).
func (r *Roadmap) List(componentRoot string, f ListFilter) ([]Item, error) {
	rows, err := r.s.ListRoadmapItems(componentRoot, store.RoadmapItemFilters{
		Epic:          f.Epic,
		PriorityState: string(f.Priority),
		Status:        string(f.Status),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, rw := range rows {
		it, err := ItemFromRow(rw)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

// Show reads one item by id; false is honest absence, not a refusal.
func (r *Roadmap) Show(id int64) (Item, bool, error) {
	row, ok, err := r.s.RoadmapItemByID(id)
	if err != nil || !ok {
		return Item{}, false, err
	}
	it, err := ItemFromRow(row)
	return it, err == nil, err
}

// Count is D8's evidence door: loop start must work identically with
// zero roadmap items, and the honest way to show that is to count.
func (r *Roadmap) Count(componentRoot string) (int, error) {
	return r.s.CountRoadmapItems(componentRoot)
}

// Transition is THE status mover (T2.md's demand: one function every
// other task calls). It takes an ID and the target — and re-reads the
// row FIRST, before consulting the table, because design R1 demands D4's
// readiness be "re-checked at the moment of transition, not cached from
// whenever each became individually true": the fresh row is the moment.
// Then the table refuses, then (for ready) IsReady refuses naming WHICH
// of the three failed, then the row moves claimed-FROM so racing
// dispatchers lose honestly rather than corrupting.
func (r *Roadmap) Transition(id int64, to Status) (Item, *Refusal) {
	return r.transitionCore(id, to, func(from string) (bool, error) {
		return r.s.TransitionRoadmapItem(id, from, string(to))
	})
}

// transitionCore is the WHOLE transition machine — fresh read, table,
// IsReady-at-the-attempt, claimed-FROM write, re-read — with the WRITE
// itself injected as `claim`. The plain door (Transition) and the audited
// door (S50 T1, audit.go) execute through this one function: there is no
// second machine to drift, which is what "one machine" promised.
func (r *Roadmap) transitionCore(id int64, to Status, claim func(from string) (bool, error)) (Item, *Refusal) {
	fresh, ok, err := r.Show(id)
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: read item %d: %v", id, err)}
	}
	if !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap item %d does not exist", id)}
	}
	res := ValidateTransition(fresh.Status, to)
	if !res.OK {
		return Item{}, &Refusal{Reason: res.Reason, Legal: res.Legal}
	}
	if to == StatusReady {
		// S27 T3: the attempt decides over the graph as it is NOW —
		// "dependencies are complete" is D4's third clause finally made
		// true, and it is read, never remembered.
		deps, err := r.Dependencies(id)
		if err != nil {
			return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: read item %d's dependencies: %v", id, err)}
		}
		if ready, why := IsReadyWithDeps(fresh, deps); !ready {
			return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap item %d: %s (D4 is decided at the attempt)", id, why), Legal: res.Legal}
		}
	}
	claimed, err := claim(string(fresh.Status))
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the move: %v", err)}
	}
	if !claimed {
		return Item{}, &Refusal{
			Reason: fmt.Sprintf("roadmap item %d moved under the %q → %q transition — re-read it and decide again",
				id, fresh.Status, to),
			Moved: true,
		}
	}
	moved, ok, err := r.Show(id)
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the move landed but the row could not be re-read: %v", err)}
	}
	if !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap item %d vanished after the move — the row is gone, say so loudly", id), Moved: true}
	}
	return moved, nil
}

// Cancel is D7 made concrete: cancelled, never delete, never failed. The
// row count does not change; the status does. The table decides who may
// (every non-terminal status; `running` refuses with the D11 sentence it
// carries, and `done` and `cancelled` are terminal) — Cancel adds no
// policy of its own, which is what "one machine" means.
func (r *Roadmap) Cancel(id int64) (Item, *Refusal) {
	return r.Transition(id, StatusCancelled)
}

// SetEstimate writes the whole range with its basis. The pair travels
// together or not at all (D3) — the door refuses the partial write so the
// database CHECK is the backstop and not the plan. An item at ANY status
// may receive an estimate: sizing an idea is planning; re-sizing a done
// item is record-keeping, and neither pretends a status changed.
func (r *Roadmap) SetEstimate(id int64, min, max int, basis string) (Item, *Refusal) {
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
	if _, err := r.s.SetRoadmapItemEstimate(id, min, max, basis); err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the estimate: %v", err)}
	}
	fresh, ok, err := r.Show(id)
	if err != nil || !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the estimate landed but the row could not be re-read: %v", err)}
	}
	return fresh, nil
}

// SetPriority is one decision, not two writes: state and label land
// together because D2's contradiction (a label on an unknown priority)
// must not exist even briefly between two statements. Downgrading to
// unknown carries no label — the store writes the NULL, the CHECK has
// the backstop, and IsReady re-decides at the next attempt.
func (r *Roadmap) SetPriority(id int64, state PriorityState, label string) (Item, *Refusal) {
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
	if _, err := r.s.SetRoadmapItemPriority(id, string(state), lp); err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the priority: %v", err)}
	}
	fresh, ok, err := r.Show(id)
	if err != nil || !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the priority landed but the row could not be re-read: %v", err)}
	}
	return fresh, nil
}

// SetArtifacts records the POINTERS D9 keeps — brief_path and plan_path,
// each either newly set (non-empty) or left alone (empty means "do not
// touch", never "unset to NULL": the unset-to-NULL would be a sentinel
// round-trip and the artifacts only ever arrive forward). This is where
// T3's dispatch files land what the loops produced.
func (r *Roadmap) SetArtifacts(id int64, briefPath, planPath string) (Item, *Refusal) {
	if briefPath == "" && planPath == "" {
		return Item{}, &Refusal{Reason: "an artifacts write names at least one pointer — naming nothing is a no-op wearing a write's clothes"}
	}
	var bp, pp *string
	if briefPath != "" {
		bp = &briefPath
	}
	if planPath != "" {
		pp = &planPath
	}
	claimed, err := r.s.SetRoadmapItemArtifacts(id, bp, pp)
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the artifacts write: %v", err)}
	}
	if !claimed {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap item %d does not exist — an artifacts write names a row that is not there", id)}
	}
	fresh, ok, err := r.Show(id)
	if err != nil || !ok {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the artifacts landed but the row could not be re-read: %v", err)}
	}
	return fresh, nil
}

// Dependencies lists the item's DIRECT upstream items with their current
// status and title — the graph readiness reads and the socket view shows
// (S27 T3). Status comes from the row at read time: an edge to an item
// that has since changed state reads the NEW state, which is the whole
// point of deriving readiness instead of caching it.
func (r *Roadmap) Dependencies(itemID int64) ([]Dependency, error) {
	rows, err := r.s.RoadmapDependencies(itemID)
	if err != nil {
		return nil, err
	}
	out := make([]Dependency, 0, len(rows))
	for _, d := range rows {
		out = append(out, Dependency{ID: d.ID, Status: Status(d.Status), Title: d.Title})
	}
	return out, nil
}

// DependentsCount is how many live items name this item as upstream —
// the number a cancel-refusal and the item view both show (S27 T3: the
// board's quiet warning before a cancel strands someone).
func (r *Roadmap) DependentsCount(itemID int64) (int, error) {
	rows, err := r.s.Dependents(itemID)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// Depend adds one edge, item waits-for dependsOn, through the store's
// one write path (the same-root law, the self/duplicate refusals and the
// cycle walk with its named closure all live there, T2). The roadmap door
// adds the two things the store cannot see from the edge table alone:
// BOTH ends exist as items with their current status, and no edge is
// written onto history — a `done` or `cancelled` end refuses, because
// R1's terminal states are the record of a decision already made, and an
// edge added there rewrites the story rather than recording intent.
func (r *Roadmap) Depend(actor Actor, itemID, dependsOnID int64) *Refusal {
	for _, id := range []int64{itemID, dependsOnID} {
		it, ok, err := r.Show(id)
		if err != nil {
			return &Refusal{Reason: fmt.Sprintf("roadmap: read item %d: %v", id, err)}
		}
		if !ok {
			return &Refusal{Reason: fmt.Sprintf("roadmap item %d does not exist — an edge names two real items or it names nothing", id)}
		}
		if it.Status == StatusDone || it.Status == StatusCancelled {
			return &Refusal{Reason: fmt.Sprintf(
				"roadmap item %d is %q — history is not rewritten; edges are authored on live items", id, it.Status)}
		}
	}
	// The duplicate pre-check is a sentence, not a constraint error: the
	// store's UNIQUE index remains the truth, this just says it human.
	deps, err := r.Dependencies(itemID)
	if err != nil {
		return &Refusal{Reason: fmt.Sprintf("roadmap: read item %d's dependencies: %v", itemID, err)}
	}
	for _, d := range deps {
		if d.ID == dependsOnID {
			return &Refusal{Reason: fmt.Sprintf("roadmap item %d already depends on item %d — the edge is already in the graph", itemID, dependsOnID)}
		}
	}
	if err := r.s.AddDependency(store.SchedulerActor{Kind: actor.Kind, ID: actor.ID}, itemID, dependsOnID); err != nil {
		return &Refusal{Reason: strings.TrimPrefix(err.Error(), "store: ")}
	}
	return nil
}

// Undepend removes one edge. A `running` item may UNWISH (S27 T3 rule 7:
// the edge records intent only — no loop is touched, no status moves);
// the end being edited is the SOURCE, so an item still short of its
// cancelled dependency is exactly whom this door exists for. The source
// must be live — a done or cancelled item's past is not edited either
// way; an edge TO a terminal item is removable precisely because the
// waiting item is the one being freed.
func (r *Roadmap) Undepend(actor Actor, itemID, dependsOnID int64) *Refusal {
	it, ok, err := r.Show(itemID)
	if err != nil {
		return &Refusal{Reason: fmt.Sprintf("roadmap: read item %d: %v", itemID, err)}
	}
	if !ok {
		return &Refusal{Reason: fmt.Sprintf("roadmap item %d does not exist", itemID)}
	}
	if it.Status == StatusDone || it.Status == StatusCancelled {
		return &Refusal{Reason: fmt.Sprintf(
			"roadmap item %d is %q — history is not rewritten; a live item un-wishes, a recorded one does not", itemID, it.Status)}
	}
	removed, err := r.s.RemoveDependency(itemID, dependsOnID)
	if err != nil {
		return &Refusal{Reason: strings.TrimPrefix(err.Error(), "store: ")}
	}
	if !removed {
		return &Refusal{Reason: fmt.Sprintf("roadmap item %d does not depend on item %d — there is no such edge to remove", itemID, dependsOnID)}
	}
	return nil
}

// RecordLoop records the loop an item materialized into (T4 calls it on
// the direct-start branch once loop.start returns; the queue branch calls
// it when S24 actually starts the item's loop). The store's guard is the
// status: the record lands only while the item still wears `running`, so
// a crash-retry cannot re-point a finished item, and the composite FK
// refuses a slug naming no loop. The stamp is SCOPED by the caller's root
// (the queue door's SetQueueItemLoop is the precedent): an id from another
// project is refused by the WHERE, not a hope.
func (r *Roadmap) RecordLoop(id int64, componentRoot, loopSlug string) (Item, *Refusal) {
	if loopSlug == "" {
		return Item{}, &Refusal{Reason: "a loop record names a loop — an empty slug is the sentinel the column forbids"}
	}
	if componentRoot == "" {
		return Item{}, &Refusal{Reason: "a loop record is scoped — naming a loop without the caller's root is the unscoped write the guard forbids"}
	}
	claimed, err := r.s.SetRoadmapItemLoop(id, componentRoot, loopSlug, string(StatusRunning))
	if err != nil {
		return Item{}, &Refusal{Reason: fmt.Sprintf("roadmap: the store refused the loop record: %v", err)}
	}
	if !claimed {
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
