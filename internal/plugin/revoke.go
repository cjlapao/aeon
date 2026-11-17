// revoke.go — revocation that REVOKES.
//
// The easy half is refusing new invocations: the state machine (T3) already
// stops enable-on-revoked; the invocation gate extends that to Task
// creation. The hard half is the sentence the brief makes unavoidable:
//
//	> A Task already in flight will complete, and its result will reach a
//	> prompt — unless something stops it.
//
// A revoked agent's answer landing in a model's context is exactly what
// revocation was supposed to prevent, so the result is refused ON ARRIVAL —
// at the point the result returns, not at creation, because at creation the
// agent was not revoked — and the refusal is recorded (AP7). The cost is
// RECORDED, not recovered: it happened, and pretending otherwise makes the
// ledger lie.
//
// The two gates here are INJECTED CLOSURES handed to internal/agentsvc's
// TaskStore (SetInvocationGate / SetArrivalGate, wired by the entry point)
// — the direction is agentsvc←cmd, never agentsvc←plugin (AS11; the layer
// sweep counts test binaries too, so even the real-Task tests live with the
// layer, not here). Nil gates in an unwired daemon change nothing (S14's
// inertness discipline).
//
// And the contrast that makes `disable` MEAN something (Q3, AP12): a
// DISABLED agent's in-flight Task COMPLETES — "not now" is cheap,
// reversible, and its work already paid for arrives normally. Only revoke
// stops the arrival. If both refused in flight, they would read identically
// and somebody would reach for disable expecting it to stop something.
package plugin

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// RefusalRecord is what AP7 demands be kept when a completed stranger's
// words are stopped at the door: the agent, the revocation instant, the
// reason, and the cost — recorded, never recovered. ReachedPrompt is false
// structurally (the refusal sentence is what the caller receives; the
// stranger's text is never handed onward) and the read asserts it.
type RefusalRecord struct {
	Agent      string
	TaskID     int64
	RevokedAt  time.Time
	Reason     string
	CostJSON   string // the reported cost, kept as the ledger has it
	CostKnown  bool   // false = unreported = UNKNOWN, never zero (S3's three states)
	RecordedAt time.Time
}

// InvocationGate is the closure handed to TaskStore.SetInvocationGate: a
// plugin agent that is not enabled is refused at CREATION — first-party
// agents and unknown names pass (they are not plugin-managed; refusing the
// 25 for lacking a plugin row would be the daemon tripping over its own
// abstraction). The durable record outranks the in-memory copy, so a
// restarted daemon refuses by what the rows say.
func InvocationGate() func(calleeAgent string) (refusal string, refused bool) {
	return func(calleeAgent string) (string, bool) {
		a, ok := Lookup(calleeAgent)
		if ok && a.FirstParty {
			return "", false
		}
		st := State("")
		if ok {
			st = a.State
		}
		if stateStore != nil {
			if durable, found := stateStore.StateOf(calleeAgent); found {
				st = durable
			}
		}
		if st == State("") {
			return "", false // neither in memory nor in the ledger: not plugin-managed at all
		}
		switch st {
		case StateEnabled:
			return "", false
		case StateDisabled:
			return fmt.Sprintf("%s is disabled — \"not now\". It has no new invocations until it is enabled again · remedy: aeon agent enable %s (its IN-FLIGHT work, unlike a revoked agent's, will still complete and arrive)", calleeAgent, calleeAgent), true
		case StateRevoked:
			at, reason := revocationOf(calleeAgent)
			return fmt.Sprintf("%s was revoked at %s — reason: %s. \"Never again\" means no new invocations, and its in-flight results are refused on arrival too (AP7)", calleeAgent, at.Format(time.RFC3339), reason), true
		}
		return "", false
	}
}

// ArrivalGate is the closure handed to TaskStore.SetArrivalGate — consulted
// when a COMPLETED async result returns. Only revoke stops arrival (Q3):
// disabled agents' work completes normally. The refusal is RECORDED here —
// at the decision point, not by a later reader — and the sentence returned
// is what replaces the stranger's words in the caller's context.
func ArrivalGate() func(calleeAgent string, taskID int64, cost *model.Cost) (refusal string, refused bool) {
	return func(calleeAgent string, taskID int64, cost *model.Cost) (string, bool) {
		a, ok := Lookup(calleeAgent)
		if ok && a.FirstParty {
			return "", false
		}
		st := State("")
		if ok {
			st = a.State
		}
		if stateStore != nil {
			if durable, found := stateStore.StateOf(calleeAgent); found {
				st = durable
			}
		}
		if st != StateRevoked {
			return "", false // including DISABLED: its Task completes (the contrast that makes disable mean something)
		}
		at, reason := revocationOf(calleeAgent)
		costJSON, known := "", false
		if cost != nil {
			if raw, err := json.Marshal(cost); err == nil {
				costJSON, known = string(raw), true
			}
		}
		rec := RefusalRecord{Agent: calleeAgent, TaskID: taskID, RevokedAt: at, Reason: reason, CostJSON: costJSON, CostKnown: known, RecordedAt: time.Now().UTC()}
		if stateStore != nil {
			if rj := stateStore.RecordRefusal(rec); rj != nil {
				// A refusal whose record cannot be written is still a
				// refusal — the door holds even when the ledger cannot be
				// read aloud (fail CLOSED, the S10 way).
				rec.Reason = reason + " (refusal record failed to write: " + rj.Error() + ")"
			}
		}
		return fmt.Sprintf("refused on arrival: %s was revoked at %s — reason: %s. The Task completed and was PAID FOR; its cost is recorded here, not recovered (%s), and its words do not enter your context (AP7).",
			calleeAgent, at.Format(time.RFC3339), reason, costLine(rec)), true
	}
}

// costLine states the cost in its three honest states — an unreported cost
// says UNKNOWN, never zero.
func costLine(r RefusalRecord) string {
	if !r.CostKnown {
		return "cost: unreported — unknown, not zero"
	}
	return "cost recorded-not-recovered: " + r.CostJSON
}

// revocationOf reads the revocation instant and reason out of the recorded
// life (the T3 transitions) — the refusal names WHEN it became never-again
// and WHY, because those are the two things the next reader asks. A
// revoked agent with no recorded row (hand-written row, ancient database)
// still refuses; the sentence says the record is missing.
func revocationOf(name string) (time.Time, string) {
	if stateStore == nil {
		return time.Time{}, "no ledger is wired — refusing anyway (fail closed)"
	}
	rows, rj := stateStore.Transitions(name)
	if rj != nil {
		return time.Time{}, "the ledger is unavailable (" + rj.Error() + ") — refusing anyway (fail closed)"
	}
	var lastAt time.Time
	var lastReason string
	found := false
	for _, t := range rows {
		if t.To == StateRevoked {
			lastAt, lastReason, found = t.At, t.Reason, true
		}
	}
	if !found {
		return time.Time{}, "the revoked row carries no transition record — refusing anyway (fail closed)"
	}
	return lastAt, lastReason
}

// Refusals reads an agent's stopped arrivals for the CLI and the panel.
func Refusals(agent string) ([]RefusalRecord, *Reject) {
	if stateStore == nil {
		return nil, refuse("audit", "refused: no ledger is wired — the refusals of %s are UNAVAILABLE, which is not the same as none (S10's third state)", agent)
	}
	return stateStore.RefusalsOf(agent)
}
