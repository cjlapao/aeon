package protocol

// S24 T7 — the queue's protocol-side gates. The behavior lives under
// internal/queue (one implementation, CG9) and is exercised there; these
// tests pin what this layer may never do: register a verb without the
// initialize gate, mutate an existing method, or answer from a half-wired
// seam.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func mustQueueParams(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	return b
}

// TestQueueMethodsAreAdditiveToProtocolVersion is CG4-of-S2's standing
// rule from the inside: an addition joins the table; it never rewrites
// an entry that was already there. The forward-compat conformance
// fixture (TestTheForwardCompatibilityFixtureExistsAndIsAccepted) is the
// standing outside proof; this is the inside one — the queue's seven
// names are present, and a sampled spine of pre-existing verbs keeps the
// exact requires-initialize it had, so no older client's expectations
// moved under the addition.
func TestQueueMethodsAreAdditiveToProtocolVersion(t *testing.T) {
	tbl := newTestTable(t)
	have := map[MethodName]bool{}
	for _, n := range tbl.Methods() {
		have[n] = true
	}
	for _, n := range []MethodName{
		MethodQueueAppend, MethodQueueList, MethodQueueReorder, MethodQueueRemove,
		MethodQueueSetSize, MethodQueueSettings, MethodQueueAdvance,
	} {
		if !have[n] {
			t.Errorf("%s is not registered — CG9's socket surface is missing a verb", n)
		}
	}
	// The sampled spine keeps its answers (a rewrite would fail HERE, not
	// in a client's field report): initialize answers pre-init, everything
	// else that predates S24 keeps its gate.
	before := map[MethodName]bool{
		MethodInitialize: false,
		MethodPing:       true,
		MethodLoopStart:  true,
		MethodPolicyList: true,
		MethodAsk:        true,
	}
	for n, wantGate := range before {
		m, ok := tbl.methods[n]
		if !ok {
			t.Fatalf("%s vanished from the table — additive means additive", n)
		}
		if m.RequiresInitialize != wantGate {
			t.Errorf("%s RequiresInitialize = %v, want %v (additive must not rewrite)", n, m.RequiresInitialize, wantGate)
		}
	}
}

// TestEveryQueueMethodRequiresInitialize is the spec's third gate: no
// queue method answers before the daemon finishes booting, the same way
// policy.* refuses. Missing a gate here would let a queue write land
// against an un-booted store — the one failure mode the initialize gate
// exists to prevent.
func TestEveryQueueMethodRequiresInitialize(t *testing.T) {
	tbl := newTestTable(t)
	for _, n := range []MethodName{
		MethodQueueAppend, MethodQueueList, MethodQueueReorder, MethodQueueRemove,
		MethodQueueSetSize, MethodQueueSettings, MethodQueueAdvance,
	} {
		m, ok := tbl.methods[n]
		if !ok {
			t.Fatalf("%s is not registered", n)
		}
		if !m.RequiresInitialize {
			t.Errorf("%s answers before Initialize — every queue method rides the gate policy.* rides", n)
		}
	}
}

// TestQueueHandlersRefuseLoudlyWhenUnwired pins the degradation path: a
// Table built without the daemon's seams (a test harness, a partial
// embed) answers ErrStoreUnavailable — a named error, never a zero-value
// row that reads like an empty queue.
func TestQueueHandlersRefuseLoudlyWhenUnwired(t *testing.T) {
	tbl := newTestTable(t) // Queue nil by construction
	_, err := tbl.handleQueueList(context.Background(), nil, mustQueueParams(t, QueueListParams{ComponentRoot: "/srv/projects/ledger"}))
	if err == nil {
		t.Fatal("an unwired queue answered — it must refuse")
	}
	e, ok := err.(*Error)
	if !ok || e.Code != ErrStoreUnavailable.Code {
		t.Fatalf("unwired queue.list err = %v, want ErrStoreUnavailable", err)
	}
	if _, err := tbl.handleQueueAdvance(context.Background(), nil, mustQueueParams(t, QueueAdvanceParams{ComponentRoot: "/srv/projects/ledger"})); err == nil {
		t.Fatal("an unwired advance answered — it must refuse")
	}
}

// TestQueueHandlersRefuseShapelessRequests pins that parameter law is
// decided BEFORE the seams are touched — a malformed request must not
// depend on whether a store exists (its refusal is about its shape).
func TestQueueHandlersRefuseShapelessRequests(t *testing.T) {
	tbl := newTestTable(t)
	cases := []struct {
		name string
		call func() error
	}{
		{"append without a blueprint", func() error {
			_, e := tbl.handleQueueAppend(context.Background(), nil, mustQueueParams(t, QueueAppendParams{ComponentRoot: "/r", Goal: "g"}))
			return e
		}},
		{"list without a root", func() error {
			_, e := tbl.handleQueueList(context.Background(), nil, mustQueueParams(t, QueueListParams{}))
			return e
		}},
		{"reorder without an id", func() error {
			_, e := tbl.handleQueueReorder(context.Background(), nil, mustQueueParams(t, QueueReorderParams{Position: 2}))
			return e
		}},
		{"remove without an id", func() error {
			_, e := tbl.handleQueueRemove(context.Background(), nil, mustQueueParams(t, QueueRemoveParams{}))
			return e
		}},
		{"set-size without an id", func() error {
			_, e := tbl.handleQueueSetSize(context.Background(), nil, mustQueueParams(t, QueueSetSizeParams{SizeMin: 1, SizeMax: 2, SizeBasis: "b"}))
			return e
		}},
		{"settings write naming only one knob", func() error {
			a := "auto"
			_, e := tbl.handleQueueSettings(context.Background(), nil, mustQueueParams(t, QueueSettingsParams{ComponentRoot: "/r", QueueAdvance: &a}))
			return e
		}},
		{"advance without a root", func() error {
			_, e := tbl.handleQueueAdvance(context.Background(), nil, mustQueueParams(t, QueueAdvanceParams{}))
			return e
		}},
	}
	for _, c := range cases {
		err := c.call()
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("%s: err = %v, want ErrBadParams — shape must be refused before any seam", c.name, err)
		}
	}
}

// TestQueueRefusalIsRegisteredAndSaidInThrees pins that the queue speaks
// with a real catalogued code: three-part Data (what/means/todo) all
// non-empty, the code in the registry, so no client ever implements
// against a number that does not exist.
func TestQueueRefusalIsRegisteredAndSaidInThrees(t *testing.T) {
	found := false
	for _, e := range ErrorRegistry() {
		if e.Code == ErrQueueRefused.Code {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ErrQueueRefused (%d) is not in ErrorRegistry", ErrQueueRefused.Code)
	}
	r := queueRefusal("a running item cannot be removed (D11)")
	if r.Data.What == "" || r.Data.Means == "" || r.Data.Todo == "" {
		t.Fatalf("refusal %v is not said in threes", r)
	}
}
