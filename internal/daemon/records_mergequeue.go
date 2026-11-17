// records_mergequeue.go — S60 T9: the merge-queue plane's FIVE catalog arms
// (MQ17, standing rule 2), answered over the SAME services the person's
// doors answer through — rule 4, one implementation, two doors: the three
// reads forward to the queue door service's own List/Show/Status (the
// identical bytes TestToolReadsMatchSocketReads pins), the own-item dequeue
// runs the queue's own Service.Dequeue with the AGENT as the store event
// actor (the ledger's closed set names `agent` precisely so this write can
// say who performed it), and the propose arm files an INERT
// mergequeue_enqueue proposal through the SAME desk validator the commit
// re-runs (TD3/MQ14 — one ValidateEnqueue, gathered facts, verbatim
// refusals).
//
// What these arms never do:
//   - reach a decision tool. enqueue-direct, reorder, retry, land, settings
//     and the six loop actions have no arm here to call — T8's guard
//     refuses each such name by word, and the surface walk proves the five
//     names below are the whole agent surface of the plane (MQ17);
//   - see another project's queue. own-view is structural: the roots an
//     agent's steps have ACTUALLY run in (store.AgentProjectRoots — S52
//     LM5's source, the step row as the running) plus the root the calling
//     step itself runs under. An item of another repository is not loudly
//     withheld, it is not in the view; show answers an out-of-view item with
//     the SAME sentence as a nonexistent id (the plane's no-oracle, LM5's
//     law);
//   - take an identity, a status, an attendedness or a base from input.
//     Identity is the step's (OL3), status is the queue's, attendedness is
//     the kind's (a committed ask is always unattended — MQ14.3), and the
//     base is the daemon's to resolve (MQ15). The strict decode refuses a
//     typed field at the door; the desk's parse refusal catches it again in
//     the payload.

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// maxMergeQueueReasonBytes is the ask's reason ceiling — the same 512 every
// propose arm holds for the sentence a person reads at the desk (T9 names it
// again in the kind's own payload validator; both must say 512).
const maxMergeQueueReasonBytes = 512

// mqOwnRoots answers the plane's own-view question: which repositories has
// THIS agent actually run in? The step table is the answer (S52 LM5's
// source — step.agent names the runner, step.component_root the ground),
// plus the root the calling step itself stands on, which a first step may
// not yet have written under its own name.
func (r *RecordsService) mqOwnRoots(ident Identity) (map[string]bool, *Failure, error) {
	if ident.Agent == "" {
		return nil, &Failure{Sentence: "refused: the merge-queue tools answer for the agent the step runs as, and this mount stamped none — identity is the daemon's, never typed (OL3)"}, nil
	}
	roots, err := r.st.AgentProjectRoots(ident.Agent)
	if err != nil {
		return nil, nil, err
	}
	set := make(map[string]bool, len(roots)+1)
	for _, root := range roots {
		set[root] = true
	}
	if ident.ComponentRoot != "" {
		set[ident.ComponentRoot] = true
	}
	return set, nil, nil
}

// mqNoItemSentence is show's single answer for an id that is not there and
// an id not in the view — one string, so no answer can ever prove that
// somebody else has something queued.
func mqNoItemSentence(id string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: no merge-queue item %s is yours to see — it may not exist, or it may live in a repository your loops have never run in; mergequeue_list shows the view you do have", id)}
}

