// Package automation holds the shapes S40's dispatcher speaks to the daemon
// — the request an event start carries, and the two kinds of "no" it can
// get back. It is deliberately small and deliberately between: the
// dispatcher (daemon) asks, the Starter answers, and neither the store nor
// the protocol package knows this file exists (arrows: daemon → automation
// → store/trigger; the store must never import here, or the outbox would
// know about the thing that reads it).
package automation

import (
	"context"
	"fmt"
	"strings"
)

// EventStartRequest is one authorised start: what to run, where, on whose
// subject, and at what depth. It carries NO Attended and NO AllowOutward
// (AU2, S20's rule): those are authored by the start path from the brief,
// never smuggled in by a request — a fire that could declare itself attended
// would be a fire that could answer its own questions.
type EventStartRequest struct {
	TriggerID, TriggerName        string
	Blueprint                     string
	WorkspaceRoot                 string
	Slug                          string
	Event, SubjectKind, SubjectID string
	SubjectVersion                int
	// KickoffTemplate is the trigger author's override of the blueprint's
	// own kickoff (AU10's precedence: trigger template > blueprint
	// template > subject body). "" means "the blueprint's, as ever".
	KickoffTemplate string
	// Depth is the automation_depth the loop row records — the dispatcher
	// passes event depth + 1 (AU5). It is data, not a request: the Starter
	// writes what it is given into the pin, and nothing infers it.
	Depth int
}

// StartResult is the loop that now exists.
type StartResult struct {
	LoopID, Slug string
}

// Starter is the daemon's start door as the dispatcher knows it. One
// method, one request, no side channels.
type Starter interface {
	Start(ctx context.Context, r EventStartRequest) (StartResult, error)
}

// Refusal is a DETERMINISTIC no: the fire was understood, and it cannot
// happen, and the sentence says why (AU8 step 7's "refused" verdict).
// Anything that is NOT a *Refusal means "cannot say" — an infrastructure
// failure the dispatcher must record as could-not-say, never dress up as a
// decision. Flattening every error into Refusal would turn a database that
// is wedged into a confident "refused", which is exactly the confident wrong
// answer AU12 exists to refuse.
//
// Reason is one of store.EventFireReasons["refused"]'s closed codes;
// Sentence is the verbatim explanation for the fire's row.
type Refusal struct {
	Reason   string
	Sentence string
}

func (r *Refusal) Error() string { return r.Reason + ": " + r.Sentence }

// AutoSlug is the deterministic loop slug of one (trigger, outbox row).
// Determinism is not decoration: it is AU13's crash-recovery handle — a
// dispatcher that restarts mid-fire recomputes the SAME slug and finds the
// loop it made. What it is NOT is the dedup: T0 measured that `loop` has
// no uniqueness on (component_root, slug), so the deterministic claim is
// the event_fire row (T5's claim-before-start), not a constraint. The
// auto- prefix keeps the namespace apart from S20's <trigger id>-<instant>
// schedule slugs, which no automation will ever collide with.
func AutoSlug(triggerID string, outboxID int64) string {
	return fmt.Sprintf("auto-%s-%d", slugPart(triggerID), outboxID)
}

// slugPart lower-cases and keeps [a-z0-9] — the characters a slug is
// believed to hold everywhere they are compared (S20's own slugs are id
// plus a formatted instant, the same alphabet). Everything else is
// dropped, never replaced: a deterministic drop cannot surprise a recovery
// reader recomputing the same slug from the same inputs.
func slugPart(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
