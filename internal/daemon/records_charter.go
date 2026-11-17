package daemon

// records_charter.go — S61 T8 (GI17): the goal-intake plane's seven agent
// ARMS — five safe reads and two inert asks. Each read forwards to the
// SAME door struct the person's socket door answers through (standing rule
// 4: one implementation, two doors — the charter, template and intake
// doors the S61 T7 block wires), so a tool answer and its socket twin
// cannot drift (TestToolReadsMatchSocketReads pins it field for field).
//
// The two asks file INERT proposal rows — `charter_save` and `intake_plan`
// — through the SAME validator proposal.create runs (the verbatim
// desk.Validate seam), the same per-agent open cap, and a payload bound
// the desk itself states (the intake plan's own 128 KiB cap; a charter
// body of "under 128 KiB is one a person actually reads at the desk"
// rides the same bound). The actor is the step's identity, stamped
// (S45 T7's rule); `intake_propose` additionally stamps the intake row
// from the CALLING LOOP's own intake session — the loop slug resolves to
// an `intake` row or the tool refuses "this tool works only inside an
// intake session", and a typed "intakeId" meets the strict decode and the
// parse refusal names it (forged linkage dies at the parse).
//
// V13 honesty: a mount that did not wire the plane refuses each arm BY
// NAME and answers nothing — no empty charter index, no status of zero.
// The plane's four writes (charter.save · template.save · intake.start ·
// intake.commit) have NO arm here, no spec, no way in: an ask is the most
// an agent may do, and the ask decides nothing (GI14, GI17).

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
)

// intakePlaneRefusal is the V13 arm refusal: named, honest, never an empty
// answer.
func intakePlaneRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the goal-intake plane, and this mount did not wire it — nothing is answered, and nothing is answered empty (V13)", name)}
}

// The plane seams, wired by the S61 T8 block in daemon.go with the VERY
// door structs the socket doors dispatch through (one implementation, two
// doors — S38 OL10's shape).
func (r *RecordsService) charterDoors() (protocol.CharterService, bool) {
	if r.charterPlane == nil {
		return nil, false
	}
	return r.charterPlane, true
}

func (r *RecordsService) templateDoors() (protocol.TemplateService, bool) {
	if r.templatePlane == nil {
		return nil, false
	}
	return r.templatePlane, true
}

func (r *RecordsService) intakeDoors() (protocol.IntakeService, bool) {
	if r.intakePlane == nil {
		return nil, false
	}
	return r.intakePlane, true
}

// intakeToolError renders a door refusal (the typed *protocol.Error or any
// plain error) into the tool's failure channel — DB19: the door's own
// sentence, never a paraphrase, never the error channel for refusals.
func intakeToolError(err error) *Failure {
	return &Failure{Sentence: "refused: " + err.Error()}
}

// ── the five reads ──────────────────────────────────────────────────────

func (r *RecordsService) charterListArm(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	cd, ok := r.charterDoors()
	if !ok {
		return nil, intakePlaneRefusal("charter_list"), nil
	}
	var in struct{}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := cd.List(ctx)
	if err != nil {
		return nil, intakeToolError(err), nil
	}
	return out, nil, nil
}

// charterShowArmInput mirrors charter_show's schema: the root and an
// optional historical revision. There is no actor field to forge with.
type charterShowArmInput struct {
	RootID   string `json:"root_id"`
	Revision int    `json:"revision"`
}

func (r *RecordsService) charterShowArm(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	cd, ok := r.charterDoors()
	if !ok {
		return nil, intakePlaneRefusal("charter_show"), nil
	}
	var in charterShowArmInput
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.RootID == "" {
		return nil, &Failure{Sentence: "refused: charter_show needs the component root's id — a charter rides a root, and charter_list names them (TD3)"}, nil
	}
	var rev *int
	if in.Revision > 0 {
		rev = &in.Revision
	}
	out, err := cd.Get(ctx, protocol.CharterGetParams{RootID: in.RootID, Revision: rev})
	if err != nil {
		return nil, intakeToolError(err), nil
	}
	return out, nil, nil
}

