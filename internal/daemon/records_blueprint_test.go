package daemon

// records_blueprint_test.go — S48 T10 (BP9): the five blueprint arms of
// the records service. What each test pins, in the order the spec lists
// them: the four READ arms answer the SAME bytes the person's socket doors
// answer (one implementation, two doors — rule 4), the two text-checking
// arms persist NOTHING (no row, no revision, no proposal), the unwired
// lint states its unavailability instead of faking an all-clear, the
// propose arm refuses a smuggled identity at the parse, refuses an
// invalid candidate with the validator's own sentence and creates NOTHING,
// files the INERT row stamped with the AGENT who asked (never a typed
// one), and stops at the caps — the per-agent open-proposal limit named
// with its number, the 1 MiB source ceiling named with its number.
//
// The validator wired here mirrors the production seam (cmd/aeon's
// blueprint_save seam: the desk shape half, then Analyze with the
// daemon's own options) because internal/daemon cannot import package
// main; the parity of the PRODUCTION seam — create and commit calling one
// function, the report verbatim — is pinned there
// (TestBlueprintSaveCreateAndCommitValidatorsAreOneFunction). What is
// pinned HERE is the arm's side of the bargain: it refuses with whatever
// the injected validator says, verbatim, and writes nothing when it does.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// bpRecordsEnv is a store-wired daemon: the arms run over the SAME seams
// the person's doors answer through (assigned after initBlueprints), and
// the create path runs over an injected validator mirroring the
// production seam. One stored blueprint seeds the reads non-empty.
type bpRecordsEnv struct {
	base recordsEnv // the loop + step every records call stamps from (OL3)
	d    *Daemon
	s    *store.Store
	ctx  context.Context
}

func newBPRecordsEnv(t *testing.T) bpRecordsEnv {
	t.Helper()
	e := bpRecordsEnv{base: newRecordsEnv(t, "")}
	e.s = e.base.s
	e.ctx = e.base.ctx
	reg := desk.New(desk.Deps{Boundary: testBoundaryOpener(e.s)}, desk.Seams{
		desk.KindBlueprintSave: {Validate: mirrorBlueprintSaveValidate},
	})
	e.d = New(Options{Store: e.s, ValidateProposal: reg.ValidateProposal})
	bpSeedRows(t, e.s, map[string]string{"bp-read": bpGate("bp-read", "ship it")})
	return e
}

// mirrorBlueprintSaveValidate is cmd/aeon's seam shape, rebuilt from
// pieces this package owns: the desk shape half, then the ONE validator
// with the daemon's own options. The failure it returns renders the
// report as the seam does, so the arms' verbatim-refusal assertions mean
// what they say about the arms.
func mirrorBlueprintSaveValidate(_ context.Context, payload []byte) error {
	if err := desk.ValidateBlueprintSave(context.Background(), payload); err != nil {
		return err
	}
	var p struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	report := blueprint.Analyze([]byte(p.Content), blueprintValidationOptions(""))
	b, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if !report.OK(false) {
		return fmt.Errorf("%s", string(b))
	}
	if p.Name != "" && report.Blueprint != p.Name {
		return fmt.Errorf("source id %q does not match selected blueprint %q", report.Blueprint, p.Name)
	}
	return nil
}

func bpAgentActor() Actor { return Actor{Kind: "agent", ID: "implementer"} }

func bpToolInput(name, content string, createOnly bool, note string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"name": name, "content": content, "create_only": createOnly, "note": note})
	return b
}

