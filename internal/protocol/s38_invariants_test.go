// s38_invariants_test.go freezes what S39 promised NOT to change while it
// opened its four human doors (loop.message, loop.run, loop.run.options,
// loop.close) on the socket. The name keeps the task's spelling: the four
// methods are the records-tier doors added through the S38 lineage (CR10 —
// they are for persons, and the narrow surface is the agent/CI mount, so
// what must stay true is that none of them ever appears there.
//
// This file is an external test package (protocol_test) on purpose:
// internal/narrow imports internal/protocol, and an internal test importing
// narrow back would be the cycle the narrow package's own comment
// (actions.go:56-61) says the type system exists to keep honest.
package protocol_test

import (
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
)

// TestProtocolVersionIsUnchangedByS38 pins the handshake version at the
// value S39 inherited and left alone. The task text said "1, the value
// recorded at S39 start"; the value recorded MEASURED at S39 start was 2 —
// the records-tier rename (S38 Phase A, commit 0da33b16, 2026-10-27, the
// same day S39's T0 ran) was a wire-breaking rename of the records tier and
// took the version with it, before S39 opened its first door. S39 adds
// methods to a live version, which is legal per version.go's rule, so the
// invariant that must hold — and this test holds — is that S39 never moved
// the number off the value it found. If this fails, either someone bumped
// PROTOCOL_VERSION inside S39, or a new wire break arrived and this line
// must record its date the way this one records S38's.
func TestProtocolVersionIsUnchangedByS38(t *testing.T) {
	if protocol.PROTOCOL_VERSION != 2 {
		t.Errorf("PROTOCOL_VERSION = %d, want 2 (the value S39 started on, per version.go as of 0da33b16); S39 must not bump it",
			protocol.PROTOCOL_VERSION)
	}
}

// TestS38MethodsAreNotOnTheNarrowSurface is CR10 as a test: the chat doors
// are socket-only. The narrow table is what an agent or a CI mount can
// reach; a loop.message, loop.run, loop.run.options or loop.close row there
// would let an agent answer its own ask, start its own run, or close the
// loop under a resting person — the exact drift S25 built the surface to
// prevent. Not one of the four may appear as any action's SocketMethod.
func TestS38MethodsAreNotOnTheNarrowSurface(t *testing.T) {
	forbidden := map[protocol.MethodName]string{
		"loop.message":     "the chat door answers to the person at the socket, never to an agent mount",
		"loop.run":         "the run door is a person's peer, never an agent's self-start",
		"loop.run.options": "the run offers are what the human sees, not what an agent reads",
		"loop.close":       "the close is a human door that refuses non-idle (CR2); an agent mount must never hold it",
	}
	for _, a := range narrow.Actions() {
		if why, bad := forbidden[a.SocketMethod]; bad {
			t.Errorf("narrow action %q projects %s — forbidden on the narrow surface: %s", a.Name, a.SocketMethod, why)
		}
	}
	// The table is live, not empty-by-accident: if the four stay out, the
	// rest must still be reachable, or this test would pass by the whole
	// surface disappearing.
	if len(narrow.Actions()) == 0 {
		t.Error("narrow.Actions() is empty — the loop's own actions must stay on the surface (this test would pass vacuously)")
	}
}
