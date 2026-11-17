package daemon

// S24 T4 — the answered half of `queueAdvance: ask` (and the answer door
// for every CG2 halt T3 raised). The raising side lives in
// queue_reactor.go; the materialization it unlocks is T3's ONE function
// (advanceNext), called from exactly two places — the reactor's `auto`
// branch and the affirmative answer here. A second materialization path
// would be the second execution path brief §4 forbids, and the
// call-site test pins that this stays true.

import (
	"context"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

// answerQueueGate is the queue's arm of the loop-answer door, tried
// BEFORE the ladder's routing. Its discriminator is not the loop's
// status — a finished loop's pin still reads whatever status the engine
// left, and asking the ladder about a queue question would answer the
// wrong ledger. The three facts that make a question the queue's:
//
//   - it is the loop's PENDING question (one at a time, the store says),
//   - its kind is `gate`, and
//   - it carries NO step (`StepID == nil`) — a ladder gate question
//     always rides the step that decides it; a queue question was raised
//     with no ladder in sight, which is exactly design §2's finding about
//     where queue questions can legally hang (the finished loop's slug).
//
// A question that is not the queue's returns handled=false and the door
// routes it as it always has.
func (d *Daemon) answerQueueGate(ctx context.Context, p protocol.LoopAnswerParams, root, slug string) (handled bool, res any, err error) {
	// Discriminator, in the order the facts are cheapest and loudest:
	// (1) the loop is a loop THIS queue minted — an untracked loop is
	//     quietly not the queue's business, exactly as T3's reactor
	//     treats its terminal events; (2) the item reads done or
	//     blocked — the two states whose questions the reactor raises
	//     (a loop running its ladder owns its gate questions: the item
	//     reads running then, and answering THOSE stays the ladder's
	//     door — measured the hard way when a brief-gate test's answer
	//     was first swallowed by a looser arm); (3) the question itself
	//     is a step-less gate on this loop's slug.
	item, tracked, terr := d.store.QueueItemByLoopSlug(root, slug)
	if terr != nil {
		return true, nil, terr
	}
	if !tracked || (item.Status != string(queue.StatusDone) && item.Status != string(queue.StatusBlocked)) {
		return false, nil, nil
	}
	q, found, err := d.store.PendingQuestion(store.Scope{ComponentRoot: root, LoopSlug: slug})
	if err != nil {
		return false, nil, err
	}
	if !found {
		// Nothing pending. A replay of an answer the QUEUE already took
		// must still be answered by the queue — refused, with the first
		// answer's timestamp — rather than falling through to the
		// ladder's "not waiting at a gate", which would answer the wrong
		// ledger with the wrong sentence.
		last, ok, lerr := d.store.LatestLoopGateQuestion(root, slug)
		if lerr != nil {
			return true, nil, lerr // an unreadable ledger answers NOTHING — falling through would guess
		}
		if ok && last.Answered() && last.StepID == nil {
			return true, map[string]any{"refused": map[string]string{
				"kind":   "already-answered",
				"detail": fmt.Sprintf("question %d was already answered at %s; a second answer is refused because the first may already have started a loop", last.ID, last.AnsweredAt),
			}}, nil
		}
		return false, nil, nil
	}
	if q.Kind != string(store.QuestionGate) || q.StepID != nil {
		return false, nil, nil
	}
	// The closed word-set is validated BEFORE anything is written: the
	// options this slice raises are `advance` and `hold`, and an answer
	// outside them is refused naming both, not silently held (a silent
	// hold on a typo would leave the person believing their "yes" landed).
	word := strings.ToLower(strings.TrimSpace(p.Answer))
	if word != "advance" && word != "hold" {
		return true, map[string]any{"refused": map[string]string{
			"kind":   "unknown-answer",
			"detail": `the queue's question is answered "advance" or "hold" — nothing was recorded`,
		}}, nil
	}
	ans, err := d.store.Answer(q.ID, p.Answer)
	if err != nil {
		return true, nil, err
	}
	if !ans.OK {
		// Claimed-ONCE (the store's rule): a replay of the first answer's
		// effect NEVER happens — the second caller reads the refusal with
		// the first answer's timestamp, and no second loop starts.
		return true, map[string]any{"refused": map[string]string{
			"kind":   "already-answered",
			"detail": ans.Reason,
		}}, nil
	}
	if word != "advance" {
		// Declined: the queue stays halted and NOTHING re-asks (CG4 — no
		// poller, no nagging; the question was asked once and answered no).
		d.log.Infof("queue: the person declined to advance on %s — the queue holds", slug)
		return true, map[string]any{"id": p.ID, "questionId": q.ID, "answer": ans.Question.Answer, "advanced": false}, nil
	}
	// Affirmative: the SAME materialization function the `auto` branch
	// calls (one path, two callers — asserted by a call-site test).
	// Answering "advance" to a BLOCKED halt starts the next ready item
	// but does NOT cancel the blocked one: cancelling a person's item is
	// a named act (the CLI `remove`), never a side effect of moving on
	// (D12's discipline, recorded as a decision).
	started := d.advanceNext(ctx, root)
	return true, map[string]any{"id": p.ID, "questionId": q.ID, "answer": ans.Question.Answer, "advanced": started}, nil
}
