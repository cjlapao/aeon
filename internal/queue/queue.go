package queue

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cjlapao/aeon/internal/store"
	"strings"
)

// Queue is the one door every later task calls. It wraps the store's
// queue methods the way internal/retrieval's Index wraps its store pair:
// the handle stays in internal/store, the decisions live here.
type Queue struct {
	s *store.Store
}

// New wires the queue to the store. One instance per daemon is the intent
// (the reactor and the socket doors share it), though the type holds no
// state beyond the handle, so tests may open as many as they like.
func New(s *store.Store) *Queue { return &Queue{s: s} }

// Refusal is DB19 again: the discriminated answer to "may this move". OK
// with a refreshed item, or a Reason that names the thing refused and the
// alternative (house refusal rule), never a bare error for a refusal.
type Refusal struct {
	Reason string
	// Moved is true when the refusal is a RACE — the claimed-FROM update
	// found the row had changed status under us. The caller re-reads and
	// decides; it is not a policy refusal.
	Moved bool
	// Legal carries the table's targets from the status we tried to move
	// FROM, so a CLI can print "ready may go to running, cancelled".
	Legal []Status
}

// Error lets a Refusal satisfy error for callers that must thread one
// (the socket layer); it is not how callers are meant to branch.
func (r *Refusal) Error() string { return r.Reason }

// NewItem is what a person (or T9's roadmap renderer) offers when
// appending. CG3's payload, minus identity: the root says which queue, the
// queue says the position.
type NewItem struct {
	Blueprint     string
	Goal          string
	WorkspaceRoot string
	Tech          string
	With          map[string]string
	// Attended is DECLARED at append time or nil for never-declared
	// (D5/D6: the default a project sets, never a consequence of who is
	// watching when the daemon finally starts the loop).
	Attended *bool
}

// Append adds the item at the end. Status is `pending` in BOTH sizing
// modes — CG1's diagram starts there regardless of mode; what sizing mode
// changes is what happens NEXT (T5's reactor moves pending → sizing), not
// where the item lands.
func (q *Queue) Append(componentRoot, blueprint, goal, workspaceRoot string, in NewItem) (QueueItem, error) {
	if blueprint == "" || goal == "" {
		return QueueItem{}, fmt.Errorf("queue: an item needs a blueprint and a goal — it materializes as loop.start with exactly these (CG3)")
	}
	if workspaceRoot == "" {
		// CG3: workspace defaults to the project's root — a queue is
		// per-project, so this is almost always implicit.
		workspaceRoot = componentRoot
	}
	var withJSON *string
	if len(in.With) > 0 {
		b, err := json.Marshal(in.With)
		if err != nil {
			return QueueItem{}, fmt.Errorf("queue: encode `with` for %q: %w", blueprint, err)
		}
		str := string(b)
		withJSON = &str
	}
	var tech *string
	if in.Tech != "" {
		tech = &in.Tech
	}
	var attended *int
	if in.Attended != nil {
		v := 0
		if *in.Attended {
			v = 1
		}
		attended = &v
	}
	row, err := q.s.AppendQueueItem(store.NewQueueItemRow{
		ComponentRoot: componentRoot,
		Status:        string(StatusPending),
		Blueprint:     blueprint,
		Goal:          goal,
		WorkspaceRoot: workspaceRoot,
		Tech:          tech,
		WithJSON:      withJSON,
		Attended:      attended,
	})
	if err != nil {
		return QueueItem{}, err
	}
	return ItemFromRow(row)
}