// mergequeueList answers mergequeue_list through the standing queue door
// service — the same List the person's mergequeue.list runs — then applies
// the own-view filter the agent's surface adds (the person sees the machine;
// the agent sees its repositories, nothing louder).
func (r *RecordsService) mergequeueList(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mergeQueue == nil {
		return nil, mergeQueuePlanelessRefusal("mergequeue_list"), nil
	}
	var in struct {
		ComponentRoot   string `json:"componentRoot"`
		IncludeTerminal bool   `json:"includeTerminal"`
		Limit           int    `json:"limit"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	roots, f, err := r.mqOwnRoots(ident)
	if f != nil || err != nil {
		return nil, f, err
	}
	out, err := r.mergeQueue.List(ctx, protocol.MergeQueueListParams{
		ComponentRoot:   strings.TrimSpace(in.ComponentRoot),
		IncludeTerminal: in.IncludeTerminal,
	})
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]int{}
	view := make([]protocol.ItemView, 0, len(out))
	for _, item := range out {
		if !roots[item.ComponentRoot] {
			continue // not withheld loudly — simply not this view (LM5's law)
		}
		key := item.ComponentRoot + "\x00" + item.Base
		seen[key]++
		if in.Limit > 0 && seen[key] > in.Limit {
			continue
		}
		view = append(view, item)
	}
	return view, nil, nil
}

// mergequeueShow answers mergequeue_show for one item: the gate reads the
// row first (unknown and out-of-view collapse into ONE sentence), and the
// answer itself is the person's own Show — same service, same bytes, digest
// and ledger included (rule 4).
func (r *RecordsService) mergequeueShow(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mergeQueue == nil {
		return nil, mergeQueuePlanelessRefusal("mergequeue_show"), nil
	}
	var in struct {
		ID string `json:"id"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return nil, &Failure{Sentence: "refused: mergequeue_show needs an id — mergequeue_list shows what exists, and a show of nothing is a refusal, not an empty answer"}, nil
	}
	roots, f, err := r.mqOwnRoots(ident)
	if f != nil || err != nil {
		return nil, f, err
	}
	item, err := r.st.ReadMergeItem(id)
	if err != nil {
		// The gate never distinguishes an unknown id from an out-of-view one;
		// a store FAULT also collapses here on purpose — the no-oracle law is
		// worth more than a debug hint, and the fault leaves the daemon log.
		return nil, mqNoItemSentence(id), nil
	}
	if !roots[item.ComponentRoot] {
		return nil, mqNoItemSentence(id), nil
	}
	out, err := r.mergeQueue.Show(ctx, protocol.MergeQueueShowParams{ID: id})
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// mergequeueStatus answers mergequeue.status for ONE repository the agent
// has run in — refused by name for a root outside the view (a status IS a
// claim about a project, where an absent list row is not).
func (r *RecordsService) mergequeueStatus(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mergeQueue == nil {
		return nil, mergeQueuePlanelessRefusal("mergequeue_status"), nil
	}
	var in struct {
		ComponentRoot string `json:"componentRoot"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	root := strings.TrimSpace(in.ComponentRoot)
	if root == "" {
		return nil, &Failure{Sentence: "refused: mergequeue_status names one repository — mergequeue_list shows the roots you have run in"}, nil
	}
	roots, f, err := r.mqOwnRoots(ident)
	if f != nil || err != nil {
		return nil, f, err
	}
	if !roots[root] {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %s is not a repository your loops have run in — status is a claim about a project, and the queue answers your view, not the map (S60 MQ17)", root)}, nil
	}
	out, err := r.mergeQueue.Status(ctx, protocol.MergeQueueStatusParams{ComponentRoot: root})
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// mergequeueDequeue is the plane's ONE agent write and the ONLY direct one
// (MQ17): this loop's own item, only while queued or conflicted — a
// removal can only narrow what the queue may do, so it needs no proposal.
// An item of another loop, or one further along than conflicted, is refused
// by name with the sentence of what a person may do; the item is NOT paused
// — the row leaves, and an agent's own resolution loop is left to end
// honestly (the driver's LC guards refuse to move a removed row; a person's
// dequeue puts loops down first, which is the door's and not this path's).
func (r *RecordsService) mergequeueDequeue(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mergeQueue == nil {
		return nil, mergeQueuePlanelessRefusal("mergequeue_dequeue"), nil
	}
	var in struct {
		ID string `json:"id"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return nil, &Failure{Sentence: "refused: mergequeue_dequeue names the item to take out — mergequeue_list shows yours"}, nil
	}
	item, err := r.st.ReadMergeItem(id)
	if err != nil {
		return nil, mqNoItemSentence(id), nil
	}
	if item.LoopSlug == "" || item.LoopSlug != ident.LoopSlug {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %s is not the item THIS loop carries (it belongs to %s) — another loop's item is that loop's — or a person's — to remove (S60 MQ17)", id, orNone(item.LoopSlug))}, nil
	}
	if item.Status != "queued" && item.Status != "conflicted" {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %s is %s, not queued or conflicted — an item in flight is being integrated and stopping that takes a person at mergequeue.dequeue (a tool does not interrupt a merge already underway)", id, item.Status)}, nil
	}
	fresh, err := mergequeue.NewService(r.st).Dequeue(id, store.MergeActor{Kind: "agent", ID: ident.Agent})
	if err != nil {
		var mr store.MergeRefusal
		if errors.As(err, &mr) {
			return nil, &Failure{Sentence: mr.Error()}, nil
		}
		return nil, nil, err
	}
	return mqItemView(fresh), nil, nil
}

// mergequeueProposeEnqueue files the plane's ONE ask (MQ14.3): the strict
// shape, the row-less OWNERSHIP refusals the socket door would give — the
// SAME validator over gathered facts through r.validateProposal (TD3: the
// desk's kind validator the commit re-runs, ValidateEnqueue included, the
// sentence travelling verbatim) — then an inert row. The payload is BUILT
// field by field from typed values, never the raw input; the base, the
// attendedness and the stamp are nobody's to type.
func (r *RecordsService) mergequeueProposeEnqueue(ctx context.Context, by Actor, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mergeQueue == nil {
		return nil, mergeQueuePlanelessRefusal("mergequeue_propose_enqueue"), nil
	}
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in struct {
		ComponentRoot string `json:"componentRoot"`
		Branch        string `json:"branch"`
		Reason        string `json:"reason"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	root := strings.TrimSpace(in.ComponentRoot)
	if root == "" {
		return nil, &Failure{Sentence: "refused: mergequeue_propose_enqueue names the repository by its component root — an empty root asks the queue to guess which project to land things into"}, nil
	}
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		return nil, &Failure{Sentence: "refused: mergequeue_propose_enqueue names the branch to queue — an empty branch is not a thing a queue can carry"}, nil
	}
	if strings.TrimSpace(in.Reason) == "" {
		return nil, &Failure{Sentence: "refused: mergequeue_propose_enqueue needs a reason — an unreasoned ask is not an ask a person can decide"}, nil
	}
	if len(in.Reason) > maxMergeQueueReasonBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: mergequeue_propose_enqueue reason is %d bytes and the cap is %d — a sentence for the desk, not a justification essay", len(in.Reason), maxMergeQueueReasonBytes)}, nil
	}
	// The ask is scoped to the view too: an agent asking another project's
	// branch into a queue it cannot see is the map leak MC2 refuses — the
	// step's identity answers the view, never a typed root.
	roots, f, err := r.mqOwnRoots(ident)
	if f != nil || err != nil {
		return nil, f, err
	}
	if !roots[root] {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %s is not a repository your loops have run in — an ask about a project you have never worked in is not a proposal, it is a map of someone else's queue", root)}, nil
	}
	payload, merr := json.Marshal(desk.MergeQueueEnqueueProposal{
		ComponentRoot: root,
		Branch:        branch,
		Reason:        in.Reason,
	})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: mergequeue_enqueue payload will not encode: %w", merr)
	}
	if len(payload) > 8*1024 {
		return nil, &Failure{Sentence: "refused: the mergequeue_enqueue payload passed 8 KiB — an ask is a sentence, not a document"}, nil
	}
	if f := r.openProposalsCap(by); f != nil {
		return nil, f, nil
	}
	if err := r.validateProposal(ctx, string(desk.KindMergeQueueEnqueue), string(payload)); err != nil {
		// The validator's sentence travels VERBATIM (TD3: what create
		// refuses, commit refuses, in the same words — and MQ14's ONE
		// ValidateEnqueue is exactly what this call reaches).
		return nil, &Failure{Sentence: err.Error()}, nil
	}
	created, err := r.proposals.CreateProposal(ctx, string(desk.KindMergeQueueEnqueue), payload, by.Kind, by.ID)
	if err != nil {
		return nil, nil, err
	}
	created.Command = protocol.ProposalCommand(created.ID)
	return protocol.PolicyProposeResult{Proposal: created, Note: protocol.PolicyProposeInertNote}, nil, nil
}

// orNone is the refusal grammar for a missing owner: "no loop" says what IS
// true (a person enqueued that row) rather than printing an empty backtick.
func orNone(s string) string {
	if s == "" {
		return "no loop — a person enqueued it"
	}
	return s
}

// mergeQueuePlanelessRefusal is the V13 shape for the family: the queue door
// service is what honours these reads (the staleness three-state, the
// digest, the state machine's own Describe), and a mount without it says
// so, never answering zero or an empty listing.
func mergeQueuePlanelessRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the merge-queue plane, and this mount did not wire it — absent is stated, never faked (V13)", name)}
}
