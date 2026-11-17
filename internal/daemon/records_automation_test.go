package daemon

// records_automation_test.go — S49 T7 (AN9, rule 1–4, §4c): the five
// automation arms of the records service. What each test pins, in the order
// the spec lists them:
//
//   - the four READ arms answer the SAME bytes the person's socket doors
//     answer, because both run the one rendering protocol.AnswerAutomation*
//     (rule 4 — the tool and its twin cannot drift field for field, and this
//     file is what catches a drift if one is ever introduced);
//   - unwired, the plane says so by name (V13) instead of answering an empty
//     listing that reads as "nothing starts agents on its own" (P3, AN5);
//   - the propose arm refuses the unknown, the archived and the already-off
//     with the validator's own sentence (TD3 — mirrored here exactly as the
//     environment file mirrors its seam, because internal/daemon cannot
//     import package main; the parity of the PRODUCTION seam is pinned
//     there, TestAutomationDisableCreateAndCommitValidatorsAreOneFunction);
//   - the inert row is stamped with the AGENT the step belongs to (OL3)
//     while the automation does not move by one field, and the plane's
//     audit stays exactly as it was (AN9: an ask is not a change);
//   - a webhook secret VALUE never rides any answer, in success or in error
//     (§4c — the fixture keeps it live so the absence is earned, not empty);
//   - the 21st ask meets the counted refusal naming 20 and proposal_list,
//     and stores nothing (S45's shared cap carried onto the kind).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// autoRecordsEnv is a store-wired daemon over T7's seeded world —
//
//	sch-morning  schedule  0 7 * * * Europe/Lisbon  — live, the ask's target
//	sch-quiet    schedule  paused (off)             — already-off refusal
//	whk-deploy   webhook   AEON_T7_SECRET           — the live secret, value set
//	evt-bugs     event     live                     — event-side ask
//	evt-old      event     archived                 — the AN6 refusal
//
// The four read arms run over the SAME seams the person's ten standing doors
// answer through (assigned inside New, above the propose-twin registration),
// and the create path runs over an injected validator mirroring cmd/aeon's
// seam. The reads are seeded NON-empty — an empty fixture would let
// "answered nothing" pass as "answered".
type autoRecordsEnv struct {
	base   recordsEnv // the loop + step every records call stamps from (OL3)
	d      *Daemon
	s      *store.Store
	ctx    context.Context
	evtBug string // the live event trigger's GENERATED id
	evtOld string // the archived one's
}

