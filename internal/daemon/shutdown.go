package daemon

// S55 T4 (LC19) — the daemon's graceful drain.
//
// At SIGTERM the daemon must stop starting work, cancel in-flight loop work
// with a typed cause, and leave interrupted steps running at their last
// checkpoint for the existing recovery walk — instead of abandoning goroutines
// and children when the process exits. No step is ever committed failed
// because the daemon shut down: the commit path (protocol/step.go) reads the
// cause off the run's context and, on ErrDaemonShutdown, leaves the step row
// running at its last checkpoint rather than committing a failure.
//
// This is the daemon's one drain, called by serve's shutdown sequence BEFORE
// d.Shutdown() (the terminals) and bootstrap.Shutdown — the order is the
// argument (LW9): the loop work settles before the transport closes, so no
// reader notifies into a dead socket.

import (
	"context"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
)

// DrainReport is the one-line summary of a drain: how many runners were in
// flight, how many settled, and how many were left running when the grace
// elapsed. Logged as one line, not a stack of per-runner noise (the brief's
// one-log-line rule). SettledSteps is the runners that settled — a runner
// settling is its in-flight step leaving the daemon's account.
type DrainReport struct {
	Runners      int
	SettledSteps int
	LeftRunning  int
}

// Drain is the daemon's graceful shutdown of its loop work (T4, LC19):
//
//  1. loopRunner.Drain() — no new runner may spawn (Start refuses);
//  2. cancelLoops(ErrDaemonShutdown) — every in-flight runner's context
//     carries the cause the step commit path reads, so a step interrupted
//     by the shutdown is left running at its last checkpoint for recovery,
//     never committed failed;
//  3. DrainWait(grace) — the wait is bounded by the shutdown ctx's deadline
//     (serve's shutdownGrace); a runner that does not settle within it is
//     LeftRunning, never waited on forever (the process is going away
//     either way, LC4.1).
//
// The order of (1) and (2) matters: Drain first, so a start racing the
// cancel is refused rather than spawned into a context about to be cancelled.
// The grace comes from ctx's deadline (serve's shutdownGrace); a ctx without
// one takes a default so the drain never blocks forever.
func (d *Daemon) Drain(ctx context.Context) DrainReport {
	if d.loopRunner == nil {
		return DrainReport{}
	}
	runners := d.loopRunner.ActiveCount()
	d.loopRunner.Drain()
	d.cancelLoops(loop.ErrDaemonShutdown)

	var grace time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		grace = time.Until(deadline)
	} else {
		grace = 30 * time.Second
	}
	d.loopRunner.DrainWait(grace)

	left := d.loopRunner.ActiveCount()
	report := DrainReport{
		Runners:      runners,
		SettledSteps: runners - left,
		LeftRunning:  left,
	}
	// one line, not a stack of per-runner noise. LeftRunning names the runners
	// the drain did not account for — they are recovered cold on restart, the
	// same as today's abandoned goroutines, but now their context carried the
	// cause so a step they held was left running, not committed failed.
	if d.log != nil {
		d.log.Infof("loop drain: %d runner(s) in flight, %d settled, %d left running",
			report.Runners, report.SettledSteps, report.LeftRunning)
	}
	return report
}
