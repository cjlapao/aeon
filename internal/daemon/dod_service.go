// dod_service.go — S57 T8 stage 4: the daemon's DoD family — the two
// methods of protocol.DoDStore over the real store (list · mark).
//
// The DoD ledger is a LOOP's (its rows are scoped by (loop_slug,
// component_root) — the migration's own pair), so the one param the seam
// carries, the loop slug, must resolve to a root before the store can
// read: exactly one loop with the slug ⇒ its root; none ⇒ loop-unknown;
// more than one ⇒ loop-slug-ambiguous — the same two-state resolution
// the merge plane uses, typed refusals rather than an empty list (an
// empty DoD would read as "nothing to finish", which is a lie when the
// truth is "that loop is nowhere" or "that slug is two loops").
//
// Mark carries the peer as actor: a mark is a person's word for a
// criterion's state, and the store's own pairing backstop decides whose
// mark a kind of evidence is (a verify_run's mark belongs to the run's
// actor). A refused mark is data (DB19) — kind and the store's sentence,
// the row exactly as it was; a no-op mark (the state it already is)
// succeeds quietly, the store's honest double-state.

package daemon

import (
	"context"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// DoDService is the daemon's DoD half (see the file header).
type DoDService struct {
	st  *store.Store
	log Logger
}

// NewDoDService answers nil for a storeless daemon (V13) — the wiring
// leaves table.DoD nil and RegisterDoD leaves the two ABSENT, not
// registered-and-refusing.
func NewDoDService(st *store.Store, log Logger) *DoDService {
	if st == nil {
		return nil
	}
	if log == nil {
		log = discardLogger{}
	}
	return &DoDService{st: st, log: log}
}

var _ protocol.DoDStore = (*DoDService)(nil)

// List answers dod.list: the loop's criteria (all briefs, in their
// order), each with its state and — when met — the evidence it names and
// the mark's actor.
func (s *DoDService) List(ctx context.Context, loopSlug string) (protocol.DoDListResult, error) {
	root, err := s.loopRootFor(loopSlug)
	if err != nil {
		return protocol.DoDListResult{}, err
	}
	items, err := s.st.ListDoD(store.Scope{LoopSlug: loopSlug, ComponentRoot: root})
	if err != nil {
		return protocol.DoDListResult{}, err
	}
	out := protocol.DoDListResult{Loop: loopSlug}
	out.Items = make([]protocol.DoDItemView, 0, len(items))
	for _, it := range items {
		out.Items = append(out.Items, protocol.DoDItemView{
			ID: it.ID, LoopSlug: it.LoopSlug, ComponentRoot: it.ComponentRoot,
			BriefID: it.BriefID, BriefVersion: it.BriefVersion, Idx: it.IDx, Text: it.Text,
			State: it.State, EvidenceKind: it.EvidenceKind, EvidenceRef: it.EvidenceRef,
			ActorKind: it.ActorKind, ActorID: it.ActorID, UpdatedAt: it.UpdatedAt,
		})
	}
	return out, nil
}

// Mark answers dod.mark: one item's state, evidence-bound. A met item
// refuses without its evidence; the store's closed sets (state, evidence
// kind) are the backstop and name the value they refused.
func (s *DoDService) Mark(ctx context.Context, actor protocol.Actor, in protocol.DoDMarkInput) (protocol.DoDWriteResult, *protocol.DoDRefusal, error) {
	ev := store.DodEvidence{}
	if in.Evidence != nil {
		ev = store.DodEvidence{Kind: in.Evidence.Kind, Ref: in.Evidence.Ref, Note: in.Evidence.Note}
	}
	w, err := s.st.MarkDoD(gateStoreActor(actor), in.ID, in.State, ev)
	if err != nil {
		return protocol.DoDWriteResult{}, nil, err
	}
	if !w.OK() {
		return protocol.DoDWriteResult{}, &protocol.DoDRefusal{Kind: string(w.Refusal), Detail: gateDetail(w.Reason)}, nil
	}
	return protocol.DoDWriteResult{Item: protocol.DoDItemView{
		ID: w.Item.ID, LoopSlug: w.Item.LoopSlug, ComponentRoot: w.Item.ComponentRoot,
		BriefID: w.Item.BriefID, BriefVersion: w.Item.BriefVersion, Idx: w.Item.IDx, Text: w.Item.Text,
		State: w.Item.State, EvidenceKind: w.Item.EvidenceKind, EvidenceRef: w.Item.EvidenceRef,
		ActorKind: w.Item.ActorKind, ActorID: w.Item.ActorID, UpdatedAt: w.Item.UpdatedAt,
	}}, nil, nil
}

// loopRootFor resolves the slug to its root (the DoD's scope is a pair):
// one loop ⇒ its root; none ⇒ loop-unknown; more than one ⇒
// loop-slug-ambiguous. Both refusals are typed — the family's code, the
// store's voice, no data the client cannot branch on.
func (s *DoDService) loopRootFor(slug string) (string, error) {
	loops, err := s.st.LoopsBySlug(slug)
	if err != nil {
		return "", err
	}
	switch len(loops) {
	case 0:
		e := protocol.ErrDodRefused.WithWhat("dod list refused: %s is not a loop", slug)
		e.Data.Kind = "loop-unknown"
		return "", e
	case 1:
		return loops[0].ComponentRoot, nil
	default:
		e := protocol.ErrDodRefused.WithWhat("dod list refused: %s is a loop slug in %d roots — a slug is unique per root, not global; the DoD lives in one loop, and a slug must name one", slug, len(loops))
		e.Data.Kind = "loop-slug-ambiguous"
		return "", e
	}
}
