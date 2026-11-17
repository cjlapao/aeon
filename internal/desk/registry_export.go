package desk

// ════════════════════════════════════════════════════════════════════════════
// The registry export (S45 T5): how the socket door gets an effect table
// without importing `cmd/aeon` (which the daemon cannot do) and without
// building a second seam map (which PM4 forbids).
//
// `cmd/aeon/proposal.go` holds the package-level `deskEffects` — the one
// Registry the CLI's commit door has always used, MCP seams included. The
// socket door needs THE SAME table. This file is the composition point the
// single-binary rule (B4) makes honest: `cmd/aeon serve` — the composition
// root, which already links both halves — injects the seam map once via
// `RegisterSeams`, and hands the daemon `EffectsFor(opener)`, which builds a
// Registry from the injected map. The CLI's `deskEffects` and the daemon's
// registry are then constructed from one value: the seams defined in
// proposal.go, the one file the PL15 sibling guard admits as a caller of
// the MCP config writer. No daemon-side seams map exists to drift.
//
// Boundary opening still arrives as a VALUE from cmd/aeon (its
// `openPolicyBoundary`, over the `policyBoundary` variable the fixtures
// point at a temp store or spy repo) — the desk never opens the store
// bridge on its own authority, and the guard's sanctioned opener file
// stays the only one that names it.
// ════════════════════════════════════════════════════════════════════════════

import (
	"errors"
	"sync"
)

var (
	registeredSeams   Seams
	registeredSeamsMu sync.RWMutex
)

// RegisterSeams injects the seam map `EffectsFor` will build daemon-side
// registries from. cmd/aeon calls it exactly once — in the same file that
// defines `deskEffects`, beside its definition — so the two doors' effect
// tables share one seam definition. Calling it twice replaces the map
// (last-writer semantics, no merge: a second registration is a wiring bug,
// and the replacement is the observable symptom, not a silent union).
func RegisterSeams(s Seams) {
	registeredSeamsMu.Lock()
	defer registeredSeamsMu.Unlock()
	registeredSeams = s
}

// RegisteredSeams returns a copy of the injected map (nil before
// registration — which `EffectsFor` answers with refusals that NAME the
// missing half, not a silent skip).
func RegisteredSeams() Seams {
	registeredSeamsMu.RLock()
	defer registeredSeamsMu.RUnlock()
	if registeredSeams == nil {
		return nil
	}
	out := Seams{}
	for k, e := range registeredSeams {
		out[k] = e
	}
	return out
}

// EffectsFor builds a Registry over the caller's boundary opener with the
// injected seam map — the exact table `deskEffects` is. A nil opener or an
// unregistered seam map is not a construction error (a daemon that started
// without the policy bridge stays answerable, like New's unwired-seam
// rule); the refusal happens at the call, naming the kind and the missing
// half. A nil opener, though, is a wiring bug worth naming at the door, so
// it refuses at construction.
func EffectsFor(opener BoundaryOpener) (*Registry, error) {
	if opener == nil {
		return nil, errors.New("refused: EffectsFor needs the boundary opener cmd/aeon owns — the daemon has no store bridge of its own")
	}
	return New(Deps{Boundary: opener}, RegisteredSeams()), nil
}
