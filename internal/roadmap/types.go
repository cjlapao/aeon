// Package roadmap owns the roadmap item's vocabulary and its one machine:
// the eight statuses (design R1), the closed transition table over them,
// D4's readiness decided at the moment of the attempt, and the door every
// other task calls instead of touching roadmap_item rows directly.
//
// It follows internal/queue's precedent deliberately — S26's design
// inherits S24's disciplines on purpose (D6 is S24's D10 applied here;
// cancelled-is-not-failed is D7 answering the same worry S24's D12 did) —
// down to the shape of this package: a pure table plus functions over it
// (machine.go's discipline, enumerated never hand-listed in tests),
// because a second place that knows whether `planned → ready` is legal is
// a second truth the day somebody edits one and not the other.
//
// The SQL lives in internal/store (house rule: the store owns the handle);
// this package owns the MEANING. Rows cross the boundary as
// store.RoadmapItemRow and are converted once, in ItemFromRow.
package roadmap

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Status is a roadmap item's rung. Eight values, design R1's closed set:
// idea (created, ideation not started) · ideating (the ordinary loop from
// brief §2 is in flight) · brief-agreed (that loop's produces: [brief.md]
// step satisfied the engine's existing missingArtifact check — R3, no new
// check anywhere) · planned (project-planner produced plan.md — S13's
// handler, dispatched like any other, D9) · ready (D4's definition,
// re-decided at the attempt by IsReady, never cached) · running
// (materialized — R4) · done · cancelled (from any non-terminal state,
// D7 — an honest outcome, never a failure).
type Status string

const (
	StatusIdea        Status = "idea"
	StatusIdeating    Status = "ideating"
	StatusBriefAgreed Status = "brief-agreed"
	StatusPlanned     Status = "planned"
	StatusReady       Status = "ready"
	StatusRunning     Status = "running"
	StatusDone        Status = "done"
	StatusCancelled   Status = "cancelled"
)

// Statuses is every status, in the machine's order (R1's diagram, read
// left to right; cancelled last as D7's off-ramp). Order matters for the
// cartesian-product test and for the list's Gantt-shaped ordering.
func Statuses() []Status {
	return []Status{
		StatusIdea, StatusIdeating, StatusBriefAgreed, StatusPlanned,
		StatusReady, StatusRunning, StatusDone, StatusCancelled,
	}
}

// ValidStatus reports whether s is one of the eight. The DATABASE also
// refuses the ninth (T1's CHECK); this is the door's vocabulary so a
// caller gets a sentence instead of a driver error.
func ValidStatus(s Status) bool {
	for _, v := range Statuses() {
		if v == s {
			return true
		}
	}
	return false
}

// PriorityState is D2's three states — and D2 is a promise inherited from
// S29's own locked words: "An unreproduced or unvalidated bug is unknown
// priority, never low." known carries a human's word; assumed carries an
// unverified one; unknown is the honest absence, never collapsed into a
// number, a blank, or a sort-last that reads as low (brief R2's risk).
type PriorityState string

const (
	PriorityKnown   PriorityState = "known"
	PriorityAssumed PriorityState = "assumed"
	PriorityUnknown PriorityState = "unknown"
)

// PriorityStates is the closed three, in honesty order (a person's word,
// an unverified word, none).
func PriorityStates() []PriorityState {
	return []PriorityState{PriorityKnown, PriorityAssumed, PriorityUnknown}
}

// ValidPriorityState reports whether p is one of the three.
func ValidPriorityState(p PriorityState) bool {
	for _, v := range PriorityStates() {
		if v == p {
			return true
		}
	}
	return false
}

// Provenance is D5's closed vocabulary — and every word is borrowed from
// machinery that already exists, never invented: `authored` is a person
// writing it, `triggered` is S19/S20's existing trigger action starting
// the loop that produced it, `triaged` is S29's hand-off (D8 there). A
// value nobody compiled is a provenance nobody claimed.
type Provenance string

const (
	ProvenanceAuthored  Provenance = "authored"
	ProvenanceTriggered Provenance = "triggered"
	ProvenanceTriaged   Provenance = "triaged"
)

// Provenances is the closed three.
func Provenances() []Provenance {
	return []Provenance{ProvenanceAuthored, ProvenanceTriggered, ProvenanceTriaged}
}

