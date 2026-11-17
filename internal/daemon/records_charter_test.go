package daemon

// records_charter_test.go — S61 T8 (GI17): the goal-intake family's seven
// arms walked at the door an agent actually stands at. The behaviour rows
// the spec names, each pinned here:
//
//   - PARITY (rule 4): every read arm answers byte-identically to the
//     person's socket door over the same rows — one implementation, two
//     doors (TestToolReadsMatchSocketReads);
//   - INERT (GI17): an ask files a `proposed` row and nothing else — no
//     charter revision, no component, no ledger step, the fixed inert note
//     and the copyable commit command riding the result (TestProposeIsInert);
//   - VALIDATOR PARITY (TD3): the arm's refusal is proposal.create's
//     refusal, byte for byte, because it is the SAME desk.Validate call
//     (TestProposeValidatorParityWithCreate);
//   - THE STAMP (GI17 behaviour 3): intake_propose works only inside an
//     intake session — the row is stamped from the calling loop's slug, a
//     foreign loop refuses with those words, and a typed "intakeId" meets
//     the strict decode and the parse refusal NAMES the field
//     (TestIntakeProposeOnlyInsideAnIntakeLoop, TestIntakeProposeStampsTheIntakeId);
//   - THE BOUND: the plan's own 128 KiB desk cap gates the payload — the
//     8 KiB ceiling of every other ask is not this kind's number
//     (TestProposePayloadBoundIsThePlanCap);
//   - and unwired is STATED, not faked (V13): a mount without the plane
//     refuses every arm by name and answers nothing empty.
//
// The seams here are the TEST mirror of cmd/aeon's charter/template seam
// files (the daemon may not import main); cmd/aeon's own parity guard
// compares the PRODUCTION seam's bytes.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type intakeArmEnv struct {
	recordsEnv
	reg *desk.Registry
}

// testSeamCharterSave / testSeamIntakePlan mirror cmd/aeon's Validate seams
// (desk shape half → charter half → store facts), over THIS env's store.
func testSeamCharterSave(s *store.Store) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		if err := desk.ValidateCharterSave(ctx, payload); err != nil {
			return err
		}
		var p desk.CharterSaveProposal
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		var body charter.Body
		if err := json.Unmarshal(p.Body, &body); err != nil {
			return err
		}
		if r := charter.Validate(body, charter.Vocabulary(nil)); r != nil {
			return errors.New(r.Sentence)
		}
		c, err := s.ReadComponent(p.RootID)
		if err != nil {
			return errors.New("refused: " + p.RootID + " is not a component")
		}
		if c.ParentID != "" {
			return errors.New("refused: " + p.RootID + " is not a root")
		}
		return nil
	}
}

func testSeamIntakePlan(s *store.Store) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		if err := desk.ValidateIntakePlan(ctx, payload); err != nil {
			return err
		}
		var p desk.IntakePlanProposal
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		var body charter.Body
		if err := json.Unmarshal(p.Charter, &body); err != nil {
			return err
		}
		if r := charter.Validate(body, charter.Vocabulary(nil)); r != nil {
			return errors.New(r.Sentence)
		}
		if _, err := s.GetIntake(p.IntakeID); err != nil {
			return errors.New("refused: intake " + p.IntakeID + " does not exist")
		}
		for _, c := range p.Components {
			if _, err := s.GetTemplate(c.Template); err != nil {
				return errors.New("refused: no such scaffold template " + c.Template)
			}
		}
		return nil
	}
}

func newIntakeArmEnv(t *testing.T) intakeArmEnv {
	t.Helper()
	e := intakeArmEnv{recordsEnv: newRecordsEnv(t, "")}
	d := &Daemon{store: e.s}
	e.svc.charterPlane = newCharterDoors(d)
	e.svc.templatePlane = newTemplateDoors(d)
	e.svc.intakePlane = newIntakeDoors(d)
	e.svc.proposals = proposalStore(e.s)
	e.reg = desk.New(desk.Deps{Boundary: testBoundaryOpener(e.s)}, desk.Seams{
		desk.KindCharterSave: {Validate: testSeamCharterSave(e.s)},
		desk.KindIntakePlan:  {Validate: testSeamIntakePlan(e.s)},
	})
	e.svc.validateProposal = e.reg.ValidateProposal
	return e
}

