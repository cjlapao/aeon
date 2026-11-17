// dod.go — S57 T8 (VG18): the two DoD methods — the daemon's surface for
// the loop's definition-of-done ledger: the read (dod.list, the loop's
// criteria with their states and the evidence each met item names) and
// the human-door write (dod.mark, evidence-bound: a met item names its
// evidence, and the store's pairing backstop says whose mark a kind of
// evidence is — manual is a person's, verify_run the engine's).
//
// The seam is the daemon's DoDStore (internal/daemon/dod_service.go).
// The design's one line "dod.list {loop}" names the loop by SLUG; the
// store's Scope needs BOTH halves (slug and root), so the daemon
// resolves the slug to the loop's own root before it asks (a loop names
// both halves — the store's Scope type says why). A slug no loop carries
// is a typed answer, not an empty list.
//
// The write stamps the actor from the CONNECTION (Table.ActorFor, the
// kernel peer credential), never from params — rejectGateIdentityKeys
// refuses an identity key in the wire, and TestNoMethodAcceptsAnActorParam
// pins the family. A manual mark is a person's act: the store refuses a
// non-user caller before SQL, so the socket's peer is the mark.
//
// The method name constants live HERE, beside their family, and not in
// method.go (the SIZE1 ruling, verify.go's header).

package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// The two `dod.*` method names live in method.go's S57 block — protogen
// enumerates that file alone, and the app's contract is what T11's panel
// reads (the DoD card, on the loop screen).

// --- the seam --------------------------------------------------------------------

// DoDStore is the daemon's DoD seam: the ledger's two doors. A
// *DoDRefusal is the engine's refusal rendered as data (DB19), never an
// error.
type DoDStore interface {
	// List answers dod.list: the loop's criteria (all briefs, in their
	// order), each with its state and — when met — the evidence it names
	// (kind and ref) and the mark's actor.
	List(ctx context.Context, loopSlug string) (DoDListResult, error)
	// Mark answers dod.mark: one item's state, evidence-bound. The
	// actor is the connection's person; a met item refuses without
	// evidence, and the store's pairing backstop decides whose mark a
	// kind of evidence is.
	Mark(ctx context.Context, actor Actor, in DoDMarkInput) (DoDWriteResult, *DoDRefusal, error)
}

// DoDRefusal is a refused DoD act as data (DB19): Kind is the store's
// closed refusal vocabulary (the machine can branch on it), Detail the
// person's sentence, printed verbatim.
type DoDRefusal struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// DoDMarkStateValues is the mark's closed set (S57 T10, VG20): defined
// ONCE here — the daemon's dod.mark agent-door check reads it, and the
// CLI's offline usage reads the same constants (dod has no vocabulary
// method; this is the whole closed set, so this is its one home).
// `unchecked` is deliberately absent: it is the materialise state, never
// a mark.
var DoDMarkStateValues = []string{"met", "unmet", "unverifiable"}

// DoDMarkInput is the mark: the item's id, the new state, and — when the
// state is met — its evidence (kind and ref; the store's closed sets
// are the backstop, and the refusal names the value it refused).
type DoDMarkInput struct {
	ID       string
	State    string
	Evidence *DoDEvidence
}

// DoDEvidence is the evidence a met item names: kind (the store's closed
// set: verify_run | manual) and ref (the run's id, or the person's
// reference for a manual mark).
type DoDEvidence struct {
	Kind string
	Ref  string
	// Note — S57 T9: the mark's free-text reason. Audit text (the ledger's
	// event row carries it); nothing reads a verdict from it.
	Note string
}

// --- the wire views -----------------------------------------------------------------

// DoDItemView is one criterion as the wire carries it: the text (the
// criterion's own words — the byte-equality the carry-over rule is
// about), the state, and, when met, the evidence and the mark's actor
// (who marked it, from the connection that did).
type DoDItemView struct {
	ID            string  `json:"id"`
	LoopSlug      string  `json:"loopSlug"`
	ComponentRoot string  `json:"componentRoot"`
	BriefID       string  `json:"briefId"`
	BriefVersion  int     `json:"briefVersion"`
	Idx           int     `json:"idx"`
	Text          string  `json:"text"`
	State         string  `json:"state"` // unchecked | met | unmet | unverifiable
	EvidenceKind  string  `json:"evidenceKind,omitempty"`
	EvidenceRef   string  `json:"evidenceRef,omitempty"`
	ActorKind     *string `json:"actorKind,omitempty"`
	ActorID       *string `json:"actorId,omitempty"`
	UpdatedAt     string  `json:"updatedAt"`
}

type DoDListResult struct {
	Loop  string        `json:"loop"`
	Items []DoDItemView `json:"items"`
}

type DoDWriteResult struct {
	Item DoDItemView `json:"item"`
}

// --- the params -----------------------------------------------------------------------

type DoDListParams struct {
	Loop string `json:"loop"`
}

type DoDMarkParams struct {
	ID       string       `json:"id"`
	State    string       `json:"state"`
	Evidence *DoDEvidence `json:"evidence,omitempty"`
}

// --- the refusal rendering ---------------------------------------------------------------

func dodRefusedError(verb string, r *DoDRefusal) *Error {
	e := ErrDodRefused.WithWhat("dod %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	return e
}

func (t *Table) dodSeam() error {
	if t.DoD == nil {
		return ErrStoreUnavailable.WithWhat("dod.* needs the DoD seam, and this build did not link it — the DoD surface is absent for the same reason (V13)")
	}
	return nil
}

// --- the handlers -------------------------------------------------------------------------

func (t *Table) handleDoDList(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.dodSeam(); err != nil {
		return nil, err
	}
	var p DoDListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Loop) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("dod.list needs the loop's slug")}
	}
	res, err := t.DoD.List(ctx, p.Loop)
	if err != nil {
		return nil, err
	}
	if res.Items == nil {
		res.Items = []DoDItemView{}
	}
	return res, nil
}

func (t *Table) handleDoDMark(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.dodSeam(); err != nil {
		return nil, err
	}
	if err := rejectGateIdentityKeys(string(MethodDoDMark), params); err != nil {
		return nil, err
	}
	var p DoDMarkParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("dod.mark needs the item's id")}
	}
	var ev *DoDEvidence
	if p.Evidence != nil {
		ev = &DoDEvidence{Kind: p.Evidence.Kind, Ref: p.Evidence.Ref, Note: p.Evidence.Note}
	}
	actor, err := t.gateActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	res, refusal, err := t.DoD.Mark(ctx, actor, DoDMarkInput{
		ID: p.ID, State: p.State, Evidence: ev,
	})
	if refusal != nil {
		return nil, dodRefusedError("mark", refusal)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// RegisterDoD conditionally registers the two DoD methods (V13: a nil
// seam leaves them ABSENT — the daemon reports the family as
// unavailable, not as a set of registered doors that all refuse).
func (t *Table) RegisterDoD() bool {
	if t.DoD == nil {
		return false
	}
	t.register(&Method{Name: MethodDoDList, RequiresInitialize: true, Handle: t.handleDoDList})
	t.register(&Method{Name: MethodDoDMark, RequiresInitialize: true, Handle: t.handleDoDMark})
	return true
}
