package protocol

import (
	"context"
	"encoding/json"
)

// S38 T15 (criterion 10): goal.show answers "what is this goal and where is
// it" from the rows alone. executionState is DERIVED — no column stores it
// (a grep guard pins that every run) — and the cost is the floor it is,
// never a confident total (P32).

type GoalShowParams struct {
	ID string `json:"id"`
}

type GoalView struct {
	ID            string `json:"id"`
	RootID        string `json:"rootId"`
	EpicID        string `json:"epicId,omitempty"`
	Version       int    `json:"version"`
	Title         string `json:"title"`
	Type          string `json:"type"`
	BodyNote      string `json:"bodyNote"`
	SourceKind    string `json:"sourceKind"`
	SourceRef     string `json:"sourceRef"`
	SourceLoop    string `json:"sourceLoop,omitempty"`
	ReviewState   string `json:"reviewState"`
	CreatedByKind string `json:"createdByKind"`
	CreatedByID   string `json:"createdById"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type GoalComponentView struct {
	ComponentID string `json:"componentId"`
	Confidence  *int   `json:"confidence,omitempty"`
	Note        string `json:"note,omitempty"`
}

type AttachmentView struct {
	ID          int64  `json:"id"`
	SubjectKind string `json:"subjectKind"`
	SubjectID   string `json:"subjectId"`
	Document    string `json:"document"`
	Rev         int    `json:"rev"`
	Currency    string `json:"currency"`
	HeadRev     int    `json:"headRev"`
	CreatedAt   string `json:"createdAt"`
}

type ReviewView struct {
	ID             int64  `json:"id"`
	SubjectKind    string `json:"subjectKind"`
	SubjectID      string `json:"subjectId"`
	SubjectVersion int    `json:"subjectVersion"`
	Decision       string `json:"decision"`
	ByKind         string `json:"byKind"`
	ByID           string `json:"byId"`
	Reason         string `json:"reason"`
	DecidedAt      string `json:"decidedAt"`
}

// LoopStatusView is one loop working this goal, as the derivation reads it.
type LoopStatusView struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
	Status        string `json:"status"`
	Blueprint     string `json:"blueprint,omitempty"`
}

// GoalCost is the money truth of a goal's loops: a floor with a flag when
// any part of it is unpriced, never a number that hides the unknown (P32).
type GoalCost struct {
	USD     float64 `json:"usd"`
	IsFloor bool    `json:"isFloor"`
}

// GoalDocumentView is one document the goal OWNS — the head of a name across
// every loop that wrote one (the brainstorm that filed the goal, the loops
// that work it). The content is a read away: loop.read with this loop and
// component root and the name.
type GoalDocumentView struct {
	Name          string `json:"name"`
	Rev           int    `json:"rev"`
	Bytes         int    `json:"bytes"`
	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`
	WrittenBy     string `json:"writtenBy,omitempty"`
	WrittenAt     string `json:"writtenAt"`
}

type GoalShowResult struct {
	Goal           GoalView            `json:"goal"`
	Components     []GoalComponentView `json:"components"`
	Documents      []GoalDocumentView  `json:"documents"`
	Attachments    []AttachmentView    `json:"attachments"`
	Reviews        []ReviewView        `json:"reviews"`
	Loops          []LoopStatusView    `json:"loops"`
	ExecutionState string              `json:"executionState"`
	Cost           GoalCost            `json:"cost"`
}

// DeriveExecutionState is the whole of the derivation, pure over the loop
// statuses — a table a test can walk without a database. The words are the
// five states the criterion names; nothing outside them is ever answered.
func DeriveExecutionState(loops []LoopStatusView) string {
	if len(loops) == 0 {
		return "not_started"
	}
	var running, waitingOrBlocked, failed bool
	var doneCount int
	for _, l := range loops {
		switch l.Status {
		case "running":
			running = true
		case "waiting", "blocked":
			// "waiting ON" the thing that must happen next: a gate answer,
			// a question, the runner's own stop. The loop cannot move
			// without someone — blocked is the honest word (criterion 10's
			// ladder, read before "done").
			waitingOrBlocked = true
		case "done":
			doneCount++
		case "failed", "aborted":
			failed = true
		}
	}
	switch {
	case running:
		return "running"
	case waitingOrBlocked:
		return "blocked"
	case doneCount == len(loops):
		return "done"
	case failed:
		return "failed"
	default:
		// Every loop still "pending" — a goal nothing is running towards.
		// not_started reads it honestly.
		return "not_started"
	}
}

func (t *Table) handleGoalShow(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p GoalShowParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: newSentenceError("refused: goal.show names a goal")}
	}
	if t.Records == nil {
		return nil, ErrStoreUnavailable.WithWhat("records are unavailable in this runtime")
	}
	res, sentence, err := t.Records.GoalShow(ctx, sess, p)
	if err != nil {
		return nil, err
	}
	if sentence != "" {
		return nil, ErrBadParams{Err: newSentenceError(sentence)}
	}
	return res, nil
}
