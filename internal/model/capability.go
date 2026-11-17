package model

import (
	"fmt"
	"slices"
	"strings"
)

// # The wire-behaviour vocabulary (S18, Q8)
//
// A `Capability` says how a route's SERVER behaves on the wire — what it
// accepts, what it streams back, what it refuses. It is a different axis from
// the metadata vocabulary in metadata.go (`text|image|tool-calling|pdf|file|
// thinking|thinking-effort`), which says what the MODEL is *for* and feeds the
// app's catalogue. The two vocabularies both contain the string `thinking`
// meaning different things — the metadata one says the model can reason, this
// one says the server returns `thinking` blocks on this wire — and they
// never share a field or a validator. `TestWireAndMetadataCapabilitiesAreDistinct`
// pins the separation; the rule here is structural: this file never calls
// `normalizeSelection`, and metadata.go never learns these five names.
//
// # Why a closed set of five (M13)
//
// "The differences are absences, not behaviours" is only a viable plan if
// every real difference between OpenAI-compatible servers is *expressible*
// as one of these flags. Each entry therefore carries a documented absence
// (`AbsenceNote`): the flag is not a feature toggle, it is a promise about
// what happens when the feature is NOT there. A sixth flag arrives with its
// absence sentence or not at all.
type Capability string

const (
	CapTools          Capability = "tools"
	CapStreamingUsage Capability = "streaming-usage"
	CapThinking       Capability = "thinking"
	CapSystemRole     Capability = "system-role"
	CapParallelTools  Capability = "parallel-tools"
)

// WireCapabilityFlags is the closed set, in canonical order. Iteration order
// is load-bearing: it is the order `routes list` prints, and a map would
// shuffle the same route's flags between two runs of the same command.
func WireCapabilityFlags() []string {
	return []string{
		string(CapTools), string(CapStreamingUsage), string(CapThinking),
		string(CapSystemRole), string(CapParallelTools),
	}
}

// absenceOf is Q8's committed table as DATA, so a test can assert it covers
// every flag — a flag whose absence is undescribed is a difference nobody
// has agreed how the product behaves around, which is exactly the silent
// branch M13 forbids.
var absenceOf = map[Capability]string{
	CapTools: "the server refuses function tools: a step on this route cannot run tools at all, " +
		"and the pre-call check (S18 T4) refuses before the request is paid for",
	CapStreamingUsage: "usage arrives at the end of the stream or not at all — the cost path's " +
		"every-field-optional shape (M9) is what makes 'not at all' survivable rather than fatal",
	CapThinking: "no thinking blocks come back: reasoning prose arrives as text or not at all, and " +
		"M27's replay rule never sees a block it needs to route to its producer",
	CapSystemRole: "the server honours no system-role position: the system prompt must ride as the " +
		"first user turn, which the adapter does and the transcript still records as one system turn",
	CapParallelTools: "at most one tool call per assistant turn: a turn emitting several is honoured " +
		"serially or refused, and the loop's serial execution (K9) already makes the serial reading correct",
}

// AbsenceNote states what the flag's ABSENCE changes — the sentence is the
// point, and the table test fails if a flag arrives without one.
func AbsenceNote(c Capability) (string, bool) {
	s, ok := absenceOf[c]
	return s, ok
}

// ValidWireCapability reports membership. It checks against the closed set,
// never against metadata.go.
func ValidWireCapability(v string) bool {
	return slices.Contains(WireCapabilityFlags(), v)
}

// NormalizeWireCapabilities validates and canonically orders the route's wire
// flags — the sibling of NormalizeMetadata and deliberately not its friend:
// it never calls normalizeSelection, because the metadata vocabulary would
// reject `tools` and `parallel-tools` and admit `image`, which is a statement
// about a model this flag set does not make.
//
// nil means UNDECLARED (the route says nothing about behaviour); the empty
// slice means the operator DECLARED "none of the five". They are different
// answers and the OptionalSelection shape carries the distinction onward.
func (r *Route) NormalizeWireCapabilities() error {
	if r.WireCapabilities == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, v := range r.WireCapabilities {
		v = strings.TrimSpace(v)
		if v == "" {
			return fmt.Errorf("wire capability of route %q is an empty string; the accepted flags "+
				"are %s, or omit the declaration entirely", r.ID, strings.Join(WireCapabilityFlags(), ", "))
		}
		if !ValidWireCapability(v) {
			return fmt.Errorf("unknown wire capability %q on route %q; the accepted flags are %s. "+
				"(These are wire flags, not the model catalogue's capabilities — those are a separate "+
				"field and a separate vocabulary.)",
				v, r.ID, strings.Join(WireCapabilityFlags(), ", "))
		}
		if seen[v] {
			return fmt.Errorf("wire capability %q is declared twice on route %q", v, r.ID)
		}
		seen[v] = true
	}
	out := make([]string, 0, len(r.WireCapabilities))
	for _, f := range WireCapabilityFlags() {
		if seen[f] {
			out = append(out, f)
		}
	}
	r.WireCapabilities = out
	return nil
}

// # The three states of a declared capability (M28, D6, P31)
//
// A flag on a route is a CLAIM. `declared` is the honest word for a claim
// nobody has checked; `verified` is what a probe's agreement earns (T3);
// `contradicted` is what a probe's disagreement records, and it is not
// deleted because the disagreement is the fact worth keeping (a route copied
// from a colleague is a plausible lie).
//
// The zero value of CapabilityState is "" and it is NOT a state — an absent
// probe row means "declared", which is a word the view layer says, not an
// empty string the record hides behind. That is why the test is named
// TestThreeCapabilityStates rather than trusting the switch to fall through.

type CapabilityState string

const (
	StateDeclared     CapabilityState = "declared"
	StateVerified     CapabilityState = "verified"
	StateContradicted CapabilityState = "contradicted"
)

// ValidCapabilityState refuses the empty string and anything else outside the
// three. Cache rows (T3) validate through here on write and on read.
func ValidCapabilityState(s CapabilityState) bool {
	switch s {
	case StateDeclared, StateVerified, StateContradicted:
		return true
	}
	return false
}
