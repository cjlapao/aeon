// Package doctor is the one screen the whole product's honesty converges on:
// it reports the three-state and multi-state values every other slice
// computed — cost, capability, retrieval availability, staleness, mountings,
// credential presence, forge, the bash policy — each in its own states.
//
// It renders; it decides nothing (I4): every value comes from the slice that
// owns it, and doctor reads and formats. That is why C15 is a rendering rule,
// not a logic one, and why flattening is so easy to do by accident — a
// formatter's instinct is to summarise. So the rule is literal: no "ok" /
// "all good" / "healthy" over an uncertain value, no green mark over a
// declared-but-unprobed capability, no $0.00 over an unmetered route, no
// "available" over a stale corpus, and a not-listening mounting is shown, not
// absent from the screen.
//
// No credential is printed (D6, C17): presence and source, never the value —
// T2's Source exists for exactly this line, and "no credential is printed
// here" is on the screen deliberately, because the absence of a key is not
// visible evidence that keys are never shown.
package doctor

import "strings"

// State is a three-state value (P31): the answer has three values, never two,
// and collapsing the third into one of the others is how a lower bound
// silently becomes a fact.
type State string

// Three-state values, in their own states — never collapsed to a boolean.
const (
	// Verified is checked and present; Declared is a claim not yet probed;
	// Contradicted is checked and absent. A declared-but-unprobed capability is
	// not verified, so it must not render as a green mark.
	Verified    State = "verified"
	Declared    State = "declared"
	Contradicted State = "contradicted"
	// Cost states: known / partial / unknown. $0.00 is for a known zero, not
	// an unmetered route (which is unknown).
	CostKnown   State = "known"
	CostPartial State = "partial"
	CostUnknown State = "unknown"
	// Retrieval availability (S22's four reasons), in their own states.
	Available   State = "available"
	Stale       State = "stale"
	NotBuilt    State = "not built"
	Unavailable State = "unavailable"
	// Mounting liveness: a mounting that is not listening is shown, not absent.
	Listening State = "listening"
	NotListening State = "not listening"
)

// Credential is a credential's presence and source — never its value (D6,
// C17). T2's Source names where it came from; the value is never carried
// here, so a renderer cannot print it.
type Credential struct {
	Name    string
	Present bool
	Source  string // T2's Source: environment, key-file — never the value
}

// Cost is a route's cost state, in its own states — known / partial / unknown.
type Cost struct {
	Amount  string
	State   State
}

// Capability is one capability a route is claimed to have, in its own states
// — a declared-but-unprobed capability is not a green mark.
type Capability struct {
	Name  string
	State State
}

// Route is a provider's cost and capabilities, each in its own states.
type Route struct {
	Provider   string
	Cost       Cost
	Capabilities []Capability
}

// Retrieval is the retrieval availability — one of the four reasons, or
// NotBuilt in the no-retrieval build (S22's D1: reads "not built", not
// missing, not an error).
type Retrieval struct {
	State State
}

// Mounting is a narrow-surface mounting's liveness — shown, never absent from
// the screen (a not-listening mounting is shown, not omitted).
type Mounting struct {
	Name  string
	State State
}

// Forge is the forge configuration: present or not, with its source.
type Forge struct {
	Present bool
	Source  string
}

// Report is the one screen: every state in its own states. doctor reads and
// formats this; it decides nothing.
type Report struct {
	Routes    []Route
	Retrieval Retrieval
	Mountings []Mounting
	Credentials []Credential
	Forge     Forge
	BashPolicyPresent bool
}

// Render is the renderer (C15, D10): it formats the report, flattening
// nothing — no "ok" / "all good" / "healthy" over an uncertain value, no green
// mark over a declared-but-unprobed capability, no $0.00 over an unmetered
// route, no "available" over a stale corpus, and a not-listening mounting is
// shown, not absent. It also says what is fine (O3), briefly. No credential
// value is printed (D6, C17) — only presence and source, and "no credential is
// printed here" on the screen.
func Render(r Report) []string {
	var out []string
	out = append(out, "doctor")
	// Routes: cost and capabilities, each in its own states.
	for _, route := range r.Routes {
		out = append(out, "  "+route.Provider)
		out = append(out, "    cost    "+costLine(route.Cost))
		for _, c := range route.Capabilities {
			out = append(out, "    "+c.Name+"   "+string(c.State))
		}
	}
	// Retrieval: one of the four reasons, or "not built" — never an error.
	out = append(out, "  retrieval   "+string(r.Retrieval.State))
	// Mountings: liveness, shown — a not-listening mounting is not omitted.
	for _, m := range r.Mountings {
		out = append(out, "  "+m.Name+"   "+string(m.State))
	}
	// Credentials: presence and source, never the value.
	out = append(out, "  credentials")
	for _, c := range r.Credentials {
		out = append(out, "    "+c.Name+"   "+credentialLine(c))
	}
	out = append(out, "    no credential is printed here")
	// Forge: present or not, with its source.
	if r.Forge.Present {
		out = append(out, "  forge       present   ("+r.Forge.Source+")")
	} else {
		out = append(out, "  forge       not configured")
	}
	// The bash policy: present or not.
	if r.BashPolicyPresent {
		out = append(out, "  bash policy present")
	} else {
		out = append(out, "  bash policy not configured")
	}
	return out
}

// costLine renders a cost in its own states: a known zero is $0.00, but an
// unmetered route is unknown, never $0.00 (C15: no $0.00 over an unmetered
// route).
func costLine(c Cost) string {
	switch c.State {
	case CostKnown:
		if c.Amount == "" {
			return "$0.00"
		}
		return c.Amount
	case CostPartial:
		return "partial"
	default: // unknown
		return "unknown"
	}
}

// credentialLine renders a credential's presence and source — never the value.
// "no credential is printed here" is on the screen deliberately (D6, C17).
func credentialLine(c Credential) string {
	if !c.Present {
		return "absent"
	}
	if c.Source == "" {
		return "present"
	}
	return "present   (" + c.Source + ")"
}

// NoFlatten is the C15/D10 assertion: the rendered text has none of the
// flattened words over an uncertain value.
func NoFlatten(r Report) []string {
	var out []string
	for _, line := range Render(r) {
		l := strings.ToLower(line)
		for _, bad := range []string{"ok", "all good", "healthy"} {
			if strings.Contains(l, bad) {
				out = append(out, line)
			}
		}
	}
	return out
}
