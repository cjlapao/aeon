package loop

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// ErrStopRequested is the cause a person's loop.stop cancels the runner's
// context with (LC2). The runner goroutine, on seeing this cause at the top of
// Run, parks the loop (the daemon-supplied park hook) rather than emitting a
// runner-error: a stop is a person's act, not a failure.
var ErrStopRequested = errors.New("loop: a person stopped this loop")

// ErrDaemonShutdown is the cause the daemon's drain cancels a runner with (T4,
// LC19). The goroutine returns on it silently — no runner-error event, no
// write — because the process is going away and the loop is recovered cold
// from rows, not parked here.
var ErrDaemonShutdown = errors.New("loop: the daemon is shutting down")

// Run advances until a durable terminal or waiting state. Waiting holds no
// goroutine; a gate answer starts another run from the saved position.
func Run(ctx context.Context, dep Deps, id string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// S32 T5 (PA15): the cap is read FRESH on every advance — never cached
		// across the run — so a cap change mid-run takes effect at the next
		// launch decision in both directions (lower and raise). A cap read that
		// cannot resolve (no component, an old row) answers 1: the serial path,
		// byte-identical, never a 0 that would stop the loop.
		cap := componentCap(ctx, dep, id)
		var result Result
		var err error
		if cap > 1 {
			// PA15: the fan-out re-reads the cap on every re-decision through
			// this reader — the component's live cap, never a cached one.
			result, err = AdvanceLoopParallel(ctx, dep, id, func() int { return componentCap(ctx, dep, id) })
		} else {
			result, err = AdvanceLoop(ctx, dep, id)
		}
		if err != nil {
			return err
		}
		// S39 CR1: idle stops the run too — a resting loop is durable
		// stasis, no goroutine (same P2 argument as waiting).
		if result.Status == "waiting" || result.Status == "blocked" || result.Status == "done" || result.Status == "aborted" || result.Status == StatusIdle {
			return nil
		}
	}
}

type runnerEntry struct {
	// cancel is the cause-aware cancel; Interrupt calls it with the cause.
	cancel context.CancelCauseFunc
	// done closes when the runner goroutine exits, so a cancel can wait for the
	// runner to settle without a second, racy check.
	done chan struct{}
}

type Runner struct {
	mu     sync.Mutex
	active map[string]*runnerEntry
	Dep    Deps
	// park is the hook the daemon supplies: on a ErrStopRequested cancellation
	// the goroutine parks the loop (the store CAS plus the loop.waiting event).
	// nil is safe — a stop with no park hook just lets the runner exit. The
	// daemon sets it (the control core's Stop, with its step hooks).
	park func(id string)
	// draining is set by Drain (T4, LC19): once the daemon is going away no
	// new runner may start, so Start returns false without spawning a goroutine
	// — an in-flight runner is settled by its own context (the daemon's Drain
	// cancels the loop context with ErrDaemonShutdown), but a brand-new one
	// would be a goroutine outliving the shutdown it is here to prevent.
	draining bool
}

