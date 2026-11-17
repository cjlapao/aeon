// Package watchdog is the pure brain behind S55's loop watchdog (T9, LC17).
//
// It turns "a running loop that is not progressing" into a set of FIRINGS, one
// per rule whose threshold the observation has crossed. It reads no clock of
// its own (now is handed in), touches no store, and holds no state: the
// episode memory that makes a firing once-per-episode lives in the daemon
// service (T9), not here. Keeping the decision pure is what lets the whole
// signal × threshold × episode surface be table-tested against nothing.
//
// # What this package does NOT do
//
// It never acts. A firing is a description ("rule X crossed, the metric is Y,
// this is episode Z"); the daemon decides the concrete store call (notify,
// park, block) and, for stop/block, which of the two by attendedness (LC17).
// And it never retries or re-dispatches anything (OD8) — there is no advance,
// no start, no resume here, by construction.
//
// The signal and action vocabularies are closed sets. The daemon serves them
// through watchdog.vocabulary (rule 4: the client builds from the daemon,
// never a local copy); this package is the one Go definition the daemon's
// vocabulary read and the evaluator both use, so the two cannot drift.
package watchdog

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// Scope is the rule's reach: a workspace rule covers every loop, a component
// rule covers one component (and, when the subject is a root, its subtree —
// a root's telemetry is the subtree total, so a root's rule reads it that
// way too).
type Scope string

const (
	// ScopeWorkspace is the whole daemon: the subject_id is the literal
	// "workspace" and every running loop is in scope.
	ScopeWorkspace Scope = "workspace"
	// ScopeComponent is one component (or a root, whose tree it covers).
	ScopeComponent Scope = "component"
)

// Scopes is the closed set, in the order the vocabulary serves it.
var Scopes = []Scope{ScopeWorkspace, ScopeComponent}

// Signal is the one thing a rule watches (LC17's closed vocabulary).
type Signal string

const (
	// SignalNoActivity: a running loop with no event, step checkpoint or step
	// row update for thresholdMs.
	SignalNoActivity Signal = "no-activity"
	// SignalSamePosition: the loop has re-dispatched the same position at
	// least threshold times (retries/redo the reject counters did not cap).
	SignalSamePosition Signal = "same-position"
	// SignalRepeatBlockReason: the same block reason observed at least
	// threshold consecutive times.
	SignalRepeatBlockReason Signal = "repeat-block-reason"
	// SignalStepOverDeadline: a step still running past its manifest
	// DeadlineAt + a grace of thresholdMs (the runtime should have stopped it).
	SignalStepOverDeadline Signal = "step-over-deadline"
)

// Signals is the closed set, in the order the vocabulary serves it.
var Signals = []Signal{
	SignalNoActivity,
	SignalSamePosition,
	SignalRepeatBlockReason,
	SignalStepOverDeadline,
}

// Action is what a firing does (LC17's closed vocabulary). notify is the
// screen event; stop and block are the two loop moves, the one taken decided
// by attendedness at fire time (LC17), not by which of the two was written.
type Action string

const (
	// ActionNotify: an event the person sees; nothing about the loop moves.
	ActionNotify Action = "notify"
	// ActionStop: park the loop (waiting/stopped) — the attended move.
	ActionStop Action = "stop"
	// ActionBlock: block the loop (blocked, kind stuck) — the unattended move.
	ActionBlock Action = "block"
)

// Actions is the closed set, in the order the vocabulary serves it.
var Actions = []Action{ActionNotify, ActionStop, ActionBlock}

// Origin names who wrote a rule: a system row (the seeded default set) or a
// user row (an operator's edit or a fresh rule). A user-origin rule is never
// re-seeded; a system one may be restored explicitly, never silently.
type Origin string

const (
	// OriginSystem is a row the seeded default set wrote.
	OriginSystem Origin = "system"
	// OriginUser is a row an operator created or edited.
	OriginUser Origin = "user"
)

