package learnings

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Retract is S52 LM2's half of memory management: memory a person takes
// back. It is a STAMP, never a delete — the body stays, its audit stays,
// its history stays; what leaves is the word. After the stamp every
// default read (List, Query, the board, forAgent, and so every prompt that
// reads through them) excludes it, and its similarity vector is dropped so
// a retracted body cannot be retrieved by a look-alike question either
// (LM5; rankWithinSet's set-intersection is the structural half of that,
// the dropped vector the hygiene half).
//
// It answers changed=true/false honestly (LM2's idempotence): re-retracting
// an already-retracted learning changes nothing and writes NO second audit
// line — an audit of non-acts is an audit nobody trusts. The row comes
// back either way, as it stands.
//
// The actor is the human the door stamped (LM8): kind user or cli, or the
// refusal names who may retract. An agent may never retract through this
// door — LM3's CHECK makes retract an act no agent-kind row may hold, and
// the door says it in words before the SQL has to; T4 gives agents the
// proposal path instead (learning_retract), which a person commits.
func (l *Learnings) Retract(id int64, reason string, actor Actor) (Learning, bool, *Refusal) {
	if r := humanDoor(actor, "retract"); r != nil {
		return Learning{}, false, r
	}
	if reason == "" {
		return Learning{}, false, &Refusal{Reason: "a retraction names its reason — a memory taken back without a sentence saying why is the silent delete this slice exists to forbid (LM2: reason is required, 1..256 bytes)"}
	}
	if len(reason) > 256 {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("a retraction reason is 1..256 bytes — %d is not a reason, and the stamp column caps it at 256 (LM2)", len(reason))}
	}
	row, ok, err := l.s.GetLearning(id)
	if err != nil {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learnings: reading learning %d for retraction: %v", id, err)}
	}
	if !ok {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learning %d does not exist — nothing was retracted (history, not a guess: a retract names a row that is there)", id)}
	}
	got, found, changed, err := l.s.SetLearningRetraction(id, true, reason, actor.Kind, actor.ID,
		newChange("retract", fmt.Sprintf("retracted learning %d (scope %s/%s)", id, row.Scope, row.ScopeKey), actor))
	if err != nil {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learnings: the store refused the retraction: %v", err)}
	}
	if !found {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learning %d does not exist — nothing was retracted", id)}
	}
	out, err := FromRow(got)
	if err != nil {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learnings: the row that came back is not a legal learning: %v", err)}
	}
	if changed {
		if _, err := l.s.DropLearningVectors(id); err != nil {
			// The stamp COMMITTED — the body is out of every default read.
			// The vector drop is the hygiene half that then failed; say so
			// exactly, because "retraction failed" would be a lie and
			// silence about the index is the other kind of leak.
			return out, true, &Refusal{Reason: fmt.Sprintf("retracted learning %d, but its similarity vector could not be dropped: %v — the body is out of every default read; similarity can still rank it until the next embedding pass clears it (LM5)", id, err)}
		}
	}
	return out, changed, nil
}

// Restore reverses the stamp (LM2). The instant clears — retracted_at goes
// back to NULL, the state the default reads answer with; what the last
// retraction said (reason, by) survives beside the row as its recent
// history. The audit line records the restore; the body never moved, so
// there is nothing to re-create.
//
// The similarity vector is NOT re-made here on the spot: the restore hands
// the body back to the default reads, and the next embedding pass (or
// learnings.embed, called now) re-adds it — EmbedPending picks up any
// active learning without a vector, and a retracted one was never eligible
// because EmbedPending reads through the same live predicate. No second
// embedding mechanism, LM5's return-to-index stated honestly.
func (l *Learnings) Restore(id int64, actor Actor) (Learning, bool, *Refusal) {
	if r := humanDoor(actor, "restore"); r != nil {
		return Learning{}, false, r
	}
	row, ok, err := l.s.GetLearning(id)
	if err != nil {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learnings: reading learning %d for restore: %v", id, err)}
	}
	if !ok {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learning %d does not exist — nothing was restored", id)}
	}
	got, found, changed, err := l.s.SetLearningRetraction(id, false, "", actor.Kind, actor.ID,
		newChange("restore", fmt.Sprintf("restored learning %d (scope %s/%s)", id, row.Scope, row.ScopeKey), actor))
	if err != nil {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learnings: the store refused the restore: %v", err)}
	}
	if !found {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learning %d does not exist — nothing was restored", id)}
	}
	out, err := FromRow(got)
	if err != nil {
		return Learning{}, false, &Refusal{Reason: fmt.Sprintf("learnings: the row that came back is not a legal learning: %v", err)}
	}
	return out, changed, nil
}

// newChange builds the audit line for a stamp act: op retract|restore (the
// learning_id is named by the store's own WHERE, so the change line rides
// with LearningID 0 and the store's check refuses only a caller who names a
// DIFFERENT row), the fixed template summary, and the actor the door
// stamped. The templates are the same discipline as the add's: the audit
// says what happened in the daemon's words — never the body, never the
// human's reason text (the reason lives in the row's stamp, where LM2
// reads it back).
func newChange(op, summary string, a Actor) *store.NewLearningChange {
	return &store.NewLearningChange{Op: op, Summary: summary, ActorKind: a.Kind, ActorID: a.ID}
}

// humanDoor is LM8 in one sentence: retract and restore are the human
// door's acts. The kind set is the same one that may say `told` — a person
// at the endpoint or at a terminal — because retracting memory is saying
// "a person decided this is wrong", and an agent or a hook saying it is
// the label lying about its source again. Agents get T4's proposal kind.
func humanDoor(a Actor, act string) *Refusal {
	if !ValidActorKind(a.Kind) {
		return &Refusal{Reason: fmt.Sprintf("actor kind %q is not one of the four — user, cli, agent, system (S52 LM4)", a.Kind)}
	}
	if a.ID == "" {
		return &Refusal{Reason: "an actor names itself — an empty actor cannot " + act + " anything (S52 LM3)"}
	}
	if !canSayTold(a.Kind) {
		return &Refusal{Reason: fmt.Sprintf("actor %s/%q may not %s a learning — retract and restore are the human door's acts (S52 LM8): only user and cli carry a person; an agent proposes a retraction (T4's learning_retract kind), never performs one", a.Kind, a.ID, act)}
	}
	return nil
}

// Change is one learning_change audit line — LM3's history row as the
// domain reads it. The wire door and the CLI render these; nothing else
// may, because the audit line is a sentence about an act, not a join to
// re-derive from.
type Change struct {
	ID         int64
	LearningID int64
	Op         string
	Summary    string
	ActorKind  string
	ActorID    string
	OccurredAt string
}

// History reads one learning's audit lines, oldest first — the whole
// career of the memory (add, retractions, restores) in the daemon's own
// fixed-template sentences, the body never among them.
func (l *Learnings) History(id int64) ([]Change, error) {
	rows, err := l.s.ListLearningChanges(id)
	if err != nil {
		return nil, err
	}
	out := make([]Change, 0, len(rows))
	for _, r := range rows {
		out = append(out, Change{
			ID: r.ID, LearningID: r.LearningID, Op: r.Op, Summary: r.Summary,
			ActorKind: r.ActorKind, ActorID: r.ActorID, OccurredAt: r.OccurredAt,
		})
	}
	return out, nil
}
