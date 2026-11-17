package daemon

import (
	"context"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// loopQuestionsAdapter serves `loop.questions` (S76): the questions of the step
// that asked last, answered and pending, oldest first.
type loopQuestionsAdapter struct{ st *store.Store }

func (a loopQuestionsAdapter) QuestionBlock(_ context.Context, loopID string, all bool) (protocol.LoopQuestionsResult, error) {
	l, err := a.st.ReadLoopByID(loopID)
	if err != nil {
		return protocol.LoopQuestionsResult{}, err
	}
	sc := store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}
	if all {
		rows, err := a.st.LoopQuestions(sc)
		if err != nil {
			return protocol.LoopQuestionsResult{}, err
		}
		out := protocol.LoopQuestionsResult{Questions: make([]protocol.LoopQuestionView, 0, len(rows))}
		for _, q := range rows {
			out.Questions = append(out.Questions, questionView(q))
		}
		return out, nil
	}
	// The block is the latest step that asked. Pending first (a parked step is
	// what a reopened chat is about), else the newest asking step.
	var step int64
	if q, ok, err := a.st.PendingQuestion(sc); err != nil {
		return protocol.LoopQuestionsResult{}, err
	} else if ok && q.StepID != nil {
		step = *q.StepID
	}
	if step == 0 {
		return protocol.LoopQuestionsResult{Questions: []protocol.LoopQuestionView{}}, nil
	}
	rows, err := a.st.StepQuestions(step)
	if err != nil {
		return protocol.LoopQuestionsResult{}, err
	}
	out := protocol.LoopQuestionsResult{Questions: make([]protocol.LoopQuestionView, 0, len(rows))}
	for _, q := range rows {
		out.Questions = append(out.Questions, questionView(q))
	}
	return out, nil
}

func questionView(q store.Question) protocol.LoopQuestionView {
	v := protocol.LoopQuestionView{
		ID: q.ID, ToolUseID: q.ToolUseID, Kind: q.Kind, Prompt: q.Body, Answers: q.Options,
		Answer: q.Answer, AskedAt: q.AskedAt, AnsweredAt: q.AnsweredAt,
	}
	if q.StepID != nil {
		v.StepID = *q.StepID
	}
	return v
}