// Rule is one watchdog_rule row, the unit the service evaluates. Threshold's
// unit is the signal's: milliseconds for the two time signals (no-activity,
// step-over-deadline — where it is the grace past the deadline), a count for
// the two count signals (same-position, repeat-block-reason). The unit is
// served by watchdog.vocabulary, never assumed by a client.
type Rule struct {
	ID        string
	Scope     Scope
	SubjectID string
	Signal    Signal
	Threshold int64
	Action    Action
	Enabled   bool
	Origin    Origin
}

// Position is the loop's current place in its ladder: the lane, the item and
// the step. It is the identity the same-position signal counts against — a
// loop re-dispatching the SAME position is the retry loop the reject
// counters did not cap, and moving to a new one is progress that resets the
// episode.
type Position struct {
	Lane string
	Item string
	Step string
}

// Hash is the position's canonical identity: the sha256 of the three fields
// joined by a separator no field can contain on its own. It is the episode
// identity for same-position, so a loop that moves is a new episode and one
// that re-dispatches the same place is not.
func (p Position) Hash() string {
	sum := sha256.Sum256([]byte(p.Lane + "\x1f" + p.Item + "\x1f" + p.Step))
	return hex.EncodeToString(sum[:])
}

// Observation is the service's read of ONE running loop at one instant —
// everything Evaluate needs, gathered by the daemon from the store (the last
// activity, the current position and its dispatch count, the tail of the
// block-reason run, the running step and its deadline). A zero Observation is
// "nothing measured": a signal whose field is absent does not fire (the
// availability probe is what says a signal is genuinely off, separately).
type Observation struct {
	LoopID string
	Status string
	// Attended is the loop's pinned attendedness (D2): attended, unattended,
	// or never recorded (nil). The service reads it to pick stop vs block;
	// Evaluate itself never does — the signal is about progress, not presence.
	Attended      *bool
	ComponentRoot string
	Root          string

	// LastActivityAt is the newest of the loop's activity sources (a step
	// checkpoint, a pin update, the loop's own start). The no-activity clock.
	LastActivityAt time.Time

	// Position and PositionDispatches are the same-position inputs: where the
	// loop is now, and how many times that exact position has been dispatched
	// (the count the reject counters did not cap).
	Position           Position
	PositionDispatches int

	// LastReasonHash, ReasonRepeats and ReasonRunSince are the
	// repeat-block-reason inputs: the digest of the current tail reason, how
	// many times that same reason has run consecutively, and when that run
	// began. ReasonRepeats is 0 when the loop has not been blocked at all.
	LastReasonHash string
	ReasonRepeats  int
	ReasonRunSince time.Time

	// StepRunningSince and StepDeadline are the step-over-deadline inputs:
	// when the running step started and its manifest DeadlineAt (zero when the
	// step declared none, in which case the signal cannot fire).
	StepRunningSince time.Time
	StepDeadline     time.Time
}

// Firing is one rule the observation has tripped. The service turns it into a
// concrete act (the action, mapped by attendedness for stop/block) and uses
// Episode for its once-per-episode memory: a firing whose Episode matches the
// last one recorded for (loop, rule) is a repeat and is dropped; a new
// Episode is fresh progress and may fire again.
type Firing struct {
	Rule    Rule
	Metric  int64
	Episode string
}

// Match reports whether a rule applies to an observation: a workspace rule
// covers every loop, a component rule covers its component and, when the
// subject is a root, the subtree under it. A disabled rule is not matched (the
// caller filters, but the guard is here too — a rule that is off must not
// fire from any code path).
func Match(rule Rule, obs Observation) bool {
	if !rule.Enabled {
		return false
	}
	switch rule.Scope {
	case ScopeWorkspace:
		return true
	case ScopeComponent:
		return rule.SubjectID != "" &&
			(obs.ComponentRoot == rule.SubjectID ||
				obs.Root == rule.SubjectID ||
				strings.HasPrefix(obs.ComponentRoot, rule.SubjectID+"/") ||
				strings.HasPrefix(obs.Root, rule.SubjectID+"/"))
	default:
		return false
	}
}

