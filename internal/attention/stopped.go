package attention

import (
	"context"
	"strconv"

	"github.com/cjlapao/aeon/internal/store"
)

// stopped.go — the `stopped` source (SV1): loops a PERSON stopped (status
// `waiting`, runtime `lastReason = 'stopped'`, no unanswered question — the
// store read carries the conjunction, one definition). The item says what
// the loop's own state says, and its door is `step.continue` with the step
// id — the door LC-era loop control actually registered.

// StoppedSource reads person-stopped loops.
type StoppedSource struct {
	Loops  store.StoppedLoopsReader
	Owners *OwnerMaps
}

// Name is the source's stable name in `sources[]`.
func (StoppedSource) Name() string { return "stopped" }

// Items lists the stopped loops. The reason is the design's fixed sentence
// (SV1's example: the item says a step was stopped and what resumes it) —
// the loop row carries only the marker, and no verdict is added to it.
func (s StoppedSource) Items(ctx context.Context, q Query) ([]Item, error) {
	rows, err := s.Loops.StoppedLoopsForScope(q.Scope.RootPaths)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, l := range rows {
		it := Item{
			Key:      KeyOf("stopped", l.LoopID, l.UpdatedAt),
			Kind:     "stopped",
			Severity: Stopped,
			Reason:   "step stopped; step.continue resumes it",
			Since:    l.UpdatedAt,
			LoopID:   l.LoopID,
			LoopSlug: l.Slug,
		}
		if l.StepID > 0 {
			it.ItemRef = strconv.FormatInt(l.StepID, 10)
		}
		if s.Owners != nil {
			it.ComponentID, it.RootID = s.Owners.Owner(l.ComponentRoot)
		}
		out = append(out, it)
	}
	return out, nil
}