// List is the ordered backlog, cancelled and done rows included — they
// are the history D12 refuses to delete, and hiding them here would put
// the deletion back through the side door.
func (q *Queue) List(componentRoot string) ([]QueueItem, error) {
	rows, err := q.s.ListQueueItems(componentRoot)
	if err != nil {
		return nil, err
	}
	out := make([]QueueItem, 0, len(rows))
	for _, r := range rows {
		it, err := ItemFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

// Show reads one item by id; false is honest absence, not a refusal.
func (q *Queue) Show(id int64) (QueueItem, bool, error) {
	row, ok, err := q.s.QueueItemByID(id)
	if err != nil || !ok {
		return QueueItem{}, false, err
	}
	it, err := ItemFromRow(row)
	return it, err == nil, err
}

// Transition is THE status mover (T2.md's demand: one function every
// other task calls). It consults AllowedTransitions, refuses through the
// table — then claims the row claimed-FROM so a racing reactor loses
// honestly rather than corrupting.
func (q *Queue) Transition(item QueueItem, to Status) (QueueItem, *Refusal) {
	res := ValidateTransition(item.Status, to)
	if !res.OK {
		return QueueItem{}, &Refusal{Reason: res.Reason, Legal: res.Legal}
	}
	claimed, err := q.s.TransitionQueueItem(item.ID, string(item.Status), string(to))
	if err != nil {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue: the store refused the move: %v", err)}
	}
	if !claimed {
		return QueueItem{}, &Refusal{
			Reason: fmt.Sprintf("queue item %d moved under the %q → %q transition — re-read it and decide again",
				item.ID, item.Status, to),
			Moved: true,
		}
	}
	fresh, ok, err := q.Show(item.ID)
	if err != nil {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue: the move landed but the row could not be re-read: %v", err)}
	}
	if !ok {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue item %d vanished after the move — the row is gone, say so loudly", item.ID), Moved: true}
	}
	return fresh, nil
}

// Remove is D12 made concrete: cancel, never delete. The row count does
// not change; the status does; a running item REFUSES (D11 — cancelling
// the queue item while its loop runs has no defined meaning, and the
// refusal is an error the caller must see, not a silent no-op).
func (q *Queue) Remove(id int64) (QueueItem, *Refusal) {
	item, ok, err := q.Show(id)
	if err != nil {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue: read item %d: %v", id, err)}
	}
	if !ok {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue item %d does not exist", id)}
	}
	return q.Transition(item, StatusCancelled)
}

// reorderable are the statuses whose POSITION is still the future's to
// decide. T2.md handed this call down explicitly and demanded it be
// recorded rather than assumed. Two calls, both recorded here:
//
//   - mid-`sizing` counts as pending's cousin — its position in the
//     backlog is independent of its sizing run, and a sizing run in
//     flight does not make the backlog's order someone else's business.
//   - `ready` counts too, which T2.md's shorthand "pending items only"
//     would have excluded: in `manual` sizing mode EVERY backlog item is
//     ready the moment it lands (CG1), so a re-order that stopped at
//     pending could never order the queue at all — the feature would be
//     a no-op in the mode the default lives in. The rule D12 actually
//     states is "a RUNNING or DONE item's position is history"; ready
//     items are neither.
//
// `running`, `done`, `blocked` and `cancelled` are in flight or history:
// their positions are fixed (D12).
var reorderable = []string{string(StatusPending), string(StatusSizing), string(StatusReady)}

// Reorder moves one movable item to relative position `toPosition`
// (1-based among the movable items) and shifts the rest to make room.
// The batch lands in one store transaction behind claimed-status guards,
// so a queue that changes under the move fails whole rather than
// half-landing two items on one position.
func (q *Queue) Reorder(id int64, toPosition int) (QueueItem, *Refusal) {
	item, ok, err := q.Show(id)
	if err != nil {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue: read item %d: %v", id, err)}
	}
	if !ok {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue item %d does not exist", id)}
	}
	movable := false
	for _, st := range reorderable {
		if string(item.Status) == st {
			movable = true
		}
	}
	if !movable {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf(
			"queue item %d is %q — a running, done, blocked or cancelled item's position is history and is fixed (D12)",
			id, item.Status)}
	}
	items, err := q.List(item.ComponentRoot)
	if err != nil {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue: list %s: %v", item.ComponentRoot, err)}
	}
	var movableItems []QueueItem
	for _, it := range items {
		for _, st := range reorderable {
			if string(it.Status) == st {
				movableItems = append(movableItems, it)
			}
		}
	}
	if toPosition < 1 || toPosition > len(movableItems) {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf(
			"position %d is outside the movable queue — %d item(s) can move", toPosition, len(movableItems))}
	}
	// The movable items keep exactly the position VALUES they already
	// occupy (running/done slots stay theirs); only who stands in which
	// slot changes among the movable ones.
	positions := make([]int, 0, len(movableItems))
	for _, it := range movableItems {
		positions = append(positions, it.Position)
	}
	sort.Ints(positions)
	ordered := make([]QueueItem, 0, len(movableItems))
	for _, it := range movableItems {
		if it.ID != id {
			ordered = append(ordered, it)
		}
	}
	moved := item
	ordered = append(ordered[:toPosition-1], append([]QueueItem{moved}, ordered[toPosition-1:]...)...)
	edits := make([]store.QueuePosEdit, 0, len(ordered))
	for i, it := range ordered {
		if it.Position == positions[i] {
			continue // no move for a row already standing where it stands
		}
		edits = append(edits, store.QueuePosEdit{ID: it.ID, Position: positions[i], RequireStatus: reorderable})
	}
	claimed, err := q.s.ReorderQueueItems(item.ComponentRoot, edits)
	if err != nil {
		return QueueItem{}, &Refusal{Reason: err.Error(), Moved: !claimed}
	}
	fresh, ok, err := q.Show(id)
	if err != nil {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue: the move landed but the row could not be re-read: %v", err)}
	}
	if !ok {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("queue item %d vanished after the move", id), Moved: true}
	}
	return fresh, nil
}

