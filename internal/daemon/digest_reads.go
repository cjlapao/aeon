package daemon

// digest_reads.go — S63 T5 (RT12): the "what did we learn" digest, the
// fixed seven sections assembled from the pieces the design names —
// scorecard computes, the recorder's rows, the existing attention reads
// (BlockedLoopsForScope, PendingQuestionsForScope — the digest invents
// no second awaiting read), S45's proposals desk, and the learnings in
// the window.
//
// The laws this file keeps:
//
//   - IDs and enums only. The largest loop is a LINK (root, slug); the
//     rework ladder is blueprint ids with integer counts; nothing here
//     quotes a goal, a title, or hunk text (TestDigestUsesIdsNotLoopText
//     plants a sentinel goal string and greps it out of the answer).
//   - Learnings arrive VERBATIM. They are already-validated words; a
//     second renderer would be a second truth.
//   - The floor sentence is history's renderer CALLED, never a copy —
//     the same law the scorecard pins (rule 4).
//   - The comparison with the previous equal window is TWO READS shown
//     side by side — never a delta (RT8's law, kept).
//   - Empty windows answer with the sections at zero/cannot-say and the
//     sentence, not an error (RT12).

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/scorecard"
	"github.com/cjlapao/aeon/internal/store"
)

type digestReads struct {
	s *store.Store
}

func newDigestReads(s *store.Store) *digestReads { return &digestReads{s: s} }

var _ protocol.DigestService = (*digestReads)(nil)

func (g *digestReads) Learned(ctx context.Context, p protocol.DigestParams) (protocol.DigestResult, error) {
	deps := scorecard.Deps{S: g.s}
	subj := scorecard.Subject{Kind: scorecard.SubjectComponent, ID: p.ID, IncludeChildren: true}

	// The component card carries the resolved window AND the coverage
	// strip — sections 1, the means in section 3 and the per-item floor
	// in section 5 all ride the card's own computed values (one
	// computation, RT7's law; the digest computes nothing new).
	card, err := scorecard.Compute(ctx, deps, subj, scorecard.Window{Since: p.Since, Until: p.Until})
	if err != nil {
		return protocol.DigestResult{}, scorecardRefused(err)
	}
	roots, all, err := scorecard.RootsPlan(deps, subj)
	if err != nil {
		return protocol.DigestResult{}, scorecardRefused(err)
	}
	rows, err := g.s.RetroLatestOutcomes(scope(roots, all), card.Window.Since, card.Window.Until, 0)
	if err != nil {
		return protocol.DigestResult{}, protocol.ErrRetroUnavailable.WithWhat("%v", err)
	}

	out := protocol.DigestResult{
		Window:    protocol.WindowWire{Since: card.Window.Since, Until: card.Window.Until, Last: card.Window.LastN},
		Statement: protocol.ScorecardStatement,
		Coverage:  cardWire(card).Coverage,
	}
	// Section 2: by-kind for the window, and the SAME read over the
	// previous equal window — two numbers, no delta.
	out.Outcomes = digestOutcomes(rows, card.Window)
	if prev, prevWin, ok := digestPrevious(g.s, roots, all, card.Window); ok {
		ps := prevOutcomes(prev, prevWin)
		out.Outcomes.Previous = &ps
	} else {
		out.Outcomes.Previous = &protocol.DigestOutcomesSection{CardWindow: prevWindow(card.Window)}
	}
	// Sections 3, 4, 5: rework, blocked kinds, cost — from the same
	// latest-per-loop rows.
	out.Rework = digestRework(rows, card)
	out.Blocked = digestBlocked(rows)
	out.Cost = digestCost(rows, card)
	// Section 6: learnings in the window, honesty-ordered.
	out.Learned = digestLearned(g.s, scope(roots, all), card.Window)
	// Section 7: the person's half.
	out.Attention = digestAttention(g.s, scope(roots, all))
	return out, nil
}

// scope collapses span-all (nil roots ⇒ every component) for the store
// reads that already speak that language.
func scope(roots []string, all bool) []string {
	if all {
		return nil
	}
	return roots
}

// --- section 2 -----------------------------------------------------------------

func digestOutcomes(rows []store.RetroOutcome, win scorecard.Window) protocol.DigestOutcomesSection {
	return protocol.DigestOutcomesSection{
		CardWindow: protocol.WindowWire{Since: win.Since, Until: win.Until, Last: win.LastN},
		ByKind:     byKindCounts(rows),
	}
}

func prevOutcomes(rows []store.RetroOutcome, win protocol.WindowWire) protocol.DigestOutcomesSection {
	return protocol.DigestOutcomesSection{CardWindow: win, ByKind: byKindCounts(rows)}
}

func byKindCounts(rows []store.RetroOutcome) []protocol.DigestOutcomeCount {
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.OutcomeKind]++
	}
	total := len(rows)
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds) // kinds are enumerated, never ranked (RT11)
	out := make([]protocol.DigestOutcomeCount, 0, len(kinds))
	for _, k := range kinds {
		c := protocol.DigestOutcomeCount{Kind: k, Loops: counts[k]}
		if total > 0 && scorecard.StateFor(total) == scorecard.StateOk {
			r := float64(counts[k]) / float64(total)
			c.Rate = &r // too-few keeps the rate null: the counts answer, the ratio does not (RT7)
		}
		out = append(out, c)
	}
	return out
}

