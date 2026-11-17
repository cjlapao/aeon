package daemon

import (
	"context"
	"sync"
)

// stepCancels tracks one cancel func per RUNNING step, so a separate RPC call
// can stop a specific step without touching any other.
//
// # Why this lives in the daemon and not in `internal/protocol`
//
// It needs real goroutine-shared state across RPC calls that arrive on
// different connections — `step.stop` on one connection has to reach the
// `context.CancelFunc` a `step`/`answer`/`step.continue` call registered on
// another. `internal/protocol` owns the method table, not daemon-lifetime
// state; this is the same reason `d.recovering` (the sibling map one line
// above `Table.Recovering`) lives here and not there.
//
// # Why a bool alongside the cancel func, not a status string sniffed later
//
// A cancelled context produces the SAME generic "the step was cancelled"
// result whether the cause was a user's `step.stop` or something else
// (connection drop, daemon shutdown). Reading that back out of the result's
// prose to decide whether to write `pause_reason` would be exactly the
// string-sniffing this codebase's own DB19 rule exists to forbid. So the
// TRUTH that a stop was deliberate is recorded here, at the one place that
// knows it — the `step.stop` call itself — and read back once, by the step's
// own run, which clears it in the same read so a later unrelated cancellation
// of the same id starts from a clean slate.
type stepCancels struct {
	mu   sync.Mutex
	byID map[int64]*trackedStep
}

type trackedStep struct {
	cancel  context.CancelFunc
	stopped bool
}

func newStepCancels() *stepCancels {
	return &stepCancels{byID: map[int64]*trackedStep{}}
}

// track wraps parent with its own cancel, registers it under id, and returns
// a context to run the step with plus a cleanup to defer. Overwriting an
// existing entry for the same id would leak the earlier cancel func and let
// a stale `step.stop` cancel the WRONG run, so a second track for a step
// already tracked is a programmer error — a step id is only ever tracked for
// the duration of exactly one run.
func (s *stepCancels) track(parent context.Context, id int64) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	if _, exists := s.byID[id]; exists {
		s.mu.Unlock()
		panic("daemon: step cancellation already tracked for this id; a step id is tracked for one run at a time")
	}
	s.byID[id] = &trackedStep{cancel: cancel}
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		delete(s.byID, id)
		s.mu.Unlock()
	}
}

// stop cancels a tracked step's context and marks it stopped, reporting
// whether one was found running. Finding none is not an error at this layer
// — the RPC handler decides what that means (already done, never started,
// wrong id) with the context it has and this one does not.
func (s *stepCancels) stop(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byID[id]
	if !ok {
		return false
	}
	t.stopped = true
	t.cancel()
	return true
}

// wasStopped reports and CLEARS whether `stop` was called for this id since
// the last time this was asked — read once, by the run that id belongs to,
// right after it returns and before the entry is untracked. The CLEARING is
// what keeps a stop from being re-lived by a later run of the same id; it is
// also why the event sink must never call this (it consults `isStopped`).
func (s *stepCancels) wasStopped(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byID[id]
	if !ok {
		return false
	}
	stopped := t.stopped
	t.stopped = false
	return stopped
}

// isStopped reports the same fact WITHOUT clearing: the sink consults it on
// every terminal event (S31 D6 — a stopped step publishes no `step.done`),
// possibly many times per run, and must leave the consuming read for the run
// itself.
func (s *stepCancels) isStopped(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byID[id]
	return ok && t.stopped
}

// isRunning reports whether a context is currently tracked for id — i.e.
// whether SOME run of this step is live right now. Read-only; it mutates
// nothing, unlike `stop`.
func (s *stepCancels) isRunning(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.byID[id]
	return ok
}