// mustIntakeRow opens one intake and binds it to one loop slug.
func mustIntakeRow(t *testing.T, e intakeArmEnv, slug string) store.Intake {
	t.Helper()
	row, ref, err := e.s.CreateIntake("walk session", "/scratch/walk", s61Actor())
	if err != nil || ref != nil {
		t.Fatalf("CreateIntake: %v %+v", err, ref)
	}
	if slug != "" {
		if ref, err := e.s.SetIntakeLoop(row.ID, slug); err != nil || ref != nil {
			t.Fatalf("SetIntakeLoop: %v %+v", err, ref)
		}
		row.LoopSlug = slug
	}
	return row
}

const s61ArmCharterBody = `{"goal":"ship the intake plane"}`

// TestIntakePlaneToolReadsMatchSocketReads: (the family file's own; TestToolReadsMatchSocketReads
// is S55 T10's standing name in this package, and TestProposeIsInert S56 T8's.) every read arm and its socket door answer
// the SAME bytes over the same rows — the parity that makes the tool a
// projection and not a second implementation (rule 4, S38 OL10's shape).
func TestIntakePlaneToolReadsMatchSocketReads(t *testing.T) {
	e := newIntakeArmEnv(t)
	root := mustRoot(t, e.s, "chartered")
	svc := &charter.Service{Store: e.s}
	if _, ref, err := svc.Save(context.Background(), s61Actor(), root.ID, charter.Body{Goal: "ship"}, "first", "person", ""); err != nil || ref != nil {
		t.Fatalf("save: %v %+v", err, ref)
	}
	mustTemplate(t, e.s, "base")
	mustIntakeRow(t, e, "walk")

	cases := []struct {
		tool  string
		input string
		door  func() (any, error)
	}{
		{"charter_list", `{}`, func() (any, error) { return e.svc.charterPlane.List(context.Background()) }},
		{"charter_show", `{"root_id":"` + root.ID + `"}`, func() (any, error) {
			return e.svc.charterPlane.Get(context.Background(), protocol.CharterGetParams{RootID: root.ID})
		}},
		{"template_list", `{}`, func() (any, error) {
			return e.svc.templatePlane.List(context.Background(), protocol.TemplateListParams{})
		}},
		{"template_show", `{"slug":"base"}`, func() (any, error) {
			return e.svc.templatePlane.Get(context.Background(), protocol.TemplateGetParams{Slug: "base"})
		}},
	}
	for _, tc := range cases {
		armOut, f, err := e.do(t, tc.tool, tc.input)
		if err != nil || f != nil {
			t.Fatalf("%s: %v %+v", tc.tool, err, f)
		}
		doorOut, err := tc.door()
		if err != nil {
			t.Fatalf("%s door: %v", tc.tool, err)
		}
		a, _ := json.Marshal(armOut)
		b, _ := json.Marshal(doorOut)
		if string(a) != string(b) {
			t.Errorf("%s drifted from its socket door:\n arm  %s\n door %s", tc.tool, a, b)
		}
	}
	// intake_status separately: the arm names the id in SNAKE (schema),
	// the door in camel — the answer must still be the same bytes.
	row := mustIntakeRowInt(t, e)
	armOut, f, err := e.do(t, "intake_status", `{"intake_id":"`+row.ID+`"}`)
	if err != nil || f != nil {
		t.Fatalf("intake_status: %v %+v", err, f)
	}
	doorOut, err := e.svc.intakePlane.Status(context.Background(), protocol.IntakeStatusParams{IntakeID: row.ID})
	if err != nil {
		t.Fatalf("intake.status door: %v", err)
	}
	a, _ := json.Marshal(armOut)
	b, _ := json.Marshal(doorOut)
	if string(a) != string(b) {
		t.Errorf("intake_status drifted from its socket door:\n arm  %s\n door %s", a, b)
	}
}

// mustIntakeRowInt re-opens the FIRST row (one session per env is enough;
// the second call would only add noise), for the status parity leg.
func mustIntakeRowInt(t *testing.T, e intakeArmEnv) store.Intake {
	t.Helper()
	rows, err := e.s.ListIntakes()
	if err != nil || len(rows) == 0 {
		t.Fatalf("no intake rows: %v", err)
	}
	return rows[0]
}

