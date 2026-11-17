package main

// autonomy_pins_test.go — S56 T1. The pre-amendment guard pins, per the
// task's file list. T1's seven named guards are the assertion logic this
// slice must leave alone; their bodies are pinned by the SHA-256 of their
// parser-extracted source (the `func` keyword through the matching `}`,
// extracted with go/parser — the S45 T6 / S56 T7 method).
//
// Six of the seven are already homed in T7's originalGuardPins table
// (autonomy_guard_test.go), which re-asserts their digests in
// TestOriginalGuardBodiesAreByteIdentical. This test asserts their
// PRESENCE there by name and file — it does not duplicate the digest
// constants, because one fact must have one home. The SEVENTH, S40's
// event-start pin (T0 item 5), is not in T7's table — T7 pinned the
// trigger-fire sibling (TestClockStartedLoopIsUnattendedAndCarriesProvenance)
// instead, a deviation recorded in T1's Notes — so it carries its own
// digest constant here, computed with T7's own helpers.
//
// The failure message says the contract out loud: the pre-amendment logic
// IS the contract, and a change here is a deliberate, listed amendment —
// never a re-pinned constant and never a quietly edited guard.

import (
	"os"
	"path/filepath"
	"testing"
)

// The six T1-named guards that live in T7's home table. file is relative
// to aeon/, exactly as T7's table spells it.
var t1HomedGuards = []struct {
	guard string
	file  string
}{
	{"TestNoKeyLevelGrantExists", "internal/narrow/trigger_test.go"},
	{"TestTriggeredLoopIsUnattended", "internal/loop/allowoutward_test.go"},
	{"TestIrreversibleDeniesToo", "internal/loop/allowoutward_test.go"},
	{"TestAllowOutwardIsPerLoop", "internal/loop/allowoutward_test.go"},
	{"TestAttendednessCannotBeInferredFromPolling", "internal/loop/allowoutward_test.go"},
	{"TestClassesAreNotConfigurable", "internal/action/class_test.go"},
}

// The seventh guard — S40's event-start pin: the event path authors
// attended exactly once and carries no outward lever at all (AU2). Pinned
// at internal/daemon/start_loop_test.go:437, measured 2026-11-01 on this
// tree by T7's guardBodyDigest (parser-extracted body, sha256, byte-for-byte).
const eventStartGuardDigest = "b52013fde2645606dd2011bd12ec175e3e28d80158a165f49a686a25f1ca922e"

func TestGuardBodiesAreByteIdenticalPreSlice(t *testing.T) {
	// (a) The six homed guards are still in T7's table, by name and file.
	// The digest check itself runs there; a missing row means the guard's
	// home pin was dropped, which is an amendment nobody listed.
	for _, g := range t1HomedGuards {
		homed := false
		for _, pin := range originalGuardPins {
			if pin.guard == g.guard && pin.file == g.file {
				homed = true
				break
			}
		}
		if !homed {
			t.Errorf("%s (%s) is missing from T7's originalGuardPins table — the pre-amendment guard's home pin is gone. The pre-slice logic is the contract: a change here is a deliberate, listed amendment, not a dropped pin", g.guard, g.file)
		}
	}

	// (b) The seventh, pinned here with its own constant.
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "daemon", "start_loop_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	got := guardBodyDigest(t, string(src), "TestTheEventStarterHasExactlyOneAttendedAndNoOutwardAssignment")
	if got != eventStartGuardDigest {
		t.Errorf("TestTheEventStarterHasExactlyOneAttendedAndNoOutwardAssignment (internal/daemon/start_loop_test.go): the pre-slice guard body changed — pinned %s, computed %s. The pre-amendment logic is the contract: an amendment here is a deliberate, listed change in T1's Notes, never a re-pinned constant.", eventStartGuardDigest, got)
	}
}
