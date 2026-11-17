package daemon

// triage_doors.go — S51 T6 (TM9, P5, the S50 RoadmapManageDoors shape).
// The offline road for `aeon triage update|history|board|health|vocabulary`
// is the SAME answers the socket doors answer through, over a store opened
// directly: the CLI constructs no triage service of its own and copies no
// law — every method here is one call into the one answer the person's
// socket door already runs (the AnswerTriageUpdate/AnswerTriageHistory
// extractions, the triageBoardOver/triageHealthOver seams, the plane's own
// pure vocabulary). The S29 verbs keep their socket-only roads untouched
// (TM9: goldens byte-identical), and nothing here starts a loop or files a
// roadmap item: validate/handoff stay daemon-alive acts — the direct door
// does not answer them at all, because offline there is no reactor and a
// door that could not keep the promise would be a lie with a fixed stderr
// line on it (S50's dispatch-absence law, carried to the enabling verbs).

import (
	"context"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

// TriageManageDoors is the offline half of the five S51 verbs. The actor
// arrives as an argument exactly as the protocol door hands it — the CLI
// stamps {cli, cli} and says so (bug_change's CHECK admitted 'cli' the day
// T1 drew it, FOR this door).
type TriageManageDoors struct{ s *store.Store }

// NewTriageManageDoors hands out the doors over a directly opened store.
func NewTriageManageDoors(s *store.Store) *TriageManageDoors { return &TriageManageDoors{s: s} }

// Update runs the ONE edit answer — TM1's narrow set, the refusal
// sentences, the audit stamp, the changed flag included.
func (d *TriageManageDoors) Update(ctx context.Context, actor triage.Actor, p protocol.TriageUpdateParams) (protocol.TriageUpdateResult, error) {
	_ = ctx
	return protocol.AnswerTriageUpdate(triage.New(d.s), p, actor)
}

// Board runs the same answer the person's board door and the step's
// bug_list twin run (rule 4 — one rendering, three doors), read-only here
// exactly as there.
func (d *TriageManageDoors) Board(ctx context.Context, p protocol.TriageBoardParams) ([]protocol.TriageBoardRow, error) {
	return triageBoardOver(ctx, p, d.s, triage.New(d.s))
}

// History runs the same audit read the socket history door and the step's
// bug_show twin run.
func (d *TriageManageDoors) History(ctx context.Context, p protocol.TriageHistoryParams) ([]protocol.TriageHistoryRow, error) {
	_ = ctx
	return protocol.AnswerTriageHistory(triage.New(d.s), p)
}

// Vocabulary is code, not rows — the same pure answer the socket returns
// untouched, the two TM9 authority statements included. No store is read.
func (d *TriageManageDoors) Vocabulary(ctx context.Context) (triage.Vocabulary, error) {
	_ = ctx
	return (&triage.Triage{}).Vocabulary(), nil
}

// Health runs the daemon's ONE health answer over THIS store: settings
// through the same constructor the daemon mounts, blueprint rows through
// the store source, which offline is the only source there is.
func (d *TriageManageDoors) Health(ctx context.Context) (protocol.TriageHealthResult, error) {
	settings := NewSettings(d.s, config.Options{})
	read := func(ctx context.Context, key string) (config.View, error) { return settings.Get(ctx, key) }
	return triageHealthOver(ctx, read, StoreBlueprintSource(d.s), true)
}