// TestProposeIsInert: an ask files a proposal row and NOTHING else — the
// charter keeps zero revisions, the component registry keeps its count,
// and the answer carries the fixed inert note and the copyable command.
func TestIntakePlaneProposeIsInert(t *testing.T) {
	e := newIntakeArmEnv(t)
	root := mustRoot(t, e.s, "chartered")
	before, err := e.s.ListComponents(store.ComponentFilter{})
	if err != nil {
		t.Fatal(err)
	}

	out, f, err := e.do(t, "charter_propose", `{"root_id":"`+root.ID+`","body":`+s61ArmCharterBody+`,"reason":"the person described a goal"}`)
	if err != nil || f != nil {
		t.Fatalf("charter_propose: %v %+v", err, f)
	}
	res, ok := out.(protocol.PolicyProposeResult)
	if !ok {
		t.Fatalf("charter_propose answered %T", out)
	}
	if res.Proposal.Kind != string(desk.KindCharterSave) || res.Proposal.Status != "proposed" {
		t.Errorf("row = %s/%s, want charter_save/proposed", res.Proposal.Kind, res.Proposal.Status)
	}
	if res.Proposal.CreatedByKind != "agent" || res.Proposal.CreatedByID != e.agent {
		t.Errorf("row stamped %s:%s, want the step's agent %s (S45 T7's rule)", res.Proposal.CreatedByKind, res.Proposal.CreatedByID, e.agent)
	}
	if res.Note != protocol.PolicyProposeInertNote {
		t.Errorf("note drifted from the machinery's own inert sentence: %q", res.Note)
	}
	if res.Proposal.Command == "" {
		t.Error("the inert answer must carry the copyable commit command")
	}
	// Nothing else moved: no revision at the charter door…
	if _, err := (&charter.Service{Store: e.s}).Get(context.Background(), root.ID); err == nil {
		t.Error("charter_propose SAVED a revision — the ask must file, never write (GI17)")
	}
	// …and no component appeared.
	after, err := e.s.ListComponents(store.ComponentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("components moved %d → %d — an inert proposal created rows", len(before), len(after))
	}
}

// TestIntakeProposeOnlyInsideAnIntakeLoop: the calling loop's slug must
// resolve to an intake row or the ask refuses with exactly those words.
func TestIntakeProposeOnlyInsideAnIntakeLoop(t *testing.T) {
	e := newIntakeArmEnv(t)
	mustTemplate(t, e.s, "base")
	// The recordsEnv loop is slug "walk" and no row carries it — refuse.
	_, f, err := e.do(t, "intake_propose", intakeArmPlanJSON)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "works only inside an intake session") {
		t.Fatalf("outside an intake session the ask must refuse by name, got %+v", f)
	}
	// Bind the row to THIS loop and the same input files.
	mustIntakeRow(t, e, "walk")
	out, f, err := e.do(t, "intake_propose", intakeArmPlanJSON)
	if err != nil || f != nil {
		t.Fatalf("inside the intake session: %v %+v", err, f)
	}
	if out.(protocol.PolicyProposeResult).Proposal.Kind != string(desk.KindIntakePlan) {
		t.Errorf("wrong kind filed: %+v", out)
	}
}

const intakeArmPlanJSON = `{"charter":` + s61ArmCharterBody + `,"components":[{"name":"Solo","slug":"solo","template":"base","role":"standalone"}],"reason":"The person asked for this project to exist."}`