// TestBlueprintToolReadsEqualSocketReads is rule 4's byte-for-byte pin:
// for all four reads, the arm's answer and the socket door's answer
// MARSHAL identically — one implementation, not two that agree today.
// (Lint rides the same read service; its evaluatedAt is a clock read per
// call, so the comparison pins everything but that field.)
func TestBlueprintToolReadsEqualSocketReads(t *testing.T) {
	e := newBPRecordsEnv(t)
	init, _ := json.Marshal(protocol.InitializeParams{ClientName: "bp", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION})
	if _, err := e.d.Table.Dispatch(e.ctx, "bp-parity", protocol.MethodInitialize, init); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		tool   string
		method protocol.MethodName
		input  string
	}{
		{"blueprint_list", protocol.MethodBlueprintList, `{}`},
		{"blueprint_show", protocol.MethodBlueprintShow, `{"name":"bp-read"}`},
		{"blueprint_validate", protocol.MethodBlueprintValidate, `{"content":"id: bp-read\nschemaVersion: 1\nversion: 1\nlanes: []\n"}`},
	}
	for _, tc := range cases {
		toolOut, f, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), tc.tool, json.RawMessage(tc.input))
		if f != nil || err != nil {
			t.Fatalf("%s arm: f=%v err=%v", tc.tool, f, err)
		}
		sockOut, serr := e.d.Table.Dispatch(e.ctx, "bp-parity", tc.method, json.RawMessage(tc.input))
		if serr != nil {
			t.Fatalf("%s socket: %v", tc.method, serr)
		}
		a, _ := json.Marshal(toolOut)
		b, _ := json.Marshal(sockOut)
		if string(a) != string(b) {
			t.Errorf("%s and %s drifted:\n tool:   %s\n socket: %s", tc.tool, tc.method, a, b)
		}
	}
	// Lint: same service, same answer apart from the per-call clock stamp.
	lintIn := `{"name":"bp-read"}`
	toolOut, f, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_lint", json.RawMessage(lintIn))
	if f != nil || err != nil {
		t.Fatalf("blueprint_lint arm: f=%v err=%v", f, err)
	}
	sockOut, serr := e.d.Table.Dispatch(e.ctx, "bp-parity", protocol.MethodBlueprintLint, json.RawMessage(lintIn))
	if serr != nil {
		t.Fatalf("blueprint.lint socket: %v", serr)
	}
	drop := func(v any) string {
		b, _ := json.Marshal(v)
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			return string(b)
		}
		delete(m, "evaluatedAt")
		b, _ = json.Marshal(m)
		return string(b)
	}
	if drop(toolOut) != drop(sockOut) {
		t.Errorf("blueprint_lint and blueprint.lint drifted:\n tool:   %s\n socket: %s", drop(toolOut), drop(sockOut))
	}
}

// TestBlueprintValidateAndLintToolsPersistNothing: the text-checking arms
// are READS. No blueprint row, no revision, no proposal appears — and the
// seed row is untouched.
func TestBlueprintValidateAndLintToolsPersistNothing(t *testing.T) {
	e := newBPRecordsEnv(t)
	before := bpSnapshot(t, e)
	content := bpGate("bp-candidate", "ship it")
	if _, f, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_validate", json.RawMessage(`{"content":`+string(mustJSONBytes(t, content))+`}`)); f != nil || err != nil {
		t.Fatalf("blueprint_validate: f=%v err=%v", f, err)
	}
	if _, f, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_lint", json.RawMessage(`{"content":`+string(mustJSONBytes(t, content))+`}`)); f != nil || err != nil {
		t.Fatalf("blueprint_lint: f=%v err=%v", f, err)
	}
	// The unknown-name lint refusal is also a read (it reaches for a row
	// and reports the miss; a refusal that wrote would be the worst kind).
	if _, _, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_lint", json.RawMessage(`{"name":"no-such-bp"}`)); err == nil {
		t.Log("unknown-name lint answered without error — still a read")
	}
	if after := bpSnapshot(t, e); after != before {
		t.Fatalf("a text-checking tool persisted:\n before: %s\n after:  %s", before, after)
	}
}

// bpSnapshot marshals everything the blueprint plane and the desk hold —
// the equality of two snapshots is the zero-writes proof.
// bpSnapshot marshals the blueprint rows, one blueprint's revisions and
// the proposed desk rows — the equality of two snapshots is the
// zero-writes proof for the text arms.
func bpSnapshot(t *testing.T, e bpRecordsEnv) string {
	t.Helper()
	return bpBlueprintsOnly(t, e) + "|" + bpProposals(t, e)
}

