package daemon

// learnings_doors.go — S52 T5 (P5, the triage_doors.go / RoadmapManageDoors
// shape). The offline road for `aeon learnings retract|restore|show|board|
// history|vocabulary|for-agent` is the SAME answer functions the person's
// socket doors answer through, over a store opened directly: the CLI
// constructs no learnings service of its own, re-validates no law, and
// copies no refusal sentence — every method here is one call into the one
// answer (the AnswerLearnings* extractions in internal/protocol). The
// S28 verbs keep their socket-only roads untouched (their prints and
// refusals do not move), and `learnings.mine.*` is the AGENT's surface
// (LM1) — there is no offline arm of an agent door and no CLI verb for
// it, only the person's `for-agent`, which answers the person's
// learnings.forAgent door.
//
// The retraction's actor law travels with it: the socket stamps the
// CONNECTION's person (S52 LM4), the offline arm stamps {cli, cli} — the
// `learning_change` CHECK admitted 'cli' the day T1 drew the audit table,
// FOR exactly this door (LM3 restricts the `agent` kind, never `cli`).

import (
	"context"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// LearningsManageDoors is the offline half of the seven memory verbs. The
// actor arrives as an argument exactly as the CLI's door hands it — the
// socket road ignores the argument and stamps the peer instead, so one
// interface serves both arms.
type LearningsManageDoors struct{ s *store.Store }

// NewLearningsManageDoors hands out the doors over a directly opened store.
func NewLearningsManageDoors(s *store.Store) *LearningsManageDoors {
	return &LearningsManageDoors{s: s}
}

// Show is the gate read the retract/restore verbs run BEFORE they act: the
// row as it stands, named honestly, including a retraction already on it.
func (d *LearningsManageDoors) Show(ctx context.Context, p protocol.LearningsShowParams) (protocol.LearningsItemResult, error) {
	_ = ctx
	return protocol.AnswerLearningsShow(learnings.New(d.s), p)
}

// Retract stamps the row with the arriving actor — LM2's idempotence and
// every refusal sentence ride the answer function, byte-identical to the
// socket road's.
func (d *LearningsManageDoors) Retract(ctx context.Context, actor learnings.Actor, p protocol.LearningsRetractParams) (protocol.LearningsActResult, error) {
	_ = ctx
	return protocol.AnswerLearningsRetract(learnings.New(d.s), p, actor)
}

// Restore is Retract's twin: the stamp comes off, the audit says so.
func (d *LearningsManageDoors) Restore(ctx context.Context, actor learnings.Actor, p protocol.LearningsRestoreParams) (protocol.LearningsActResult, error) {
	_ = ctx
	return protocol.AnswerLearningsRestore(learnings.New(d.s), p, actor)
}

// Board runs LM6's filter set in the plane — the same answer, the same
// Describe sentences, no filter logic out here.
func (d *LearningsManageDoors) Board(ctx context.Context, p protocol.LearningsBoardParams) (protocol.LearningsBoardResult, error) {
	_ = ctx
	return protocol.AnswerLearningsBoard(learnings.New(d.s), p)
}

// History reads the append-only career of one row through the one answer.
func (d *LearningsManageDoors) History(ctx context.Context, p protocol.LearningsHistoryParams) (protocol.LearningsHistoryResult, error) {
	_ = ctx
	return protocol.AnswerLearningsHistory(learnings.New(d.s), p)
}

// Vocabulary needs no store: the closed sets are the code the daemon
// ships, and the offline door speaks the same bytes because they are the
// same function.
func (d *LearningsManageDoors) Vocabulary(ctx context.Context) (protocol.LearningsVocabularyResult, error) {
	_ = ctx
	return protocol.AnswerLearningsVocabulary(), nil
}

// ForAgent answers the person's `for-agent` read — the named agent's
// readable set, ranked once, every hit with its `why` (LM5).
func (d *LearningsManageDoors) ForAgent(ctx context.Context, p protocol.LearningsForAgentParams) (protocol.LearningsForAgentResult, error) {
	return protocol.AnswerLearningsForAgentQuery(ctx, learnings.New(d.s), p)
}