// TestIntakeProposeStampsTheIntakeId: the wire shape has NO intakeId field
// — a typed one is a parse refusal that names it — and the filed payload
// carries the row the DAEMON stamped from the loop's identity.
func TestIntakeProposeStampsTheIntakeId(t *testing.T) {
	e := newIntakeArmEnv(t)
	mustTemplate(t, e.s, "base")
	row := mustIntakeRow(t, e, "walk")

	_, f, err := e.do(t, "intake_propose", `{"intakeId":"`+row.ID+`",`+intakeArmPlanJSON[1:])
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "intakeId") {
		t.Fatalf("a forged intakeId must die at the parse refusal NAMING it, got %+v", f)
	}

	out, f, err := e.do(t, "intake_propose", intakeArmPlanJSON)
	if err != nil || f != nil {
		t.Fatalf("intake_propose: %v %+v", err, f)
	}
	var filed desk.IntakePlanProposal
	if err := json.Unmarshal(out.(protocol.PolicyProposeResult).Proposal.Payload, &filed); err != nil {
		t.Fatal(err)
	}
	if filed.IntakeID != row.ID {
		t.Errorf("stamped intakeId = %q, want the calling loop's own row %q", filed.IntakeID, row.ID)
	}
}

// TestProposeValidatorParityWithCreate: the arm's refusal is the desk
// registry's refusal byte for byte — the SAME call proposal.create runs
// (TD3: one validator, every door).
func TestProposeValidatorParityWithCreate(t *testing.T) {
	e := newIntakeArmEnv(t)
	root := mustRoot(t, e.s, "chartered")
	// A charter body the SHAPE half takes and the charter half refuses.
	badBody := `{"goal":""}`
	_, f, err := e.do(t, "charter_propose", `{"root_id":"`+root.ID+`","body":`+badBody+`,"reason":"parity probe"}`)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(desk.CharterSaveProposal{RootID: root.ID, Body: json.RawMessage(badBody), Reason: "parity probe"})
	if err != nil {
		t.Fatal(err)
	}
	direct := e.reg.ValidateProposal(context.Background(), string(desk.KindCharterSave), string(payload))
	if direct == nil {
		t.Fatal("the registry took a body the arm refused — the halves drifted")
	}
	if f == nil || f.Sentence != direct.Error() {
		t.Fatalf("arm said %v, create-door validator says %v — refusals must be byte-identical", f, direct)
	}
}

// TestProposePayloadBoundIsThePlanCap: the intake plan's own desk cap
// (128 KiB) is the ceiling for these two asks, not the 8 KiB every other
// ask carries — and above it the ask refuses, never truncates.
func TestProposePayloadBoundIsThePlanCap(t *testing.T) {
	e := newIntakeArmEnv(t)
	root := mustRoot(t, e.s, "chartered")
	if desk.IntakePlanCapsOf().PayloadBytes != 128*1024 {
		t.Fatalf("the desk plan cap moved: %d — this test names the number on purpose", desk.IntakePlanCapsOf().PayloadBytes)
	}
	big := `{"goal":"` + strings.Repeat("g", 130*1024) + `"}`
	_, f, err := e.do(t, "charter_propose", `{"root_id":"`+root.ID+`","body":`+big+`,"reason":"over the cap"}`)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "131072") {
		t.Fatalf("over-cap payload must refuse naming 131072, got %+v", f)
	}
	// The same bytes are over every OTHER kind's ceiling — proof the bound
	// is this family's own number, not the shared one: the refusal above
	// quotes the plan cap, never 8192.
	if f != nil && strings.Contains(f.Sentence, "8192") {
		t.Errorf("the refusal quoted the 8 KiB ceiling — these arms carry the plan cap: %s", f.Sentence)
	}
}

// TestUnwiredIntakePlaneRefusesByName: V13 at the arms — no plane, no
// answer, not even an empty one.
func TestUnwiredIntakePlaneRefusesByName(t *testing.T) {
	e := newRecordsEnv(t, "") // no planes wired at all
	for _, tc := range []struct{ tool, input string }{
		{"charter_list", `{}`}, {"charter_show", `{"root_id":"x"}`}, {"template_list", `{}`},
		{"template_show", `{"slug":"x"}`}, {"intake_status", `{"intake_id":"x"}`},
		{"charter_propose", `{"root_id":"x","body":{"goal":"g"},"reason":"r"}`},
		{"intake_propose", intakeArmPlanJSON},
	} {
		_, f, err := e.do(t, tc.tool, tc.input)
		if err != nil {
			t.Fatal(err)
		}
		if f == nil || !strings.Contains(f.Sentence, "did not wire it") {
			t.Errorf("%s on an unwired mount answered %v %+v — V13 names the absence", tc.tool, f, err)
		}
	}
}
