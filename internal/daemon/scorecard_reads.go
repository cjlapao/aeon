package daemon

// scorecard_reads.go — S63 T5 (RT10): the daemon's read side of
// internal/scorecard, answering the five protocol doors. This file is a
// NAMED importer of the scorecard package in RT3.1's allow-list, and it
// is thin on purpose: it computes nothing itself. Every number on every
// card is computed inside internal/scorecard, and this file only
// mirrors the package's types onto the wire (the protocol package may
// not import scorecard, so the mirror lives here — and the fixture that
// pins the wire shape is what keeps the mirror true).
//
// A store error never escapes as itself: the plane's one refusal voice
// (ErrScorecardRefused) carries the store's sentence in `what`, per
// DB19 — nobody paraphrases a refusal, including this file.

import (
	"context"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/scorecard"
	"github.com/cjlapao/aeon/internal/store"
)

type scorecardReads struct {
	s *store.Store
}

// newScorecardReads is wired only where the daemon has a store, so the
// seam is never a nil-with-a-method-set (the V13 trap this mount
// pattern has always refused).
func newScorecardReads(s *store.Store) *scorecardReads { return &scorecardReads{s: s} }

var _ protocol.ScorecardService = (*scorecardReads)(nil)

func (r *scorecardReads) deps() scorecard.Deps { return scorecard.Deps{S: r.s} }

func (r *scorecardReads) Agent(ctx context.Context, p protocol.ScorecardAgentParams) (protocol.ScorecardCardResult, error) {
	card, err := scorecard.Compute(ctx, r.deps(),
		scorecard.Subject{Kind: scorecard.SubjectAgent, ID: p.Agent, RootID: p.RootID},
		scorecard.Window{Since: p.Since, Until: p.Until, LastN: p.Last})
	if err != nil {
		return protocol.ScorecardCardResult{}, scorecardRefused(err)
	}
	return protocol.ScorecardCardResult{Card: cardWire(card), Statement: protocol.ScorecardStatement}, nil
}

func (r *scorecardReads) Blueprint(ctx context.Context, p protocol.ScorecardBlueprintParams) (protocol.ScorecardCardResult, error) {
	card, err := scorecard.Compute(ctx, r.deps(),
		scorecard.Subject{Kind: scorecard.SubjectBlueprint, ID: p.Blueprint, RootID: p.RootID},
		scorecard.Window{Since: p.Since, Until: p.Until, LastN: p.Last})
	if err != nil {
		return protocol.ScorecardCardResult{}, scorecardRefused(err)
	}
	return protocol.ScorecardCardResult{Card: cardWire(card), Statement: protocol.ScorecardStatement}, nil
}

func (r *scorecardReads) Component(ctx context.Context, p protocol.ScorecardComponentParams) (protocol.ScorecardComponentResult, error) {
	card, err := scorecard.Compute(ctx, r.deps(),
		scorecard.Subject{Kind: scorecard.SubjectComponent, ID: p.ID, IncludeChildren: p.IncludeChildren},
		scorecard.Window{Since: p.Since, Until: p.Until, LastN: p.Last})
	if err != nil {
		return protocol.ScorecardComponentResult{}, scorecardRefused(err)
	}
	out := protocol.ScorecardComponentResult{
		Card:      cardWire(card),
		Breakdown: breakdownWires(card),
		Statement: protocol.ScorecardStatement,
	}
	return out, nil
}

func (r *scorecardReads) Compare(ctx context.Context, p protocol.ScorecardCompareParams) (protocol.ScorecardCompareResult, error) {
	kind := scorecard.SubjectKind(p.Kind)
	if kind == "" {
		kind = scorecard.SubjectComponent
	}
	subj := scorecard.Subject{Kind: kind, ID: p.Subject}
	if kind == scorecard.SubjectComponent {
		subj.IncludeChildren = true // the person comparing trees compares the tree a screen shows
	}
	a, err := scorecard.Compute(ctx, r.deps(), subj, scWindow(p.WindowA))
	if err != nil {
		return protocol.ScorecardCompareResult{}, scorecardRefused(err)
	}
	b, err := scorecard.Compute(ctx, r.deps(), subj, scWindow(p.WindowB))
	if err != nil {
		return protocol.ScorecardCompareResult{}, scorecardRefused(err)
	}
	// TWO cards, two windows, no delta (RT8): what a person reads is
	// written down twice, never subtracted into a new derived metric.
	return protocol.ScorecardCompareResult{
		A: cardWire(a), B: cardWire(b),
		WindowA: p.WindowA, WindowB: p.WindowB,
		Statement: protocol.ScorecardStatement,
	}, nil
}

func (r *scorecardReads) Vocabulary(ctx context.Context) (protocol.ScorecardVocabularyResult, error) {
	out := protocol.ScorecardVocabularyResult{
		MinSample:  scorecard.MinSample,
		Milestones: scorecard.Milestones(),
		Statement:  protocol.ScorecardStatement,
	}
	for _, m := range scorecard.Metrics() {
		out.Metrics = append(out.Metrics, protocol.MetricInfoWire{Name: m.Name, Description: m.Description, Scope: m.Scope})
	}
	for _, st := range scorecard.States() {
		out.States = append(out.States, string(st))
	}
	for _, w := range scorecard.Windows() {
		out.Windows = append(out.Windows, protocol.WindowPresetWire{Name: w.Name, Days: w.Days})
	}
	return out, nil
}

// --- the mirror ---------------------------------------------------------------

func scorecardRefused(err error) error {
	return protocol.ErrScorecardRefused.WithWhat("%v", err)
}

func scWindow(w protocol.ScorecardWindow) scorecard.Window {
	return scorecard.Window{Since: w.Since, Until: w.Until, LastN: w.Last}
}

func cardWire(c scorecard.Card) protocol.CardWire {
	metrics := make(map[string]protocol.MetricWire, len(c.Metrics))
	for name, m := range c.Metrics {
		metrics[name] = protocol.MetricWire{
			Value: m.Value, N: m.N, State: string(m.State), Note: m.Note, Detail: m.Detail,
		}
	}
	return protocol.CardWire{
		Subject: protocol.SubjectWire{
			Kind: string(c.Subject.Kind), ID: c.Subject.ID,
			IncludeChildren: c.Subject.IncludeChildren, RootID: c.Subject.RootID,
		},
		Window: protocol.WindowWire{Since: c.Window.Since, Until: c.Window.Until, Last: c.Window.LastN},
		Coverage: protocol.CoverageWire{
			RecordedSince:    c.Coverage.RecordedSince,
			LoopsInHistory:   c.Coverage.LoopsInHistory,
			LoopsRecorded:    c.Coverage.LoopsRecorded,
			LoopsNotRecorded: c.Coverage.LoopsNotRecorded,
		},
		// The card's own statement is the COVERAGE sentence (RT9); the
		// result's statement field is RT3's constant. Two sentences, two
		// fields, never conflated.
		Statement: c.Statement,
		Metrics:   metrics,
	}
}

func breakdownWires(c scorecard.Card) []protocol.BreakdownWire {
	var out []protocol.BreakdownWire
	for _, b := range c.Breakdown {
		out = append(out, protocol.BreakdownWire{
			Component: b.Component, Root: b.Root, Loops: b.Loops, Done: b.Done, Blocked: b.Blocked,
		})
	}
	return out
}
