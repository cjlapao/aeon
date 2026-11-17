package daemon

// records_environment.go — S59 T7 (EV16, EV7): the five environment arms of
// the records service. Four are reads that forward to the SAME services the
// person's twenty socket doors answer through — the environment seam for the
// three `environment.*` reads, the secret-reference seam for the list — so
// the tool door and the socket door cannot drift field for field (rule 4).
// The fifth files the INERT environment_restrict row through the shared
// create path: the shared create validator (TD3 — `execenv.Restricts` runs
// there, the widening list verbatim on refusal), the per-agent open-proposal
// cap (S45's named constant, the counted refusal naming it and proposal_list),
// and the author STAMPED from the step's identity — never typed (OL3).
//
// Three things this file does NOT do, and each is a rule rather than an
// omission:
//
//   - it never writes an environment, a binding, a provision or a secret
//     reference (EV16/EV18; T6's guard refuses such a tool by name, and no
//     such arm exists to call);
//   - it never lets a source or a value through. secretref_list answers
//     protocol.SecretRefListView, the type with no `source` field at all —
//     §4c's two types rather than one redaction, applied to a model this time
//     (EV7). The three environment reads are already value-free by the plane's
//     own construction (names and literals only);
//   - it never takes an identity or a baseline from the input. The scope is
//     the step's (resolveComponent, the same tree rule goal_list and loop_list
//     use); the revision a proposal is written against is READ from the row
//     here and stamped into the payload, because a baseline a model can type
//     is a baseline that can be wrong on purpose.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// maxEnvironmentSpecBytes is the plane's own spec ceiling, re-declared where
// the tool meets it (the store refuses a spec this large too, and the desk
// kind carries the same bound — a spec the write door would refuse never
// becomes an inert row).
const maxEnvironmentSpecBytes = 1 << 20

// maxEnvironmentReasonBytes is environment_propose_restrict's reason ceiling —
// the records plane's own reason discipline, the number every other propose
// arm uses for the sentence a person reads at the desk.
const maxEnvironmentReasonBytes = 512