// bpBlueprintsOnly is the READ side of the snapshot: what an agent sees
// through the standing doors. A proposal sitting in the desk is not part
// of it — inertness is precisely that the two do not move together.
func bpBlueprintsOnly(t *testing.T, e bpRecordsEnv) string {
	t.Helper()
	list, err := e.d.Table.ListBlueprints(protocol.BlueprintListParams{})
	if err != nil {
		t.Fatal(err)
	}
	lb, _ := json.Marshal(list)
	revs, err := e.s.RevisionList(e.ctx, "bp-read", 100)
	if err != nil {
		t.Fatal(err)
	}
	rb, _ := json.Marshal(revs)
	return string(lb) + "|" + string(rb)
}

func bpProposals(t *testing.T, e bpRecordsEnv) string {
	t.Helper()
	rows, err := e.s.ListProposals("proposed")
	if err != nil {
		t.Fatal(err)
	}
	pb, _ := json.Marshal(rows)
	return string(pb)
}

// TestBlueprintLintUnwiredCatalogIsExplicit: lint's policy view belongs
// to the tools catalog; without it the answer STATES the unavailability —
// never an empty advisories list that reads as all-clear (rule 4's tail).
// A RecordsService built around a bare read service (catalog never wired)
// is exactly what a reads-less mount answers with.
func TestBlueprintLintUnwiredCatalogIsExplicit(t *testing.T) {
	e := newBPRecordsEnv(t)
	svc := NewRecordsService(e.s)
	svc.blueprints = newBlueprintReads(e.s) // catalog nil — the unwired state
	out, f, err := svc.Do(e.ctx, bpAgentActor(), "blueprint_lint", json.RawMessage(`{"name":"bp-read"}`))
	if f != nil || err != nil {
		t.Fatalf("lint with an unwired catalog: f=%v err=%v", f, err)
	}
	res, ok := out.(protocol.BlueprintLintResult)
	if !ok {
		t.Fatalf("lint answered %T, want the lint result", out)
	}
	if res.Unavailable == "" {
		t.Fatal("the unwired catalog answered silently — an empty advisories list is an all-clear nobody earned")
	}
	if res.EvaluatedAt == "" {
		t.Error("even an unavailable lint must stamp WHEN it looked")
	}
	if res.Advisories == nil {
		t.Error("advisories must be an empty array, never null")
	}
}

// TestBlueprintProposeRefusesSmuggledIdentity: rule 1 at the service,
// beside the catalog schema's own walk. The strict decode is the wall.
func TestBlueprintProposeRefusesSmuggledIdentity(t *testing.T) {
	e := newBPRecordsEnv(t)
	content := bpGate("bp-new", "ship it")
	for _, smuggle := range []string{
		`"actor":{"kind":"user","id":"root"}`, `"createdBy":"u9"`, `"rev":7`, `"origin":"system"`, `"kind":"blueprint_save"`, `"status":"applied"`,
	} {
		in := `{"name":"bp-new","content":` + string(mustJSONBytes(t, content)) + `,` + smuggle + `}`
		_, f, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_propose", json.RawMessage(in))
		if err != nil || f == nil || !strings.Contains(f.Sentence, "refused:") {
			t.Errorf("smuggled %s: f=%v err=%v, want a refused: Failure", smuggle, f, err)
		}
	}
}