func NewRunner(dep Deps) *Runner { return &Runner{active: map[string]*runnerEntry{}, Dep: dep} }
func (r *Runner) Start(ctx context.Context, id string) bool {
	r.mu.Lock()
	if _, ok := r.active[id]; ok {
		r.mu.Unlock()
		return false
	}
	// Once draining, no new runner may start (T4, LC19): the daemon is going
	// away, so a brand-new goroutine would outlive the shutdown. An in-flight
	// runner is settled by its own context (the drain cancels it with
	// ErrDaemonShutdown), but this one is refused up front — the caller gets
	// the honest "no" rather than a goroutine that lives past the process.
	if r.draining {
		r.mu.Unlock()
		return false
	}
	// Derive the runner's context from the caller's: the cause a later
	// Interrupt sets reaches Run's ctx.Err() check, so a stop and a daemon
	// shutdown are distinguishable causes, not one generic cancellation.
	derived, cancel := context.WithCancelCause(ctx)
	entry := &runnerEntry{cancel: cancel, done: make(chan struct{})}
	r.active[id] = entry
	r.mu.Unlock()
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.active, id)
			r.mu.Unlock()
			close(entry.done)
		}()
		err := Run(derived, r.Dep, id)
		if err == nil {
			return
		}
		// The cause of the cancellation decides what a stop means (LC2, LC19):
		// a person's stop parks; a daemon shutdown returns silently. Neither is
		// a runner-error. A plain cancellation (no cause) is the old path.
		switch cause := context.Cause(derived); {
		case cause == ErrStopRequested:
			if r.park != nil {
				r.park(id)
			}
			return
		case cause == ErrDaemonShutdown:
			return
		}
		if _, ok := err.(PinRefusal); ok {
			_ = BlockCorruptPin(r.Dep, id, err)
			return
		}
		emit(r.Dep, "loop.blocked", map[string]any{"loopId": id, "reason": BlockedReason{Kind: "runner-error", Detail: err.Error()}})
	}()
	return true
}

// SetPark installs the hook the runner goroutine calls when its context is
// cancelled with ErrStopRequested. The daemon supplies it (the control core's
// Stop, with its step hooks); until it is set a stop just lets the runner exit.
func (r *Runner) SetPark(fn func(id string)) {
	r.mu.Lock()
	r.park = fn
	r.mu.Unlock()
}

// Interrupt cancels a running loop's context with the given cause (LC2 step 3).
// It returns whether a runner was found; a loop with no runner (already parked,
// between actions, or idle) has nothing to interrupt and is not an error. The
// cause is what the goroutine reads to decide whether to park (ErrStopRequested)
// or return silently (ErrDaemonShutdown).
func (r *Runner) Interrupt(id string, cause error) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.active[id]
	if !ok {
		return false
	}
	entry.cancel(cause)
	return true
}

// Wait blocks until a loop's runner goroutine exits or d elapses. A loop with
// no runner (already exited) is already settled and returns true at once; a
// runner that does not settle within d returns false (the caller still commits —
// the cancel does not wait forever, LC4.1).
func (r *Runner) Wait(id string, d time.Duration) bool {
	r.mu.Lock()
	entry, ok := r.active[id]
	r.mu.Unlock()
	if !ok {
		return true
	}
	select {
	case <-entry.done:
		return true
	case <-time.After(d):
		return false
	}
}
func (r *Runner) Active(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.active[id]
	return ok
}

// ActiveCount is the number of runners still in flight (T4, LC19): the daemon's
// Drain reads it before and after the cancel to report Runners and LeftRunning
// as one line. A runner that settles removes itself from active in its defer,
// so the count after DrainWait is exactly the number that did not settle.
func (r *Runner) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
}

// Drain marks the runner drained (T4, LC19): from now on Start refuses, so no
// new goroutine may outlive the daemon that is going away. The daemon calls
// this as the FIRST step of its shutdown — before it cancels the loop context
// with ErrDaemonShutdown — because a start that began after the cancel would
// be a goroutine the drain could not account for.
func (r *Runner) Drain() {
	r.mu.Lock()
	r.draining = true
	r.mu.Unlock()
}

// Draining reports whether Drain has been called (T4, LC19). The daemon's
// start-family doors (loop.start / loop.run / loop.answer) read it BEFORE
// they write any row: a start refused up front leaves the store exactly as
// it found it, and the caller gets the typed "the daemon is shutting down"
// refusal rather than a loop row with no runner behind it. Start itself is
// the safety net for any other path: it returns false once draining, so no
// goroutine can outlive the drain even where a door has no check.
func (r *Runner) Draining() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.draining
}

