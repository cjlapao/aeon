package desk

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// Boundary is the desk's authoring seam: the four things an effect needs from
// the policy write boundary, named for what the effect is doing rather than
// re-declaring the boundary's own surface. It is a consumer-side interface in
// the policy/S44 pattern — the desk says what it needs, the caller adapts — and
// it exists so the store handle, the `RepoFuncs` bridge and the authoring calls
// themselves stay in the wiring file the PL15 guard already names, while the
// DECISION to author lives here.
//
// The adapter that satisfies it is a forwarding type: it adds no rule, caches
// nothing, and every call still clears `internal/policy`'s write boundary — the
// closed sets, PL1's authorship check, the per-type validators. Moving the
// adapter in here is T6's, once the guard states the desk positively (PM10).
type Boundary interface {
	// AddPolicy stores one rule through the write boundary. A non-nil reject is
	// the domain's answer, not an infrastructure failure (DB19).
	AddPolicy(ctx context.Context, p policy.Policy, now time.Time) *policy.Reject

	// RemovePolicy removes a rule by id. The bool is "a row was there".
	RemovePolicy(ctx context.Context, id string) (bool, error)

	// ListPolicies reads the whole set as of one instant — the read the
	// idempotency gate and the seed runner's "unchanged" count are measured
	// against. No cache (N8): two calls are two reads.
	ListPolicies(ctx context.Context, at time.Time) ([]policy.Policy, *policy.Reject)

	// RecordAuthoredPolicy writes the one audit row a stored rule earns. It is
	// the boundary's record door; nothing else in the desk writes an event.
	RecordAuthoredPolicy(ctx context.Context, subject, policyID string, at time.Time) *policy.Reject
}

// BoundaryOpener opens a boundary and hands back its close. Every effect calls
// it and closes before returning, so a commit makes the same three fresh reads
// (validate, gate, apply) that S35 T2 wrote and Phase A's goldens pinned.
//
// It is a function value, not a handle, for the same reason the MCP and import
// effects are seams: the two doors reach the store differently (the CLI through
// its justified direct-open exception, the daemon through the handle it already
// owns) and the desk has no business knowing which.
type BoundaryOpener func() (Boundary, func(), error)

// OpenBoundary opens the boundary an effect runs over, refusing a nil opener
// with the name of what is missing rather than a nil dereference: a registry
// built without a boundary is a wiring bug, and the answer has to say so.
func OpenBoundary(opener BoundaryOpener) (Boundary, func(), error) {
	if opener == nil {
		return nil, nil, fmt.Errorf("refused: the desk has no boundary opener — desk.Deps.Boundary was never wired, so no policy effect can be committed")
	}
	b, closeFn, err := opener()
	if err != nil {
		return nil, nil, err
	}
	if b == nil {
		return nil, nil, fmt.Errorf("refused: the desk's boundary opener returned no boundary and no error")
	}
	if closeFn == nil {
		closeFn = func() {}
	}
	return b, closeFn, nil
}

// now is the desk's only clock read, and it reads the clock it was handed (D5).
func (d Deps) now() time.Time {
	if d.Clock == nil {
		return time.Now()
	}
	return d.Clock()
}
