package narrow

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// control_surface_test.go — S20 T8's boundary (R6, TR12): the two off
// switches, and exactly one of them is reachable from here.
//
// TriggerAccess is REACHABLE from the narrow surface in the only way
// anyone reaches policy: as a question the engine answers (S10's
// policy.evaluate path), never as a button to press. Pause and resume are
// NOT here at all — operational control on the surface that agents use
// hands a caller the means to bypass the guards written to constrain it.
// This test greps the WHOLE table for the clock's verbs, and the
// forbidden-token scan (now carrying "fire", with camelCase split so
// trigger.fireNow tokenizes honestly) is the machine; this is the proof
// the machine has teeth.

func TestPauseNotReachableFromNarrowSurface(t *testing.T) {
	verbs := []string{"pause", "resume", "fire", "firenow", "fire-now", "unpause"}
	// (b) the trigger's socket methods themselves — a narrow action may
	// not project ANY of them, however it names itself:
	// trigger.list|pause|resume|fireNow|history are CLI-and-app, ever.
	controlMethods := []protocol.MethodName{
		protocol.MethodTriggerList, protocol.MethodTriggerPause,
		protocol.MethodTriggerResume, protocol.MethodTriggerFireNow,
		protocol.MethodTriggerHistory,
	}
	for _, a := range Actions() {
		for _, tok := range verbs {
			if strings.Contains(strings.ToLower(a.Name+"."+string(a.SocketMethod)), tok) {
				t.Fatalf("action %q projects verb %q — operational control (TR12) is CLI and app only; "+
					"the narrow surface must not carry the clock's verbs (R6)", a.Name, tok)
			}
		}
		for _, m := range controlMethods {
			if a.SocketMethod == m {
				t.Fatalf("action %q projects the control-plane method %q — it exists for CLI and app, "+
					"and existence on the socket table is not eligibility here", a.Name, m)
			}
		}
	}
	// The forbidden list itself carries the words — the scan is the
	// machine; this pins that the machine has teeth today, not someday.
	have := map[string]bool{}
	for _, w := range ForbiddenOperationalControl {
		have[w] = true
	}
	for _, w := range []string{"pause", "resume", "fire"} {
		if !have[w] {
			t.Fatalf("ForbiddenOperationalControl lost %q — TR12's verbs must be forbidden by word, present and pinned", w)
		}
	}
	// And the tokeniser reads camelCase honestly — "trigger.fireNow" must
	// yield the token "fire", or the guard is decorative (measured: the
	// pre-split scanner saw one token "firenow" and waved it through).
	var sawFire bool
	for _, tok := range tokenize(string(protocol.MethodTriggerFireNow)) {
		if tok == "fire" {
			sawFire = true
		}
	}
	if !sawFire {
		t.Fatal(`tokenize("trigger.fireNow") never yields "fire" — the forbidden word would never match`)
	}
}

func TestPauseAndTriggerAccessAreReachableApart(t *testing.T) {
	// The distinct-reachability half of TR12's table, pinned as surface
	// topology: governance lives in policy (S10's methods), operations in
	// the CLI/app seams — and NEITHER is an action here. If someone ever
	// "helpfully" projects a policy ADD through narrow, the authorship
	// boundary (PL1: allows from operators only) dies at the surface.
	for _, a := range Actions() {
		for _, tok := range []string{"policy", "pause", "resume"} {
			if strings.Contains(strings.ToLower(a.Name+"."+string(a.SocketMethod)), tok) {
				t.Fatalf("action %q touches %q — governance is authored through policy's own boundary, "+
					"operations through CLI and app; neither is a narrow verb", a.Name, tok)
			}
		}
	}
}
