package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/cjlapao/aeon/internal/model"
)

// ReceiptWriter belongs to one step. It cannot address another step's rows.
type ReceiptWriter interface {
	Begin(context.Context, string, string, string) error
	Complete(context.Context, string, string, bool) error
}

// Checkpoint is the durable state needed to classify a crash without retaining
// any runtime object. The caller writes it atomically with transcript and cost.
type Checkpoint struct {
	Request     StepRequest
	Messages    []model.Message
	Manifest    ContextManifest
	Usage       model.Usage
	Cost        model.Cost
	Compactions []CompactionRecord

	// SteerIDs names the steer rows this checkpoint DELIVERS (S31 T4, D4):
	// the writer flips exactly these rows in the same transaction that writes
	// this transcript, which is the only shape that makes "pending WITH the
	// text already in it" unrepresentable. Empty means an ordinary checkpoint.
	// Iteration is the loop pass these steers were delivered into — recorded,
	// not inferred, so `step.steers` can say which turn read them.
	SteerIDs  []int64
	Iteration int

	// MailboxIDs names the agent_message rows this checkpoint DELIVERS
	// (S70 MB4): the same D4 law the steers obey — the fold and the stamp
	// are one write, or "pending with its text already in the transcript"
	// is representable again and the replay injects the message twice.
	MailboxIDs []int64
}

type CheckpointWriter interface {
	Save(context.Context, Checkpoint) error
}

// ToolClass is an optional capability of a toolset. The real daemon adapter
// supplies it; scripted tools without this capability are treated as safe.
type ToolClass interface{ Class(string) string }

func toolArgDigest(tool string, input json.RawMessage) string {
	h := sha256.New()
	_, _ = h.Write([]byte(tool))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(input)
	return hex.EncodeToString(h.Sum(nil))
}

func (st *LoopState) checkpoint(ctx context.Context, d LoopDeps) error {
	if d.Checkpoint == nil {
		return nil
	}
	return d.Checkpoint.Save(ctx, Checkpoint{Request: st.Request, Messages: st.Messages,
		Manifest: *st.Manifest, Usage: st.Usage, Cost: st.Cost, Compactions: st.Compactions})
}

// checkpointWithSteers is the D4 instant: the transcript containing the
// steers and their delivery are ONE write. A writer without steer support
// (an adapter whose backing store cannot flip rows transactionally) fails
// HERE rather than silently dropping the flip — a delivered-looking step
// whose rows stay pending would re-inject on the next drain, and "the
// feature was off" is a wiring fact, not something to discover mid-step.
func (st *LoopState) checkpointWithSteers(ctx context.Context, d LoopDeps, steerIDs []int64) error {
	if d.Checkpoint == nil {
		return nil
	}
	return d.Checkpoint.Save(ctx, Checkpoint{Request: st.Request, Messages: st.Messages,
		Manifest: *st.Manifest, Usage: st.Usage, Cost: st.Cost, Compactions: st.Compactions,
		SteerIDs: steerIDs, Iteration: st.Manifest.IterationsUsed})
}

// checkpointMailbox is the mailbox's D4 instant (S70 MB4), identical in law
// to checkpointWithSteers: the transcript containing the peer's words and
// their delivery stamp are ONE write. A writer whose backing store cannot
// flip agent_message rows transactionally fails HERE, named — a
// delivered-looking step whose rows stay pending would re-inject on the
// next drain, and "the feature was off" is a wiring fact, not something to
// discover mid-step.
func (st *LoopState) checkpointMailbox(ctx context.Context, d LoopDeps, mailboxIDs []int64) error {
	if d.Checkpoint == nil {
		return nil
	}
	return d.Checkpoint.Save(ctx, Checkpoint{Request: st.Request, Messages: st.Messages,
		Manifest: *st.Manifest, Usage: st.Usage, Cost: st.Cost, Compactions: st.Compactions,
		MailboxIDs: mailboxIDs, Iteration: st.Manifest.IterationsUsed})
}
