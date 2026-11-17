// records_roadmap.go — S50 T5 (RM8): the roadmap arms of the records
// service — five since S27 T3 (the graph's two edges joined: they author
// and remove INTENT through the SAME shared answers the person's doors
// run, the store's laws verbatim, the author stamped as the step's slug).
// The original three. Two are reads that forward to the SAME shared answers the
// person's socket doors run — protocol.AnswerRoadmap* is the one rendering
// each door shares, so the tool door and the socket door cannot drift field
// for field (rule 4); roadmap_list twins roadmap.board (RM8's table: a step
// asking "the roadmap" gets the honest board — readiness, blocker, counts —
// not the single-root list), and roadmap_show answers the item beside its
// trail, in one call, out of the SAME two shared answers the person splits
// across show and history. The third files the INERT roadmap_idea row
// through the shared create path: the desk-then-store validator (TD3 — the
// SAME function both doors run, which is what refuses the unknown component
// and the identical open twin, verbatim, here at the desk), the per-agent
// open-proposal cap (S45's named constant, the counted refusal naming it
// and proposal_list), and the author STAMPED from the step's identity —
// never typed (OL3).
//
// Three things this file does NOT do, and each is a rule rather than an
// omission:
//
//   - it never writes the roadmap table (T4's guard family pinned the eight
//     write doors as the person's alone; the one write-shaped arm here
//     creates a proposal row in the proposal tables and touches nothing
//     else — and when a PERSON commits that row, the COMMITTER's identity
//     walks the person's own door (roadmap_add via the seam's Apply) with
//     its own one audit line — this file never holds a pointer to any of
//     the eight write paths, and the read plane it does hold cannot
//     compile one);
//   - it never lets an idea arrive decided: the arm BUILDS the payload
//     field by field from the documented params, so a smuggled "actor",
//     "status", "priorityState", "estimate" or "provenance" has nowhere to
//     ride even past the strict decode — the desk's parse refuses it by
//     name, and an idea that arrived ranked would be an idea that arrived
//     decided;
//   - it never takes an identity from the input (the author is the stamped
//     step; the committing person arrives at commit time from the commit
//     door, not from here).

package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
)