// environmentList answers environment_list through the standing environment
// door — the same `List` the socket's environment.list runs. The filters are
// the caller's; the rows are the plane's.
func (r *RecordsService) environmentList(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.environments == nil {
		return nil, environmentPlanelessRefusal("environment_list"), nil
	}
	var in protocol.EnvironmentListParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := r.environments.List(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// environmentShow answers environment_show through the same `Show` the
// person's door runs: one environment in full — spec as NAMES, capability
// chips, bindings, revision. An empty id and slug is a refusal, not an empty
// answer (P3).
func (r *RecordsService) environmentShow(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.environments == nil {
		return nil, environmentPlanelessRefusal("environment_show"), nil
	}
	var in protocol.EnvironmentShowParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if strings.TrimSpace(in.ID) == "" && strings.TrimSpace(in.Slug) == "" {
		return nil, &Failure{Sentence: "refused: environment_show needs an id or a slug — environment_list shows what exists, and a show of nothing is a refusal, not an empty answer"}, nil
	}
	out, err := r.environments.Show(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// environmentEffective answers environment_effective for the CALLER's own
// tree: no componentId means the step's own component, and a named one must
// live in the step's root (resolveComponent, the tree rule goal_list and
// loop_list already hold). A foreign component is refused before anything is
// read, naming both — what a step runs in is its own business, and the map of
// what another loop runs with is not a read a step gets (OL3, MC2's shape).
//
// `attended` is the honest three-state pointer the plane answers with: nil
// leaves the door to decide from the step's own loop, and an unattended loop
// with no binding is refused rather than answered with the inherit EV6
// forbids.
func (r *RecordsService) environmentEffective(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.environments == nil {
		return nil, environmentPlanelessRefusal("environment_effective"), nil
	}
	var in struct {
		ComponentID string `json:"componentId"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	componentID := strings.TrimSpace(in.ComponentID)
	if componentID == "" {
		componentID = ident.ComponentID
	} else if componentID != ident.ComponentID && componentID != ident.RootID {
		if _, f := r.resolveComponent(ident, componentID); f != nil {
			return nil, &Failure{Sentence: fmt.Sprintf(
				"refused: the component %s is not in your loop's tree (your component is %s, its root %s) — what a step runs in is its own tree's business, and a cross-tree act from a step is a person's act (S59 T7)",
				componentID, ident.ComponentID, ident.RootID)}, nil
		}
	}
	// No attended field, here or in the params: an agent declaring its own
	// attendedness would be a fourth door on the value G14 reserves to the
	// three start doors, and the spawn path never consults it — so the answer
	// the arm gave would not be the answer the step gets. The daemon answers
	// attendedness; nobody sends it (S59 T12, internal/loop TestQ4Recorded).
	out, err := r.environments.Effective(ctx, protocol.EnvironmentEffectiveParams{
		ComponentID: componentID,
	})
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return out, nil, nil
}

// secretrefList answers secretref_list — the reference NAMES, the variable
// each injects as, and the KIND of source, in the type that has no field for
// the source itself. The person's twin keeps `source`; this one cannot carry
// it, and "cannot" is the whole rule (EV7, §4c).
func (r *RecordsService) secretrefList(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	if r.secretRefs == nil {
		return nil, environmentPlanelessRefusal("secretref_list"), nil
	}
	var in protocol.SecretRefListParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	views, err := r.secretRefs.List(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	out := make([]protocol.SecretRefListView, 0, len(views))
	for _, v := range views {
		out = append(out, v.AgentView())
	}
	return out, nil, nil
}

// environmentProposeRestrict files the INERT row. The shape refuses HERE what
// the schema refuses at parse (blank id, empty spec, oversized reason), the
// shared create validator refuses what the WRITE door would refuse — the
// validator is `execenv.Restricts`, and every way the proposal is looser than
// the row today comes back named, verbatim, TD3 — and the cap refuses what a
// runaway loop would file.
//
// The baseline revision is read from the row here and stamped into the
// payload: the proposal is an ask against THE row as it stands now, which is
// what lets the commit refuse a tighten whose environment moved under it
// instead of clobbering somebody's edit.
//
// Author: the stamped step identity (by), carried to created_by_*; the
// COMMITTER, not this door, owns the update when a person decides. Nothing
// outside the proposal tables is touched (EV16).
func (r *RecordsService) environmentProposeRestrict(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in protocol.EnvironmentProposeRestrictParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	id := strings.TrimSpace(in.EnvironmentID)
	if id == "" {
		return nil, &Failure{Sentence: "refused: environment_propose_restrict needs a non-blank environmentId — environment_list shows what exists"}, nil
	}
	if strings.TrimSpace(in.Spec) == "" {
		return nil, &Failure{Sentence: "refused: environment_propose_restrict needs the FULL proposed spec — never a patch, never empty (an environment is decided whole)"}, nil
	}
	if len(in.Spec) > maxEnvironmentSpecBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: environment_propose_restrict spec is %d bytes and the cap is %d (1 MiB) — the plane's own ceiling; a spec that large is not a description", len(in.Spec), maxEnvironmentSpecBytes)}, nil
	}
	if strings.TrimSpace(in.Reason) == "" {
		return nil, &Failure{Sentence: "refused: environment_propose_restrict needs a reason — an unreasoned ask is not an ask a person can decide"}, nil
	}
	if len(in.Reason) > maxEnvironmentReasonBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: environment_propose_restrict reason is %d bytes and the cap is %d — a sentence for the desk, not a justification essay", len(in.Reason), maxEnvironmentReasonBytes)}, nil
	}
	// The baseline is READ, never typed: the row the ask is about, as it
	// stands at this instant. An unknown or archived row is refused HERE —
	// a proposal against a row nobody can tighten is inert AND invalid, and
	// TD3 says nothing like that gets stored.
	row, err := r.st.GetEnvironment(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: %s is not an environment (id or slug) — environment_list shows what exists", id)}, nil
		}
		return nil, nil, err
	}
	if row.Archived() {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %s is archived — an archived row is history, not a draft (environment.restore is the person's door)", row.Slug)}, nil
	}
	// The payload is BUILT here, field by field — never the raw input carried
	// through. A smuggled "backend", "source", "actor" or "revision" has
	// already met the strict decode above and meets the desk's parse refusal
	// below if it somehow arrives.
	payload, merr := json.Marshal(desk.EnvironmentRestrictProposal{
		EnvironmentID: id,
		Spec:          in.Spec,
		BaseRevision:  row.Revision,
		Reason:        in.Reason,
	})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: environment_propose_restrict payload will not encode: %w", merr)
	}
	if len(payload) > desk.MaxEnvironmentSpecBytes+8*1024 {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: the environment_restrict payload is %d bytes and the cap is %d (1 MiB spec + 8 KiB of JSON around it) — a runaway loop must not bury the desk", len(payload), desk.MaxEnvironmentSpecBytes+8*1024)}, nil
	}
	if f := r.openProposalsCap(by); f != nil {
		return nil, f, nil
	}
	if err := r.validateProposal(ctx, string(desk.KindEnvironmentRestrict), string(payload)); err != nil {
		// The validator's sentence travels VERBATIM (TD3: what create refuses,
		// commit refuses, in the same words — and a proposal that would widen
		// what a loop runs with never becomes an inert row).
		return nil, &Failure{Sentence: err.Error()}, nil
	}
	created, err := r.proposals.CreateProposal(ctx, string(desk.KindEnvironmentRestrict), payload, by.Kind, by.ID)
	if err != nil {
		return nil, nil, err
	}
	created.Command = protocol.ProposalCommand(created.ID)
	return protocol.PolicyProposeResult{Proposal: created, Note: protocol.PolicyProposeInertNote}, nil, nil
}

// environmentPlanelessRefusal is the V13 shape for the family: the plane's own
// services are what honour these doors (the scrubbed answer, the names-only
// projection, the binding truth), and a mount without them says so, never
// answering zero or an empty listing.
func environmentPlanelessRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the execution-environment plane, and this mount did not wire it — absent is stated, never faked (V13)", name)}
}