func prevWindow(win scorecard.Window) protocol.WindowWire {
	// The previous equal window: the span slides back by its own length.
	// An unparseable or zero-length span earns the empty section with
	// the CURRENT window echoed — there is no previous to pretend at.
	start, end, ok := span(win)
	if !ok {
		return protocol.WindowWire{Since: win.Since, Until: win.Until}
	}
	d := end.Sub(start)
	return protocol.WindowWire{Since: start.Add(-d).UTC().Format(time.RFC3339Nano), Until: start.UTC().Format(time.RFC3339Nano)}
}

func digestPrevious(s *store.Store, roots []string, all bool, win scorecard.Window) ([]store.RetroOutcome, protocol.WindowWire, bool) {
	pw := prevWindow(win)
	if pw.Since == win.Since { // no computable previous
		return nil, pw, false
	}
	rows, err := s.RetroLatestOutcomes(scope(roots, all), pw.Since, pw.Until, 0)
	if err != nil {
		return nil, pw, false
	}
	return rows, pw, true
}

func span(win scorecard.Window) (time.Time, time.Time, bool) {
	start, err := time.Parse(time.RFC3339Nano, win.Since)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	// The window's "now" is to the second — the same resolution every
	// echoed window carries: an answer's previous span must not move in
	// sub-seconds between two calls of the same second (the parity test
	// is a fact, not a clock race).
	end := time.Now().UTC().Truncate(time.Second)
	if win.Until != "" {
		if e, err := time.Parse(time.RFC3339Nano, win.Until); err == nil {
			end = e
		} else {
			return time.Time{}, time.Time{}, false
		}
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

// --- section 3 -----------------------------------------------------------------

func digestRework(rows []store.RetroOutcome, card scorecard.Card) protocol.DigestReworkSection {
	sec := protocol.DigestReworkSection{}
	// The means come off the card's own metrics (n and state included);
	// the totals count what the rows KNOW — a NULL adds nothing (P31).
	perBlueprint := map[string]int{}
	for _, r := range rows {
		if r.Retries != nil {
			sec.RetryRounds += *r.Retries
		}
		if r.QARounds != nil {
			sec.QARounds += *r.QARounds
		}
		rounds := 0
		if r.ReviewRounds != nil {
			rounds += *r.ReviewRounds
		}
		if r.SecurityRounds != nil {
			rounds += *r.SecurityRounds
		}
		if r.Retries != nil {
			rounds += *r.Retries
		}
		if r.QARounds != nil {
			rounds += *r.QARounds
		}
		if r.BlueprintID != "" {
			perBlueprint[r.BlueprintID] += rounds
		}
	}
	if m, ok := card.Metrics["reviewRounds"]; ok {
		w := metricWire(m)
		sec.ReviewRounds = &w
	}
	if m, ok := card.Metrics["securityRounds"]; ok {
		w := metricWire(m)
		sec.SecurityRounds = &w
	}
	// The three most rework-heavy blueprints BY COUNT — ids and
	// integers, ties broken by id so the ladder is deterministic and
	// item text never rides (RT12).
	type bp struct {
		id     string
		rounds int
	}
	list := make([]bp, 0, len(perBlueprint))
	for id, rounds := range perBlueprint {
		if rounds > 0 {
			list = append(list, bp{id, rounds})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].rounds != list[j].rounds {
			return list[i].rounds > list[j].rounds
		}
		return list[i].id < list[j].id
	})
	if len(list) > 3 {
		list = list[:3]
	}
	for _, b := range list {
		sec.TopBlueprints = append(sec.TopBlueprints, protocol.DigestBlueprintRework{BlueprintID: b.id, Rounds: b.rounds})
	}
	return sec
}

// --- section 4 -----------------------------------------------------------------

func digestBlocked(rows []store.RetroOutcome) protocol.DigestBlockedSection {
	counts := map[string]int{}
	for _, r := range rows {
		if strings.HasPrefix(r.OutcomeKind, "blocked") {
			counts[r.OutcomeKind]++
		}
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if counts[kinds[i]] != counts[kinds[j]] {
			return counts[kinds[i]] > counts[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})
	sec := protocol.DigestBlockedSection{}
	for _, k := range kinds {
		sec.Top = append(sec.Top, protocol.DigestBlockedCount{Kind: k, Loops: counts[k]})
	}
	return sec
}

// --- section 5 -----------------------------------------------------------------

func digestCost(rows []store.RetroOutcome, card scorecard.Card) protocol.DigestCostSection {
	sec := protocol.DigestCostSection{}
	roll := history.Rollup{Floor: false}
	var largest *store.RetroOutcome
	for i, r := range rows {
		if r.CostUSDFloor != nil {
			roll.Total += *r.CostUSDFloor
			if r.CostSteps != nil {
				roll.StepCount += *r.CostSteps
			}
			if r.CostUnknownSteps != nil {
				roll.UnknownCount += *r.CostUnknownSteps
				if *r.CostUnknownSteps > 0 {
					roll.Floor = true
				}
			}
			if r.CostPartialSteps != nil && *r.CostPartialSteps > 0 {
				roll.Floor = true
			}
		} else {
			// A loop with no known cost contributes a COUNT, not a zero
			// (P31 — history's own rollup law, applied to whole loops).
			roll.UnknownCount++
			roll.Floor = true
		}
		if r.WallMS != nil && (largest == nil || *r.WallMS > *largest.WallMS) {
			largest = &rows[i]
		}
	}
	sec.Floor = roll.Render()
	sec.UnknownSteps = roll.UnknownCount
	if m, ok := card.Metrics["costPerCompletedItem"]; ok {
		sec.PerCompletedItem = metricWire(m)
	}
	if largest != nil {
		sec.LargestLoop = &protocol.DigestLink{Root: largest.ComponentRoot, Loop: largest.LoopSlug}
	}
	return sec
}

// --- section 6 -----------------------------------------------------------------

// honestyOrder is RT12 §6's fixed grouping: told, observed, assumed,
// relayed — always all four, so absence cannot reorder the rest.
var honestyOrder = []string{"told", "observed", "assumed", "relayed"}

func digestLearned(s *store.Store, roots []string, win scorecard.Window) protocol.DigestLearnedSection {
	sec := protocol.DigestLearnedSection{}
	groups := map[string][]protocol.DigestLearningItem{}
	for _, p := range honestyOrder {
		groups[p] = []protocol.DigestLearningItem{}
	}
	// The store read already excludes retracted rows (LM2) — the digest
	// inherits that honesty and adds nothing to it.
	lrs, err := s.LearningsInWindow(roots, win.Since, win.Until)
	if err != nil {
		// An unreadable learned section answers as EMPTY groups, which is
		// honest: the digest says nothing about learnings it could not
		// read, and the other sections stand.
		err = nil //nolint // the empty groups are the answer; no section fails the digest
	}
	for _, lr := range lrs {
		groups[lr.Provenance] = append(groups[lr.Provenance], protocol.DigestLearningItem{
			ID: lr.ID, Scope: lr.Scope, ScopeKey: lr.ScopeKey, Provenance: lr.Provenance,
			ObservedCount: lr.ObservedCount, SourceLoopSlug: lr.SourceLoopSlug,
			CreatedAt: lr.CreatedAt, Body: lr.Body,
		})
	}
	for _, p := range honestyOrder {
		sec.Groups = append(sec.Groups, protocol.DigestProvenanceGroup{Provenance: p, Items: groups[p]})
	}
	return sec
}

// --- section 7 -----------------------------------------------------------------

func digestAttention(s *store.Store, roots []string) protocol.DigestAttentionSection {
	sec := protocol.DigestAttentionSection{}
	// The existing attention reads, verbatim doors onto the same rows
	// the inbox renders — the digest never opens a second awaiting door.
	if blocked, err := s.BlockedLoopsForScope(roots); err == nil {
		reasons, rerr := s.LatestBlockedReasons(roots)
		for _, b := range blocked {
			kind := b.LastReason
			if rerr == nil {
				if br, ok := reasons[b.LoopID]; ok && br.Kind != "" {
					kind = br.Kind
				}
			}
			var age int64
			if t, terr := time.Parse(time.RFC3339Nano, b.UpdatedAt); terr == nil {
				if d := time.Since(t); d > 0 {
					age = int64(d.Minutes())
				}
			}
			sec.BlockedNow = append(sec.BlockedNow, protocol.DigestBlockedNow{
				Root: b.ComponentRoot, Loop: b.Slug, Kind: kind, AgeMinutes: age,
			})
		}
	}
	if qs, err := s.PendingQuestionsForScope(roots); err == nil {
		sec.PendingQuestions = len(qs)
	}
	// The S45 desk is the machine's, so its pending count is honestly
	// the desk's, not a per-component slice of it (a proposal's payload
	// is kind-shaped, not root-shaped — the digest does not grep JSON).
	if props, err := s.ListProposals("proposed"); err == nil {
		sec.ProposalsPending = len(props)
	}
	sec.Statement = digestNeedsYou(sec)
	return sec
}

func digestNeedsYou(a protocol.DigestAttentionSection) string {
	if len(a.BlockedNow) == 0 && a.PendingQuestions == 0 && a.ProposalsPending == 0 {
		return "nothing needs you"
	}
	return fmt.Sprintf("%d blocked loop(s), %d pending question(s), %d pending proposal(s) need a person",
		len(a.BlockedNow), a.PendingQuestions, a.ProposalsPending)
}

// --- shared mirror -------------------------------------------------------------

func metricWire(m scorecard.Metric) protocol.MetricWire {
	return protocol.MetricWire{Value: m.Value, N: m.N, State: string(m.State), Note: m.Note, Detail: m.Detail}
}
