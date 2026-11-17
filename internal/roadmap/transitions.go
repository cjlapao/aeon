package roadmap

import (
	"fmt"
	"sort"
	"strings"
)

// AllowedTransitions is design R1's machine, edge for edge:
//
//	idea     → ideating    (the ordinary loop from brief §2 is started —
//	                       T3's ideation dispatch, R3's produces: check)
//	ideating → brief-agreed (the loop's done carried brief.md past the
//	                       engine's existing missingArtifact check)
//	brief-agreed → planned (project-planner produced plan.md — S13's
//	                       handler dispatched like any other, D9)
//	planned  → ready       (D4, re-decided at the attempt — IsReady)
//	ready    → running     (materialized, R4 — queue first when the
//	                       project has one, direct loop.start otherwise)
//	running  → done
//	{idea, ideating, brief-agreed, planned, ready} → cancelled (D7: an
//	                       honest outcome, never a failure)
//
// Two edges are deliberately absent, and both are S24's rules carried on
// purpose: running → cancelled is refused (a loop in flight is the loop's
// own record, not roadmap history — the same D11 that closes the queue,
// and T2.md names it: "running has no edge to cancelled"); done and
// cancelled are terminal — the ledger keeps its completed and cancelled
// rows and never reopens either.

func AllowedTransitions() map[Status][]Status {
	return map[Status][]Status{
		StatusIdea:        {StatusIdeating, StatusCancelled},
		StatusIdeating:    {StatusBriefAgreed, StatusCancelled},
		StatusBriefAgreed: {StatusPlanned, StatusCancelled},
		StatusPlanned:     {StatusReady, StatusCancelled},
		StatusReady:       {StatusRunning, StatusCancelled},
		StatusRunning:     {StatusDone},
		StatusDone:        {},
		StatusCancelled:   {},
	}
}

// TransitionResult is the DB19 shape machine.go set (and queue's carried):
// a discriminated result, not a bare error — the caller branches on OK and
// quotes Reason.
type TransitionResult struct {
	OK     bool
	Reason string
	// Legal carries the targets the table allows from `from`, so a refusal
	// can tell the caller what CAN be done (the house refusal rule: name
	// the alternative).
	Legal []Status
}

// ValidateTransition answers "may this item move" without touching a row.
// A self-transition is idempotent, not wrong — machine.go's carried rule,
// which is what makes a retried reactor write safe. Note what is NOT
// checked here: D4's readiness is IsReady's, consulted at the moment of
// the attempt by the door (design R1: "re-checked at the moment of
// transition, not cached from whenever each became individually true"),
// so a pure table-check stays pure.
func ValidateTransition(from, to Status) TransitionResult {
	if !ValidStatus(from) {
		return TransitionResult{Reason: fmt.Sprintf("roadmap status %q is not one of the eight", from)}
	}
	if !ValidStatus(to) {
		return TransitionResult{Reason: fmt.Sprintf("roadmap status %q is not one of the eight", to)}
	}
	legal := AllowedTransitions()[from]
	if from == to {
		return TransitionResult{OK: true, Legal: legal}
	}
	for _, s := range legal {
		if s == to {
			return TransitionResult{OK: true, Legal: legal}
		}
	}
	reason := fmt.Sprintf("roadmap item cannot go from %q to %q", from, to)
	if from == StatusRunning && to == StatusCancelled {
		reason += " — a loop in flight is the loop's own record, not roadmap history (S24's D11 carried); the item has no meaning to cancel while its loop runs"
	}
	if from == StatusReady && to == StatusIdeating {
		reason += " — an item past planning does not re-ideate; cancel it and write the next idea (D7 keeps the record)"
	}
	if len(legal) == 0 {
		reason += fmt.Sprintf(" — %q is terminal", from)
	}
	return TransitionResult{Reason: reason, Legal: legal}
}

