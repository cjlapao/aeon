package protocol

import (
	"context"
	"encoding/json"
)

// S38 T15 (criteria 8, 10): the board read and the human's edit rule.
// task.list mirrors the store's board filter — names, not ids, cross the
// wire, and the daemon resolves them — and the cost a row carries is the
// store's own floor-flagged figure, never a client-side sum (P32).

type TaskListParams struct {
	Root          string `json:"root,omitempty"`
	Component     string `json:"component,omitempty"`
	Assignee      string `json:"assignee,omitempty"`
	AssigneeKind  string `json:"assigneeKind,omitempty"`
	Status        string `json:"status,omitempty"`
	Type          string `json:"type,omitempty"`
	Manual        *bool  `json:"manual,omitempty"`
	ApprovalState string `json:"approvalState,omitempty"`
	Goal          string `json:"goal,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

// TaskView is one board row: the task fields the surfaces render plus the
// floor-flagged cost and the loops an org task is worked by.
type TaskView struct {
	ID            string   `json:"id"`
	TaskID        string   `json:"taskid"`
	LoopSlug      string   `json:"loopSlug"`
	ComponentRoot string   `json:"componentRoot"`
	ComponentID   string   `json:"componentId,omitempty"`
	Title         string   `json:"title"`
	Type          string   `json:"type,omitempty"`
	Tech          string   `json:"tech,omitempty"`
	Status        string   `json:"status"`
	Manual        bool     `json:"manual"`
	AssigneeKind  string   `json:"assigneeKind,omitempty"`
	Assignee      string   `json:"assignee,omitempty"`
	GoalID        string   `json:"goalId,omitempty"`
	ApprovalState string   `json:"approvalState"`
	DependsOn     []string `json:"dependsOn,omitempty"`
	LoopSlugs     []string `json:"loopSlugs"`
	CostUSD       float64  `json:"costUsd"`
	CostIsFloor   bool     `json:"costIsFloor"`
	CreatedByID   string   `json:"createdById,omitempty"`
	CreatedAt     string   `json:"createdAt,omitempty"`
	UpdatedAt     string   `json:"updatedAt"`
}

type TaskListResult struct {
	Tasks []TaskView `json:"tasks"`
}

func (t *Table) handleTaskList(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p TaskListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Limit < 0 {
		return nil, ErrBadParams{Err: newSentenceError("refused: limit must not be negative")}
	}
	if t.Records == nil {
		return nil, ErrStoreUnavailable.WithWhat("records are unavailable in this runtime")
	}
	res, err := t.Records.TaskList(ctx, sess, p)
	if err != nil {
		return nil, err
	}
	if res.Tasks == nil {
		res.Tasks = []TaskView{}
	}
	return res, nil
}

// TaskChangedPayload is the board's one kind (OL14): every transition —
// created (previous ""), moved, assigned, decided — is task.changed with the
// previous state inside it, so a client that missed one rung can still tell
// what moved. It rides the project filter like every other real event: a
// task belongs to a component, so the notification belongs to that root.
type TaskChangedPayload struct {
	ID          string `json:"id"`
	Manual      bool   `json:"manual"`
	Status      string `json:"status"`
	Previous    string `json:"previous,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
	LoopSlug    string `json:"loopSlug,omitempty"`
	ComponentID string `json:"componentId,omitempty"`

	componentRoot string
}

// EventComponentRoot marks the payload as rooted — the daemon-wide default
// is reserved for events that genuinely have no project.
func (p TaskChangedPayload) EventComponentRoot() string { return p.componentRoot }
