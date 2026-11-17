// records_verify.go — S57 T9 (VG19): the nine gate catalog arms — the
// records-service half of the catalog's nine gate specs. Six are READS
// over the SAME VerifyStore / MergeRuleStore / DoDStore seams the socket's
// twenty-two human doors answer through, with the scope INJECTED from the
// step's identity (the caller's own component and loop — never a param,
// OL3) and a foreign scope refused, naming the caller's own. Three are the
// agent's permitted acts: two PROPOSES that arm the proposal desk's
// restrictive kinds (verify_command add-only, merge_rule
// Tightens-only-at-create) and create INERT rows through proposal.create's
// one shared path (8 KiB cap, per-caller cap, the shared validator — TD3),
// and one DoD MARK (met only from a passing run of the caller's own loop on
// the component's CURRENT head; unmet / unverifiable freely; manual
// evidence is a person's mark and the store's pairing backstop says so
// before SQL).
//
// A person never reaches this file: the person half of all nine names is
// turned back before the switch by personDoorRefusal, pointing the six
// reads at T8's unscoped socket reads and the three twins at the person's
// own doors (proposal.create with the gate kind, dod.mark). A mount
// without a gate seam refuses the reads with a named sentence (V13), never
// answers zero. Nothing here grants anything (S57 T9's ruling) — the
// DefaultGrant set is untouched, and a grant of any of these nine names is
// a person's act on the socket.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/protocol"
)

// gateSeamRefusal is the V13 sentence for a gate read on a mount whose
// gate service was not wired: the plane is ABSENT here, and an absence is
// stated, never answered zero.
func gateSeamRefusal(tool, plane string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the %s gate service, and this mount did not wire it — the gate surface is absent for the same reason (V13), never answered zero", tool, plane)}
}

// foreignScopeRefusal is VG19's scope sentence: the param named a component
// or a loop that is not the caller's own. The refusal names BOTH — what
// was asked and what the caller carries — because a cross-component act
// from a step is a person's act, not a param's (OL3), and a scope that
// silently slid is a read of someone else's tree.
func foreignScopeRefusal(kind, asked, own string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: the %s %s is not your %s %s — a gate read is scoped to the loop's own %s, and a cross-%s act from a step is a person's act (VG19)", kind, asked, kind, own, kind, kind)}
}

// --- the six reads: the same seams the human doors use, scope injected -----