// TestBlueprintProposeFilesTheInertRowByTheAgentActor: the author is the
// STAMPED step identity (OL3), the row is PROPOSED (TD2), and nothing
// else moved — no blueprint row, no revision. Inertness beyond this (a
// sitting row changes no read, commit is a person's act) is T9's
// TestBlueprintProposalsAreInert over the same store machinery.
func TestBlueprintProposeFilesTheInertRowByTheAgentActor(t *testing.T) {
	e := newBPRecordsEnv(t)
	before := bpBlueprintsOnly(t, e)
	content := bpGate("bp-new", "ship it")
	out, f, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_propose", bpToolInput("bp-new", content, true, "one lane, gate-only"))
	if f != nil || err != nil {
		t.Fatalf("propose: f=%v err=%v", f, err)
	}
	res, ok := out.(protocol.PolicyProposeResult)
	if !ok {
		t.Fatalf("propose answered %T, want the inert propose result", out)
	}
	if res.Note != protocol.PolicyProposeInertNote {
		t.Errorf("inertness note drifted: %q", res.Note)
	}
	if res.Proposal.Kind != "blueprint_save" || res.Proposal.Status != "proposed" {
		t.Errorf("row is %s/%s, want blueprint_save/proposed", res.Proposal.Kind, res.Proposal.Status)
	}
	if res.Proposal.CreatedByKind != "agent" || res.Proposal.CreatedByID != "implementer" {
		t.Errorf("author is %s/%s, want the stamped agent (OL3)", res.Proposal.CreatedByKind, res.Proposal.CreatedByID)
	}
	// The payload is the DESK's shape (createOnly camel), carrying the
	// whole source — never a patch.
	var payload struct {
		Name       string `json:"name"`
		Content    string `json:"content"`
		CreateOnly bool   `json:"createOnly"`
		Note       string `json:"note"`
	}
	if err := json.Unmarshal(res.Proposal.Payload, &payload); err != nil {
		t.Fatalf("stored payload will not parse: %v", err)
	}
	if payload.Name != "bp-new" || payload.Content != content || !payload.CreateOnly {
		t.Errorf("stored payload drifted: %+v", payload)
	}
	if _, found, err := e.s.BlueprintGet(e.ctx, "bp-new"); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatal("the propose arm WROTE a blueprint row — BP9: an agent proposes, a person commits")
	}
	if revs, err := e.s.RevisionList(e.ctx, "bp-new", 10); err != nil {
		t.Fatal(err)
	} else if len(revs) != 0 {
		t.Fatalf("the propose arm wrote %d revisions", len(revs))
	}
	if after := bpBlueprintsOnly(t, e); after != before {
		t.Fatalf("the sitting proposal changed what an agent can read:\n before: %s\n after:  %s", before, after)
	}
}

// TestBlueprintProposeInvalidRefusedAtCreateWithReportAndCreatesNothing:
// rule 2. The validator's sentence — the Analyze report, JSON — travels
// VERBATIM, and the row count does not move. Inert nonsense is never
// carried to a person's desk.
func TestBlueprintProposeInvalidRefusedAtCreateWithReportAndCreatesNothing(t *testing.T) {
	e := newBPRecordsEnv(t)
	rows, err := e.s.ListProposals("proposed")
	if err != nil {
		t.Fatal(err)
	}
	before := len(rows)
	broken := "id: broken-idea\nschemaVersion: 1\nversion: 1\nlanes: []\n"
	_, f, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_propose", bpToolInput("broken-idea", broken, true, "worth a look"))
	if err != nil || f == nil {
		t.Fatalf("an invalid candidate was accepted: f=%v err=%v", f, err)
	}
	if !strings.Contains(f.Sentence, "diagnostics") || !strings.Contains(f.Sentence, `"state":"invalid"`) {
		t.Errorf("the refusal paraphrased the report: %s", f.Sentence)
	}
	want, _ := json.Marshal(blueprint.Analyze([]byte(broken), blueprintValidationOptions("")))
	if f.Sentence != string(want) {
		t.Errorf("the report was paraphrased:\n got %s\nwant %s", f.Sentence, want)
	}
	after, err := e.s.ListProposals("proposed")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != before {
		t.Fatalf("the refused proposal still created a row (%d → %d)", before, len(after))
	}
	if _, found, _ := e.s.BlueprintGet(e.ctx, "broken-idea"); found {
		t.Fatal("the refused candidate reached the row table")
	}
}

