package attention

import (
	"context"
	"strconv"

	"github.com/cjlapao/aeon/internal/store"
)

// more_sources.go — the three sources that join questions, blocked and stopped (S58): `gates` (the name
// SV1 reserved for the verification plane), `proposals` and `triage`. Same discipline as the first
// three: each reads one store predicate, writes nothing, carries a FIXED reason sentence (a shape fact,
// not row text), and leaves the act to the door its kind names. All three are `needs-you`: each is a
// decision only a person can make.

// GatesSource reads gates whose latest run failed.
type GatesSource struct {
	Gates  store.FailedGatesReader
	Owners *OwnerMaps
}

func (GatesSource) Name() string { return "gates" }

func (s GatesSource) Items(ctx context.Context, q Query) ([]Item, error) {
	rows, err := s.Gates.FailedGatesForScope(q.Scope.RootPaths)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, g := range rows {
		it := Item{
			Key:      KeyOf("gates", g.RunID, g.StartedAt),
			Kind:     "gate-failed",
			Severity: NeedsYou,
			Reason:   "the latest verification run failed", // fixed sentence; the run's own summary stays behind verify.runShow
			Since:    g.StartedAt,
			LoopSlug: g.LoopSlug,
			ItemRef:  g.RunID,
		}
		if s.Owners != nil {
			it.ComponentID, it.RootID = s.Owners.Owner(g.ComponentRoot)
		}
		out = append(out, it)
	}
	return out, nil
}

// ProposalsSource reads proposals waiting for a decision. A proposal is the daemon's, not a
// component's: it appears only in an unscoped evaluation, never under a component's own scope.
type ProposalsSource struct {
	Proposals store.OpenProposalsReader
}

func (ProposalsSource) Name() string { return "proposals" }

func (s ProposalsSource) Items(ctx context.Context, q Query) ([]Item, error) {
	if len(q.Scope.RootPaths) > 0 {
		return []Item{}, nil
	}
	rows, err := s.Proposals.OpenProposals()
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, p := range rows {
		out = append(out, Item{
			Key:      KeyOf("proposals", strconv.FormatInt(p.ID, 10), p.CreatedAt),
			Kind:     "proposal",
			Severity: NeedsYou,
			Reason:   "a " + p.Kind + " proposal waits for a decision", // kind is the closed proposal vocabulary
			Since:    p.CreatedAt,
			ItemRef:  strconv.FormatInt(p.ID, 10),
		})
	}
	return out, nil
}

// TriageSource reads bugs waiting for validation, one item per component.
type TriageSource struct {
	Bugs   store.UnvalidatedBugsReader
	Owners *OwnerMaps
}

func (TriageSource) Name() string { return "triage" }

func (s TriageSource) Items(ctx context.Context, q Query) ([]Item, error) {
	rows, err := s.Bugs.UnvalidatedBugsForScope(q.Scope.RootPaths)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, b := range rows {
		it := Item{
			Key:      KeyOf("triage", b.ComponentRoot, b.Oldest),
			Kind:     "bugs-unvalidated",
			Severity: NeedsYou,
			Reason:   strconv.Itoa(b.Count) + " bugs wait for validation",
			Since:    b.Oldest,
			ItemRef:  strconv.Itoa(b.Count),
		}
		if b.Count == 1 {
			it.Reason = "1 bug waits for validation"
		}
		if s.Owners != nil {
			it.ComponentID, it.RootID = s.Owners.Owner(b.ComponentRoot)
		}
		out = append(out, it)
	}
	return out, nil
}
