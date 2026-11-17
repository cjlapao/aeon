package graph

// audit.go — S54 T1: the two audited graph OPERATIONS (reparse, delete).
//
// graph.build is NOT here — it runs as an async worker and its audit rides
// the manager's audited start (internal/asyncworkers), so the ledger op
// and this plane's two inline ops never share a writer by accident.
//
// The service methods mirror their unaudited twins guard for guard; the
// only difference is who the store audited writer gets. The audit's op,
// subject and summary are the store writer's facts (delete →
// "graph.delete started" over the root; reparse → "graph.reparse started"
// over the root) — the door hands in the actor and nothing else, so a
// caller cannot compose a summary the schema did not intend.
//
// The reparse skip stays audit-silent: a path the slice does not claim
// returns (nil, nil) BEFORE any store write, and an operation that never
// started writes no audit row (SP3's "refused starts nothing, and nothing
// is audited").

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cjlapao/aeon/internal/store"
)

// Actor is the audit's author pair this plane accepts: 'user' from the
// socket peer, 'cli' from the offline direct door. The store refuses
// anything else before SQL; the protocol door refuses it before this call
// (SP3, SP4).
type Actor struct {
	Kind string
	ID   string
}

func (a Actor) toStore() store.PlatformActor {
	return store.PlatformActor{Kind: a.Kind, ID: a.ID}
}

// DeleteAudited is Delete with its platform_change audit row committed in
// the same transaction as the deletes (SP3). The guards are Delete's word
// for word — an audited door refuses in the same sentences, so the
// answer a client sees never depends on which twin it reached.
func (s *Service) DeleteAudited(ctx context.Context, root string, a Actor) (int, error) {
	if root == "" {
		return 0, fmt.Errorf("graph delete: refused — project_root is required, never defaulted (T5 §5)")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return 0, fmt.Errorf("graph delete %s: %w", root, err)
	}
	return s.st.DeleteGraphAudited(abs, a.toStore())
}

// RebuildFileAtAudited is RebuildFileAt with the audit riding the
// reparse's first store transaction (DeleteGraphFile's). The
// not-a-claimed-file skip returns first, silent and un-audited, exactly as
// the unaudited twin skips.
func (s *Service) RebuildFileAtAudited(ctx context.Context, root, absPath string, a Actor) (*BuildReport, error) {
	if root == "" {
		return nil, fmt.Errorf("graph reparse: refused — project_root is required, never defaulted (T5 §5)")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("graph reparse %s: %w", root, err)
	}
	target := absPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(abs, absPath)
	}
	if _, ok := LookupByPath(target); !ok {
		return nil, nil // not a file this slice claims; skip, not error — and no audit
	}
	rep := &BuildReport{Root: root}
	sactor := a.toStore()
	if err := s.rebuildOne(ctx, abs, target, rep, &sactor); err != nil {
		return nil, err
	}
	rep.Files = 1
	return rep, nil
}
