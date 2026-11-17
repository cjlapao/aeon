package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// `loop.documents` — the documents a loop's agents have filed in the database (plan.md, discovery.md, answers.md,
// tasks/T1.md …): their names, revisions, sizes and who wrote them, newest first. Content is read with `loop.read`.
// These are the loop's internal files: they live in the store, not in the workspace, so `loop.files` never shows them.

const MethodLoopDocuments MethodName = "loop.documents"

// LoopDocumentsParams names the loop.
type LoopDocumentsParams struct {
	ID string `json:"id"`
}

// LoopDocumentView is one document, at its current revision.
type LoopDocumentView struct {
	File      string `json:"file"`
	Rev       int    `json:"rev"`
	Bytes     int    `json:"bytes"`
	SHA256    string `json:"sha256,omitempty"`
	WrittenBy string `json:"writtenBy,omitempty"`
	WrittenAt string `json:"writtenAt"`
	OwnerGoal string `json:"ownerGoal,omitempty"`
}

// LoopDocumentsResult lists a loop's documents.
type LoopDocumentsResult struct {
	Slug          string             `json:"slug"`
	ComponentRoot string             `json:"componentRoot"`
	Documents     []LoopDocumentView `json:"documents"`
}

func (t *Table) handleLoopDocuments(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopDocumentsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("loop.documents needs the loop's id")}
	}
	if t.ListLoopDocuments == nil {
		return nil, ErrStoreUnavailable
	}
	return t.ListLoopDocuments(ctx, p)
}
