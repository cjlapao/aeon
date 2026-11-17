package protocol

// learnings_answers.go — S52 T5 (P5, the S51 T6 AnswerTriageUpdate law).
//
// The six memory answers — show, retract, restore, board, history,
// vocabulary, forAgent — live here as functions over the service, not as
// methods on the socket Table. The socket handlers call them with the
// peer-resolved actor; the CLI's offline arm calls them over a directly
// opened store with {cli, cli}. ONE function per verb means byte-identical
// refusals through both doors is a property of the code's shape, not a
// parity test that hopes — the doors cannot disagree because there is
// nothing second to disagree with. The renderer for retraction state, the
// unknown-id error, the refusal sentences: all ride in here, decided once.

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/learnings"
)

// AnswerLearningsShow is the by-id read: the flagged view, and its own
// refusal when no row answers — the SAME refusal whether a gate read comes
// over the socket or the offline arm names a row that never was.
func AnswerLearningsShow(l *learnings.Learnings, p LearningsShowParams) (LearningsItemResult, error) {
	if p.ID <= 0 {
		return LearningsItemResult{}, ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id")}
	}
	got, ok, err := l.Show(p.ID)
	if err != nil {
		return LearningsItemResult{}, err
	}
	if !ok {
		return LearningsItemResult{}, learningsRefusal(fmt.Sprintf("learning %d does not exist", p.ID))
	}
	return LearningsItemResult{Learning: learningsView(got)}, nil
}

// AnswerLearningsRetract stamps the row and says whether this call moved
// it (LM2: a second retract is a no-echo, not an error and not an audit
// line). A named-absence id keeps its OWN error (ErrLearningUnknown — the
// row is not there at all, which is a different truth from a refusal of a
// legal act on a real row); every other refusal is the plane's sentence,
// verbatim, from the same *Error the socket road relays.
func AnswerLearningsRetract(l *learnings.Learnings, p LearningsRetractParams, a learnings.Actor) (LearningsActResult, error) {
	if p.ID <= 0 {
		return LearningsActResult{}, ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id — a retraction names the memory it takes back")}
	}
	got, changed, ref := l.Retract(p.ID, p.Reason, a)
	if ref != nil {
		if !l.Exists(p.ID) && ref.Reason != "" {
			return LearningsActResult{}, ErrLearningUnknown.WithWhat("learning %d does not exist — %s", p.ID, ref.Reason)
		}
		return LearningsActResult{}, learningsRefusal(ref.Reason)
	}
	return LearningsActResult{Learning: learningsView(got), Changed: changed}, nil
}

// AnswerLearningsRestore is retract's twin: the stamp comes off, the audit
// gains the restore line, the row re-enters every default read (LM2).
func AnswerLearningsRestore(l *learnings.Learnings, p LearningsRestoreParams, a learnings.Actor) (LearningsActResult, error) {
	if p.ID <= 0 {
		return LearningsActResult{}, ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id — a restore names the memory it puts back")}
	}
	got, changed, ref := l.Restore(p.ID, a)
	if ref != nil {
		if !l.Exists(p.ID) {
			return LearningsActResult{}, ErrLearningUnknown.WithWhat("learning %d does not exist — %s", p.ID, ref.Reason)
		}
		return LearningsActResult{}, learningsRefusal(ref.Reason)
	}
	return LearningsActResult{Learning: learningsView(got), Changed: changed}, nil
}

// AnswerLearningsBoard runs LM6's filter set IN THE PLANE and composes
// every row with the one Describe formatter — the board's sentence is the
// daemon's own words on both doors.
func AnswerLearningsBoard(l *learnings.Learnings, p LearningsBoardParams) (LearningsBoardResult, error) {
	rows, err := l.Board(learnings.BoardFilter{
		Scope:            p.Scope,
		Provenance:       p.Provenance,
		Tag:              p.Tag,
		Agent:            p.Agent,
		Project:          p.Project,
		Loop:             p.Loop,
		IncludeRetracted: p.IncludeRetracted,
	})
	if err != nil {
		return LearningsBoardResult{}, err
	}
	out := make([]LearningsBoardRow, 0, len(rows))
	for _, lr := range rows {
		out = append(out, LearningsBoardRow{LearningsView: learningsView(lr), Describe: learnings.Describe(lr)})
	}
	return LearningsBoardResult{Learnings: out}, nil
}

