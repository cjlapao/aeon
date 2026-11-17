// roadmap_reads.go — S50 T5 (rule 4): the ONE rendering behind the roadmap
// read doors and their tool twins.
//
// The catalog gives a step roadmap_list and roadmap_show; the socket gives
// a person roadmap.board, roadmap.show and roadmap.history. Both doors must
// answer with the SAME bytes, and the only honest way to promise that is
// for both to run ONE function — the same law the automation reads obey
// (protocol.AnswerAutomation*). Each Answer here takes the already-decoded
// params and the seam the caller checked, and returns the SAME result value
// the socket handler returns; the handlers below became thin: decode, then
// Answer. A rendering fork between person and step is how RM4 leaks a
// second truth, and thin handlers cannot fork.

package protocol

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/roadmap"
)

// AnswerRoadmapList is roadmap.list's answer, shared with no one else (the
// tool twin of the LIST shape is roadmap_list → roadmap.board, RM8's table:
// a step asking "the roadmap" gets the honest board, with readiness and
// blocker — the richer door).
// roadmapListShape and roadmapShowShape are the shape refusals split out as
// named functions so BOTH doors can run them before the seam is even
// resolved — the house law is that a shape refusal must not depend on
// whether a store was wired (TestRoadmapHandlersRefuseShapelessRequests;
// one definition, and the Answer below runs it too so the arm that holds a
// live plane runs the same words).
func roadmapListShape(p RoadmapListParams) error {
	if p.ComponentRoot == "" {
		return ErrBadParams{Err: fmt.Errorf("componentRoot is required — the roadmap is scoped to one project (N2: no cross-project view)")}
	}
	return nil
}

func roadmapShowShape(p RoadmapShowParams) error {
	if p.ID <= 0 {
		return ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id")}
	}
	return nil
}

func AnswerRoadmapList(r *roadmap.Roadmap, p RoadmapListParams) (any, error) {
	if err := roadmapListShape(p); err != nil {
		return nil, err
	}
	items, err := r.List(p.ComponentRoot, roadmap.ListFilter{
		Epic: p.Epic, Priority: roadmap.PriorityState(p.PriorityState), Status: roadmap.Status(p.Status),
	})
	if err != nil {
		return nil, err
	}
	views := make([]RoadmapItemView, 0, len(items))
	for _, it := range items {
		v, err := roadmapGraphFields(r, it)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return RoadmapListResult{ComponentRoot: p.ComponentRoot, Items: views}, nil
}

// RoadmapShowResult is roadmap.show's answer: the item view PLUS the
// daemon's own one-sentence Describe of it (S50 T6 — the dispatch
// seatbelt prints the authority statement with the item's Describe, and
// the sentence is the daemon's RM5 formatter, never a client-side
// re-rendering; `describe` is additive, every pre-S50 reader of show's
// answer reads `item` untouched).
type RoadmapShowResult struct {
	Item     RoadmapItemView `json:"item"`
	Describe string          `json:"describe"`
}

// AnswerRoadmapShow is roadmap.show's answer, one item in full.
func AnswerRoadmapShow(r *roadmap.Roadmap, p RoadmapShowParams) (any, error) {
	if err := roadmapShowShape(p); err != nil {
		return nil, err
	}
	it, ok, err := r.Show(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, roadmapRefusal(fmt.Sprintf("roadmap item %d does not exist", p.ID))
	}
	v, err := roadmapGraphFields(r, it)
	if err != nil {
		return nil, err
	}
	return RoadmapShowResult{Item: v, Describe: roadmap.Describe(it)}, nil
}

// RoadmapShowFullResult is the TOOL answer for roadmap_show: the item in
// full WITH the trail beside it. The socket splits these into two doors
// (show and history — a UI page opens what it needs); a step gets one
// answer, because a step that has to make two calls will make one and
// guess the other. Both halves are the shared views — nothing here is a
// second rendering.
type RoadmapShowFullResult struct {
	Item    RoadmapItemView      `json:"item"`
	Changes []RoadmapHistoryView `json:"changes"`
}

// AnswerRoadmapShowFull composes the two shared answers — show's row (and
// its unknown-row refusal, in show's own words) and history's page, in
// history's own default for a step that asks no limit.
func AnswerRoadmapShowFull(ctx context.Context, r *roadmap.Roadmap, id int64) (*RoadmapShowFullResult, error) {
	out, err := AnswerRoadmapShow(r, RoadmapShowParams{ID: id})
	if err != nil {
		return nil, err
	}
	item := out.(RoadmapShowResult).Item
	rows, err := AnswerRoadmapHistory(ctx, r, RoadmapHistoryParams{ID: id})
	if err != nil {
		return nil, err
	}
	return &RoadmapShowFullResult{
		Item:    item,
		Changes: rows.(RoadmapHistoryResult).Changes,
	}, nil
}

// AnswerRoadmapBoard is roadmap.board's answer: rows with their Describe,
// readiness PAIRED with the sentence behind it, and the loop block — the
// cross-component view, also what a step's roadmap_list gets.
func AnswerRoadmapBoard(r *roadmap.Roadmap, p RoadmapBoardParams) (any, error) {
	ans, ref := r.Board(roadmap.BoardFilter{
		ComponentRoot: p.ComponentRoot, Status: roadmap.Status(p.Status), Epic: p.Epic,
	})
	if ref != nil {
		return nil, roadmapRefusal(ref.Reason)
	}
	out := RoadmapBoardResult{
		Rows:   make([]RoadmapBoardRowView, 0, len(ans.Rows)),
		Groups: ans.Groups,
	}
	for _, row := range ans.Rows {
		v := RoadmapBoardRowView{
			Item: roadmapItemView(row.Item), Describe: row.Describe,
			Ready: row.Ready, Blocker: row.Blocker,
		}
		if row.Loop != nil {
			v.Loop = &RoadmapBoardLoopView{Slug: row.Loop.Slug, Status: row.Loop.Status}
		}
		out.Rows = append(out.Rows, v)
	}
	return out, nil
}

// AnswerRoadmapHistory is roadmap.history's answer: the trail, which is
// facts already decided — the view reshapes nothing about them.
func AnswerRoadmapHistory(ctx context.Context, r *roadmap.Roadmap, p RoadmapHistoryParams) (any, error) {
	if err := roadmapShowShape(RoadmapShowParams{ID: p.ID}); err != nil {
		return nil, err
	}
	if p.Limit < 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("limit cannot be negative — 0 means the default page")}
	}
	if _, found, err := r.Show(p.ID); err != nil {
		return nil, err
	} else if !found {
		return nil, ErrRoadmapUnknown.WithWhat("no roadmap item %d exists — the trail belongs to a row", p.ID)
	}
	rows, err := r.History(ctx, p.ID, p.Limit)
	if err != nil {
		return nil, err
	}
	out := RoadmapHistoryResult{Changes: make([]RoadmapHistoryView, 0, len(rows))}
	for _, ch := range rows {
		out.Changes = append(out.Changes, RoadmapHistoryView{
			ID: ch.ID, ItemID: ch.ItemID, Op: ch.Op, Summary: ch.Summary,
			ActorKind: ch.ActorKind, ActorID: ch.ActorID, OccurredAt: ch.OccurredAt,
		})
	}
	return out, nil
}