// verifyList answers verify_list: the caller's own component's gates —
// the EFFECTIVE set (own plus the root's) by default, the raw own set on
// request, narrowed to a requirement when asked. The component id is the
// stamp; the param never names it.
func (r *RecordsService) verifyList(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.verify == nil {
		return nil, gateSeamRefusal("verify_list", "verify"), nil
	}
	var in struct {
		Effective   *bool  `json:"effective"`
		Requirement string `json:"requirement"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	effective := true
	if in.Effective != nil {
		effective = *in.Effective
	}
	res, err := r.verify.List(ctx, protocol.VerifyListQuery{
		ComponentID: ident.ComponentID,
		Effective:   effective,
		Requirement: in.Requirement,
	})
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return res, nil, nil
}

// verifyShow answers verify_show: one gate with its event trail, scoped to
// the caller's own component — a gate on another component is refused
// before it is read, naming both.
func (r *RecordsService) verifyShow(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.verify == nil {
		return nil, gateSeamRefusal("verify_show", "verify"), nil
	}
	var in struct {
		GateID string `json:"gateId"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.GateID == "" {
		return nil, &Failure{Sentence: "refused: verify_show needs a gateId — the id is listed by verify_list, and a show of nothing is a refusal, not an empty answer"}, nil
	}
	res, err := r.verify.Show(ctx, in.GateID)
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	if res.Gate.ComponentID != ident.ComponentID {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: gate %s belongs to component %s, not your component %s — a gate read is scoped to the loop's own component (VG19)", in.GateID, res.Gate.ComponentID, ident.ComponentID)}, nil
	}
	return res, nil, nil
}

// verifyRuns answers verify_runs: the caller's component's finished runs,
// newest first — its OWN loop by default, one named loop of the same
// component never (the scope is the loop's, and the loop is the
// caller's), narrowed to a state when asked, capped. The list view never
// carries tails (the wire shape has none).
func (r *RecordsService) verifyRuns(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.verify == nil {
		return nil, gateSeamRefusal("verify_runs", "verify"), nil
	}
	var in struct {
		LoopSlug string `json:"loopSlug"`
		State    string `json:"state"`
		Limit    int    `json:"limit"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.LoopSlug != "" && in.LoopSlug != ident.LoopSlug {
		return nil, foreignScopeRefusal("loop", in.LoopSlug, ident.LoopSlug), nil
	}
	loop := in.LoopSlug
	if loop == "" {
		loop = ident.LoopSlug
	}
	res, err := r.verify.Runs(ctx, protocol.VerifyRunsQuery{
		ComponentID: ident.ComponentID,
		LoopSlug:    loop,
		State:       in.State,
		Limit:       in.Limit,
	})
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return res, nil, nil
}

// mergeruleShow answers mergerule_show: the caller's component's EFFECTIVE
// merge rule (own, else the root's, else the baseline — the store's
// resolution), with its source and the autonomy floor. The param is
// empty by shape; the component is the stamp.
func (r *RecordsService) mergeruleShow(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mergeRule == nil {
		return nil, gateSeamRefusal("mergerule_show", "merge rule"), nil
	}
	var in struct{}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	res, err := r.mergeRule.Get(ctx, ident.ComponentID, true)
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return res, nil, nil
}

// mergerulePreview answers mergerule_preview: the caller's component's
// rule beside a dry evaluation — over its own tree, or over one PR of it
// when the param names one. The component defaults to the caller's own; a
// foreign one is refused, naming the caller's own. No PR is an honest
// could-not-say, never a verdict.
func (r *RecordsService) mergerulePreview(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.mergeRule == nil {
		return nil, gateSeamRefusal("mergerule_preview", "merge rule"), nil
	}
	var in struct {
		ComponentID string `json:"componentId"`
		Pr          *int   `json:"pr"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.ComponentID != "" && in.ComponentID != ident.ComponentID {
		return nil, foreignScopeRefusal("component", in.ComponentID, ident.ComponentID), nil
	}
	comp := in.ComponentID
	if comp == "" {
		comp = ident.ComponentID
	}
	pr := 0
	if in.Pr != nil {
		pr = *in.Pr
	}
	res, err := r.mergeRule.Preview(ctx, comp, pr)
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return res, nil, nil
}

// dodList answers dod_list: the caller's OWN loop's definition-of-done
// ledger (all briefs, in order, with states and the evidence each met item
// names). A named loop that is not the caller's is refused.
func (r *RecordsService) dodList(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.dod == nil {
		return nil, gateSeamRefusal("dod_list", "DoD"), nil
	}
	var in struct {
		LoopSlug string `json:"loopSlug"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.LoopSlug != "" && in.LoopSlug != ident.LoopSlug {
		return nil, foreignScopeRefusal("loop", in.LoopSlug, ident.LoopSlug), nil
	}
	loop := in.LoopSlug
	if loop == "" {
		loop = ident.LoopSlug
	}
	res, err := r.dod.List(ctx, loop)
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	return res, nil, nil
}

// --- the three twins: the agent's permitted acts ---------------------------

// verifyProposeAdd answers verify_propose_add: a NEW gate on the caller's
// own component, filed as an INERT verify_command proposal (op add by
// shape — an agent may add a gate, never update, archive or re-point one).
// The arm builds the desk payload from the schema's fields and the stamp;
// the shared validator (TD3) re-runs the desk's half and the store's half
// (component exists, name free) with their own sentences, and the row
// applies nothing until a person commits it.
func (r *RecordsService) verifyProposeAdd(ctx context.Context, by Actor, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in struct {
		Name        string `json:"name"`
		Command     string `json:"command"`
		Requirement string `json:"requirement"`
		TimeoutS    int    `json:"timeoutS"`
		Reason      string `json:"reason"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if f := reasonRefusal(in.Reason, "verify_propose_add"); f != nil {
		return nil, f, nil
	}
	// op is "add" by the arm's shape (VG17), and componentId is the stamp,
	// never a param (OL3): the payload is built, not taken.
	payload, merr := json.Marshal(desk.VerifyCommandProposal{
		Op:          "add",
		Name:        in.Name,
		Command:     in.Command,
		Requirement: in.Requirement,
		TimeoutS:    in.TimeoutS,
		ComponentID: ident.ComponentID,
		Reason:      in.Reason,
	})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: verify_propose_add payload will not encode: %w", merr)
	}
	return r.createInertProposal(ctx, by, string(desk.KindVerifyCommand), payload)
}

// mergeruleProposeTighten answers mergerule_propose_tighten: the FULL
// proposed rule on the caller's own component, filed as an INERT merge_rule
// proposal. The rule is proposed whole (T8's law — a half-rule could hide a
// loosening in the fields it does not name); the wire speaks the schema's
// field names, the store speaks its own (Go names), and the arm is the one
// translation. The shared validator re-runs the desk's half (shape,
// reason) and the store's half (the closed sets, and mergerule.Tightens
// field by field against the component's EFFECTIVE rule — a loosening is
// refused at create, each loosened field named).
func (r *RecordsService) mergeruleProposeTighten(ctx context.Context, by Actor, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if f, ok := r.proposeDoors(); !ok {
		return nil, f, nil
	}
	var in struct {
		Fields struct {
			Enabled                bool     `json:"enabled"`
			RequireLocal           bool     `json:"requireLocal"`
			RequireDod             string   `json:"requireDod"`
			RequiredChecks         []string `json:"requiredChecks"`
			ChecksPolicy           string   `json:"checksPolicy"`
			NoChecks               string   `json:"noChecks"`
			ReviewMode             string   `json:"reviewMode"`
			AeonReviewAgents       []string `json:"aeonReviewAgents"`
			RequireThreadsResolved bool     `json:"requireThreadsResolved"`
			Method                 string   `json:"method"`
		} `json:"fields"`
		Reason string `json:"reason"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if f := reasonRefusal(in.Reason, "mergerule_propose_tighten"); f != nil {
		return nil, f, nil
	}
	rule := mergerule.Rule{
		ComponentID:            ident.ComponentID,
		Enabled:                in.Fields.Enabled,
		RequireLocal:           in.Fields.RequireLocal,
		RequireDoD:             in.Fields.RequireDod,
		RequiredChecks:         in.Fields.RequiredChecks,
		ChecksPolicy:           in.Fields.ChecksPolicy,
		NoChecks:               in.Fields.NoChecks,
		ReviewMode:             in.Fields.ReviewMode,
		AeonReviewAgents:       in.Fields.AeonReviewAgents,
		RequireThreadsResolved: in.Fields.RequireThreadsResolved,
		Method:                 in.Fields.Method,
	}
	ruleJSON, merr := json.Marshal(rule)
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: mergerule_propose_tighten rule will not encode: %w", merr)
	}
	payload, merr := json.Marshal(struct {
		Rule   json.RawMessage `json:"rule"`
		Reason string          `json:"reason"`
	}{Rule: ruleJSON, Reason: in.Reason})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: mergerule_propose_tighten payload will not encode: %w", merr)
	}
	return r.createInertProposal(ctx, by, string(desk.KindMergeRule), payload)
}

// dodMarkAsAgent answers dod_mark: the agent's mark on its OWN loop's
// ledger. unmet and unverifiable are free (a mark is a statement, and the
// note rides the audit row); met is the constrained one — it needs the
// run: a PASSING verify run of the caller's own loop, on the component's
// CURRENT head. The service's half checks the scope (the item is in the
// caller's loop), the evidence shape (a met names the run; a non-met does
// not) and the head's currency (git is not a store concern — VG10's
// currency rule applied to the mark); the store's half re-checks the run
// in the database (exists, pass, the item's loop) and the pairing (an
// agent's manual evidence is refused before SQL). A no-op mark succeeds
// quietly, as the store's honest double-state.
func (r *RecordsService) dodMarkAsAgent(ctx context.Context, by Actor, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.dod == nil {
		return nil, gateSeamRefusal("dod_mark", "DoD"), nil
	}
	if r.verify == nil {
		return nil, gateSeamRefusal("dod_mark", "verify"), nil
	}
	var in struct {
		ItemID        string `json:"itemId"`
		State         string `json:"state"`
		EvidenceRunID string `json:"evidenceRunId"`
		Note          string `json:"note"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.ItemID == "" {
		return nil, &Failure{Sentence: "refused: dod_mark needs an itemId — the items are listed by dod_list, and a mark of nothing is a refusal, not a no-op"}, nil
	}
	if !slices.Contains(protocol.DoDMarkStateValues, in.State) {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: dod_mark state %q — a mark's closed set from an agent is met, unmet, unverifiable; unchecked is the materialise state, not a mark", in.State)}, nil
	}
	if len(in.Note) > 512 {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: the note is %d bytes and the bound is 512 — a note is audit text, not a document (T9)", len(in.Note))}, nil
	}
	// The scope: the item must be in the caller's OWN loop. The store
	// pairs the evidence against the item's loop, but it cannot see the
	// caller — the scope is the arm's to hold.
	list, err := r.dod.List(ctx, ident.LoopSlug)
	if err != nil {
		return nil, wireRefusal(err), nil
	}
	itemKnown := false
	for _, it := range list.Items {
		if it.ID == in.ItemID {
			itemKnown = true
			break
		}
	}
	if !itemKnown {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %s is not a definition-of-done item of your loop %s — a mark belongs to the loop it verifies, and this item is another loop's (or is not there)", in.ItemID, ident.LoopSlug)}, nil
	}
	ev := &protocol.DoDEvidence{Note: in.Note}
	if in.State == "met" {
		if in.EvidenceRunID == "" {
			return nil, &Failure{Sentence: "refused: a met mark names its evidence — a passing verify run of your loop (the evidenceRunId is in verify_runs); an unmet or unverifiable mark names nothing (T9)"}, nil
		}
		// The service's evidence half (the store's half re-checks the same
		// facts in the DB with its own sentences — a drift is refused
		// there too): the run exists, it passed, it is this loop's, and it
		// sits on the component's CURRENT head — a run on an old sha is
		// stale evidence (VG10's currency rule).
		show, serr := r.verify.RunShow(ctx, in.EvidenceRunID)
		if serr != nil {
			return nil, wireRefusal(serr), nil
		}
		run := show.Run
		if run.State != "pass" {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: run %s ended %s, not pass — a met mark names a passing run (T5/T9)", run.ID, run.State)}, nil
		}
		if run.LoopSlug != ident.LoopSlug {
			return nil, foreignScopeRefusal("loop", run.LoopSlug, ident.LoopSlug), nil
		}
		// EV10: the daemon's own git read, so it carries the daemon's git
		// environment — an empty one would refuse rather than inherit this process.
		head, herr := git.NewClient(daemonGitEnv()).LocalHEAD(ctx, ident.ComponentRoot)
		if herr != nil {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: the current head of %s cannot be read (%v) — a met mark needs the run on the CURRENT head, and a head that cannot be read is not evidence; the three-state honesty is said, not hidden (VG10)", ident.ComponentRoot, herr)}, nil
		}
		if run.HeadSHA != head {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: run %s sat on head %s, but %s now stands at %s — a met mark on a stale head is stale evidence (VG10's currency rule, applied to the mark)", run.ID, run.HeadSHA, ident.ComponentRoot, head)}, nil
		}
		ev.Kind = "verify_run"
		ev.Ref = in.EvidenceRunID
	} else {
		if in.EvidenceRunID != "" {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: %s is not a met mark — a run id names evidence only a met carries; drop the evidenceRunId, or mark it met with the passing run (T9)", in.State)}, nil
		}
	}
	res, refusal, merr := r.dod.Mark(ctx, protocol.Actor{Kind: by.Kind, ID: by.ID}, protocol.DoDMarkInput{
		ID: in.ItemID, State: in.State, Evidence: ev,
	})
	if merr != nil {
		return nil, nil, merr
	}
	if refusal != nil {
		return nil, &Failure{Sentence: refusal.Detail}, nil
	}
	return res, nil, nil
}
