// learning_kinds.go — S52 T4: the sixteenth proposal kind, learning_retract,
// as a registry entry in this package.
//
// The desk keeps the pure half, exactly the roadmap_kinds.go shape: the
// payload SHAPE (a strict decode — a smuggled "actor", "reason" longer
// than the stamp column, or any field besides {id, reason} is a PARSE
// refusal; the payload is a pointer at a row and the sentence a person
// reads, never a claim about who filed the ask or what else may move).
// The reason bound mirrors what the retraction itself demands — the
// domain's Retract refuses 1..256 bytes because the stamp column caps it
// there —
// so an ask that passes here can never fail at Apply on the size of its
// own words; id is positive because 0 and below are not row ids.
//
// What the desk may NOT do is read the learning. The store halves — the
// row exists, it is not already retracted, and (at the agent's create
// door only) it is inside the proposing agent's readable scopes — arrive
// as `Seams` from `cmd/aeon` plus the daemon arm's readable check, so
// create and commit run ONE desk-then-store function (S35 TD3) and commit
// writes with the COMMITTING PERSON as actor through learnings.Retract —
// stamp, one learning_change line, vector drop, all carrying the person.
//
// The kind narrows and nothing else, in AN9's and EV16's sense: a
// learning_retract ask can only take a memory BACK. There is deliberately
// no learning_restore, learning_edit or learning_add kind — restoring is
// the person's door (LM7), editing does not exist in this plane at all,
// and an agent's own filing already has its capped tool; T3's guards
// refuse each such name by word.

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

// KindLearningRetract is S52 T4's kind, sixteenth in the store's order
// (migration 20261104171000; the store mirror is store.ProposalKinds).
const KindLearningRetract Kind = "learning_retract"

// maxLearningRetractReasonBytes is the retraction's own bound: the domain
// refuses a reason over 256 bytes because LM2's stamp column caps it
// there. A proposal that could not be applied for the length of its own
// reason is an ask that wastes a commit.
const maxLearningRetractReasonBytes = 256

// LearningRetractProposal is learning_retract's payload — WHICH memory and
// WHY it should come back, nothing else. There is no actor, no scope, no
// body field: the row is named by id, the readable check is the door's,
// and the reason the person commits travels to the stamp verbatim.
type LearningRetractProposal struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// validateLearningRetractPayload is the desk's pure half of Validate (the
// shape). The store halves — the row exists and is not already retracted
// — are the seam's, and the seam's Validate (which runs this function
// first) is what BOTH doors run.
func validateLearningRetractPayload(_ context.Context, payload []byte) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var p LearningRetractProposal
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: learning_retract payload is {\"id\":…, \"reason\":…} and carries no field besides them: %v", err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("refused: learning_retract payload carries trailing JSON: %v", err)
	}
	if p.ID <= 0 {
		return fmt.Errorf("refused: learning_retract names a learning by its positive id — %d is not a row this plane has ever filed", p.ID)
	}
	if strings.TrimSpace(p.Reason) == "" {
		return fmt.Errorf("refused: learning_retract needs a reason — a memory asked back without a sentence saying why is the silent delete this plane forbids (S52 LM2: reason is required, 1..%d bytes)", maxLearningRetractReasonBytes)
	}
	if len(p.Reason) > maxLearningRetractReasonBytes {
		return fmt.Errorf("refused: learning_retract reason is %d bytes and the retraction's own cap is %d — the stamp column cannot hold longer, so no commit could ever land this ask", len(p.Reason), maxLearningRetractReasonBytes)
	}
	return nil
}

// ValidateLearningRetract is the desk half exported for the seam wrapper
// (the seam runs it, then the store halves — one function per door, TD3).
func ValidateLearningRetract(ctx context.Context, payload []byte) error {
	return validateLearningRetractPayload(ctx, payload)
}
