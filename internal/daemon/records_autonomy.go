package daemon

// records_autonomy.go — S56 T8 (AL11, 2026-11-01): the autonomy family's
// five catalog arms, implemented ON the records service so each has its
// socket twin and its tool through the one Do (internal/records' catalog is
// the definition).
//
// THE READS (autonomy_list, autonomy_show, autonomy_effective) answer
// through the SAME protocol.AutonomyService the socket's autonomy.* methods
// read through — one implementation, two doors, the seed_status precedent
// generalised; the parity walk pins both directions every run. The agent
// filter set is deliberately the schema's: rootId and includeInactive. The
// socket's list carries a full state filter too; the TOOL schema does not
// carry a state field (the spec's "no state field anywhere" rule wins over
// its own spec table — recorded as a deviation in T8's Notes), so the arm
// passes "" for it.
//
// THE PROPOSES (autonomy_propose_revoke, autonomy_propose_deny) create an
// INERT proposal row of the two restrictive kinds through the SAME create
// path as proposal.create (records_policy.go's createInertProposal: size
// cap, per-agent cap, the shared validator — TD3, refused means no row —
// then the shared ProposalStore). They never touch the grant tables: no
// read of the authoring surface appears in this file. Unlike the policy
// arms there is NO arm-level reason check: the reason rule (1–512 bytes)
// lives in the seam's desk half (desk.ProposeReasonSentence), which the
// shared validator runs — one definition, the same bytes at create and at
// commit (the create/commit parity test compares the sentence).
//
// AL11 at this layer: a grant is never created, widened or revoked by
// anything in this file — the writes here are proposal rows, and the two
// kinds are the restrictive ones (revoke, deny). No schema, payload or
// sentence on these paths carries the word allow.
import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
)

// — the three reads —

// autonomylessRefusal is the V13 half: a mount without the autonomy seam
// (storeless, or the seam not wired) says so rather than answering zero —
// the same three-state honesty the policy arms keep.
func autonomylessRefusal(tool string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the autonomy surface, and this build did not wire it — the autonomy methods are absent for the same reason (V13)", tool)}
}

func (r *RecordsService) autonomyList(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.autonomyService == nil {
		return nil, autonomylessRefusal("autonomy_list"), nil
	}
	var in struct {
		RootID          string `json:"rootId"`
		IncludeInactive bool   `json:"includeInactive"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	grants, err := r.autonomyService.List(ctx, in.RootID, "", in.IncludeInactive)
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return protocol.AutonomyListResult{Grants: grants}, nil, nil
}

func (r *RecordsService) autonomyShow(ctx context.Context, input json.RawMessage) (any, *Failure, error) {
	if r.autonomyService == nil {
		return nil, autonomylessRefusal("autonomy_show"), nil
	}
	var in struct {
		ID string `json:"id"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	grant, lastChange, usedCount, pinnedLoops, ref, err := r.autonomyService.Show(ctx, in.ID)
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	if ref != nil {
		return nil, &Failure{Sentence: ref.Sentence}, nil
	}
	return protocol.AutonomyShowResult{
		Grant:       grant,
		LastChange:  lastChange,
		UsedCount:   usedCount,
		PinnedLoops: pinnedLoops,
	}, nil, nil
}

func (r *RecordsService) autonomyEffective(ctx context.Context, input json.RawMessage) (any, *Failure, error) {
	if r.autonomyService == nil {
		return nil, autonomylessRefusal("autonomy_effective"), nil
	}
	var in struct {
		RootID string   `json:"rootId"`
		Labels []string `json:"labels"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	view, ref, err := r.autonomyService.Effective(ctx, in.RootID, "", in.Labels)
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	if ref != nil {
		return nil, &Failure{Sentence: ref.Sentence}, nil
	}
	return view, nil, nil
}

// — the two proposes —

// autonomyProposeRevokeInput is autonomy_propose_revoke's input, mirrored
// from the catalog schema: the grant id and the why, and NOTHING else
// (decode's DisallowUnknownFields makes a smuggled state/conditions/actor a
// parse refusal). The input shape IS the stored payload shape, which IS the
// desk's payload shape (desk.AutonomyRevokePayload) — three names, one
// shape, so the create validator and the commit validator read the same
// bytes (TD3).
type autonomyProposeRevokeInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

func (r *RecordsService) autonomyProposeRevoke(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in autonomyProposeRevokeInput
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if len(in.ID) == 0 {
		return nil, &Failure{Sentence: "refused: autonomy_propose_revoke needs the grant's id — a revocation from nothing is a proposal a person would waste a commit on (TD3); see it with autonomy_list"}, nil
	}
	payload, merr := json.Marshal(in)
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: autonomy_propose_revoke payload will not encode: %w", merr)
	}
	return r.createInertProposal(ctx, by, string(desk.KindAutonomyRevoke), payload)
}

// autonomyProposeDenyInput is autonomy_propose_deny's input, mirrored from
// the catalog schema: the node (a root or a child) and the subject in the
// two-shape vocabulary (an action by name, or the one class), and the why.
// state is implied (deny) — the shape has no field to carry an allow, and
// an unknown one is a parse refusal (AL11).
type autonomyProposeDenyInput struct {
	NodeID  string `json:"nodeId"`
	Subject struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"subject"`
	Reason string `json:"reason"`
}

func (r *RecordsService) autonomyProposeDeny(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in autonomyProposeDenyInput
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if len(in.NodeID) == 0 {
		return nil, &Failure{Sentence: "refused: autonomy_propose_deny needs the node's id — a root or a child — a veto needs somewhere to sit (TD3)"}, nil
	}
	// The stored payload is the desk's OWN payload shape
	// (desk.AutonomyDenyPayload): re-encode through it, not through the
	// input struct, so the create/commit validators decode one shape.
	payload, merr := json.Marshal(struct {
		NodeID  string `json:"nodeId"`
		Subject struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"subject"`
		Reason string `json:"reason"`
	}{
		NodeID: in.NodeID,
		Subject: struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		}{Kind: in.Subject.Kind, Name: in.Subject.Name},
		Reason: in.Reason,
	})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: autonomy_propose_deny payload will not encode: %w", merr)
	}
	return r.createInertProposal(ctx, by, string(desk.KindAutonomyDeny), payload)
}