// ValidStoredProvenance reports whether a row MAY CARRY p at rest. The
// person's door keeps the closed three (D5); the store learned a fourth
// word when S61's intake records step began filing the first roadmap of a
// project — `intake` (the T1 migration widened the CHECK on purpose, and a
// plane that cannot read the records it files is not a plane).
func ValidStoredProvenance(p Provenance) bool {
	return ValidProvenance(p) || p == Provenance("intake")
}

// ValidProvenance reports whether p is one of the three.
func ValidProvenance(p Provenance) bool {
	for _, v := range Provenances() {
		if v == p {
			return true
		}
	}
	return false
}

// Estimate is a range with its basis, never a bare figure (D3, P32 — the
// same shape S24's D8 sizing and S22's reindex estimate use). T1's CHECK
// makes a partial one UNWRITABLE at the database; the type keeps the three
// facts together at the door too.
type Estimate struct {
	Min   int
	Max   int
	Basis string
}

// Item is the Go mirror of T1's row. Epic is the plain tag R2 settled (a
// tag, not a row); BriefPath/PlanPath are POINTERS to artifacts, never
// content (D9 — plan.md and its owner S13 stay the plan's home); LoopSlug
// is unset until materialization — absence is the empty string here and
// NULL in the row (D6: the item's status is its own field, never read off
// whatever loop/brief/plan it points to).
type Item struct {
	ID            int64
	ComponentRoot string
	Title         string
	Epic          string
	Status        Status
	Priority      PriorityState
	PriorityLabel string
	Estimate      *Estimate
	Provenance    Provenance
	BriefPath     string
	PlanPath      string
	LoopSlug      string
	CreatedAt     string
	UpdatedAt     string
}

// Materialized reports whether this item has produced a loop.
func (i Item) Materialized() bool { return i.LoopSlug != "" }

// ItemFromRow is the ONE conversion, exported for the daemon's dispatch
// files the way queue.ItemFromRow is exported for its reactor — a second
// row-shaping site would be a second truth about what the columns mean. A
// row carrying vocabulary Go cannot name is said loudly, not guessed at.
func ItemFromRow(r store.RoadmapItemRow) (Item, error) {
	it := Item{
		ID:            r.ID,
		ComponentRoot: r.ComponentRoot,
		Title:         r.Title,
		Status:        Status(r.Status),
		Priority:      PriorityState(r.PriorityState),
		Provenance:    Provenance(r.Provenance),
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
	if r.Epic != nil {
		it.Epic = *r.Epic
	}
	if r.PriorityLabel != nil {
		it.PriorityLabel = *r.PriorityLabel
	}
	if r.BriefPath != nil {
		it.BriefPath = *r.BriefPath
	}
	if r.PlanPath != nil {
		it.PlanPath = *r.PlanPath
	}
	if r.LoopSlug != nil {
		it.LoopSlug = *r.LoopSlug
	}
	if r.EstimateMin != nil || r.EstimateMax != nil || r.EstimateBasis != nil {
		if r.EstimateMin == nil || r.EstimateMax == nil || r.EstimateBasis == nil {
			return Item{}, fmt.Errorf("roadmap: item %d carries a partial estimate — T1's CHECK says all three or none, and a row that got through means something bypassed the door", r.ID)
		}
		it.Estimate = &Estimate{Min: *r.EstimateMin, Max: *r.EstimateMax, Basis: *r.EstimateBasis}
	}
	if !ValidStatus(it.Status) {
		return Item{}, fmt.Errorf("roadmap: item %d carries status %q, which is not one of the eight", r.ID, r.Status)
	}
	if !ValidPriorityState(it.Priority) {
		return Item{}, fmt.Errorf("roadmap: item %d carries priority_state %q, which is not one of the three", r.ID, r.PriorityState)
	}
	if !ValidStoredProvenance(it.Provenance) {
		return Item{}, fmt.Errorf("roadmap: item %d carries provenance %q, which is not one of the four the store may hold — authored, triggered, triaged, or intake (S61's records step)", r.ID, r.Provenance)
	}
	if it.Priority == PriorityUnknown && it.PriorityLabel != "" {
		return Item{}, fmt.Errorf("roadmap: item %d is unknown priority AND carries label %q — D2's contradiction is unwritable at the database, so a row like this means the CHECK was bypassed", r.ID, it.PriorityLabel)
	}
	return it, nil
}
