package protocol

import (
	"github.com/cjlapao/aeon/internal/runtime"
)

// continuation.go — the drain for the fire-and-forget answer/continue
// goroutines. handleAnswer and step.continue each spawn continueAnsweredStep
// on its own goroutine and return at once (the request carries only a queued
// continuation; nothing in it depends on the connection's context). That is
// the right shape for the connection's lifetime, but it opens a window the
// store does not survive: the goroutine keeps writing the transcript and
// checkpoint after the request is done, and a caller that closes the store
// before the goroutine finishes closes it out from under the write. The store
// nils its *sql.DB on Close, so the next ReadTranscript/BeginTx dereferences
// a nil pointer and panics — the location wanders between runs because it is
// whichever write the goroutine reaches first.
//
// The fix is a drain, not a lifetime tie: the continuation must still outlive
// the CONNECTION (that is the design), but it must not outlive the STORE.
// continueAnsweredStepAsync counts each continuation into continuationWG, and
// WaitContinuations is the single place a caller asks "are we done writing"
// before it closes the store.

// continueAnsweredStepAsync starts a continuation as a tracked goroutine. It
// is the ONLY way to start one — the two spawn sites call it rather than the
// bare `go t.continueAnsweredStep`, so a new spawn site cannot forget the
// drain by re-typing the goroutine.
func (t *Table) continueAnsweredStepAsync(stepID int64, loopSlug string, runner resumeRunner, req runtime.ResumeRequest, toolset runtime.ToolCaller) {
	t.continuationWG.Add(1)
	go func() {
		defer t.continuationWG.Done()
		t.continueAnsweredStep(stepID, loopSlug, runner, req, toolset)
	}()
}

// WaitContinuations blocks until every answer/continue goroutine has
// finished. A caller MUST call it before closing the store — a continuation
// still writing the transcript into a closed store panics database/sql (the
// store's *sql.DB is nil after Close). A daemon calls it on shutdown; a test
// calls it before its cleanup closes the store.
func (t *Table) WaitContinuations() { t.continuationWG.Wait() }