// TestBlueprintProposeCapsAreCounted: the two ceilings, each named with
// its number. Over-size content is refused BY NAME at the arm before the
// validator ever sees it; the open-proposal cap refuses the 21st with the
// limit and the way to see the pile (proposal_list) — S45's counted
// refusal, the same constant, the same machinery.
func TestBlueprintProposeCapsAreCounted(t *testing.T) {
	e := newBPRecordsEnv(t)
	// — the size half —
	huge := bpGate("bp-huge", "ship it") + strings.Repeat("#", maxBlueprintContentBytes+1)
	_, f, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_propose", bpToolInput("bp-huge", huge, true, "too big"))
	if err != nil || f == nil || !strings.Contains(f.Sentence, "1048576") || !strings.Contains(f.Sentence, "blueprint_propose") {
		t.Fatalf("over-size propose: f=%v err=%v, want a refusal naming blueprint_propose and 1048576", f, err)
	}
	longNote := strings.Repeat("n", maxBlueprintNoteBytes+1)
	_, f, err = e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_propose", bpToolInput("bp-note", bpGate("bp-note", "ship it"), true, longNote))
	if err != nil || f == nil || !strings.Contains(f.Sentence, "256") {
		t.Fatalf("over-long note: f=%v err=%v, want a refusal naming 256", f, err)
	}
	// The same ceiling guards the two text arms by name.
	if _, f, err := e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_validate", json.RawMessage(`{"content":"`+strings.Repeat("a", maxBlueprintContentBytes+1)+`"}`)); err != nil || f == nil || !strings.Contains(f.Sentence, "blueprint_validate") {
		t.Fatalf("over-size validate: f=%v err=%v", f, err)
	}
	// — the per-agent open-proposal half —
	now := "2026-11-03T12:00:00Z"
	for i := 0; i < desk.MaxOpenProposalsPerAgent; i++ {
		if _, err := e.s.CreateProposal("blueprint_save", fmt.Sprintf(`{"name":"bp-flood-%d","content":"x","createOnly":true,"note":"flood"}`, i), store.Actor{Kind: "agent", ID: "implementer"}, now); err != nil {
			t.Fatalf("stage %d: %v", i, err)
		}
	}
	_, f, err = e.d.recordsSvc.Do(e.ctx, bpAgentActor(), "blueprint_propose", bpToolInput("bp-21st", bpGate("bp-21st", "ship it"), true, "one too many"))
	if err != nil || f == nil {
		t.Fatalf("the 21st proposal was accepted: f=%v err=%v", f, err)
	}
	if !strings.Contains(f.Sentence, "20") || !strings.Contains(f.Sentence, "proposal_list") {
		t.Errorf("the cap refusal is not counted and pointed: %s", f.Sentence)
	}
	// A DIFFERENT agent is unaffected — the cap is per agent, not global.
	if _, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: "reviewer"}, "blueprint_propose", bpToolInput("bp-other", bpGate("bp-other", "ship it"), true, "different author")); f != nil || err != nil {
		t.Fatalf("another agent at the cap: f=%v err=%v", f, err)
	}
}

// TestBlueprintReadsRefuseWithoutTheService: the arms belong to the read
// service (V13) — a RecordsService mounted without it refuses BY NAME,
// never answers an empty list that reads as "no blueprints exist".
func TestBlueprintReadsRefuseWithoutTheService(t *testing.T) {
	e := newBPRecordsEnv(t)
	svc := NewRecordsService(e.s) // no seams assigned — a reads-less mount
	for _, tc := range []struct{ tool, input string }{
		{"blueprint_list", `{}`},
		{"blueprint_show", `{"name":"x"}`},
		{"blueprint_validate", `{"content":"id: x\n"}`},
		{"blueprint_lint", `{"content":"id: x\n"}`},
	} {
		_, f, err := svc.Do(e.ctx, bpAgentActor(), tc.tool, json.RawMessage(tc.input))
		if err != nil || f == nil || !strings.Contains(f.Sentence, tc.tool) || !strings.Contains(f.Sentence, "V13") {
			t.Errorf("%s without the service: f=%v err=%v, want a V13 refusal naming the tool", tc.tool, f, err)
		}
	}
}
