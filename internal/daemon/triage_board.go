package daemon

// ════════════════════════════════════════════════════════════════════════════
// triage.board — the backlog read the daemon owns (S51 T2, TM5).
//
// The board is one row per bug seen WHOLE, and assembling a whole row
// crosses three readers the socket handler must not touch directly: the
// bug_change audit (attempts count, last attempt), the vec index (the
// SCORED duplicate candidates) and the roadmap rows (the hand-off
// status). So it is a seam, shaped like the plane's other daemon seams.
//
// Every piece is a READ, and that is the load-bearing word
// (TestBoardCandidatesAreReadOnly): the suggestion path STORES an
// embedding because asking for one computes one; a board of forty bugs
// that wrote forty vec rows would be a read with an authorship problem.
// Here nothing is stored, linked, validated or moved — a board that
// writes is a board that lies about being a board.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

// triageBoardCandidates is how near the board looks for candidates per
// row — the same cap the single-bug suggest defaults to, named here
// because the board chose it for a board.
const triageBoardCandidates = 3

// triageBoard answers triage.board over the table's one triage instance.
func (d *Daemon) triageBoard(ctx context.Context, p protocol.TriageBoardParams) (any, error) {
	if d.store == nil || d.Table == nil || d.Table.Triage == nil {
		return nil, fmt.Errorf("triage: the board has no store behind it")
	}
	return triageBoardOver(ctx, p, d.store, d.Table.Triage)
}

// triageBoardOver is the board as ONE answer over an explicit store and
// triage plane (S51 T6, the T5 fileAs rule): the daemon's seam and the
// CLI's direct door run the same function — the same reads, the same
// read-only promise, one implementation, two doors.
func triageBoardOver(ctx context.Context, p protocol.TriageBoardParams, s *store.Store, tr *triage.Triage) ([]protocol.TriageBoardRow, error) {
	_ = ctx
	f, err := triageBoardFilter(p)
	if err != nil {
		return nil, err
	}
	bugs, err := tr.List(p.ComponentRoot, f)
	if err != nil {
		return nil, err
	}
	if p.HandedOff != nil {
		kept := bugs[:0:0]
		for _, b := range bugs {
			handed := b.RoadmapItemID != nil
			if handed == *p.HandedOff {
				kept = append(kept, b)
			}
		}
		bugs = kept
	}
	ids := make([]int64, len(bugs))
	for i, b := range bugs {
		ids[i] = b.ID
	}
	stats, err := s.BugChangeStats(ids)
	if err != nil {
		return nil, err
	}
	rows := make([]protocol.TriageBoardRow, 0, len(bugs))
	for _, b := range bugs {
		row := protocol.TriageBoardRow{Bug: protocol.NewTriageView(b)}
		desc := triage.Describe(b)
		row.Describe = protocol.TriageDescribeView{
			ValidationSentence: desc.ValidationSentence,
			PrioritySentence:   desc.PrioritySentence,
			Duplicate:          desc.Duplicate,
			Handoff:            desc.Handoff,
		}
		st := stats[b.ID] // absent key ⇒ the honest zero digest
		row.Attempts = st.Attempts
		row.LastAttemptAt = st.LastAttemptAt
		// The scored candidates: the SAME k-NN the suggest door uses,
		// read-only — no embedding is stored, nothing is linked (D5).
		vec := triageEmbedding(b.Title + " " + b.Body)
		cands, err := tr.SuggestScored(b, triageEmbedDim, vec, triageBoardCandidates)
		if err != nil {
			return nil, err
		}
		row.Candidates = make([]protocol.TriageScoredView, len(cands))
		for i, c := range cands {
			row.Candidates[i] = protocol.TriageScoredView{
				Bug:      protocol.NewTriageView(c.Bug),
				Distance: c.Distance,
			}
		}
		if b.RoadmapItemID != nil {
			status, err := triageBoardRoadmapStatus(s, *b.RoadmapItemID)
			if err != nil {
				return nil, err
			}
			row.RoadmapStatus = status
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// triageBoardFilter validates the filter words against the CLOSED sets
// before they reach SQL (D1/D2): the board answers only in the vocabulary
// `triage.vocabulary` publishes, and a word outside it — including the
// hidden `brainstorm` that is S51 T3's own setting word, never a bug
// state — is refused BY NAME. An empty filter word means no filter.
func triageBoardFilter(p protocol.TriageBoardParams) (triage.ListFilter, error) {
	var f triage.ListFilter
	if p.Validation != "" {
		v := triage.ValidationState(p.Validation)
		if !triage.ValidValidationState(v) {
			return f, fmt.Errorf("triage: the board's validation filter %q is not one of the four (triage.vocabulary answers the set)", p.Validation)
		}
		f.Validation = v
	}
	if p.Priority != "" {
		pr := triage.PriorityState(p.Priority)
		if !triage.ValidPriorityState(pr) {
			return f, fmt.Errorf("triage: the board's priority filter %q is not one of the three (triage.vocabulary answers the set)", p.Priority)
		}
		f.Priority = pr
	}
	return f, nil
}

// triageBoardRoadmapStatus reads the hand-off target's CURRENT status —
// the board shows where the work is now, not where it was left (D8). An
// item row that has gone answers honestly instead of blank.
func triageBoardRoadmapStatus(s *store.Store, itemID int64) (string, error) {
	item, ok, err := s.RoadmapItemByID(itemID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "unknown (the roadmap item row is absent)", nil
	}
	return string(item.Status), nil
}
