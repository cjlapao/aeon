package protocol

// loop_questions.go — S76: the whole block of questions a parked step asked.
//
// `question.pending` answers ONE question (the oldest unanswered), which is right
// for a monitor deciding whether to visit. A person reopening a chat needs the
// block: every question the step asked, with the answers already given, so the
// remaining ones can be answered in place and nothing already answered is asked
// again. This read is that block; it is conditionally registered with the seam
// (V13) and answers an empty list, never an error, for a loop with no questions.

import (
	"context"
	"encoding/json"
	"fmt"
)

// LoopQuestionView is one question of the block.
type LoopQuestionView struct {
	ID     int64 `json:"id"`
	StepID int64 `json:"stepId,omitempty"`
	// ToolUseID names the tool call that asked it, so a replayed transcript can
	// draw the answered card where the call was.
	ToolUseID  string   `json:"toolUseId,omitempty"`
	Kind       string   `json:"kind"`
	Prompt     string   `json:"prompt"`
	Answers    []string `json:"answers"`
	Answer     string   `json:"answer,omitempty"`
	AskedAt    string   `json:"askedAt"`
	AnsweredAt string   `json:"answeredAt,omitempty"`
}

// LoopQuestionsParams names the loop by the id `loop.list` prints.
type LoopQuestionsParams struct {
	ID string `json:"id"`
	// All asks for every question the loop ever asked, not only the latest
	// block, so a replay of a finished loop shows each one with its answer.
	All bool `json:"all,omitempty"`
}

// LoopQuestionsResult is the latest block: the questions of the step that asked
// last, oldest first.
type LoopQuestionsResult struct {
	Questions []LoopQuestionView `json:"questions"`
}

// LoopQuestions is the daemon's adapter.
type LoopQuestions interface {
	QuestionBlock(ctx context.Context, loopID string, all bool) (LoopQuestionsResult, error)
}

// RegisterLoopQuestions registers the read when the seam is wired.
func (t *Table) RegisterLoopQuestions() bool {
	if t.LoopQuestions == nil {
		return false
	}
	t.register(&Method{Name: MethodLoopQuestions, RequiresInitialize: true, Handle: t.handleLoopQuestions})
	return true
}

func (t *Table) handleLoopQuestions(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if t.LoopQuestions == nil {
		return nil, ErrNotWired.WithWhat("the loop question block is not wired — the daemon started without a store")
	}
	var p LoopQuestionsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — name the loop whose questions to read")}
	}
	res, err := t.LoopQuestions.QuestionBlock(ctx, p.ID, p.All)
	if err != nil {
		return nil, err
	}
	if res.Questions == nil {
		res.Questions = []LoopQuestionView{}
	}
	for i := range res.Questions {
		if res.Questions[i].Answers == nil {
			res.Questions[i].Answers = []string{}
		}
	}
	return res, nil
}
