package daemon

import (
	"context"
	"testing"
	"time"
)

func TestTrackingAStepLetsStopCancelItsContext(t *testing.T) {
	c := newStepCancels()
	ctx, done := c.track(context.Background(), 1)
	defer done()

	if !c.stop(1) {
		t.Fatal("stop reported no tracked step for id 1")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the context was not cancelled")
	}
}

func TestStoppingAnUntrackedStepIsNotAnErrorButReportsFalse(t *testing.T) {
	// Already done, never started, or the wrong id — this layer does not
	// distinguish; the RPC handler decides what "not running" means with
	// context this one does not have.
	c := newStepCancels()
	if c.stop(999) {
		t.Fatal("stop reported a tracked step that was never tracked")
	}
}

func TestStoppingOneStepDoesNotTouchAnother(t *testing.T) {
	c := newStepCancels()
	ctxA, doneA := c.track(context.Background(), 1)
	defer doneA()
	ctxB, doneB := c.track(context.Background(), 2)
	defer doneB()

	if !c.stop(1) {
		t.Fatal("stop(1) found nothing")
	}
	select {
	case <-ctxA.Done():
	case <-time.After(time.Second):
		t.Fatal("step 1's context was not cancelled")
	}
	select {
	case <-ctxB.Done():
		t.Fatal("step 2's context was cancelled by stopping step 1")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWasStoppedIsTrueOnceThenClearsItself(t *testing.T) {
	// Read once, by the run that id belongs to. A second read — a later,
	// unrelated run reusing the same id after `done()` untracked it and a
	// fresh `track` re-registered it — must not see a stale "yes".
	c := newStepCancels()
	_, done := c.track(context.Background(), 1)
	c.stop(1)

	if !c.wasStopped(1) {
		t.Fatal("wasStopped is false right after stop")
	}
	if c.wasStopped(1) {
		t.Fatal("wasStopped stayed true on a second read")
	}
	done()
}

func TestWasStoppedOnAnUntrackedStepIsFalse(t *testing.T) {
	c := newStepCancels()
	if c.wasStopped(1) {
		t.Fatal("an id that was never tracked reports stopped")
	}
}

func TestDoneUntracksSoAFutureStopFindsNothing(t *testing.T) {
	c := newStepCancels()
	_, done := c.track(context.Background(), 1)
	done()

	if c.stop(1) {
		t.Fatal("stop found an entry that done() should have removed")
	}
}

func TestTrackingTheSameIDTwiceWithoutDoneIsAProgrammerErrorAndPanics(t *testing.T) {
	// A step id is tracked for exactly one run. Overwriting a live entry
	// would leak the earlier cancel func and let a stale `step.stop` cancel
	// the WRONG run — so this is a panic, not a silently-replaced entry,
	// the same way this project treats every other construction-time
	// invariant violation.
	c := newStepCancels()
	_, done := c.track(context.Background(), 1)
	defer done()

	defer func() {
		if recover() == nil {
			t.Fatal("tracking the same id twice did not panic")
		}
	}()
	c.track(context.Background(), 1)
}

func TestTrackDerivesFromTheGivenParentContext(t *testing.T) {
	// Cancelling the PARENT must also cancel the tracked context — stop is
	// one way in, not the only way.
	parent, cancelParent := context.WithCancel(context.Background())
	c := newStepCancels()
	ctx, done := c.track(parent, 1)
	defer done()

	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelling the parent did not cancel the tracked context")
	}
}

func TestIsRunningReflectsExactlyWhatIsTracked(t *testing.T) {
	c := newStepCancels()
	if c.isRunning(1) {
		t.Fatal("isRunning is true before anything was tracked")
	}
	_, done := c.track(context.Background(), 1)
	if !c.isRunning(1) {
		t.Fatal("isRunning is false while tracked")
	}
	done()
	if c.isRunning(1) {
		t.Fatal("isRunning stayed true after done()")
	}
}

func TestIsRunningDoesNotMutateAnything(t *testing.T) {
	// A read that happened to clear a flag would make the SECOND caller to
	// ask "is it running" get a different, wrong answer than the first.
	c := newStepCancels()
	_, done := c.track(context.Background(), 1)
	defer done()
	first := c.isRunning(1)
	second := c.isRunning(1)
	if first != second || !first {
		t.Fatalf("isRunning gave %v then %v for the same tracked step", first, second)
	}
}
