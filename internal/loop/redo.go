package loop

import (
	"fmt"
	"strings"
)

// The redo — L32's answer to the send-back problem: "a bare retry re-runs
// the same call and spends a round reproducing the same output". A reject
// that routes to a rework step must arrive at that step KNOWING why it was
// sent back, and with its attempt number — the handler sizes the last try
// differently from the first (L34: the handler does not count its own
// attempts; the engine hands them over).
//
// The reason is not prose the engine invented: it is the rejected step's
// recorded reason (StepStates[..].Reason), the same value the view renders
// and step.show reads. The redo carries it verbatim.

// Redo is the pending context for one rework dispatch. Step names the step
// that rejected; Reason is its recorded reason; Attempt/Max ride from the
// on_reject edge's counter (Max 0 is the counter-less form — a rework
// without an on_reject counter says "sent back", not a numbered try it
// cannot honour).
type Redo struct {
	Step    string `json:"step"`
	Reason  string `json:"reason"`
	Attempt int    `json:"attempt"`
	Max     int    `json:"max"`
}

// NewRedo refuses the forbidden shape: a redo with no reason is exactly the
// bare retry L32 forbids, so it is refused AT CONSTRUCTION naming the step,
// not discovered downstream when a handler re-produces the same wrong output.
func NewRedo(step, reason string, attempt, max int) (Redo, error) {
	if strings.TrimSpace(step) == "" {
		return Redo{}, fmt.Errorf("loop: a redo must name the step that sent it back")
	}
	if strings.TrimSpace(reason) == "" {
		return Redo{}, fmt.Errorf("loop: the redo of %q has no reason — a bare retry re-runs the call that just failed and spends the round reproducing the same output (L32); refused", step)
	}
	if max < 0 {
		return Redo{}, fmt.Errorf("loop: redo of %q has a negative max (%d)", step, max)
	}
	if max > 0 && (attempt < 1 || attempt > max) {
		return Redo{}, fmt.Errorf("loop: redo of %q asks for attempt %d of max %d — outside the budget", step, attempt, max)
	}
	if max == 0 && attempt != 0 {
		return Redo{}, fmt.Errorf("loop: redo of %q carries an attempt number (%d) with no counter (max 0) — the engine cannot honour a number it is not counting", step, attempt)
	}
	return Redo{Step: step, Reason: reason, Attempt: attempt, Max: max}, nil
}

// InputSentence is the one renderer of a redo, appended verbatim to the
// re-entered step's input. It says what sent the dispatch back, where in
// the budget this try is, and why — cold, the handler needs nothing else.
func (r Redo) InputSentence() string {
	if r.Max > 0 {
		return fmt.Sprintf("redo of %q: retry %d of %d — %s", r.Step, r.Attempt, r.Max, r.Reason)
	}
	return fmt.Sprintf("redo of %q: sent back — %s", r.Step, r.Reason)
}

// takeRedo consumes the pending redo for a position — the reason travels
// with ITS redo, it does not live on the step. A second send-back produces
// a second redo carrying the NEW reason; a stale sentence riding a later
// dispatch would have the handler fix a defect that is already fixed.
func takeRedo(st *LoopState, pos Position) (Redo, bool) {
	if len(st.Redos) == 0 {
		return Redo{}, false
	}
	key := stepKey(pos)
	r, ok := st.Redos[key]
	if !ok {
		return Redo{}, false
	}
	delete(st.Redos, key)
	if len(st.Redos) == 0 {
		st.Redos = nil
	}
	return r, true
}

// pendingRedo reads without consuming — for the decide/tests' view of what
// a dispatch will carry.
func pendingRedo(st LoopState, pos Position) (Redo, bool) {
	r, ok := st.Redos[stepKey(pos)]
	return r, ok
}
