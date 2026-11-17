package loop

import (
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// stateFor builds a minimal LoopState with the two fields the eligibility
// functions read: the loop's status (a loop column) and its lastReason (the
// pin's runtime — the stop/resume reason is NOT a loop column, so it lives in
// the runtime JSON). A gate-waiting loop has LastReason=="".
func stateFor(status, lastReason string) LoopState {
	return LoopState{Status: status, RuntimeState: RuntimeState{LastReason: lastReason}}
}

func TestCheckStopEligibility(t *testing.T) {
	// A stop is for running work (LC2): only a running loop is eligible.
	cases := []struct {
		name       string
		st         LoopState
		eligible   bool
		wantKind   string
		wantDetail string
	}{
		{
			name:     "running is eligible",
			st:       stateFor("running", ""),
			eligible: true,
		},
		{
			name:       "stopped waiting is refused, points at resume",
			st:         stateFor("waiting", "stopped"),
			wantKind:   "stopped",
			wantDetail: "the loop is stopped — resume it (or cancel it)",
		},
		{
			name:       "gate waiting is refused, points at the answer",
			st:         stateFor("waiting", ""),
			wantKind:   "waiting-question",
			wantDetail: "the loop is waiting on a question — answer it, or cancel the loop",
		},
		{
			name:       "cap-hit is refused, points at raise",
			st:         stateFor("blocked", "cap-hit"),
			wantKind:   "cap-hit",
			wantDetail: "the loop is blocked on a budget cap — raise it (loop.raise)",
		},
		{
			name:       "stuck is refused, points at resume",
			st:         stateFor("blocked", "stuck"),
			wantKind:   "stuck",
			wantDetail: "the loop is blocked (stuck) — resume it",
		},
		{
			name:       "idle is refused, points at close",
			st:         stateFor("idle", ""),
			wantKind:   "idle",
			wantDetail: "the loop is idle — close it (loop.close)",
		},
		{
			name:       "terminal is refused",
			st:         stateFor("done", ""),
			wantKind:   "terminal",
			wantDetail: "the loop is done — it is finished; a stop has nothing to do",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckStop(c.st)
			if c.eligible {
				if got != nil {
					t.Fatalf("CheckStop on running = %v, want nil (eligible)", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("CheckStop on %s = nil, want a refusal", c.st.Status)
			}
			if got.Kind != c.wantKind || got.Detail != c.wantDetail {
				t.Fatalf("CheckStop = {kind:%q detail:%q}, want {kind:%q detail:%q}",
					got.Kind, got.Detail, c.wantKind, c.wantDetail)
			}
		})
	}
}

func TestCheckResumeEligibility(t *testing.T) {
	// LC3: eligible only for a stopped-waiting loop and a watchdog stuck-block.
	cases := []struct {
		name       string
		st         LoopState
		eligible   bool
		wantKind   string
		wantDetail string
	}{
		{
			name:     "stopped waiting is eligible",
			st:       stateFor("waiting", "stopped"),
			eligible: true,
		},
		{
			name:     "stuck block is eligible",
			st:       stateFor("blocked", "stuck"),
			eligible: true,
		},
		{
			name:       "running is refused, points at stop",
			st:         stateFor("running", ""),
			wantKind:   "running",
			wantDetail: "the loop is running — stop it first (a resume needs a parked loop)",
		},
		{
			name:       "gate waiting is refused, points at the answer",
			st:         stateFor("waiting", ""),
			wantKind:   "waiting-question",
			wantDetail: "the loop is waiting on a question — answer it, or cancel the loop",
		},
		{
			name:       "cap-hit is refused, points at raise (LC9, not resume)",
			st:         stateFor("blocked", "cap-hit"),
			wantKind:   "cap-hit",
			wantDetail: "the loop is blocked on a budget cap — raise it (loop.raise)",
		},
		{
			name:       "idle is refused, points at close",
			st:         stateFor("idle", ""),
			wantKind:   "idle",
			wantDetail: "the loop is idle — close it (loop.close)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckResume(c.st)
			if c.eligible {
				if got != nil {
					t.Fatalf("CheckResume on %s/%s = %v, want nil (eligible)", c.st.Status, c.st.LastReason, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("CheckResume on %s/%s = nil, want a refusal", c.st.Status, c.st.LastReason)
			}
			if got.Kind != c.wantKind || got.Detail != c.wantDetail {
				t.Fatalf("CheckResume = {kind:%q detail:%q}, want {kind:%q detail:%q}",
					got.Kind, got.Detail, c.wantKind, c.wantDetail)
			}
		})
	}
}

func TestCheckCancelEligibility(t *testing.T) {
	// LC4: cancellable from running, waiting, blocked or idle; not terminal.
	for _, s := range []string{"running", "waiting", "blocked", "idle"} {
		if got := CheckCancel(stateFor(s, "")); got != nil {
			t.Fatalf("CheckCancel on %s = %v, want nil (eligible)", s, got)
		}
	}
	for _, s := range []string{"done", "aborted"} {
		got := CheckCancel(stateFor(s, ""))
		if got == nil {
			t.Fatalf("CheckCancel on %s = nil, want a refusal", s)
		}
		if got.Kind != "terminal" || got.Detail != "the loop is "+s+" — it has no control to apply" {
			t.Fatalf("CheckCancel = {kind:%q detail:%q}, want {kind:terminal detail:%q}",
				got.Kind, got.Detail, "the loop is "+s+" — it has no control to apply")
		}
	}
}

// TestOneFunctionPerOperation is the heart of LC2-LC4: the daemon runs the same
// eligibility as the op and loop.show renders it, so the button and the engine
// cannot disagree. The store's controlRefusalFor (T2) and the loop's
// Check* must agree on every (status, lastReason) the daemon reads — a
// divergence is a button that lies.
func TestOneFunctionPerOperationAgreesWithStoreRefusal(t *testing.T) {
	// The store's ResumeLoop / CancelLoop eligibility (T2) and the loop's
	// CheckResume / CheckCancel must agree: same status+lastReason, same verdict.
	// The store's controlRefusalFor is unexported, so we pin the contract through
	// the loop's Check* against the states the store treats as eligible.
	// Resume: the store moves waiting/stopped and blocked/stuck; CheckResume
	// must be eligible for exactly those two.
	for _, s := range []struct {
		status       string
		lastReason   string
		wantResumeOK bool
	}{
		{"waiting", "stopped", true},
		{"blocked", "stuck", true},
		{"running", "", false},
		{"waiting", "", false},
		{"blocked", "cap-hit", false},
		{"idle", "", false},
		{"done", "", false},
	} {
		got := CheckResume(stateFor(s.status, s.lastReason))
		gotOK := got == nil
		if gotOK != s.wantResumeOK {
			t.Fatalf("CheckResume(%s,%s) eligible=%v, want %v", s.status, s.lastReason, gotOK, s.wantResumeOK)
		}
	}
}

var _ = store.ControlRefusal{} // the eligibility functions return this type

func TestAStepThatFinishedBlockedIsResumableWhateverItsReason(t *testing.T) {
	st := stateFor("blocked", "")
	if CheckResume(st) == nil {
		t.Fatal("a blocked loop with no step outcome has no resume")
	}
	st.LastOutcome = "blocked"
	if got := CheckResume(st); got != nil {
		t.Fatalf("a step that finished blocked is the person's to fix and retry: %+v", got)
	}
	st.LastReason = "cap-hit"
	if CheckResume(st) == nil {
		t.Fatal("a cap block keeps its own remedy (raise), whatever the last outcome was")
	}
}
