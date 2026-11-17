package runtime

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/model"
)

// The pre-call capability check (S18 T4, M29): a step that needs `tools`
// against a route that has honestly declared it does not have them is
// refused HERE — in the runtime, before compilation reaches a provider,
// before a context is resolved, before a request is paid for. Discovering
// it at the first tool call wastes every step before it.
//
// # What refuses, and what deliberately does not
//
// A refusal needs a DECLARATION to stand on. `WireCapabilities == nil` means
// the route has never said anything — and an undeclared route is not a route
// that has said "no". That is the same division the probe honours (a
// contradiction judges only declared capabilities, I5): the check judges the
// operator's stated answer, never the absence of one. `[]` — declared-none —
// is the operator stating "no", and a step needing tools against it refuses.
//
// # Distinct from the probe's contradiction (T3), on purpose
//
//	"step requires 'tools'; route does not have it"          this file — fix the ROUTE
//	"route declares 'tools' but the probe contradicted it"   probe.go     — fix the SERVER, or the claim
//
// Conflating them would tell someone to change a route that was right, or to
// trust a server that was wrong. The two sentences share no substring a
// careless reader could match, and TestDistinctFromContradiction pins it.

// CapabilityRefusal is a domain refusal, not an infrastructure failure
// (S4's DB19): `errors.As` finds it, and nothing resembling a transport
// error ever will. Its Error() text is the sentence every surface prints
// verbatim — one formatter, like the policy verdict.
type CapabilityRefusal struct {
	Capability model.Capability
	RouteID    string
}

func (r *CapabilityRefusal) Error() string {
	// "no model call was made" is part of the sentence, not decoration: the
	// first question anyone asks a refusal about a paid API is whether they
	// were charged, and this is the one place that knows the answer is no.
	return fmt.Sprintf("refused: step requires capability '%s'; route '%s' does not have it (no model call was made)",
		r.Capability, r.RouteID)
}

// Required returns the capabilities a compiled step needs. A non-empty
// toolset needs `tools`; a step carrying reasoning needs `thinking`. Nothing
// else in the current set is step-dependent today — the shape is what
// matters, because the next capability that is will have a place here and
// nowhere else.
//
// It reads the STEP, never the route: the route's own default effort is the
// route owner's configuration, and a step that asks for reasoning on this
// call is the one paying for it.
func Required(req StepRequest) []model.Capability {
	var need []model.Capability
	if len(req.Tools) > 0 {
		need = append(need, model.CapTools)
	}
	if req.ReasoningEffort != "" {
		need = append(need, model.CapThinking)
	}
	return need
}

// Check refuses a step whose route has DECLARED it lacks a capability the
// step requires, naming both sides. The first missing capability in
// Required's order is the one named — a step missing two capabilities is
// fixed one honest declaration at a time, and a sentence naming both invites
// a fix for the capability the operator never meant to offer.
func Check(route model.Route, req StepRequest) error {
	if route.WireCapabilities == nil {
		// Never declared: no stated "no" to stand on. (The probe is how an
		// undeclared route earns an answer; nothing here auto-fires, M32.)
		return nil
	}
	declared := make(map[model.Capability]bool, len(route.WireCapabilities))
	for _, c := range route.WireCapabilities {
		declared[model.Capability(c)] = true
	}
	for _, cap := range Required(req) {
		if !declared[cap] {
			return &CapabilityRefusal{Capability: cap, RouteID: route.ID}
		}
	}
	return nil
}
