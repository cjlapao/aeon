package attention

// severity.go — the closed severity vocabulary and its order (SV2).
//
// "Shape, not records" (rule 6): the classes are CODE, like the policy
// vocabulary — no table lists them, and a row cannot invent a new one. The
// order is the ranking the inbox sorts by, strongest first; within a class
// the OLDEST waits longest (SV2).

// Severity is one attention class.
type Severity string

// The five classes, in SV2's order. The meanings are the design's:
// needs-you (a question, an escalation, a cap hit, a failed gate, a
// scheduler item), stuck (S55's watchdog), blocked, stopped, info.
const (
	NeedsYou Severity = "needs-you"
	Stuck    Severity = "stuck"
	Blocked  Severity = "blocked"
	Stopped  Severity = "stopped"
	Info     Severity = "info"
)

// Severities is the closed vocabulary in rank order — the one list the
// wire validation and the ordering agree on.
func Severities() []Severity {
	return []Severity{NeedsYou, Stuck, Blocked, Stopped, Info}
}

// ValidSeverity is the wire-boundary check: a param naming a severity the
// code does not carry is a REFUSAL (DB19's discipline), never a filter that
// silently matches nothing.
func ValidSeverity(s string) bool {
	for _, v := range Severities() {
		if string(v) == s {
			return true
		}
	}
	return false
}

// rank is the sort weight: lower ranks earlier. An unknown severity — only
// reachable through a source with a bug, since the vocabulary is closed —
// ranks last rather than jumping the queue.
func (s Severity) rank() int {
	for i, v := range Severities() {
		if v == s {
			return i
		}
	}
	return len(Severities())
}

// Less is the inbox order (SV2): severity class first, then the OLDEST
// `since` first, then the key — a total order, so two evaluations of an
// unchanged store agree byte for byte (TestSeverityOrderAndAgeTieBreak).
//
// An unparseable `since` (AgeSeconds -1 upstream; here, a failed parse)
// sorts LAST within its class: an item whose age is unknown waits behind
// items whose age is known, rather than claiming to be ancient.
func Less(a, b Item) bool {
	if ra, rb := a.Severity.rank(), b.Severity.rank(); ra != rb {
		return ra < rb
	}
	ta, aok := parseSince(a.Since)
	tb, bok := parseSince(b.Since)
	switch {
	case aok && bok:
		if !ta.Equal(tb) {
			return ta.Before(tb)
		}
	case aok != bok:
		return aok // known ages before unknown ones
	}
	return a.Key < b.Key
}
