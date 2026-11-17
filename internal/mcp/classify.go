package mcp

// classify.go — the Q4 answer, and the no-default rule, in one file.
//
// **Q4: classification is declared in Aeon's configuration, not asked of the
// server.** Asking means trusting a stranger's self-assessment of how
// dangerous its own tool is — and MC6 refuses a *default* for exactly that
// reason, so accepting the server's claim would be the same mistake with
// extra steps. Our config is reviewable; the server's word is not.
//
// What the server's word IS: a hint that may **RAISE** a class we set and
// may never **LOWER** one. `raiseClasses` below is the only merge, and the
// direction is the whole safety argument — a hint can only move the system
// toward caution, so a server that lies kind costs nothing, and a server
// that admits damage is believed.
//
// What a hint can never do: **invent** a class. An unclassified tool stays
// refused whatever the server claims about itself; TestServerHintNeverInventsAClass
// is the pin. (The dangerous default and the kind stranger are the same
// failure wearing different clothes.)

import (
	"strings"

	"github.com/cjlapao/aeon/internal/tools"
)

// ToolAnnotations is the subset of MCP's tool annotations this client reads.
// The fields are POINTERs on purpose: absent must stay distinguishable from
// false, and "the server said nothing" is not "the server denied it".
// IdempotentHint and OpenWorldHint are accepted and ignored — accepted so
// the wire shape matches the spec, ignored because a recovery class and an
// effect class do not consult them. A future slice that needs them does it
// in its own reviewed diff, not by discovering this struct exists.
type ToolAnnotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

// raiseClasses merges the server's hint into OUR classes. No `default:`
// arm exists here and TestNoDefaultClassificationExists fails the build if
// one is ever added: the switch over an enumerated vocabulary must enumerate,
// and a fallback that classifies is this file's one unacceptable shape (MC6,
// and R3's "it will look like tidy code").
func raiseClasses(rerun tools.RerunClass, effect EffectClass, hint *ToolAnnotations) (tools.RerunClass, EffectClass) {
	if hint == nil {
		return rerun, effect
	}
	if hint.DestructiveHint != nil && *hint.DestructiveHint {
		// A destructive admission outranks any rerun class below detectable
		// (a crashed recovery re-running a destructive call is the failure
		// W16 exists for) and any effect below outward (the damage is not
		// inside the workspace).
		switch rerun {
		case tools.RerunSafe:
			rerun = tools.RerunDetectable
		case tools.RerunDetectable:
		case tools.RerunUnsafe:
		}
		switch effect {
		case EffectClassLocal:
			effect = EffectClassOutward
		case EffectClassOutward:
		case EffectClassIrreversible:
		}
	}
	// ReadOnlyHint is READ and deliberately unused. It asserts LESS danger,
	// and a stranger's kind word about its own tool is not evidence — it is
	// the pressure MC6 was written against. Reading it (rather than not
	// parsing it at all) keeps the wire shape honest and the refusal
	// explicit for whoever greps for "readOnlyHint" next year.
	_ = hint.ReadOnlyHint
	return rerun, effect
}

// withHint is the only place a Tool's classes are ever merged with a
// server's report — registry.go calls THIS (in classify.go), so the merge
// rule has exactly one home (TestClassifyFileIsTheOnlyRaiseSite enforces it).
func (t Tool) withHint(hint *ToolAnnotations) Tool {
	t.Rerun, t.Effect = raiseClasses(t.Rerun, t.Effect, hint)
	return t
}

// UnavailableReason renders the T3 refusal: the tool exists for this agent,
// and it is refused because WE have not classified it. Distinct from
// `unknown tool` (T5) and from `refused: … · ToolAccess "…"` (S10) — three
// facts, three sentences, and the sentence names what is missing because
// "unavailable" alone is not actionable.
func UnavailableReason(qualified string, missing []string) string {
	parts := make([]string, 0, 2)
	for _, m := range missing {
		switch m {
		case "rerun":
			parts = append(parts, "no re-run class")
		case "effect":
			parts = append(parts, "no effect class")
		}
	}
	return "unavailable: " + qualified + " is unclassified and cannot be called — " +
		strings.Join(parts, ", ") + " (MC6)"
}
