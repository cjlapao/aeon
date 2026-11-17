package protocol

import (
	"context"
	"encoding/json"
	"errors"
)

// S38 T15 (OL11): the three decisions. Their MethodName consts live in
// method.go beside the whole table (protogen reads the consts there). They are NOT catalog actions — no
// step can hold these tools (a grep guard in daemon says so every run) — and
// they are the ONLY methods that reach the store's Decide* doors. The actor
// is never in params: decodeParams refuses unknown fields, and the daemon
// stamps store.Actor{Kind:"user", ID:<peer uid>} from the connection.

// DecideView is store.DecideResult as the wire sees it. A second decision
// is not an error: Changed=false with the first decider's who/when is the
// answer, because claimed-FROM keeps the ledger's verdict stable and the
// person asking deserves to learn who got there first.
type DecideView struct {
	Changed   bool   `json:"changed"`
	State     string `json:"state"`
	DecidedBy string `json:"decidedBy,omitempty"`
	DecidedAt string `json:"decidedAt,omitempty"`
}

type GoalDecideParams struct {
	ID       string `json:"id"`
	Version  int    `json:"version"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

type BriefDecideParams struct {
	LoopID string `json:"loopId"`
	// Component optionally disambiguates the slug — two components may
	// run the same slug, and a person naming one names its root (S38 T16).
	Component string `json:"component,omitempty"`
	Version   int    `json:"version"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason,omitempty"`
}

type TaskDecideParams struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

var (
	errDecideShape        = errors.New("a decision names its record and its version")
	errDecisionVocabulary = errors.New(`refused: decision is not one of approved, rejected, parked`)
)

func newSentenceError(sentence string) error { return errors.New(sentence) }

var reviewDecisions = map[string]bool{"approved": true, "rejected": true, "parked": true}
var approvalDecisions = map[string]bool{"approved": true, "rejected": true}

func (t *Table) handleGoalDecide(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p GoalDecideParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID == "" || p.Version <= 0 {
		return nil, ErrBadParams{Err: errDecideShape}
	}
	if !reviewDecisions[p.Decision] {
		return nil, ErrBadParams{Err: errDecisionVocabulary}
	}
	if t.Records == nil {
		return nil, ErrStoreUnavailable.WithWhat("records are unavailable in this runtime")
	}
	view, sentence, err := t.Records.DecideGoal(ctx, sess, p)
	if err != nil {
		return nil, err
	}
	if sentence != "" {
		return nil, ErrBadParams{Err: newSentenceError(sentence)}
	}
	return view, nil
}

func (t *Table) handleBriefDecide(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p BriefDecideParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.LoopID == "" || p.Version <= 0 {
		return nil, ErrBadParams{Err: errDecideShape}
	}
	if !reviewDecisions[p.Decision] {
		return nil, ErrBadParams{Err: errDecisionVocabulary}
	}
	if t.Records == nil {
		return nil, ErrStoreUnavailable.WithWhat("records are unavailable in this runtime")
	}
	view, sentence, err := t.Records.DecideBrief(ctx, sess, p)
	if err != nil {
		return nil, err
	}
	if sentence != "" {
		return nil, ErrBadParams{Err: newSentenceError(sentence)}
	}
	return view, nil
}

func (t *Table) handleTaskDecide(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p TaskDecideParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: errDecideShape}
	}
	if !approvalDecisions[p.Decision] {
		return nil, ErrBadParams{Err: newSentenceError(`refused: decision "` + p.Decision + `" is not one of approved, rejected — a task approval has no third state`)}
	}
	if t.Records == nil {
		return nil, ErrStoreUnavailable.WithWhat("records are unavailable in this runtime")
	}
	view, sentence, err := t.Records.DecideTask(ctx, sess, p)
	if err != nil {
		return nil, err
	}
	if sentence != "" {
		return nil, ErrBadParams{Err: newSentenceError(sentence)}
	}
	return view, nil
}
