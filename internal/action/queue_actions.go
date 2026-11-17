package action

// S60 T6 · `queue.enqueue` — the blueprint's door INTO the queue (MQ8/MQ13/
// MQ14.1), kept out of landing.go because it is the opposite direction of
// travel: enqueue is a loop asking to be queued; landing is the queue
// acting on a loop.
//
// The caller cannot choose anything here. The branch is the loop's PINNED
// feature branch, the base the loop's pinned base, attended the loop's
// pinned attendedness, the actor the pinned blueprint — MQ13 says it
// exactly: "there is nothing for a caller to choose". An unattended loop
// therefore can NEVER enqueue itself as attended through this door, which
// is the same attack S46's own-view rule and AL11 close elsewhere.
//
// `mode = off` does NOT stop an enqueue (OD11): the row sits `queued` and
// a mode turned on later lands what already finished. Stopping the driver
// is what `off` means; it is never a refuse.

import (
	"context"
	"fmt"
	"strconv"
)

// NoBaseSentence is MQ15's refusal — "a base is genuinely unknown". The
// canonical rendering lives in the merge-queue package's validator (it is
// the sentence the human door and the action must speak identically); this
// package cannot import that one, so the string is reproduced here and
// TestEnqueueUnknownBaseSpeaksTheMQ15Sentence pins the two byte-identical
// across the boundary — drift between the two fails that test, not a reader.
const NoBaseSentence = "no base branch is known for this repository — set its default branch (`aeon component update <id> --default-branch main`)"

// QueueActions is the queue's one action. Like ForgeActions it records its
// evidence through the shared sink.
type QueueActions struct {
	// Enq is the queue service seam. Required: an enqueue with nothing to
	// enqueue into is an action that hopes.
	Enq QueueEnqueuer

	// Record is the same evidence sink the other actions write through.
	Record func(Evidence)
}

func (q *QueueActions) record(e Evidence) {
	if q.Record != nil {
		q.Record(e)
	}
}

// Enqueue is check-then-act by way of the service: an open item for this
// branch already IS the answer and comes back unchanged. Everything it
// needs is already CODE-side by the time it arrives (the loop's pinned
// shape filled the Args); this method adds no facts of its own — it only
// refuses, forwards, and records what the queue answered.
func (q *QueueActions) Enqueue(ctx context.Context, in QueueEnqueueInput) (QueueEnqueueResult, error) {
	if q.Enq == nil {
		return QueueEnqueueResult{}, fmt.Errorf("queue.enqueue: no queue service wired")
	}
	if in.ComponentRoot == "" {
		return QueueEnqueueResult{}, fmt.Errorf("queue.enqueue refuses: no component root — the queue is per-repository and this loop is not attached to one")
	}
	if in.Branch == "" {
		return QueueEnqueueResult{}, fmt.Errorf("queue.enqueue refuses: the loop carries no pinned feature branch — there is nothing to land")
	}
	if in.BaseBranch == "" {
		// MQ15: no pin AND no default AND no origin/HEAD — the honest
		// answer names the fix, and it is the SAME sentence the validator
		// and the human door speak.
		return QueueEnqueueResult{}, fmt.Errorf("queue.enqueue refuses: %s", NoBaseSentence)
	}
	res, err := q.Enq.Enqueue(ctx, in)
	if err != nil {
		return QueueEnqueueResult{}, err
	}
	if res.ItemID == "" {
		return QueueEnqueueResult{}, fmt.Errorf("queue.enqueue: the queue answered with an empty item id — a row that cannot name itself is not a row")
	}
	q.record(Evidence{Action: QueueEnqueue, Key: "queue_item_id", Value: res.ItemID})
	return res, nil
}

// QueueEvidence is the evidence the dispatch hands back: the row's id and
// where it sits — numbers, never prose.
func QueueEvidence(res QueueEnqueueResult) map[string]string {
	return map[string]string{
		"queue_item_id": res.ItemID,
		"queue_status":  res.Status,
		"queue_pos":     strconv.Itoa(res.Position),
		"queue_reused":  strconv.FormatBool(res.Reused),
	}
}
