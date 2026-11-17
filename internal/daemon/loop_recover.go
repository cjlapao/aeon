package daemon

import (
	"context"
	"errors"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// loop_recover.go — loop.recover, in place when it can be.
//
// A blocked step keeps its transcript. Recovery tells the agent, inside that conversation, that something went wrong and to
// try the last step again, then lets the runner run the same step from its checkpoint: the agent keeps its context, and
// nothing is dispatched fresh. When there is no transcript to continue (the step never started), it falls back to running
// the step again from a clean receipt.

// closeDanglingToolUses answers any tool call the last assistant message made that never got a result (the step stopped
// before it could), so the transcript is valid again and the model is told those calls were not completed.
func closeDanglingToolUses(msgs []model.Message, reason string) []model.Message {
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != model.RoleAssistant {
		return msgs
	}
	var results []model.Block
	for _, b := range msgs[len(msgs)-1].Content {
		if b.Kind == model.BlockToolUse {
			results = append(results, model.ToolResultBlock(b.ID, "not completed: "+reason, true))
		}
	}
	if len(results) == 0 {
		return msgs
	}
	return append(append([]model.Message{}, msgs...), model.Message{Role: model.RoleUser, Content: results})
}

func (d *Daemon) recoverBlocked(ctx context.Context, dep loop.Deps, id string) (any, error) {
	st, err := loop.Pinned(ctx, d.store, id)
	if err != nil {
		return nil, err
	}
	if r := loop.CheckRecover(st); r != nil {
		return map[string]any{"refused": map[string]string{"kind": r.Kind, "detail": r.Detail}}, nil
	}
	if stepID := loop.BlockedStepID(st); stepID > 0 && d.Table.Questions != nil {
		status, _, serr := d.Table.Questions.StepState(stepID)
		if serr == nil && status != string(store.StepRunning) && status != string(store.StepSuspended) {
			snapshot, lerr := d.Table.Questions.LoadResume(stepID)
			if lerr == nil && len(snapshot.Messages) > 0 {
				reason := st.LastReason
				msgs := appendResumeUserText(closeDanglingToolUses(snapshot.Messages, reason), loop.RecoverNote(reason))
				// The continue seam resumes a SUSPENDED step; a blocked one is parked there first, then continued with the note.
				if err := d.store.SetStepStatus(stepID, store.StepSuspended, ""); err == nil {
					if err := d.Table.Questions.ContinueWithMessages(stepID, msgs); err == nil {
						if err := loop.RecoverInPlace(ctx, dep, id); err != nil {
							var r *loop.RetryRefusal
							if errors.As(err, &r) {
								return map[string]any{"refused": map[string]string{"kind": r.Kind, "detail": r.Detail}}, nil
							}
							return nil, err
						}
						if d.loopRunner != nil {
							d.loopRunner.Start(d.loopCtx, id)
						}
						return map[string]any{"recovered": true, "how": "in-place", "status": "running"}, nil
					}
				}
			}
		}
	}
	res, err := loop.RecoverBlockedStep(ctx, dep, id)
	var r *loop.RetryRefusal
	if errors.As(err, &r) {
		return map[string]any{"refused": map[string]string{"kind": r.Kind, "detail": r.Detail}}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"recovered": true, "how": "redispatch", "status": res.Status, "position": res.Position}, nil
}
