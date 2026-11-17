// mergequeue.go — S60 T9: the seventeenth proposal kind, mergequeue_enqueue,
// as a registry entry in this package.
//
// The desk keeps the pure half, exactly the learning_kinds.go shape: the
// payload SHAPE — a strict decode. A smuggled "actor", "attended", "status"
// or "baseBranch" (the base is resolved by the daemon at enqueue, never
// proposed — MQ15), a root or branch blank-padded to nothing, a reason
// outside 1..512 bytes, or any field besides {componentRoot, branch,
// reason} is a PARSE refusal. The payload names a repository, a branch, and
// the sentence a person reads at the desk — never a claim about who filed
// the ask, whether a human will watch the landing, or which base to aim at.
//
// What the desk may NOT do is read git or the queue. The store halves — the
// root is a component, the branch exists locally or on the remote, the base
// is known, the branch is not already open in the queue — belong to
// mergequeue.ValidateEnqueue, the ONE function every enqueue path runs
// (MQ14: blueprint, person, proposal — one validator); the daemon arm and
// the seam each gather the facts and run that same function at CREATE and
// again at COMMIT (S35 TD3), so a payload the socket mergequeue.enqueue
// refuses is refused by the tool with the same words, and a row that moved
// between the two runs still refuses at the second.
//
// The kind asks and nothing else, in AL11's and MQ14's sense: a
// mergequeue_enqueue ask can only make a branch JOIN the queue, and the
// landed row is unattended (MQ14.3) — asking costs the person one commit
// and buys no agent a standing door. There is deliberately no
// mergequeue_land, mergequeue_retry, mergequeue_reorder or
// mergequeue_settings kind — landing, retrying, reordering and widening
// move by a person alone through T8's doors, and T8's guards refuse each
// such name by word.

package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// KindMergeQueueEnqueue is S60 T9's kind, seventeenth in the store's order
// (migration 20261104193000; the store mirror is store.ProposalKinds).
const KindMergeQueueEnqueue Kind = "mergequeue_enqueue"

// maxMergeQueueEnqueueReasonBytes is the ask's own bound (T9): a reason of
// 1..512 bytes — long enough to say why the branch belongs in the queue,
// short enough that the desk shows it whole.
const maxMergeQueueEnqueueReasonBytes = 512

// maxMergeQueueEnqueuePayloadBytes is the envelope cap (T9 behaviour 6):
// a payload over 8 KiB is a payload nobody reads at the desk.
const maxMergeQueueEnqueuePayloadBytes = 8 * 1024

// MergeQueueEnqueueProposal is mergequeue_enqueue's payload — WHICH
// repository, WHICH branch, and WHY it should join, nothing else. There is
// no actor, no attended flag, no base: the commit stamps {proposal, <id>}
// into the row's enqueued_by (MQ14.3), the landed item is unattended, and
// the daemon resolves the base by MQ15's order — an ask that could name a
// base could aim a branch at a base its author was never shown.
type MergeQueueEnqueueProposal struct {
	ComponentRoot string `json:"componentRoot"`
	Branch        string `json:"branch"`
	Reason        string `json:"reason"`
}

// validateMergeQueueEnqueuePayload is the desk's pure half of Validate (the
// shape). The store-and-git halves are ValidateEnqueue over gathered facts,
// run by the seam's Validate and the arm alike — one function per door
// (TD3), never two validators.
func validateMergeQueueEnqueuePayload(_ context.Context, payload []byte) error {
	if len(payload) > maxMergeQueueEnqueuePayloadBytes {
		return fmt.Errorf("refused: mergequeue_enqueue payload is %d bytes and the desk's cap is %d — an ask is a sentence, not a document", len(payload), maxMergeQueueEnqueuePayloadBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var p MergeQueueEnqueueProposal
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: mergequeue_enqueue payload is {\"componentRoot\":…, \"branch\":…, \"reason\":…} and carries no field besides them (no actor, no attended, no base — MQ14.3/MQ15): %v", err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("refused: mergequeue_enqueue payload carries trailing JSON: %v", err)
	}
	if strings.TrimSpace(p.ComponentRoot) == "" {
		return fmt.Errorf("refused: mergequeue_enqueue names the repository by its component root — an empty root asks the queue to guess which project to land things into")
	}
	if strings.TrimSpace(p.Branch) == "" {
		return fmt.Errorf("refused: mergequeue_enqueue names the branch to queue — an empty branch is not a thing a queue can carry")
	}
	if strings.TrimSpace(p.Reason) == "" {
		return fmt.Errorf("refused: mergequeue_enqueue needs a reason — a branch asked into the queue without a sentence saying why is the silent queue this plane forbids (1..%d bytes)", maxMergeQueueEnqueueReasonBytes)
	}
	if len(p.Reason) > maxMergeQueueEnqueueReasonBytes {
		return fmt.Errorf("refused: mergequeue_enqueue reason is %d bytes and the desk's cap is %d — an ask the person cannot read whole is an ask they cannot decide", len(p.Reason), maxMergeQueueEnqueueReasonBytes)
	}
	return nil
}

// ValidateMergeQueueEnqueue is the desk half exported for the seam wrapper
// (the seam runs it, then the ValidateEnqueue-over-facts half — one function
// per door, TD3).
func ValidateMergeQueueEnqueue(ctx context.Context, payload []byte) error {
	return validateMergeQueueEnqueuePayload(ctx, payload)
}
