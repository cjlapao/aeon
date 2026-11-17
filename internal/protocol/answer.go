package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/runtime"
)

// AnswerParams identifies the suspended step. QuestionID is optional; when it
// is absent the oldest pending question for this step is answered.
type AnswerParams struct {
	StepID     int64  `json:"stepId"`
	QuestionID int64  `json:"questionId,omitempty"`
	Answer     string `json:"answer"`
}

type AnswerResponse struct {
	StepID     int64  `json:"stepId"`
	QuestionID int64  `json:"questionId"`
	AnsweredAt string `json:"answeredAt"`
	Status     string `json:"status"`
}

type resumeRunner interface {
	Resume(context.Context, runtime.ResumeRequest, runtime.RunOpts) runtime.StepResult
}

func (t *Table) handleAnswer(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p AnswerParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// The connection's own actor, never a param: a rule answer writes policy as THIS person.
	res, err := t.answerQuestion(ctx, func() Actor { return t.ActorFor(ctx, sess) }, p)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// AnswerQuestion is the answer door opened from INSIDE the process
// (S70 T2d): a peer's posted reply answers the waiting caller's question
// through the SAME guards, the same answer row, the same MarkRunning, the
// same queued continuation as the socket door — one truth, two doors. The
// socket's handleAnswer decodes and calls this; nothing bypasses the
// Recovering check, the pending-question check, or the durable-state
// validation, because there is no second copy of them.
func (t *Table) AnswerQuestion(p AnswerParams) (AnswerResponse, error) {
	return t.answerQuestion(context.Background(), nil, p)
}

// answerQuestion is the one answer path. `actorOf` is non-nil only from the socket door, where a connection (and
// so an actor) exists: only then may an answer also write a rule. The in-process door (a peer's reply)
// answers questions with text and can never author policy.
func (t *Table) answerQuestion(ctx context.Context, actorOf func() Actor, p AnswerParams) (AnswerResponse, error) {
	human := actorOf != nil
	if p.StepID <= 0 {
		return AnswerResponse{}, ErrBadParams{Err: fmt.Errorf("stepId must be positive")}
	}
	if t.Recovering != nil && t.Recovering(p.StepID) {
		return AnswerResponse{}, ErrStepRecovering.WithWhat("step %d is recovering", p.StepID)
	}
	if t.Questions == nil || t.Steps == nil {
		return AnswerResponse{}, ErrStoreUnavailable
	}
	state, err := t.Questions.CheckStepQuestion(p.StepID, p.QuestionID)
	if err != nil {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d's question could not be read: %v", p.StepID, err)
	}
	if state.Refusal != "" {
		if state.AnsweredAt != "" {
			return AnswerResponse{}, ErrQuestionAnswered.WithWhat("%s", state.Refusal)
		}
		return AnswerResponse{}, ErrNoPendingQuestion.WithWhat("%s", state.Refusal)
	}
	runner, ok := t.Runner.(resumeRunner)
	if !ok {
		return AnswerResponse{}, ErrStepUnavailable
	}

	// Read and validate the durable state before accepting an irreversible
	// answer. A missing route or transcript must leave the question pending.
	snapshot, err := t.Questions.LoadResume(p.StepID)
	if err != nil {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d cannot be resumed: %v", p.StepID, err)
	}
	var saved stepSnapshot
	if err := json.Unmarshal(snapshot.Manifest, &saved); err != nil || saved.Request.WorkspaceRoot == "" {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d has no usable saved request and manifest", p.StepID)
	}
	// Q6, answered (S18 T8): a loop-owned step resumes on the LOOP's current
	// route, not the one the step began on. If either half of S6/S7 had kept
	// reading the step's saved route name, a loop could never be resumed on
	// another provider at all. A step outside any loop has no newer opinion
	// than its own row — the saved name stands.
	routeName := saved.Request.Route.ID
	if cur, ok := t.Questions.StepRoute(p.StepID); ok && cur != "" {
		routeName = cur
	}
	route, err := t.routeForStep(routeName)
	if err != nil {
		return AnswerResponse{}, err
	}
	// The canonical transcript can be replayed through the route's current
	// provider. Replacing the whole route allows a user to move a suspended
	// step to another provider under the same route id before answering.
	saved.Request.Route = withThinking(route, saved.Request.Thinking)
	granted := saved.Request.Handler.Tools
	manifest := saved.ContextManifest
	toolset, _, err := t.Runner.BuildTools(granted, saved.Request.WorkspaceRoot, saved.Request.Handler.Name,
		runtime.FrozenManifest{Entries: manifest.Policies, Hash: manifest.PoliciesHash, Frozen: manifest.PoliciesFrozen})
	if err != nil {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d cannot rebuild its tool grant: %v", p.StepID, err)
	}
	// MD5: the resumed toolset may carry cached MCP server children (a composite
	// caller); close them when resume finishes — not immediately after
	// BuildTools. A no-op when no MCP bundle is wired (MD8).
	defer func() {
		if c, ok := toolset.(io.Closer); ok {
			_ = c.Close()
		}
	}()

	// A permission question's rule answers (approve for this loop / always allow / always deny) write their
	// rule FIRST, at the human door, and only then is the question answered: a rule that could not be
	// written leaves the question pending with the reason, never a half-done approval.
	if state.Question.Kind == "permission" && IsRuleAnswer(p.Answer) {
		if !human {
			return AnswerResponse{}, ErrBadParams{Err: fmt.Errorf("%q is a person's answer and is not accepted on this door", p.Answer)}
		}
		if !offered(state.Question.Options, p.Answer) {
			return AnswerResponse{}, ErrBadParams{Err: fmt.Errorf("%q is not one of this question's answers %v", p.Answer, state.Question.Options)}
		}
		use, ok := blockedUse(snapshot.Messages, state.Question.ToolUseID)
		if !ok {
			return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d's blocked call could not be found, so no rule can name it", p.StepID)
		}
		subject := saved.Request.Handler.Name
		if t.PolicySubject != nil {
			subject = t.PolicySubject(subject)
		}
		if err := t.authorRuleAnswer(ctx, actorOf(), p.Answer, subject, saved.LoopSlug, use); err != nil {
			return AnswerResponse{}, err
		}
	}
	answered, err := t.Questions.AnswerStep(p.StepID, p.QuestionID, p.Answer)
	if err != nil {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("step %d could not record its answer: %v", p.StepID, err)
	}
	if answered.Refusal != "" {
		if answered.AnsweredAt != "" {
			return AnswerResponse{}, ErrQuestionAnswered.WithWhat("%s", answered.Refusal)
		}
		return AnswerResponse{}, ErrNoPendingQuestion.WithWhat("%s", answered.Refusal)
	}
	if err := t.Questions.MarkRunning(p.StepID); err != nil {
		return AnswerResponse{}, ErrResumeUnavailable.WithWhat("answer %d was saved but step %d could not be marked running: %v", answered.Question.ID, p.StepID, err)
	}
	if t.OnStepQuestionAnswered != nil {
		t.OnStepQuestionAnswered(p.StepID)
	}
	if t.OnEvent != nil {
		// answeredBy is who gave the answer: the person on the socket, or another door (a client acting for no
		// one). A reopened loop reads it to say "approved by you" only for what a person approved.
		answeredBy := "person"
		if !human {
			answeredBy = "client"
		}
		t.OnEvent("question.answered", map[string]any{"stepId": p.StepID, "questionId": answered.Question.ID, "answer": answered.Answer, "answeredAt": answered.AnsweredAt, "answeredBy": answeredBy, "kind": answered.Question.Kind})
	}
	req := runtime.ResumeRequest{Request: saved.Request, Messages: snapshot.Messages, Manifest: saved.ContextManifest,
		Question: answered.Question, Answer: permissionResume(answered.Question.Kind, answered.Answer), Usage: saved.Usage, Compactions: saved.Compactions}
	// The request returns with only a queued continuation. Nothing in this
	// goroutine depends on the connection's context or lifetime — but it must
	// not outlive the STORE either, so it is tracked (continueAnsweredStepAsync)
	// rather than spawned bare: a caller closing the store waits on it first.
	t.continueAnsweredStepAsync(p.StepID, saved.LoopSlug, runner, req, toolset)
	return AnswerResponse{StepID: p.StepID, QuestionID: answered.Question.ID, AnsweredAt: answered.AnsweredAt, Status: "running"}, nil
}

// --- S16: the code-action ask resolves through the SAME door -------------
//
// A code-action ask (S16's effect classes) reuses S11's permission kind
// (Q5's answer): same affordance, same answer verbs, same resume path — a
// fourth kind is only warranted when something must DISPATCH differently,
// and nothing does. What stays here rather than in the action package is
// the VOCABULARY, beside the answer handler that consumes it: an ask whose
// options drifted from the answerer's verbs is a question that can never
// be answered, and both halves belong in one file for that reason.

// ActionAskOptions are the answer verbs for a code-action ask — exactly
// S11's permission verbs (runtime/loop.go's ["approve","deny"]), pinned by
// TestActionAskReusesThePermissionVerbs so the two cannot drift.
var ActionAskOptions = []string{"approve", "deny"}

// ResolveActionAsk maps an answer to the execution decision the loop's
// action gate will honour. Anything that is not exactly "approve" is a
// deny — an unrecognised answer never executes an outward-facing effect;
// silence is not consent, and neither is a typo.
func ResolveActionAsk(answer string) (execute bool, err error) {
	switch answer {
	case "approve":
		return true, nil
	case "deny":
		return false, nil
	default:
		return false, fmt.Errorf("refused: %q is not an answer to an action ask — the verbs are approve and deny", answer)
	}
}

// permissionResume is what a step resumes with: for a permission question the rule answers collapse to the one
// verb the runtime acts on; every other kind of question keeps the person's words as they are.
func permissionResume(kind, answer string) string {
	if kind == "permission" {
		return resumeVerb(answer)
	}
	return answer
}
