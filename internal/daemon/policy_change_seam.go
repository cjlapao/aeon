package daemon

// policy_change_seam.go — S45 T3 (PM8): the daemon's adapter from the
// protocol's read-only PolicyChangeStore seam onto the store's
// policy_change accessors. The proposalStoreAdapter pattern (S35 T3): the
// wire view is the protocol's, the row shape is the store's, this file is
// the one layer that knows both, and the adaptation is field-for-field
// because the protocol's view mirrors the columns exactly.
//
// READS ONLY — by design and by shape. The audit rows are written by the
// T4/T5 authoring doors beside the writes they record (PM8: one row per
// authoring write, in the same transaction where the store allows it); T3
// ships the table with NO production writer, the doors arrive with T4/T5,
// and the store-side pins the caller set — doors only — by
// TestPolicyChangeWritersAreOnlyTheAuthoringDoors. An adapter that could
// append, rewrite or remove an audit row would be the append-only trigger
// bypassed in Go by the door that was never meant to hold it.

import (
	"context"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// policyChangeStoreAdapter is the protocol.PolicyChangeStore the socket's
// policy.changes read calls.
type policyChangeStoreAdapter struct {
	s *store.Store
}

// policyChangeStore returns the seam over a real store; nil is the
// storeless (test-mode) daemon, where the method stays registered and the
// handler answers ErrStoreUnavailable rather than dereferencing.
func policyChangeStore(s *store.Store) protocol.PolicyChangeStore {
	if s == nil {
		return nil
	}
	return &policyChangeStoreAdapter{s: s}
}

func (a *policyChangeStoreAdapter) PolicyChangeList(ctx context.Context, policyID string, limit int) ([]protocol.PolicyChangeView, error) {
	rows, err := a.s.PolicyChangeList(ctx, policyID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.PolicyChangeView, len(rows))
	for i, c := range rows {
		out[i] = protocol.PolicyChangeView{
			ID:         c.ID,
			Op:         c.Op,
			PolicyID:   c.PolicyID,
			ProposalID: c.ProposalID,
			Summary:    c.Summary,
			ActorKind:  c.ActorKind,
			ActorID:    c.ActorID,
			OccurredAt: c.OccurredAt,
		}
	}
	return out, nil
}

func (a *policyChangeStoreAdapter) PolicyChangeCount(ctx context.Context) (int, error) {
	return a.s.PolicyChangeCount(ctx)
}
