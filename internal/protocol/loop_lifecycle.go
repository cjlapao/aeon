package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// # Archiving and deleting a loop
//
// `loop.setArchived` hides a loop from the lists and keeps everything (it can be shown again); its open work is closed,
// not done, and opened again on restore. `loop.delete` removes
// the loop and what hangs from it, and keeps what was already counted and the work: the component's telemetry is not
// touched, a tombstone keeps the loop's own totals, and tasks, roadmap items and queue items stay with their link to
// the loop cut. Both are the person's doors, and neither works on a loop that is running: stop it first.
// `loop.tombstones` lists what deletions left.

const (
	MethodLoopSetArchived MethodName = "loop.setArchived"
	MethodLoopDelete      MethodName = "loop.delete"
	MethodLoopTombstones  MethodName = "loop.tombstones"
)

// LoopSetArchivedParams hides (archived: true) or shows (false) a loop.
type LoopSetArchivedParams struct {
	ID       string `json:"id"`
	Archived bool   `json:"archived"`
	Note     string `json:"note,omitempty"`
}

// LoopSetArchivedResult says what is now true.
type LoopSetArchivedResult struct {
	ID         string `json:"id"`
	Archived   bool   `json:"archived"`
	ArchivedAt string `json:"archivedAt,omitempty"`
}

// LoopDeleteParams deletes a loop. Confirm must be the loop's slug: the daemon checks it, so a stray call cannot
// delete anything.
type LoopDeleteParams struct {
	ID      string `json:"id"`
	Confirm string `json:"confirm"`
	Note    string `json:"note,omitempty"`
}

// LoopTombstoneView is what a deleted loop left behind.
type LoopTombstoneView struct {
	LoopID        string   `json:"loopId"`
	Slug          string   `json:"slug"`
	ComponentRoot string   `json:"componentRoot"`
	Title         string   `json:"title"`
	Goal          string   `json:"goal,omitempty"`
	Status        string   `json:"status,omitempty"`
	StartedAt     string   `json:"startedAt,omitempty"`
	DeletedAt     string   `json:"deletedAt"`
	DeletedBy     string   `json:"deletedBy,omitempty"`
	Note          string   `json:"note,omitempty"`
	Steps         int      `json:"steps"`
	Turns         int      `json:"turns"`
	ToolCalls     int      `json:"toolCalls"`
	InputTokens   int64    `json:"inputTokens"`
	OutputTokens  int64    `json:"outputTokens"`
	WallMs        int64    `json:"wallMs"`
	CostUSD       *float64 `json:"costUsd,omitempty"`
	CostState     string   `json:"costState"`
}

// LoopDeleteResult is the tombstone the deletion left, plus what was kept.
type LoopDeleteResult struct {
	Tombstone LoopTombstoneView `json:"tombstone"`
}

// LoopTombstonesParams scopes the list to a component; empty lists every deleted loop.
type LoopTombstonesParams struct {
	ComponentRoot string `json:"componentRoot,omitempty"`
}

// DetachedLinkView says a task, roadmap item or queue item lost its loop to a deletion: kind is task, roadmap or
// queue, id is that row's id.
type DetachedLinkView struct {
	Kind          string   `json:"kind"`
	ID            string   `json:"id"`
	LoopSlug      string   `json:"loopSlug"`
	ComponentRoot string   `json:"componentRoot"`
	DetachedAt    string   `json:"detachedAt"`
	CostUSD       *float64 `json:"costUsd,omitempty"`
	CostFloor     bool     `json:"costFloor,omitempty"`
}

// WorkClosureView says a task, roadmap item, queue entry or goal was closed — not done — because its loop was
// archived or deleted. kind is task, roadmap, queue or goal; reason is loop-archived or loop-deleted.
type WorkClosureView struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Reason        string `json:"reason"`
	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`
	PriorStatus   string `json:"priorStatus"`
	ClosedAt      string `json:"closedAt"`
}

// LoopTombstonesResult lists deleted loops, newest first, the work whose link to a loop a deletion cut, and the work
// closed (not done) because its loop was archived or deleted.
type LoopTombstonesResult struct {
	Tombstones []LoopTombstoneView `json:"tombstones"`
	Detached   []DetachedLinkView  `json:"detached"`
	Closed     []WorkClosureView   `json:"closed"`
}

func (t *Table) handleLoopSetArchived(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopSetArchivedParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("loop.setArchived needs the loop's id")}
	}
	if t.SetLoopArchived == nil {
		return nil, ErrStoreUnavailable
	}
	return t.SetLoopArchived(ctx, p)
}

func (t *Table) handleLoopDelete(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	var p LoopDeleteParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Confirm) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("loop.delete needs the loop's id and confirm set to the loop's slug")}
	}
	if t.DeleteLoop == nil {
		return nil, ErrStoreUnavailable
	}
	return t.DeleteLoop(ctx, t.ActorFor(ctx, sess), p)
}

func (t *Table) handleLoopTombstones(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopTombstonesParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if t.ListLoopTombstones == nil {
		return nil, ErrStoreUnavailable
	}
	return t.ListLoopTombstones(ctx, p)
}