// roadmapList answers roadmap_list through the shared board answer the
// person's roadmap.board door runs — rows with Describe, readiness PAIRED
// with the exact blocker behind it, the loop block, and per-epic counts of
// the FILTERED rows. The refusal of an unknown status rides through verbatim
// (P3: a filter word the ladder does not know is named, not ignored).
func (r *RecordsService) roadmapList(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.roadmapPlane == nil {
		return nil, roadmapPlanelessRefusal("roadmap_list"), nil
	}
	var in protocol.RoadmapBoardParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := protocol.AnswerRoadmapBoard(r.roadmapPlane, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// roadmapShow answers roadmap_show with the item in full beside its trail:
// the shared show answer (and its unknown-row refusal, in the plane's own
// words) plus the shared history page at its default limit. The socket
// splits these across two doors because a UI page opens what it needs; a
// step gets one answer, because a step that has to make two calls will
// make one and guess the other.
func (r *RecordsService) roadmapShow(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.roadmapPlane == nil {
		return nil, roadmapPlanelessRefusal("roadmap_show"), nil
	}
	var in protocol.RoadmapShowParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := protocol.AnswerRoadmapShowFull(ctx, r.roadmapPlane, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// roadmapProposeIdea files the INERT roadmap_idea row — the roadmap plane's
// ONE agent ask (RM8). The refusals come from the seam's desk-then-store
// validator below, not from a second spelling up here: the strict shape
// (WHICH IDEA and NOTE, no rank, size, status or actor), the component root
// read in the registry, and no identical OPEN twin already on the board —
// the SAME function the commit door re-runs (TD3), so what create refuses,
// commit refuses, verbatim, and an ask that adds what the board carries
// never becomes an inert row. The payload is BUILT here, field by field —
// never the raw input carried through — so there is no room for a smuggled
// "priorityState", "estimate", "status" or "actor" even if the strict
// decode above somehow missed one: the desk's parse refusal names it.
//
// Author: the stamped step identity (by), carried to created_by_*; the
// COMMITTER, not this door, owns the item when a person decides — Apply
// writes through roadmap's own create with provenance authored and the
// committing person as actor, one 'add' audit line (T5's seam). Nothing
// outside the proposal tables is touched.
func (r *RecordsService) roadmapProposeIdea(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in protocol.RoadmapProposeIdeaParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	payload, merr := json.Marshal(desk.RoadmapIdeaProposal{
		ComponentRoot: in.ComponentRoot,
		Title:         in.Title,
		Epic:          in.Epic,
		Note:          in.Note,
	})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: roadmap_propose_idea payload will not encode: %w", merr)
	}
	if f := r.openProposalsCap(by); f != nil {
		return nil, f, nil
	}
	if err := r.validateProposal(ctx, string(desk.KindRoadmapIdea), string(payload)); err != nil {
		// The validator's sentence travels VERBATIM (TD3: what create refuses,
		// commit refuses, in the same words — and an ask naming an unknown
		// component, or re-asking what the board already carries open, never
		// becomes an inert row).
		return nil, &Failure{Sentence: err.Error()}, nil
	}
	created, err := r.proposals.CreateProposal(ctx, string(desk.KindRoadmapIdea), payload, by.Kind, by.ID)
	if err != nil {
		return nil, nil, err
	}
	created.Command = protocol.ProposalCommand(created.ID)
	return protocol.PolicyProposeResult{Proposal: created, Note: protocol.PolicyProposeInertNote}, nil, nil
}

// roadmapDepend answers roadmap_depend through the SAME shared answer the
// person's roadmap.depend door runs — protocol.AnswerRoadmapDepend is the
// one road both doors travel (rule 4; AnswerTriageFileAs's precedent for a
// WRITE twin). The author is the STAMPED step resolved to its agent slug
// (OL3 — the store's control vocabulary admits an agent actor with an id;
// an edge is intent, and intent has always been sayable by a worker: the
// refusal sentences — self, cross-root, cycle-naming-closure, cancelled,
// duplicate, terminal — travel verbatim from the store through the shared
// answer, which is the whole point of sharing it. Nothing here softens a
// law; nothing here enforces one either.
func (r *RecordsService) roadmapDepend(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.roadmapPlane == nil {
		return nil, roadmapPlanelessRefusal("roadmap_depend"), nil
	}
	var in protocol.RoadmapDependParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	slug, f2 := r.agentSlug(ident.Agent)
	if f2 != nil {
		return nil, f2, nil
	}
	out, err := protocol.AnswerRoadmapDepend(r.roadmapPlane, roadmap.Actor{Kind: "agent", ID: slug}, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// roadmapUndepend answers roadmap_undepend — AnswerRoadmapDepend's twin,
// same shared answer, same stamped author. Removing an edge touches the
// graph and nothing else; an item already running may un-wish.
func (r *RecordsService) roadmapUndepend(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.roadmapPlane == nil {
		return nil, roadmapPlanelessRefusal("roadmap_undepend"), nil
	}
	var in protocol.RoadmapUndependParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	slug, f2 := r.agentSlug(ident.Agent)
	if f2 != nil {
		return nil, f2, nil
	}
	out, err := protocol.AnswerRoadmapUndepend(r.roadmapPlane, roadmap.Actor{Kind: "agent", ID: slug}, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// roadmapPlanelessRefusal is the V13 shape for the family: the roadmap
// service is what honours these doors (the board's readiness sentences, the
// blocker pairing, the counts, the trail), and a mount without it says so,
// never answering an empty board.
func roadmapPlanelessRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the roadmap plane, and this mount did not wire it — absent is stated, never faked (V13)", name)}
}
