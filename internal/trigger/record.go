package trigger

import (
	"fmt"
	"time"
)

// Outcome is TR4 made a type: "did this fire?" has THREE answers, and the
// third is the one that matters. Two nullable booleans would admit a fourth
// combination that means nothing, and somebody would render it — so the
// closed set is Go here and a CHECK in the row (migration 0032), and the
// store refuses any spelling this type does not author.
//
// The set is five, not three, because a fire that was EXCLUDED and a fire
// that was REFUSED are different facts with different owners: "did not fire"
// is the schedule's own contract speaking (window, pause), "skipped" is the
// previous run still going (TR5), and "refused" is governance (TriggerAccess,
// TR9/TR12). Collapsing them into "did not fire" is the P31 failure mode in
// its cosmetic form — every row still reads as a non-event, and the signal
// is gone.
type Outcome string

const (
	// OutcomeFired: the loop started; Fire names its id.
	OutcomeFired Outcome = "fired"
	// OutcomeDidNotFire: the schedule's own contract excluded it — window,
	// pause. Reason carries which (D7's "and says why").
	OutcomeDidNotFire Outcome = "did_not_fire"
	// OutcomeCouldNotSay: the daemon was down across this instant (T4).
	// Absent-from-the-log would have been indistinguishable from
	// never-should-have-happened; this row is why it is not.
	OutcomeCouldNotSay Outcome = "could_not_say"
	// OutcomeSkipped: an overlapping fire — the previous run is still
	// going (TR5), named in Reason. Overrun is a signal, and recording it
	// is what makes it visible rather than inferable from gaps.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeRefused: TriggerAccess denied the kind (T7), or a delivery
	// failed verification (T6). Governance and authentication, each with
	// the sentence that owns the refusal.
	OutcomeRefused Outcome = "refused"
)

// Valid names the set for tests and callers — the enumeration that keeps
// "closed" an assertion rather than a comment (T4's gate).
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeFired, OutcomeDidNotFire, OutcomeCouldNotSay, OutcomeSkipped, OutcomeRefused:
		return true
	}
	return false
}

// Render is the single spelling of an outcome for humans (T4 extends it to
// the three-answers never-collapse rendering; the words live here so no
// second formatter is ever needed).
func (o Outcome) Render() string {
	switch o {
	case OutcomeFired:
		return "fired"
	case OutcomeDidNotFire:
		return "did not fire"
	case OutcomeCouldNotSay:
		return "could not say"
	case OutcomeSkipped:
		return "skipped"
	case OutcomeRefused:
		return "refused"
	}
	// An outcome outside the set cannot arrive from the store (CHECK) or
	// from this package (constants); if one is forged in memory, the
	// rendering says so rather than silently picking a neighbour.
	return fmt.Sprintf("unauthored outcome %q", string(o))
}

// The provenance words for loop.started_by. They are NOT the Kind words:
// a schedule is a KIND of row; "trigger" is what a clock fire SAYS about
// the loop it started (R-3's started_by vocabulary — trigger / webhook /
// "" for a person's own). Conflating the two vocabularies is how a row
// edit later reads as a permission change.
const (
	ProvenanceTrigger = "trigger"
	ProvenanceWebhook = "webhook"
)

// Fire is one row of trigger_fire: a question ("did this fire?") and its
// answer. The starter of the loop sees ONLY this struct — which is D1's
// shape: no field on it may name attendedness or outward permission, and
// TestStarterCarriesNoHint enforces that against the struct itself.
type Fire struct {
	// TriggerID is the schedule's id; Instant is the scheduled wall-clock
	// moment the answer belongs to (RFC3339 at the row boundary); At is
	// when the answer was written.
	TriggerID string
	Blueprint string
	Workspace string
	Goal      string
	// With carries the fire's blueprint inputs — for webhooks, the
	// labelled payload entry (T6/Q4). Values here are data for the loop,
	// never permissions for it.
	With map[string]string
	// Provenance is "trigger" or "webhook" — the loop's started_by (R-3).
	// A fire authors it; no caller of StartFromTrigger supplies it.
	Provenance string
	Instant    time.Time
	Outcome    Outcome
	Reason     string
	LoopID     string
	At         time.Time
}

// Slug builds the fired loop's slug: the schedule named beside the instant
// it fired for, so `aeon loop list` prints the provenance sentence a person
// can read without opening a row (demo §1's shape).
func (f Fire) Slug() string {
	return fmt.Sprintf("%s-%s", f.TriggerID, f.Instant.UTC().Format("20060102-1504"))
}