func (r *RecordsService) templateListArm(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	td, ok := r.templateDoors()
	if !ok {
		return nil, intakePlaneRefusal("template_list"), nil
	}
	var in struct {
		StackTag string `json:"stack_tag"`
		Origin   string `json:"origin"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	out, err := td.List(ctx, protocol.TemplateListParams{StackTag: in.StackTag, Origin: in.Origin})
	if err != nil {
		return nil, intakeToolError(err), nil
	}
	return out, nil, nil
}

func (r *RecordsService) templateShowArm(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	td, ok := r.templateDoors()
	if !ok {
		return nil, intakePlaneRefusal("template_show"), nil
	}
	var in struct {
		Slug string `json:"slug"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.Slug == "" {
		return nil, &Failure{Sentence: "refused: template_show needs the template's slug, exactly as template_list names it (TD3)"}, nil
	}
	out, err := td.Get(ctx, protocol.TemplateGetParams{Slug: in.Slug})
	if err != nil {
		return nil, intakeToolError(err), nil
	}
	return out, nil, nil
}

func (r *RecordsService) intakeStatusArm(ctx context.Context, _ Identity, input json.RawMessage) (any, *Failure, error) {
	id, ok := r.intakeDoors()
	if !ok {
		return nil, intakePlaneRefusal("intake_status"), nil
	}
	var in struct {
		IntakeID string `json:"intake_id"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.IntakeID == "" {
		return nil, &Failure{Sentence: "refused: intake_status needs the intake row's id — the intake session's context names it (TD3)"}, nil
	}
	out, err := id.Status(ctx, protocol.IntakeStatusParams{IntakeID: in.IntakeID})
	if err != nil {
		return nil, intakeToolError(err), nil
	}
	return out, nil, nil
}

// ── the two asks ────────────────────────────────────────────────────────

// charterProposeArmInput mirrors charter_propose's schema. The stored
// payload is re-encoded through the desk's OWN shape
// (desk.CharterSaveProposal), not through this struct, so the create/commit
// validators decode one shape (the blueprint and autonomy arms' law).
type charterProposeArmInput struct {
	RootID string          `json:"root_id"`
	Body   json.RawMessage `json:"body"`
	Reason string          `json:"reason"`
}

func (r *RecordsService) charterProposeArm(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if _, ok := r.charterDoors(); !ok {
		return nil, intakePlaneRefusal("charter_propose"), nil
	}
	var in charterProposeArmInput
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.RootID == "" {
		return nil, &Failure{Sentence: "refused: charter_propose needs the component ROOT's id the charter belongs to — a charter rides a root (TD3)"}, nil
	}
	if len(in.Body) == 0 {
		return nil, &Failure{Sentence: "refused: charter_propose needs the charter document (the body, as data) — an empty body is refused by the save door's validator too, and a proposal a person could not commit is a proposal wasted"}, nil
	}
	if in.Reason == "" {
		return nil, &Failure{Sentence: "refused: charter_propose needs the reason — the proposal's own sentence for the person deciding, not the charter's (TD3)"}, nil
	}
	payload, merr := json.Marshal(desk.CharterSaveProposal{RootID: in.RootID, Body: in.Body, Reason: in.Reason})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: charter_propose payload will not encode: %w", merr)
	}
	return r.createInertProposalBounded(ctx, by, string(desk.KindCharterSave), payload, desk.IntakePlanCapsOf().PayloadBytes)
}

// intakeProposeArmInput mirrors intake_propose's schema — the plan MINUS
// the intakeId. There IS no intakeId field: the decode is strict
// (DisallowUnknownFields), so a forged `intakeId` key dies in the parse
// refusal that names the unknown field (TestIntakeProposeStampsTheIntakeId
// pins the naming). The camelCase keys mirror desk.IntakePlanProposal one
// for one so the re-encode is the validator's own shape.
type intakeProposeArmInput struct {
	Charter     json.RawMessage            `json:"charter"`
	Components  []desk.IntakePlanComponent `json:"components"`
	ProjectName string                     `json:"projectName,omitempty"`
	Epics       []desk.IntakePlanEpic      `json:"epics,omitempty"`
	Goals       []desk.IntakePlanGoal      `json:"goals,omitempty"`
	Items       []desk.IntakePlanItem      `json:"items,omitempty"`
	Reason      string                     `json:"reason"`
}

func (r *RecordsService) intakeProposeArm(ctx context.Context, by Actor, input json.RawMessage) (any, *Failure, error) {
	if _, ok := r.intakeDoors(); !ok {
		return nil, intakePlaneRefusal("intake_propose"), nil
	}
	var in intakeProposeArmInput
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	// The identity half (GI17 behaviour 3): the plan belongs to the CALLING
	// LOOP's intake session. The loop slug resolves to an `intake` row
	// through the store or the tool refuses — the row is stamped, never
	// typed, and a loop that is not an intake session has no row to find.
	ident, f := r.Identify(ctx)
	if f != nil {
		return nil, f, nil
	}
	if ident.LoopSlug == "" {
		return nil, &Failure{Sentence: "refused: intake_propose works only inside an intake session, and this step's identity resolves no loop — the intake row is read from the store, never guessed (GI17)"}, nil
	}
	rows, serr := r.st.ListIntakes()
	if serr != nil {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: the intake sessions could not be read (%v) — the row is stamped by lookup, so a store that cannot answer stops the ask", serr)}, nil
	}
	stamp := ""
	for _, row := range rows {
		if row.LoopSlug == ident.LoopSlug {
			stamp = row.ID
			break
		}
	}
	if stamp == "" {
		return nil, &Failure{Sentence: "refused: this tool works only inside an intake session — loop " + ident.LoopSlug + " resolves no intake row, and a plan without its ledger row is a promise about nothing (GI17)"}, nil
	}
	payload, merr := json.Marshal(desk.IntakePlanProposal{
		IntakeID: stamp, Charter: in.Charter, Components: in.Components, ProjectName: in.ProjectName,
		Epics: in.Epics, Goals: in.Goals, Items: in.Items, Reason: in.Reason,
	})
	if merr != nil {
		return nil, nil, fmt.Errorf("daemon: intake_propose payload will not encode: %w", merr)
	}
	return r.createInertProposalBounded(ctx, by, string(desk.KindIntakePlan), payload, desk.IntakePlanCapsOf().PayloadBytes)
}
