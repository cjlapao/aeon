// Package retro is S63's write side: the recorder that turns a loop's
// terminal event into outcome facts (RT2) and nothing else. RT3.2's
// narrowness is a law of this package, not a preference: it reads through
// the store and writes through the store — no import of loop, runtime,
// policy, queue, roadmap or triage, and no call that can start a loop,
// change a grant or edit a setting. A T5 guard test enforces the import
// set from the outside; this header states it from the inside.
//
// Nothing here computes a score, a rate or a ranking (RT4: every tally is
// derived at read time elsewhere), and nothing here writes a learning
// (that is T3's reconcile, a different file with a different lock).
package retro

import "strings"

// blockedKinds is the closed set T0 measured (T0 Notes §4): every
// BlockedReason.Kind the engine can emit, plus the watchdog's `stuck`.
// `step-<outcome>` is deliberately NOT here — it is parameterized, and an
// unmapped value is what `other:` is for; mapping a family of unbounded
// strings would make the prefix a lie.
var blockedKinds = map[string]bool{
	"budget-unreadable":       true, // advance.go:313
	"cap-hit":                 true, // advance.go:330/438, continue.go:116
	"model-error":             true, // advance.go:351
	"contract-unverifiable":   true, // contract.go:33
	"backlog-append":          true, // advance.go:505
	"exhaustion":              true, // advance.go:549
	"unresolvable-reject":     true, // advance.go:557
	"unit-error":              true, // advance_parallel.go:353
	"pin-unparseable":         true, // continue.go:238
	"escalation-answer":       true, // escalate.go:358
	"gate-answer":             true, // runner.go:321
	"runner-error":            true, // runner.go:133
	"action-denied":           true, // execute.go:110/157/206
	"action-failed":           true, // execute.go:212
	"action-conflict":         true, // state.go:102
	"action-denied-by-person": true, // state.go:102
	"action-refused":          true, // state.go:102
	"stuck":                   true, // store/watchdog.go:746
}

// maxOtherTail is RT2's cap on the `other:<kind>` tail. The whole kind
// string must also fit retro_outcome's CHECK (≤ 48 chars, T1), and the
// "blocked/other:" prefix eats 14 of them — so the tail is capped twice:
// once at 40 by the rule, and once so the assembled kind stays a legal
// row. Both caps live here so the schema and the rule cannot drift.
const (
	maxOtherTail = 40
	maxKindLen   = 48
)

// leadingToken is RT2's "the reason string's leading token": everything up
// to the first space, colon or comma, lowercased. A structured
// BlockedReason.Kind needs no cutting — the daemon hands it over already
// extracted (the queue reactor's duck-typing precedent: retro must not
// import loop, so the *loop.BlockedReason branch lives in the daemon).
func leadingToken(reason string) string {
	t := strings.TrimSpace(reason)
	for i, r := range t {
		if r == ' ' || r == '\t' || r == ':' || r == ',' || r == ';' {
			t = t[:i]
			break
		}
	}
	return strings.ToLower(t)
}

// NormalizeBlockedKind maps a measured reason kind onto the closed
// outcome_kind vocabulary: a known kind becomes "blocked/<kind>", an
// unknown one becomes "blocked/other:<kind>" capped per maxOtherTail.
// Detail text never reaches here — only the kind token (the Do-NOT: no
// free text in a fact).
func NormalizeBlockedKind(reason string) string {
	t := leadingToken(reason)
	if t == "" {
		return "blocked/other:unknown"
	}
	if blockedKinds[t] {
		return "blocked/" + t
	}
	return otherKind("blocked/other:", t)
}

// otherKind caps the tail first at RT2's 40 and then at whatever the
// family prefix ("blocked/other:", "other:") leaves inside the schema's
// 48. Two families reach here: the blocked one (RT2) and the finished
// one (an unknown status word is `other:finished-<token>` — the `other:`
// namespace without the blocked/ path, because it is not a block).
func otherKind(prefix, t string) string {
	if len(t) > maxOtherTail {
		t = t[:maxOtherTail]
	}
	t = strings.ToValidUTF8(t, "")
	if len(prefix)+len(t) > maxKindLen {
		t = t[:maxKindLen-len(prefix)]
	}
	return prefix + t
}

// OutcomeKindForFinished maps a loop.finished status. T0 measured the
// event's status vocabulary as done|aborted (S55's cancel writes the
// aborted flag and endStatus produces `aborted`; `cancelled` is the
// loop_run STATE word, not the event's — rest.go:47). `cancelled` still
// maps, defensively: if a future emit carries it, the fact says it.
// An unknown status has no kind to fabricate — it is not the recorder's
// business to decide which words are typos, so it lands in `other:` and
// the growing shape of the closed set stays honest about what arrived.
func OutcomeKindForFinished(status string) string {
	switch status {
	case "done", "aborted", "cancelled":
		return status
	}
	t := leadingToken(status)
	if t == "" {
		return otherKind("other:", "finished-unknown")
	}
	return otherKind("other:finished-", t)
}

// TerminalKey is RT2's idempotency key, per the decision T0 shipped
// (T0 Notes: the event's append seq is NOT reachable from publishLoop's
// reactors): the outcome kind beside the pin's updated_at, which the
// recorder re-reads (store mints it with ms precision, isoUTC). A blocked
// then done history differs in kind; a replayed event differs in neither
// half — and the unique index, not this function, is what refuses it.
func TerminalKey(outcomeKind, pinUpdatedAt string) string {
	return outcomeKind + "@" + pinUpdatedAt
}