// metricFor computes the metric a rule's signal reads off the observation,
// and whether the signal is measurable at all (ok false means the field the
// signal needs is absent — the signal does not fire, and it is not an error).
// The threshold is NOT applied here: a signal that is measurable but under
// threshold is a different answer (no firing) from one that is absent.
func metricFor(signal Signal, obs Observation, threshold int64, now time.Time) (metric int64, ok bool) {
	switch signal {
	case SignalNoActivity:
		if obs.LastActivityAt.IsZero() {
			return 0, false
		}
		return now.Sub(obs.LastActivityAt).Milliseconds(), true
	case SignalSamePosition:
		return int64(obs.PositionDispatches), true
	case SignalRepeatBlockReason:
		if obs.ReasonRepeats == 0 {
			return 0, false // no block run at all: nothing to repeat
		}
		return int64(obs.ReasonRepeats), true
	case SignalStepOverDeadline:
		if obs.StepDeadline.IsZero() {
			return 0, false // the step declared no deadline: the signal is off
		}
		grace := obs.StepDeadline.Add(time.Duration(threshold) * time.Millisecond)
		if now.After(grace) {
			return now.Sub(grace).Milliseconds(), true
		}
		return 0, false // still inside the deadline + grace: not over yet
	default:
		return 0, false
	}
}

// crossed reports whether a measurable signal's metric has reached the rule's
// threshold. The two time signals fire at-or-past their threshold; the two
// count signals fire at-or-past theirs; step-over-deadline's metric is
// already the overshoot, so any measurable result is a crossing.
func crossed(signal Signal, metric int64, threshold int64) bool {
	switch signal {
	case SignalStepOverDeadline:
		return metric >= 0 // measurable here only when over the deadline + grace
	default:
		return metric >= threshold
	}
}

// episodeFor renders the episode identity for a firing (LC17's
// once-per-episode rule). The shape is the signal's: each encodes the thing
// whose CHANGE is the loop's progress, so a loop that progresses gets a new
// episode and may fire again, while one that merely keeps stalling stays in
// the same episode and fires once.
func episodeFor(signal Signal, obs Observation) string {
	switch signal {
	case SignalNoActivity:
		// A new moment of activity is a new episode: silence after it is
		// fresh, not a repeat of the old quiet.
		return "act:" + obs.LastActivityAt.UTC().Format(time.RFC3339Nano)
	case SignalSamePosition:
		// A new position is a new episode; re-dispatching the same one is not.
		return "pos:" + obs.Position.Hash()
	case SignalRepeatBlockReason:
		// A new reason, or a new run of the same reason, is a new episode.
		return "run:" + obs.LastReasonHash + ":" + obs.ReasonRunSince.UTC().Format(time.RFC3339Nano)
	case SignalStepOverDeadline:
		// A new running step is a new episode; the same step lingering is not.
		return "step:" + obs.StepRunningSince.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
}

// Evaluate is the whole decision: over every rule that matches the
// observation and is enabled, the ones whose signal is measurable and whose
// metric has crossed the threshold, each as a Firing carrying its episode.
// It is pure — a function of (rules, obs, now) only — and deterministic in
// rule order, so the table test pins the exact set and order.
func Evaluate(rules []Rule, obs Observation, now time.Time) []Firing {
	var out []Firing
	for _, rule := range rules {
		if !Match(rule, obs) {
			continue
		}
		metric, ok := metricFor(rule.Signal, obs, rule.Threshold, now)
		if !ok {
			continue
		}
		if !crossed(rule.Signal, metric, rule.Threshold) {
			continue
		}
		out = append(out, Firing{Rule: rule, Metric: metric, Episode: episodeFor(rule.Signal, obs)})
	}
	return out
}
