package daemon

// roadmap_manage_doors.go — S50 T6 (RM9, P5, the blueprint BP12 shape).
// The offline road for `aeon roadmap update|history|board|health|vocabulary`
// is the SAME answers the socket doors answer through, over a store opened
// directly: the CLI constructs no roadmap service of its own and copies no
// law — every method here is one call into the one answer function that
// the person's socket door already runs. The dispatch verbs
// (ideate/plan/materialize) are DELIBERATELY absent: they start loops,
// and offline there is no loop-runner to start; the road that refuses
// them is the daemon's absence, stated at the CLI, not a half-started
// promise here.

import (
	"context"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// RoadmapManageDoors is the offline half of the five S50 verbs. The actor
// arrives as an argument exactly as the protocol door hands it — the CLI
// stamps {cli, cli} and says so (the audit CHECK admitted 'cli' the day
// T1 drew it, FOR this door).
type RoadmapManageDoors struct{ s *store.Store }

// NewRoadmapManageDoors hands out the doors over a directly opened store.
func NewRoadmapManageDoors(s *store.Store) *RoadmapManageDoors { return &RoadmapManageDoors{s: s} }

// Update runs the ONE update answer — refusals, audit stamp, fresh row and
// changed flag included.
func (d *RoadmapManageDoors) Update(ctx context.Context, actor roadmap.Actor, p protocol.RoadmapUpdateParams) (protocol.RoadmapUpdateResult, error) {
	out, err := protocol.AnswerRoadmapUpdate(roadmap.New(d.s), p, actor)
	if err != nil {
		return protocol.RoadmapUpdateResult{}, err
	}
	return out.(protocol.RoadmapUpdateResult), nil
}

// Board runs the same answer function the person's board door and the
// step's roadmap_list twin run (T5's rule 4 — one rendering, three doors).
func (d *RoadmapManageDoors) Board(ctx context.Context, p protocol.RoadmapBoardParams) (protocol.RoadmapBoardResult, error) {
	_ = ctx
	out, err := protocol.AnswerRoadmapBoard(roadmap.New(d.s), p)
	if err != nil {
		return protocol.RoadmapBoardResult{}, err
	}
	return out.(protocol.RoadmapBoardResult), nil
}

// History runs the same answer the socket history door and the step's
// roadmap_show twin run.
func (d *RoadmapManageDoors) History(ctx context.Context, p protocol.RoadmapHistoryParams) (protocol.RoadmapHistoryResult, error) {
	out, err := protocol.AnswerRoadmapHistory(ctx, roadmap.New(d.s), p)
	if err != nil {
		return protocol.RoadmapHistoryResult{}, err
	}
	return out.(protocol.RoadmapHistoryResult), nil
}

// Vocabulary is code, not rows — the same pure answer the socket returns
// untouched, dispatch statement included.
func (d *RoadmapManageDoors) Vocabulary(ctx context.Context) (roadmap.VocabularyAnswer, error) {
	_ = ctx
	return roadmap.Vocabulary(), nil
}

// Health runs the daemon's ONE health answer over THIS store: settings
// through the same constructor the daemon mounts (environment included —
// both read the same os.LookupEnv discipline), blueprint rows through the
// store source, which offline is the only source there is.
func (d *RoadmapManageDoors) Health(ctx context.Context) (protocol.RoadmapHealthResult, error) {
	settings := NewSettings(d.s, config.Options{})
	read := func(ctx context.Context, key string) (config.View, error) { return settings.Get(ctx, key) }
	return roadmapHealthOver(ctx, read, StoreBlueprintSource(d.s)), nil
}