// IsReady is D4 at the one moment it is consulted: the planned → ready
// attempt. All three conditions, re-evaluated from the item AS IT IS NOW,
// never cached from whenever each individually became true — a priority
// can be downgraded to unknown after a brief was agreed (a re-triaged
// item, say) and a stale cached ready would miss that. D4's words are
// "a brief is agreed, dependencies are complete, and priority is not
// unknown"; this is T2.md's operationalisation of them — brief agreed is
// the brief pointer set, "dependencies complete" is the item having its
// own plan (the plan IS the item's dependency record; plan content is
// S13's and a dependency GRAPH is nobody's until S27 consumes this), and
// priority rides D2's non-unknown.
//
// It returns the three facts and a refusal sentence naming WHICH failed,
// because the house refusal rule does not stop at the door.
func IsReady(item Item) (bool, string) {
	return IsReadyWithDeps(item, nil)
}

// Dependency is one upstream edge as readiness sees it: the named item's
// id, its CURRENT status, and its title — read from the row, never typed
// (S27 T3; a title quoted here is the same user text roadmap.show already
// returns, and it never rides an audit summary, CN16).
type Dependency struct {
	ID     int64
	Status Status
	Title  string
}

// IsReadyWithDeps is D4's "dependencies are complete" made TRUE (S27 T3).
// The three original facts are decided exactly as IsReady decided them —
// the property T1's goldens pin survives unchanged for an edge-less item,
// and the edge-less path IS the nil call — and the graph adds two
// segments, only ever when they exist:
//
//   - an open dependency (any status but done or cancelled) reads
//     `waiting on item N ("<title>" — <status>)`, one segment per item,
//     id order;
//   - a CANCELLED dependency is its own sentence —
//     `item N was cancelled — remove the dependency or cancel this item` —
//     because "waiting forever" and "waiting on a dead item" are not the
//     human's same problem.
//
// Readiness is still derived at the read, never stored (D4): the walk is
// over the graph as it is when asked, and the day the last open item
// closes is already the next pick's truth.
func IsReadyWithDeps(item Item, deps []Dependency) (bool, string) {
	var missing []string
	if item.BriefPath == "" {
		missing = append(missing, "no brief agreed (brief_path unset)")
	}
	if item.PlanPath == "" {
		missing = append(missing, "no plan produced (plan_path unset — the project-planner step has not landed)")
	}
	if item.Priority == PriorityUnknown {
		missing = append(missing, "priority is unknown (D2 — an unknown priority never becomes ready by waiting)")
	}
	var waiting, dead []string
	for _, d := range deps {
		switch d.Status {
		case StatusDone:
			continue // a completed dependency is what readiness is made of
		case StatusCancelled:
			dead = append(dead, fmt.Sprintf("item %d was cancelled — remove the dependency or cancel this item", d.ID))
		default:
			waiting = append(waiting, fmt.Sprintf("waiting on item %d (%q — %s)", d.ID, d.Title, d.Status))
		}
	}
	missing = append(missing, waiting...)
	missing = append(missing, dead...)
	if len(missing) > 0 {
		return false, fmt.Sprintf("not ready: %s", strings.Join(missing, "; "))
	}
	return true, ""
}
// SortedTargets orders a refusal's legal targets for stable sentences.
func SortedTargets(from Status) []Status {
	legal := append([]Status(nil), AllowedTransitions()[from]...)
	sort.Slice(legal, func(i, j int) bool { return legal[i] < legal[j] })
	return legal
}

// LegalSentence renders the table's answer for a refusal message:
// "idea may go to cancelled, ideating" — the alternative named, not just
// the refusal.
func LegalSentence(from Status) string {
	legal := SortedTargets(from)
	if len(legal) == 0 {
		return fmt.Sprintf("%q is terminal", from)
	}
	parts := make([]string, len(legal))
	for i, s := range legal {
		parts[i] = string(s)
	}
	return fmt.Sprintf("%q may go to %s", from, strings.Join(parts, ", "))
}