// NextReady answers D2: what may start next in this project, or nothing
// while anything is running. It does NOT claim — the advancer follows
// with Transition(item, StatusRunning) and the claimed-FROM update
// decides the race.
func (q *Queue) NextReady(componentRoot string) (QueueItem, bool, *Refusal) {
	row, ok, err := q.s.NextReadyQueueItem(componentRoot)
	if err != nil {
		return QueueItem{}, false, &Refusal{Reason: fmt.Sprintf("queue: next ready in %s: %v", componentRoot, err)}
	}
	if !ok {
		return QueueItem{}, false, nil
	}
	it, err := ItemFromRow(row)
	if err != nil {
		return QueueItem{}, false, &Refusal{Reason: err.Error()}
	}
	return it, true, nil
}

// ItemFromRow converts a store row once, refusing loudly at the door
// rather than dropping a corrupt `with` map on the floor. Exported
// because the reactor reads rows directly for its lookup fast path —
// conversion still has exactly ONE definition (a second converter in the
// daemon would be the second truth this slice forbids).
func ItemFromRow(r store.QueueItemRow) (QueueItem, error) {
	it := QueueItem{
		ID: r.ID, ComponentRoot: r.ComponentRoot, Position: r.Position,
		Status: Status(r.Status), Blueprint: r.Blueprint, Goal: r.Goal,
		WorkspaceRoot: r.WorkspaceRoot,
		LoopSlug:      derefOr(r.LoopSlug, ""),
		CreatedAt:     r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.Tech != nil {
		it.Tech = *r.Tech
	}
	if r.Attended != nil {
		v := *r.Attended != 0
		it.Attended = &v
	}
	if r.SizeMin != nil && r.SizeMax != nil && r.SizeBasis != nil {
		it.Size = &Size{Min: *r.SizeMin, Max: *r.SizeMax, Basis: *r.SizeBasis}
	}
	if r.WithJSON != nil {
		var m map[string]string
		if err := json.Unmarshal([]byte(*r.WithJSON), &m); err != nil {
			return QueueItem{}, fmt.Errorf("queue item %d carries a `with` map that is not JSON (%v) — the row is corrupt, refusing to read it as nothing", r.ID, err)
		}
		it.With = m
	}
	return it, nil
}

func derefOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// ReportSizing lands a sizing run's report: the three facts (range +
// basis, D8) and the sizing → ready edge in the store's single claimed
// statement. A nil/invalid report is refused BEFORE the door is reached
// — the tool refuses at the call and the schema refuses at the row; this
// middle refusal is for the handoff path, where a model-shaped struct
// arrives without ever having passed the tool.
func (q *Queue) ReportSizing(item QueueItem, size Size) (QueueItem, *Refusal) {
	if item.Status != StatusSizing {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf(
			"only a %q item can receive a size report — item %d reads %q", StatusSizing, item.ID, item.Status)}
	}
	if r := validateSize(size); r != nil {
		return QueueItem{}, r
	}
	return q.landSize(item, size, StatusSizing)
}

