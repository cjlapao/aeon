package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// QuestionStore adapts the existing transactional question and transcript
// operations without exposing storage types to the protocol package.
type QuestionStore struct{ store *store.Store }

func (s *QuestionStore) CheckStepQuestion(stepID, questionID int64) (protocol.AnsweredQuestion, error) {
	var q store.Question
	var found bool
	var err error
	if questionID > 0 {
		q, err = s.store.ReadQuestion(questionID)
		if err != nil {
			return protocol.AnsweredQuestion{}, err
		}
		found = q.StepID != nil && *q.StepID == stepID
	} else {
		q, found, err = s.store.PendingStepQuestion(stepID)
		if err != nil {
			return protocol.AnsweredQuestion{}, err
		}
		if !found {
			q, found, err = s.store.LatestStepQuestion(stepID)
			if err != nil {
				return protocol.AnsweredQuestion{}, err
			}
		}
	}
	if !found {
		return protocol.AnsweredQuestion{Refusal: fmt.Sprintf("step %d has no pending question", stepID)}, nil
	}
	if q.Answered() {
		return protocol.AnsweredQuestion{Refusal: fmt.Sprintf("question %d was already answered at %s", q.ID, q.AnsweredAt), AnsweredAt: q.AnsweredAt}, nil
	}
	return protocol.AnsweredQuestion{}, nil
}

func NewQuestionStore(s *store.Store) *QuestionStore {
	if s == nil {
		return nil
	}
	return &QuestionStore{store: s}
}

func (s *QuestionStore) AskStep(stepID int64, slug, root string, q runtime.PendingQuestion) (int64, error) {
	row, err := s.store.Ask(store.Scope{LoopSlug: slug, ComponentRoot: root}, store.NewQuestion{
		StepID: &stepID, ToolUseID: q.ToolUseID, Kind: store.QuestionKind(q.Kind), Body: q.Body, Options: q.Options,
	})
	return row.ID, err
}

func (s *QuestionStore) AnswerStep(stepID, questionID int64, answer string) (protocol.AnsweredQuestion, error) {
	var q store.Question
	var err error
	if questionID > 0 {
		q, err = s.store.ReadQuestion(questionID)
		if err != nil {
			return protocol.AnsweredQuestion{}, err
		}
		if q.StepID == nil || *q.StepID != stepID {
			return protocol.AnsweredQuestion{}, fmt.Errorf("question %d does not belong to step %d", questionID, stepID)
		}
	} else {
		var ok bool
		q, ok, err = s.store.PendingStepQuestion(stepID)
		if err != nil {
			return protocol.AnsweredQuestion{}, err
		}
		if !ok {
			latest, found, err := s.store.LatestStepQuestion(stepID)
			if err != nil {
				return protocol.AnsweredQuestion{}, err
			}
			if found && latest.Answered() {
				return protocol.AnsweredQuestion{Refusal: fmt.Sprintf("question %d was already answered at %s", latest.ID, latest.AnsweredAt), AnsweredAt: latest.AnsweredAt}, nil
			}
			return protocol.AnsweredQuestion{Refusal: fmt.Sprintf("step %d has no pending question", stepID)}, nil
		}
	}
	result, err := s.store.Answer(q.ID, answer)
	if err != nil {
		return protocol.AnsweredQuestion{}, err
	}
	if !result.OK {
		return protocol.AnsweredQuestion{Refusal: result.Reason, AnsweredAt: result.Question.AnsweredAt}, nil
	}
	return protocol.AnsweredQuestion{Question: runtime.PendingQuestion{
		ID: result.Question.ID, Kind: result.Question.Kind, Body: result.Question.Body,
		Options: result.Question.Options, ToolUseID: result.Question.ToolUseID,
	}, Answer: result.Question.Answer, AnsweredAt: result.Question.AnsweredAt}, nil
}

func (s *QuestionStore) LoadResume(stepID int64) (protocol.ResumeSnapshot, error) {
	st, err := s.store.ReadStep(stepID)
	if err != nil {
		return protocol.ResumeSnapshot{}, err
	}
	tr := s.store.ReadTranscript(stepID)
	if !tr.OK {
		return protocol.ResumeSnapshot{}, fmt.Errorf("transcript of step %d: %s", stepID, tr.Reason())
	}
	return protocol.ResumeSnapshot{Manifest: st.ContextManifestRaw, Messages: tr.Messages}, nil
}

// StepRoute answers Q6 for the answer door (S18 T8): a step owned by a loop
// resumes on the loop's CURRENT route — the same RuntimeState.Route the next
// dispatch reads — not the route name frozen in the step's saved request.
// A step no loop owns (an ask) has no newer opinion than its own row, and a
// state that cannot be read returns ok=false so the caller keeps the saved
// name rather than guessing.
func (s *QuestionStore) StepRoute(stepID int64) (string, bool) {
	st, err := s.store.ReadStep(stepID)
	if err != nil || st.LoopSlug == "" || st.ComponentRoot == "" {
		return "", false
	}
	l, err := s.store.GetLoop(store.Scope{LoopSlug: st.LoopSlug, ComponentRoot: st.ComponentRoot})
	if err != nil || l.ID == "" {
		return "", false
	}
	pin, err := s.store.ReadPinnedLoop(l.ID)
	if err != nil {
		return "", false
	}
	var saved loop.RuntimeState
	if json.Unmarshal([]byte(pin.RuntimeJSON), &saved) != nil {
		return "", false
	}
	return saved.Route, saved.Route != ""
}

func (s *QuestionStore) ReplaceStepMessages(stepID int64, msgs []model.Message) error {
	tr := s.store.ReadTranscript(stepID)
	if !tr.OK {
		return fmt.Errorf("transcript of step %d: %s", stepID, tr.Reason())
	}
	if tr.Count == 0 {
		return s.store.AppendMessages(stepID, msgs)
	}
	return s.store.ReplaceRange(stepID, 1, int64(tr.Count), msgs)
}

func (s *QuestionStore) MarkRunning(stepID int64) error {
	if err := s.store.SetStepStatus(stepID, store.StepRunning, ""); err != nil {
		return err
	}
	// A running step never carries a stale pause reason from the time it was
	// not. Cleared unconditionally — harmless on the real-question path,
	// where it was already empty, and the one thing `step.continue`'s path
	// depends on to be true afterwards.
	return s.store.SetStepPauseReason(stepID, "")
}

func (s *QuestionStore) StepState(stepID int64) (status, pauseReason string, err error) {
	st, err := s.store.ReadStep(stepID)
	if err != nil {
		return "", "", err
	}
	return st.Status, st.PauseReason, nil
}

// ContinueWithMessages is `step.continue {message}`'s exactly-once seam
// (S31 D6): the flip to running and the transcript replacement happen in ONE
// store transaction, so the person's message is neither lost by a crash nor
// doubled by a retry. The refusal it returns when the row is no longer
// suspended is the same guard, playing the race-net role MarkRunning never
// had (MarkRunning's two-write shape predates this requirement; it stays for
// the message-less path, where there is nothing to deliver exactly once).
func (s *QuestionStore) ContinueWithMessages(stepID int64, msgs []model.Message) error {
	return s.store.ContinueStepWithMessages(context.Background(), stepID, msgs)
}
