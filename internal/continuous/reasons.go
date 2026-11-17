package continuous

import (
	"fmt"
	"strings"
)

// Reason is CN15's CLOSED vocabulary: every reason a unit did not start is
// one of these ids, and the id is the whole fact — a sentence renders from
// ids and enums only (SEC-033: no user text ever rides a decision). The
// set is exactly design.md CN15's line; TestReasonVocabularyIsClosed
// compares the constants, their sentences, and the fixture cut from the
// doc, so drift between doc and code is a red test, not a rumour.
type Reason string

const (
	// Step 1 — the master switch. Nothing else is evaluated while these
	// hold, so they are the only reasons a tick may answer with.
	ReasonMasterOff Reason = "master-off"
	ReasonPaused    Reason = "paused"

	// Step 2 — the repository's own gates.
	ReasonRepoOff      Reason = "repo-off"
	ReasonRepoOccupied Reason = "repo-occupied" // CN8: names the loop and its state
	ReasonWindowClosed Reason = "window-closed" // CN10: names the next open instant

	// Step 3 — the candidate's own facts.
	ReasonQueueLive           Reason = "queue-live"           // CN6.2: roadmap waits for a drained repository
	ReasonAttendedItem        Reason = "attended-item"        // S24 D5: a declared-attended item is not the scheduler's
	ReasonPriorityUnknown     Reason = "priority-unknown"     // D2: unknown never becomes ready by waiting
	ReasonNotReady            Reason = "not-ready"            // IsReady's facts, carried as ids
	ReasonDependencyOpen      Reason = "dependency-open"      // CN7: names the waiting ids
	ReasonDependencyCancelled Reason = "dependency-cancelled" // CN7: its own sentence, never "waiting"
	ReasonAlreadyClaimed      Reason = "already-claimed"      // T2's claim: one owner, said plainly

	// Step 5 — admission, the seams and the backstops.
	ReasonCapacity         Reason = "capacity"           // CN9: named used/cap/weight
	ReasonWeightExceedsCap Reason = "weight-exceeds-cap" // CN9: refused ONCE, naming the setting
	ReasonRateLimit        Reason = "rate-limit"         // CN12: names the leave-instant
	ReasonBudgetExhausted  Reason = "budget-exhausted"   // S55's answer, carried as a snapshot fact
	ReasonGrantAbsent      Reason = "grant-absent"       // S56: absent is stated, never faked (V13)
	ReasonTemplateInvalid  Reason = "template-invalid"   // CN5 at the pick: item untouched, needs-you
)

// reasons are the constants in CN15's doc order — the closed set, once.
func reasons() []Reason {
	return []Reason{
		ReasonMasterOff, ReasonPaused, ReasonRepoOff, ReasonRepoOccupied, ReasonQueueLive,
		ReasonAttendedItem, ReasonPriorityUnknown, ReasonNotReady, ReasonDependencyOpen,
		ReasonDependencyCancelled, ReasonWindowClosed, ReasonCapacity, ReasonWeightExceedsCap,
		ReasonRateLimit, ReasonBudgetExhausted, ReasonGrantAbsent, ReasonTemplateInvalid,
		ReasonAlreadyClaimed,
	}
}

// Reasons exposes the closed set for doors that list it (scheduler.status,
// T9's CLI vocabulary verb).
func Reasons() []Reason { return reasons() }

// Detail is a bounded, ids-and-enums string the reason's SENDER controls:
// loop slugs, states, numeric settings, RFC3339 instants, item ids. Titles
// and paths are user text and never enter a Detail — the types here make
// that structural (the snapshot itself carries booleans, not paths), so a
// caller cannot smuggle text in.
//
// Sentence renders a reason with its detail. Every sentence names the
// thing a person would act on — the setting, the loop, the instant — and
// the way out, in the same words the audit line will carry.
func Sentence(r Reason, detail string) string {
	switch r {
	case ReasonMasterOff:
		return "the continuous scheduler is off (master enabled=false) — nothing is evaluated while the master switch is off"
	case ReasonPaused:
		return "the continuous scheduler is paused — resume it (scheduler.pause enabled=false) and the next tick re-decides everything"
	case ReasonRepoOff:
		return "the repository is not enabled for the continuous scheduler" + withDetail(r, detail)
	case ReasonRepoOccupied:
		return "the repository is occupied by loop " + detail + " — one loop per repository at a time (CN8); a person's start is never blocked, only noticed next tick"
	case ReasonWindowClosed:
		return "the repository is outside its working-hours window — it opens again at " + detail + " (CN10)"
	case ReasonQueueLive:
		return "the repository has live queue rows" + withDetail(r, detail) + " — roadmap work is selected only into a drained repository (CN6.2)"
	case ReasonAttendedItem:
		return "the queue item is declared attended" + withDetail(r, detail) + " — a person is steering it; the scheduler never takes an attended item"
	case ReasonPriorityUnknown:
		return "priority is unknown (D2) — an unknown priority never becomes ready by waiting; roadmap.priority sets it"
	case ReasonNotReady:
		return "not ready: " + detail + " — the readiness facts are derived at the pick, never stored (D4)"
	case ReasonDependencyOpen:
		return "waits on open dependencies: " + detail + " (CN7) — the pick re-decides readiness every tick, so this resolves the day the last dependency closes"
	case ReasonDependencyCancelled:
		return "depends on a cancelled item: " + detail + " — remove the dependency (roadmap.undepend) or cancel this item; waiting on a dead item is not the same problem as waiting (CN7)"
	case ReasonAlreadyClaimed:
		return "already claimed by another start (the claim names its owner) — the scheduler claims before it starts, so two starts never share an item"
	case ReasonCapacity:
		return "no agent slots left: " + detail + " against max_slots — deferred, never preempted (CN9)"
	case ReasonWeightExceedsCap:
		return "the repository's weight exceeds max_slots: " + detail + " — refused once, not deferred forever; raise the repository's weight or max_slots (CN9)"
	case ReasonRateLimit:
		return "the repository's starts in the last hour reached max_starts_per_root_per_hour — the oldest start leaves the window at " + detail + " (CN12)"
	case ReasonBudgetExhausted:
		return "the loop budget refuses a start for this repository: " + detail + " (S55)"
	case ReasonGrantAbsent:
		return "no autonomy grant authorises this start: " + detail + " — absent is stated, never faked (S56/V13)"
	case ReasonTemplateInvalid:
		return "the repository's work template no longer validates: " + detail + " — refused at the pick, the item is untouched, and this is a needs-you (CN5)"
	default:
		// Reaching this line means a Reason not in the closed set was
		// rendered. There is no honest sentence for an id outside the
		// vocabulary, so the answer says exactly that rather than
		// inventing one — the drift shows up in the audit line itself.
		return fmt.Sprintf("unknown reason %q — the vocabulary is closed; this decision was rendered outside it", string(r))
	}
}

// withDetail appends " (<detail>)" when a detail exists; some repositories
// of a reason carry one (the root, the count) and some do not.
func withDetail(r Reason, detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

// subjectIDs renders the id lists the dependency and capacity details use
// — ids only, sorted by the caller where order matters.
func subjectIDs(prefix string, ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s%d", prefix, id))
	}
	return strings.Join(parts, ", ")
}