// validateSize is D8's law in one place: a range, ordered, with a basis.
// Both writers of size (the skill report and the person's hand-written
// set-size) pass through it before any door is reached — a second copy
// of the law would be a second truth.
func validateSize(size Size) *Refusal {
	if size.Min < 1 {
		return &Refusal{Reason: "sizeMin must be at least 1 — a range that includes nothing sizes nothing (D8)"}
	}
	if size.Max < size.Min {
		return &Refusal{Reason: fmt.Sprintf("sizeMax (%d) is below sizeMin (%d) — the range is inverted", size.Max, size.Min)}
	}
	if strings.TrimSpace(size.Basis) == "" {
		return &Refusal{Reason: "a size report without its basis is D8's bare number — refused"}
	}
	return nil
}

// landSize is the shared tail: one claimed statement from the named
// status to `ready`, then the fresh read.
func (q *Queue) landSize(item QueueItem, size Size, from Status) (QueueItem, *Refusal) {
	ok, err := q.s.SizeAndReadyQueueItem(item.ID, item.ComponentRoot, size.Min, size.Max, size.Basis, string(from))
	if err != nil {
		return QueueItem{}, &Refusal{Reason: err.Error()}
	}
	if !ok {
		return QueueItem{}, &Refusal{Moved: true, Reason: fmt.Sprintf(
			"item %d moved out of %q under the size landing", item.ID, from)}
	}
	row, found, err := q.s.QueueItemByID(item.ID)
	if err != nil || !found {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("item %d vanished between the landing and the read", item.ID)}
	}
	it, err := ItemFromRow(row)
	if err != nil {
		return QueueItem{}, &Refusal{Reason: err.Error()}
	}
	return it, nil
}

// RevertSizing is CG5's documented edge: a failed or abandoned sizing run
// leaves the item pending — eligible to be sized again — rather than
// stuck in `sizing` forever. It is just the legal sizing → pending edge
// with a name that says why it exists.
func (q *Queue) RevertSizing(item QueueItem) (QueueItem, *Refusal) {
	return q.Transition(item, StatusPending)
}

// SetSize is the `manual` half of D7: the person writes the size by hand
// and the item becomes eligible — one claimed statement from `pending`
// to `ready`, exactly like the skill report's landing, with the same law
// applied first. In `skill` mode the act is REFUSED BY NAME: there the
// sizing run's `report_size` is the only writer of size (D9), and a
// silent no-op would leave the person believing their number landed.
// A missing item is a refusal naming the id — the socket speaks in
// refusals, not silences.
func (q *Queue) SetSize(id int64, size Size) (QueueItem, *Refusal) {
	item, found, err := q.Show(id)
	if err != nil {
		return QueueItem{}, &Refusal{Reason: err.Error()}
	}
	if !found {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf("there is no queue item %d — nothing was sized", id)}
	}
	_, sizingMode, known, serr := q.s.ComponentQueueSettings(item.ComponentRoot)
	if serr != nil {
		return QueueItem{}, &Refusal{Reason: serr.Error()}
	}
	if !known {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf(
			"%s has no queue settings — its items still run by the slice's defaults, but a hand-written size needs the project named", item.ComponentRoot)}
	}
	if sizingMode == "skill" {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf(
			"%s sizes by skill: report_size is the only writer of size (D9) — a hand-written size for item %d is refused by name", item.ComponentRoot, id)}
	}
	if item.Status != StatusPending {
		return QueueItem{}, &Refusal{Reason: fmt.Sprintf(
			"only a %q item takes a hand-written size — item %d reads %q (D7)", StatusPending, id, item.Status)}
	}
	if r := validateSize(size); r != nil {
		return QueueItem{}, r
	}
	return q.landSize(item, size, StatusPending)
}

// Settings is the read half of the component's queue settings (D3/D4/
// D7's knobs): `known=false` means the component row was not found — the
// three-state answer, never a silent default.
func (q *Queue) Settings(componentRoot string) (advance, sizingMode string, known bool, err error) {
	return q.s.ComponentQueueSettings(componentRoot)
}

// SetSettings is the write half — it rides the store's door untouched
// (the CHECKs there are the vocabulary; this file does not re-run them).
// Returns false when the component is not found.
func (q *Queue) SetSettings(componentRoot, advance, sizingMode string) (bool, error) {
	return q.s.SetComponentQueueSettings(componentRoot, advance, sizingMode)
}