// AnswerLearningsMap lays the board's learnings out by meaning. The filters are the board's, run by the
// same function, so the map and the board are two views of one set. A learning with no stored vector is
// counted and left off — never positioned by a guess — and the answer carries the daemon's sentence for
// what the positions are.
func AnswerLearningsMap(l *learnings.Learnings, p LearningsMapParams) (LearningsMapResult, error) {
	if p.Neighbours < 0 {
		return LearningsMapResult{}, ErrBadParams{Err: fmt.Errorf("neighbours is %d — a count of nearest neighbours cannot be negative", p.Neighbours)}
	}
	res, err := l.Map(learnings.BoardFilter{
		Scope:            p.Scope,
		Provenance:       p.Provenance,
		Tag:              p.Tag,
		Agent:            p.Agent,
		Project:          p.Project,
		Loop:             p.Loop,
		IncludeRetracted: p.IncludeRetracted,
	}, p.Neighbours)
	if err != nil {
		return LearningsMapResult{}, err
	}
	out := LearningsMapResult{
		Projected: res.Projected, Model: res.Model, Dim: res.Dim, Total: res.Total, Embedded: res.Embedded,
		Omitted: res.Omitted, CanEmbed: res.CanEmbed, Basis: res.Basis,
		Points: make([]LearningsMapPoint, 0, len(res.Points)),
		Links:  make([]LearningsMapLink, 0, len(res.Links)),
	}
	for _, pt := range res.Points {
		out.Points = append(out.Points, LearningsMapPoint{ID: pt.ID, X: pt.X, Y: pt.Y})
	}
	for _, lk := range res.Links {
		out.Links = append(out.Links, LearningsMapLink{A: lk.A, B: lk.B, Similarity: lk.Similarity})
	}
	return out, nil
}

// AnswerLearningsHistory asks the append-only audit for one row's career.
// The id must exist first — history of a row that never was is a refusal,
// never the empty list the pre-audit rows earn honestly (the empty career
// of a real row means exactly one thing: it predates the audit).
func AnswerLearningsHistory(l *learnings.Learnings, p LearningsHistoryParams) (LearningsHistoryResult, error) {
	if p.ID <= 0 {
		return LearningsHistoryResult{}, ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id — history is asked per learning")}
	}
	if !l.Exists(p.ID) {
		return LearningsHistoryResult{}, ErrLearningUnknown.WithWhat("history was asked for learning %d; no row answers to that id", p.ID)
	}
	changes, err := l.History(p.ID)
	if err != nil {
		return LearningsHistoryResult{}, err
	}
	out := make([]LearningsChangeView, 0, len(changes))
	for _, c := range changes {
		out = append(out, LearningsChangeView{
			ID: c.ID, LearningID: c.LearningID, Op: c.Op, Summary: c.Summary,
			ActorKind: c.ActorKind, ActorID: c.ActorID, OccurredAt: c.OccurredAt,
		})
	}
	return LearningsHistoryResult{Changes: out}, nil
}

// AnswerLearningsVocabulary is the closed sets as the CODE ships them —
// no store read, so the offline door speaks the same bytes because they
// are the same function's output.
func AnswerLearningsVocabulary() LearningsVocabularyResult {
	var res LearningsVocabularyResult
	for _, s := range learnings.Scopes() {
		res.Scopes = append(res.Scopes, string(s))
	}
	for _, p := range learnings.Provenances() {
		res.Provenances = append(res.Provenances, string(p))
	}
	res.ActorKinds = learnings.ActorKinds()
	res.M1Ordering = learnings.M1OrderingNote
	res.RetractLaw = "retract is a stamp, never a delete: the body, its audit and its history survive; every default read and the similarity index exclude it until a restore (S52 LM2/LM5)"
	res.Limits.ReasonMin = 1
	res.Limits.ReasonMax = 256
	res.Limits.SummaryMax = 1024
	res.Limits.ForAgentLimitDef = learnings.ForAgentLimitDefault
	res.Limits.ForAgentLimitMax = learnings.ForAgentLimitMax
	res.TagGrammar.Exact = true
	res.TagGrammar.Prefix = true
	return res
}

// AnswerLearningsForAgentQuery is the person's `for-agent` read AND the
// socket door's body: the named agent's readable set, ranked once (LM5),
// every hit carrying its `why`. The rendering is T4's
// AnswerLearningsForAgent — one renderer, both doors and the agent twin.
func AnswerLearningsForAgentQuery(ctx context.Context, l *learnings.Learnings, p LearningsForAgentParams) (LearningsForAgentResult, error) {
	if p.Agent == "" {
		return LearningsForAgentResult{}, ErrBadParams{Err: fmt.Errorf("agent is required — forAgent answers a question about ONE agent, named by its slug")}
	}
	hits, ref := l.ForAgent(ctx, p.Agent, p.Query, p.Limit)
	if ref != nil {
		return LearningsForAgentResult{}, learningsRefusal(ref.Reason)
	}
	return AnswerLearningsForAgent(hits), nil
}