// DrainWait blocks until every in-flight runner has settled, or d elapses. The
// daemon calls it AFTER cancelling the loop context: the ErrDaemonShutdown
// cause makes each runner's Run return, which closes its done and the goroutine
// removes itself from active. A runner that does not settle within d is the
// caller's LeftRunning count — it is never waited on forever (LC4.1, the same
// bound Wait argues), because the process is going away either way.
func (r *Runner) DrainWait(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		r.mu.Lock()
		empty := len(r.active) == 0
		r.mu.Unlock()
		if empty {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// AnswerGate validates the closed answer set, then saves the answer and the
// next position atomically. The caller starts a fresh runner afterward.
func AnswerGate(ctx context.Context, dep Deps, id, answer string) (store.Question, error) {
	st, err := Pinned(ctx, dep.Store, id)
	if err != nil {
		return store.Question{}, err
	}
	if st.Status != "waiting" || st.QuestionID == 0 {
		return store.Question{}, fmt.Errorf("loop %s is not waiting at a gate", id)
	}
	action := DecideGateForAnswer(st)
	if action.Step == nil || action.Step.Question == "" {
		return store.Question{}, fmt.Errorf("loop %s is waiting on a step question", id)
	}
	key := action.Step.Question
	q, ok := st.Blueprint.Questions[key]
	if !ok {
		return store.Question{}, fmt.Errorf("gate question %q is absent", key)
	}
	allowed := false
	for _, option := range q.Answers {
		if answer == option {
			allowed = true
			break
		}
	}
	if !allowed {
		return store.Question{}, fmt.Errorf("answer %q is not one of %v", answer, q.Answers)
	}
	st.Answers[key] = answer
	st.StepStates[stepKey(st.Position)] = StepState{Status: "done", Outcome: answer}
	st.LaneStates[st.Position.Lane] = "ok"
	switch answer {
	case "abort":
		st.Flags["aborted"] = true
		if st.Blueprint.OnAbort == "" {
			st.Status = "blocked"
			st.LastReason = "aborted by user; blueprint has no terminal handler"
		} else {
			st.Position = Position{Lane: st.Blueprint.OnAbort}
			st.Status = "running"
		}
	case "approve", "merge-now", "leave-open", "yes", "continue":
		st.Position, err = NextPosition(st.Blueprint, st, st.Position)
		if err != nil {
			return store.Question{}, err
		}
		st.Status = "running"
	default:
		if err := routeReject(ctx, dep, id, &st, *action.Step, action.Position); err != nil {
			var ex *exhaustionError
			var ew *escalationWait
			switch {
			case errors.As(err, &ex):
				// handleExhaustion already blocked the loop with the exact
				// O1 sentence (T4) — kept, not prefixed.
			case errors.As(err, &ew):
				st.Status = "waiting"
			default:
				st.Status = "blocked"
				st.LastReason = "gate answer " + answer + ": " + err.Error()
			}
		} else {
			st.Status = "running"
		}
	}
	st.QuestionID = 0
	raw, err := st.RuntimeJSON()
	if err != nil {
		return store.Question{}, err
	}
	question, err := dep.Store.AnswerGate(id, action.QuestionID, answer, st.Status, raw)
	if err != nil {
		return store.Question{}, err
	}
	emit(dep, "loop.advanced", map[string]any{"loopId": id, "lane": action.Position.Lane, "questionId": action.QuestionID, "answer": answer, "outcome": answer})
	if st.Status == "blocked" {
		emit(dep, "loop.blocked", map[string]any{"loopId": id, "reason": BlockedReason{Kind: "gate-answer", Detail: st.LastReason}})
	}
	return question, nil
}

func DecideGateForAnswer(st LoopState) Result {
	result := Result{LoopID: st.ID, Status: st.Status, Position: st.Position, QuestionID: st.QuestionID}
	if st.Blueprint == nil {
		return result
	}
	for _, lane := range st.Blueprint.Lanes {
		if lane.ID != st.Position.Lane {
			continue
		}
		if lane.Step != nil && lane.Step.Kind == "gate" {
			result.Step = lane.Step
			return result
		}
		for i := range lane.Ladder {
			if lane.Ladder[i].ID == st.Position.Step && lane.Ladder[i].Kind == "gate" {
				result.Step = &lane.Ladder[i]
				return result
			}
		}
	}
	return result
}