func newAutoRecordsEnv(t *testing.T) autoRecordsEnv {
	t.Helper()
	e := autoRecordsEnv{base: newRecordsEnv(t, "")}
	e.s = e.base.s
	e.ctx = e.base.ctx
	ctx := context.Background()
	ws := t.TempDir()
	pc, err := e.s.CreateComponent(store.NewComponent{Name: "auto", Slug: "auto", Root: ws})
	if err != nil || !pc.OK() {
		t.Fatalf("component: %v", err)
	}
	if err := e.s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "sch-morning", Kind: "schedule", Blueprint: "bp-morning", Workspace: ws, Goal: "morning sweep",
		Expr: "0 7 * * *", Zone: "Europe/Lisbon", CreatedAt: "2026-10-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed sch-morning: %v", err)
	}
	if err := e.s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "sch-quiet", Kind: "schedule", Blueprint: "bp-quiet", Workspace: ws, Goal: "already stopped",
		Expr: "0 0 1 1 *", Zone: "UTC", Paused: true, CreatedAt: "2026-10-02T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed sch-quiet: %v", err)
	}
	if err := e.s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "whk-deploy", Kind: "webhook", Blueprint: "bp-deploy", Workspace: ws, Goal: "deploy the release",
		SecretEnv: "AEON_T7_SECRET", CreatedAt: "2026-10-03T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed whk-deploy: %v", err)
	}
	live, err := e.s.CreateEventTrigger(autoActor, store.NewEventTrigger{
		Name: "task squad", Blueprint: "bp-bugfix", Kickoff: "fix it",
		Match: store.Match{Event: "task.created", TaskType: []string{"bug"}}, MaxRunsPerHour: 4,
	})
	if err != nil {
		t.Fatalf("seed evt-bugs: %v", err)
	}
	e.evtBug = live.ID
	old, err := e.s.CreateEventTrigger(autoActor, store.NewEventTrigger{
		Name: "retired", Blueprint: "bp-old", Kickoff: "gone",
		Match: store.Match{Event: "goal.approved"}, MaxRunsPerHour: 10,
	})
	if err != nil {
		t.Fatalf("seed evt-old: %v", err)
	}
	e.evtOld = old.ID
	db, err := e.s.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE event_trigger SET enabled = 0, archived_at = '2026-11-01T00:00:00Z', archived_reason = 'retired by the person' WHERE id = ?`, old.ID); err != nil {
		t.Fatalf("archive evt-old: %v", err)
	}
	// The secret is LIVE for this test file's runs: health answers whether
	// it is SET (and its NAME), and TestNoEnvValueInAnyAutomationToolOutput
	// is worth nothing if the value was never there to leak.
	t.Setenv("AEON_T7_SECRET", "topsecret")
	reg := desk.New(desk.Deps{Boundary: testBoundaryOpener(e.s)}, desk.Seams{
		desk.KindAutomationDisable: {Validate: mirrorAutomationDisableValidate(e.s)},
	})
	e.d = New(Options{Store: e.s, ValidateProposal: reg.ValidateProposal})
	initConn(t, e.d, ctx, "c1", "test")
	if e.d.recordsSvc == nil {
		t.Fatal("the daemon built no records service — the arms have nothing to run on")
	}
	if e.d.recordsSvc.automationReads == nil {
		t.Fatal("the daemon did not hand the records service the automation plane's own seam — the read arms would refuse (V13) on a wired mount")
	}
	return e
}

// mirrorAutomationDisableValidate is cmd/aeon's automation_disable seam
// rebuilt from pieces this package owns — the desk shape half, then the row,
// then the two AN9 rules — so the arm's verbatim-refusal assertions mean what
// they say about the ARM. The parity of the production seam (create and
// commit calling one function) is pinned in cmd/aeon.
func mirrorAutomationDisableValidate(st *store.Store) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		if err := desk.ValidateAutomationDisable(ctx, payload); err != nil {
			return err
		}
		var p desk.AutomationDisableProposal
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		var off, archived bool
		switch p.Source {
		case "event":
			row, err := st.GetEventTrigger(p.ID)
			if err != nil {
				return fmt.Errorf("refused: no event automation %q exists — automation.list shows what exists (S49 T6)", p.ID)
			}
			off, archived = !row.Enabled, row.ArchivedAt != ""
		default:
			row, err := st.TriggerSchedule(ctx, p.ID)
			if err != nil {
				// The id may exist UNDER THE OTHER FAMILY — the cross-hall
				// naming is the store read's own honesty, carried (AN1).
				if other, oerr := st.GetEventTrigger(p.ID); oerr == nil && other.ID != "" {
					return fmt.Errorf("refused: %q is an event automation, not the %s named — the source is part of the name (S49 T6, AN1)", p.ID, p.Source)
				}
				return fmt.Errorf("refused: no %s automation %q exists — automation.list shows what exists (S49 T6)", p.Source, p.ID)
			}
			if row.Kind != p.Source {
				return fmt.Errorf("refused: %q is a %s, not the %s named — the source is part of the source-named identity (S49 T6, AN1)", p.ID, row.Kind, p.Source)
			}
			off, archived = row.Paused, row.ArchivedAt != ""
		}
		if archived {
			return fmt.Errorf("refused: %s/%s is archived — an archived automation is history, not a draft, and its switch is already past off; automation.unarchive is the person's door (S49 T6, AN6)", p.Source, p.ID)
		}
		if off {
			return fmt.Errorf("refused: %s/%s is already off — nothing to disable. An ask that changes nothing is refused at create, not carried to a commit nobody should have to make (S49 T6, AN9)", p.Source, p.ID)
		}
		return nil
	}
}

func autoDisableInput(source, id, reason string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"source": source, "id": id, "reason": reason})
	return b
}

// TestToolReadsEqualSocketReads is rule 4 in the direction that matters: the
// tool door and the socket door run the SAME rendering over the SAME seam,
// so their answers are the same bytes — not two renderings of the same idea
// that can drift a statement, a count or a cursor a field at a time. The
// health pair is compared with its evaluatedAt stripped AND both stamps
// proven present: two calls straddle a clock tick, and the honest stamp is
// the moment each read happened — the strip is the honest equality, not a
// gap.
func TestToolReadsEqualSocketReads(t *testing.T) {
	e := newAutoRecordsEnv(t)
	cases := []struct {
		tool   string
		input  string
		method protocol.MethodName
	}{
		{"automation_list", `{}`, protocol.MethodAutomationList},
		{"automation_list", `{"source":"schedule"}`, protocol.MethodAutomationList},
		{"automation_list", `{"q":"no-such-thing"}`, protocol.MethodAutomationList},
		{"automation_show", `{"source":"schedule","id":"sch-morning"}`, protocol.MethodAutomationShow},
		{"automation_fires", `{}`, protocol.MethodAutomationFires},
		{"automation_fires", `{"source":"schedule","limit":2}`, protocol.MethodAutomationFires},
		{"automation_health", `{"source":"webhook"}`, protocol.MethodAutomationHealth},
	}
	for _, tc := range cases {
		toolOut, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, tc.tool, json.RawMessage(tc.input))
		if f != nil || err != nil {
			t.Fatalf("%s %s: failure=%v err=%v", tc.tool, tc.input, f, err)
		}
		sockOut, serr := e.d.Table.Dispatch(context.Background(), "c1", tc.method, json.RawMessage(tc.input))
		if serr != nil {
			t.Fatalf("%s's socket door %s: %v", tc.tool, tc.method, serr)
		}
		a, _ := json.Marshal(toolOut)
		b, _ := json.Marshal(sockOut)
		if tc.tool == "automation_health" {
			sa := stripEvaluatedAt(t, string(a))
			sb := stripEvaluatedAt(t, string(b))
			if string(a) == sa || string(b) == sb {
				t.Fatalf("%s answer carries no evaluatedAt — the stamp is the honesty of the snapshot (AN10)", tc.tool)
			}
			a, b = []byte(sa), []byte(sb)
		}
		if string(a) != string(b) {
			t.Errorf("%s %s answered\n %s\nand its socket door answered\n %s — two renderings of one row (rule 4)", tc.tool, tc.input, a, b)
		}
	}
}

func stripEvaluatedAt(t *testing.T, s string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("strip: %v", err)
	}
	if v, ok := m["evaluatedAt"]; !ok || v == "" {
		t.Fatal("health answered without its evaluatedAt")
	}
	delete(m, "evaluatedAt")
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestAutomationArmsRefuseAPlanelessMount is the V13 shape carried onto the
// family: a bare service (the unit-test mount, the storeless daemon) refuses
// by name, and never answers an empty listing that would read as AN5's
// store-empty sentence.
func TestAutomationArmsRefuseAPlanelessMount(t *testing.T) {
	e := newAutoRecordsEnv(t)
	bare := NewRecordsService(e.s) // no seams assigned at all
	for _, tool := range []string{"automation_list", "automation_show", "automation_fires", "automation_health"} {
		out, f, err := bare.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, tool, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if out != nil {
			t.Errorf("%s answered %v over a nil seam — absence must be stated, not answered", tool, out)
		}
		if f == nil || !strings.Contains(f.Sentence, tool) || !strings.Contains(f.Sentence, "V13") {
			t.Errorf("%s's refusal is %v, want one naming the tool and V13", tool, f)
		}
	}
}

// TestProposeDisableRefusedAtCreateWhenAlreadyOffOrUnknown is TD3 at the
// create door: the unknown, the cross-family id, the archived and the
// already-off each meet the validator's sentence VERBATIM, and nothing is
// stored — an inert row that decides nothing is still a row.
func TestProposeDisableRefusedAtCreateWhenAlreadyOffOrUnknown(t *testing.T) {
	e := newAutoRecordsEnv(t)
	cases := []struct{ source, id, want string }{
		{"schedule", "no-such-thing", `no schedule automation "no-such-thing" exists`},
		{"event", "sch-morning", `no event automation "sch-morning" exists`},
		{"schedule", e.evtBug, "is an event automation, not the schedule named"},
		{"event", e.evtOld, "is archived"},
		{"schedule", "sch-quiet", "is already off"},
	}
	for _, tc := range cases {
		_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "automation_propose_disable",
			autoDisableInput(tc.source, tc.id, "the nightly budget is spent and nothing downstream reads the output"))
		if err != nil {
			t.Fatalf("%s/%s: %v", tc.source, tc.id, err)
		}
		if f == nil || !strings.Contains(f.Sentence, tc.want) {
			t.Errorf("%s/%s: refusal=%v, want it to carry %q verbatim", tc.source, tc.id, f, tc.want)
		}
	}
	if n := countProposals(t, e.s, string(desk.KindAutomationDisable)); n != 0 {
		t.Errorf("%d rows stored after four refusals — what create refuses never becomes a row (TD3)", n)
	}
}

// TestProposeDisableCreatesInertProposalByTheAgentActor is AN9 whole: the
// row, the author, the inertness. The ask is the model's (created_by_*
// carries the STEP, never a typed actor — OL3); the automation does not move
// by one field; the plane's audit stays exactly as it was; and the answer
// says what the row IS (the inert note) and WHO may land it (the command).
func TestProposeDisableCreatesInertProposalByTheAgentActor(t *testing.T) {
	e := newAutoRecordsEnv(t)
	out, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "automation_propose_disable",
		autoDisableInput("schedule", "sch-morning", "the embedder is out of quota and every run since Tuesday burns the retry budget"))
	if f != nil || err != nil {
		t.Fatalf("the ask should stand: failure=%v err=%v", f, err)
	}
	res, ok := out.(protocol.PolicyProposeResult)
	if !ok {
		t.Fatalf("answered %T, want protocol.PolicyProposeResult", out)
	}
	if res.Note != protocol.PolicyProposeInertNote {
		t.Errorf("the note is %q, not the one inertness sentence the conformance fixture copies", res.Note)
	}
	if res.Proposal.Command == "" {
		t.Error("no decision command on the answer — the person's door would not be named to the agent")
	}
	row, found := store.Proposal{}, false
	rows, err := e.s.ListProposals("proposed")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Kind == string(desk.KindAutomationDisable) {
			row, found = r, true
		}
	}
	if !found {
		t.Fatal("no automation_disable row was stored")
	}
	if row.CreatedByKind != "agent" || row.CreatedByID != e.base.agent {
		t.Errorf("authored by %s/%s, want agent/%s — authorship is the step's, never typed (OL3)", row.CreatedByKind, row.CreatedByID, e.base.agent)
	}
	var payload desk.AutomationDisableProposal
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.Source != "schedule" || payload.ID != "sch-morning" || payload.Reason == "" {
		t.Errorf("the payload lost the ask: %+v", payload)
	}
	// The automation itself did not move: still on the clock, not paused.
	tr, err := e.s.TriggerSchedule(context.Background(), "sch-morning")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Paused {
		t.Error("the ASK paused the schedule — an ask constrains nothing until a person commits it (AN9, TD2)")
	}
	// And the plane's audit says nothing happened either: the ask added no
	// automation_change row (the audit is for changes).
	n, err := e.s.AutomationChangeList(context.Background(), "schedule", "sch-morning", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 0 {
		t.Errorf("%d automation_change rows after an inert ask — the audit is for changes, and this was not one", len(n))
	}
}

// TestNoEnvValueInAnyAutomationToolOutput is §4c with the credential live:
// the webhook's secret env is SET (the fixture exported it) and its NAME
// rides health as the two-type answer it is; the VALUE meets every one of
// the five tool answers and appears in none.
func TestNoEnvValueInAnyAutomationToolOutput(t *testing.T) {
	e := newAutoRecordsEnv(t)
	agent := Actor{Kind: "agent", ID: e.base.agent}
	calls := []struct{ tool, input string }{
		{"automation_list", `{}`},
		{"automation_show", `{"source":"webhook","id":"whk-deploy"}`},
		{"automation_fires", `{"source":"webhook"}`},
		{"automation_health", `{"source":"webhook"}`},
		{"automation_propose_disable", `{"source":"webhook","id":"whk-deploy","reason":"the endpoint moved and the old token should stop being callable"}`},
	}
	for _, c := range calls {
		out, f, err := e.d.recordsSvc.Do(e.ctx, agent, c.tool, json.RawMessage(c.input))
		if f != nil || err != nil {
			t.Fatalf("%s: failure=%v err=%v", c.tool, f, err)
		}
		b, _ := json.Marshal(out)
		if strings.Contains(string(b), "topsecret") {
			t.Errorf("%s leaked the live secret value: %s", c.tool, b)
		}
	}
	// The NAME rides health — the absence above is the two types working,
	// not a fixture whose secret never made it into the answer at all.
	h, hf, hErr := e.d.recordsSvc.Do(e.ctx, agent, "automation_health", json.RawMessage(`{"source":"webhook"}`))
	if hf != nil || hErr != nil {
		t.Fatalf("health: %v %v", hf, hErr)
	}
	hb, _ := json.Marshal(h)
	if !strings.Contains(string(hb), "AEON_T7_SECRET") {
		t.Errorf("health did not even name the secret env — the fixture is empty and the test above proves nothing: %s", hb)
	}
}

// TestProposalLimit is S45's shared cap carried onto the new kind: at the
// limit the ask meets a refusal that NAMES the constant and the read that
// lists what is open, and stores nothing.
func TestProposalLimit(t *testing.T) {
	e := newAutoRecordsEnv(t)
	for i := 0; i < desk.MaxOpenProposalsPerAgent; i++ {
		if _, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "automation_propose_disable",
			autoDisableInput("event", e.evtBug, fmt.Sprintf("cap filler %d", i))); f != nil || err != nil {
			t.Fatalf("filler %d: failure=%v err=%v", i, f, err)
		}
	}
	_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "agent", ID: e.base.agent}, "automation_propose_disable",
		autoDisableInput("event", e.evtBug, "one too many"))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatalf("the %dth proposal stood — the cap is not applied to this kind (S45's shared rule)", desk.MaxOpenProposalsPerAgent+1)
	}
	if !strings.Contains(f.Sentence, fmt.Sprint(desk.MaxOpenProposalsPerAgent)) || !strings.Contains(f.Sentence, "proposal_list") {
		t.Errorf("the cap refusal does not name the number or the read: %s", f.Sentence)
	}
	if n := countProposals(t, e.s, string(desk.KindAutomationDisable)); n != desk.MaxOpenProposalsPerAgent {
		t.Errorf("%d rows stored, want the cap's %d — a counted refusal that stores is not counted", n, desk.MaxOpenProposalsPerAgent)
	}
}

// TestAutomationToolDoorsRefuseAPerson is MC2's shape carried onto the
// family: these five doors answer the step, and a person is pointed at the
// plane's own ten standing doors by name — served the agent's self-scoped
// view, a person would be shown a narrower truth than the one they hold.
func TestAutomationToolDoorsRefuseAPerson(t *testing.T) {
	e := newAutoRecordsEnv(t)
	for _, tool := range []string{"automation_list", "automation_show", "automation_fires", "automation_health", "automation_propose_disable"} {
		_, f, err := e.d.recordsSvc.Do(e.ctx, Actor{Kind: "user", ID: "person"}, tool, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "person's door") {
			t.Errorf("%s served a person the agent's door (refusal=%v) — the self-scoped view is not the person's view", tool, f)
		}
	}
}
