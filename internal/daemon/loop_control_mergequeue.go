package daemon

// loop_control_mergequeue.go — the merge-queue plane's use of loop control
// (S60 T8). The CAS transitions — CancelLoop, ResumeLoop — have exactly one
// door, the loop-control service (LC2–LC4; TestOnlyTheControlServiceCalls
// TheCAS walks the tree and blesses the control-file family). The queue
// never cancels or resumes with its own hands: every queue site — the
// dequeue's stop-everything-first walk, the stale-landing retire, the
// blocked-pin resume, the terminal-item reaper — asks these two helpers,
// which live IN the blessed family (loop_control*.go — watchdog.go's own
// extension point), so the one-door law holds by construction and not by
// exception. What the helpers add is only the plane's reason sentences;
// the CAS itself is the control service's, byte for byte.

import (
	"context"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/store"
)

// mqControlCancel is the queue plane's ONLY way to stop a loop: the
// control service's CancelLoop, asked through the control file family. The
// caller keeps its own answers to a refusal (retire-by-stop-word, logged
// and retried on the next tick) — the helpers never swallow one.
func (d *Daemon) mqControlCancel(ctx context.Context, id string, actor store.Actor, reason string) (loop.LoopCancelResult, *store.ControlRefusal, error) {
	return d.CancelLoop(ctx, id, actor, reason)
}

// mqControlResume is the queue plane's ONLY way to resume a loop — the
// blocked-pin nudge, nothing more.
func (d *Daemon) mqControlResume(ctx context.Context, id string, actor store.Actor, message string) (loop.LoopResumeResult, *store.ControlRefusal, error) {
	return d.ResumeLoop(ctx, id, actor, message)
}
