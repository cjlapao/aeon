package main

import (
	"os"
	"strings"
	"testing"
)

// trigger_cli_test.go — T8's call-site rules, checked against SOURCE
// rather than vibes: the CLI must contain no control logic (P34: it dials
// the socket and prints; one implementation lives in
// internal/trigger/control.go, and the daemon's seam calls EXACTLY those
// functions), and S27's sketch must stand narrowed to what S27 actually
// owns now that S20 built the clock.

// readSource strips COMMENTS before any scan: these rules guard CODE
// (imports, calls, parameters), and the files' own honest prose about
// attendedness or control.go must not trip the scans it describes —
// otherwise the tests would only pass on files that stopped explaining
// themselves, which is the wrong incentive.
func readSource(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func TestTriggerSocketAndCLICallSameControlFunctions(t *testing.T) {
	verbs := readSource(t, "trigger.go")
	// S49 T8: the trigger verbs now speak through the plane's shared door,
	// so the dial of the six methods lives in the door half (its socketDoor)
	// while trigger.go keeps only verbs and printers. The scan reads both
	// halves — the call-site rule does not care which CLI file holds the
	// dial, only that trigger.go itself still holds no control logic
	// (clause 2 stays on trigger.go alone; the door half is guarded by
	// TestAutomationCLINeverImportsStoreOutsideDirectDoor instead).
	doorHalf := readSource(t, "automation_client.go")
	cli := verbs + "\n" + doorHalf
	seam := readSource(t, "../../internal/daemon/trigger_control.go")
	// S49 T4 moved the four WRITE closures (and left List/History reads
	// behind); the scan reads both halves — the one implementation rule
	// does not care which daemon file holds the calls, only that
	// control.go is their single home.
	seam += "\n" + readSource(t, "../../internal/daemon/automation_write.go")
	control := readSource(t, "../../internal/trigger/control.go")

	// (1) The CLI dials exactly the five methods…
	for _, m := range []string{
		"protocol.MethodTriggerList", "protocol.MethodTriggerPause",
		"protocol.MethodTriggerResume", "protocol.MethodTriggerFireNow",
		"protocol.MethodTriggerHistory", "protocol.MethodTriggerAdd",
	} {
		if !strings.Contains(cli, m) {
			t.Errorf("the CLI never dials %s", m)
		}
	}
	// (2) …and holds NO control logic: no store handle, no scheduler, no
	// write of a row — a second implementation is what this test exists to
	// make impossible (P34 daemon-first).
	for _, banned := range []string{"github.com/cjlapao/aeon/internal/store", "github.com/cjlapao/aeon/internal/trigger", "SaveTriggerSchedule", "SetCovered", "Scheduler"} {
		if strings.Contains(verbs, banned) {
			t.Errorf("the CLI touches %q — control logic lives in control.go, reached via the socket, once", banned)
		}
	}
	// (3) The daemon seam calls the SAME control.go functions — no
	// reimplementation beside the socket.
	for _, fn := range []string{"d.triggers.List(", "d.triggers.Pause(", "d.triggers.Resume(", "d.triggers.FireNow(", "d.triggers.Add("} {
		if !strings.Contains(seam, fn) {
			t.Errorf("the socket seam never calls %s — a second control-plane implementation is being born", fn)
		}
	}
	// (4) control.go is where those functions actually are (the seam's
	// calls resolve HERE, one home).
	for _, def := range []string{"func (sc *Scheduler) List(", "func (sc *Scheduler) Pause(", "func (sc *Scheduler) Resume(", "func (sc *Scheduler) FireNow(", "func (sc *Scheduler) Add("} {
		if !strings.Contains(control, def) {
			t.Errorf("control.go no longer defines %s — the seam is calling into a rename", def)
		}
	}
	// (5) fire-now's honesty rule travels BOTH doors: the seam's fire-now
	// cannot name a party — no attended-shaped parameter may appear on the
	// control seam (the Fire struct's own pin guards the value; this
	// guards the seam from growing a way to ask).
	for _, banned := range []string{"Attended", "attended:"} {
		if strings.Contains(seam, banned) {
			t.Errorf("the control seam carries %q — no attended-shaped parameter may exist on this seam; the Fire struct's own pin guards the value, this guards the seam from growing a way to ask (TR2)", banned)
		}
	}
}

func TestS27SketchIsSelectionOnly(t *testing.T) {
	b, err := os.ReadFile("../../docs/aeon/S27-continuous/SKETCH.md")
	if err != nil {
		t.Fatalf("the sketch vanished: %v", err)
	}
	doc := string(b) // raw — this scan guards the PROSE itself
	if !strings.Contains(doc, "NARROWED 2026-10-27 (S20 T8)") {
		t.Fatal("the sketch still claims the clock — S20 T8's narrowing never landed")
	}
	// The clock is recorded as S20's, with the pointer a reader follows.
	for _, must := range []string{"internal/trigger/", "S20", "work selection", "../S20-triggers/"} {
		if !strings.Contains(doc, must) {
			t.Errorf("the narrowed sketch lost %q", must)
		}
	}
	// What is genuinely S27's survives: selection, readiness, caps, and
	// the open questions it was written for (they were written for a
	// reason — preserving them is scope 4's rule).
	for _, must := range []string{"What \"ready\" means", "How many loops run at once", "blocks", "caps"} {
		if !strings.Contains(doc, must) {
			t.Errorf("an open question or owned concern vanished from the sketch: %q", must)
		}
	}
	// And the conflation is GONE: the sketch may not still speak of the
	// scheduler as the first unattended caller — S20 built that, and the
	// old claim would have S27 re-derive a built slice.
	for _, gone := range []string{
		"A scheduler is a **new kind of caller**",
		"Answer, already recorded in S11**: an auto-started loop is **unattended by construction.** Nobody is there to declare",
	} {
		if strings.Contains(doc, gone) {
			t.Errorf("the sketch still asserts the pre-S20 framing %q — the clock is built; S27 selects work", gone)
		}
	}
}
