package daemon

// records_triage_test.go — S51 T5 (TM6, rule 4): the three bug arms of the
// records service. What each test pins, in the order the spec lists them:
//
//   - the two READ arms answer the SAME thing the person's socket doors
//     answer — bug_show runs the very protocol.AnswerTriageShow the handler
//     calls (compared through canonical JSON, field for field), and
//     bug_list forwards through the injected closure untouched, params and
//     answer alike (rule 4 — one implementation, two doors; production
//     wires the SAME function value the Table seam holds);
//   - unwired, the arms say so by name (V13) instead of answering an empty
//     backlog that would read as "nothing was filed" (P3);
//   - the filing arm files UNVALIDATED and priority-unknown, filed_by the
//     resolved SLUG, with exactly one audit row — op 'file', actor kind
//     'agent', actor id the slug — and refuses a stamped id that names no
//     agent, a blank field, a smuggled identity or state field, all before
//     a row exists;
//   - at triage.MaxOpenClaimsPerAgent the counted refusal names the cap,
//     the number held, and how to list them; nothing files past it;
//   - a person meets triage.file by name (the pointer sentence), never an
//     agent filing under a step.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

func newTriageRecordsEnv(t *testing.T) (recordsEnv, *triage.Triage) {
	t.Helper()
	e := newRecordsEnv(t, "")
	// A ROOT component to file into (the harness's /repo/alpha is a child;
	// triage is scoped to a project root — org-group is the root).
	tr := triage.New(e.s)
	e.svc.triagePlane = tr
	e.svc.triageBoard = func(_ context.Context, p protocol.TriageBoardParams) (any, error) {
		return map[string]any{"echoed": p}, nil
	}
	return e, tr
}

func seedAgent(t *testing.T, e recordsEnv, name, slug string) {
	t.Helper()
	if err := e.s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Name: name, Slug: slug, Description: "d", Prompt: "p", Version: "1"}, "2026-11-04T00:00:00Z"); err != nil {
		t.Fatalf("seed agent %s: %v", slug, err)
	}
}

func TestBugShowAnswersTheDoorRenderingByteForByte(t *testing.T) {
	e, tr := newTriageRecordsEnv(t)
	b, ref := tr.CreateAudited("/repo/org-group", triage.NewBug{Title: "the walk times out on the second page", Body: "steps: open the walk; page twice; timeout", FiledBy: "person"}, triage.Actor{Kind: "user", ID: "person"})
	if ref != nil {
		t.Fatalf("seed: %s", ref.Reason)
	}
	toolOut, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "bug_show", json.RawMessage(`{"id":`+itoa(b.ID)+`}`))
	if err != nil || f != nil {
		t.Fatalf("bug_show: f=%v err=%v", f, err)
	}
	doorOut, err := protocol.AnswerTriageShow(tr, b.ID) // the very call handleTriageShow now makes
	if err != nil {
		t.Fatal(err)
	}
	a := triageCanon(t, toolOut)
	bc := triageCanon(t, doorOut)
	if string(a) != string(bc) {
		t.Errorf("the tool answer drifted from the socket answer:\n tool: %s\n door: %s", a, bc)
	}
	// And the unknown row rides the plane's own refusal, not a fake view.
	_, f2, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "bug_show", json.RawMessage(`{"id":9999}`))
	if err == nil && f2 == nil {
		t.Error("an unknown id answered instead of refusing")
	} else if err != nil {
		var ae *protocol.Error // the plane's refusal, carried through with its detail
		if !errors.As(err, &ae) || !strings.Contains(ae.Data.What, "does not exist") {
			t.Errorf("refusal words: %v", err)
		}
	}
}

func TestBugListForwardsThroughTheOneBoardAnswer(t *testing.T) {
	e, _ := newTriageRecordsEnv(t)
	in := json.RawMessage(`{"componentRoot":"/repo/org-group","validation":"unvalidated"}`)
	toolOut, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "bug_list", in)
	if err != nil || f != nil {
		t.Fatalf("bug_list: f=%v err=%v", f, err)
	}
	echoed, ok := toolOut.(map[string]any)
	if !ok {
		t.Fatalf("the board answer was not passed through: %T", toolOut)
	}
	p := string(triageCanon(t, echoed["echoed"]))
	if !strings.Contains(p, "org-group") || !strings.Contains(p, "unvalidated") {
		t.Errorf("params were rewritten in transit: %s (rule 4 — the one closure the Table seam holds)", p)
	}
}

func TestBugArmsRefuseAPlanelessMount(t *testing.T) {
	e := newRecordsEnv(t, "") // never wired — the V13 side
	for _, name := range []string{"bug_list", "bug_show", "bug_file"} {
		_, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, name, json.RawMessage(`{"componentRoot":"/repo/org-group","id":1,"title":"t","body":"b"}`))
		if err != nil || f == nil {
			t.Errorf("%s on a planeless mount answered instead of refusing (f=%v err=%v)", name, f, err)
			continue
		}
		if !strings.Contains(f.Sentence, "triage plane") || !strings.Contains(f.Sentence, name) {
			t.Errorf("%s refusal does not name itself and the plane: %s", name, f.Sentence)
		}
	}
}

func TestBugFileFilesAnUnvalidatedClaimWithOneAgentAuditRow(t *testing.T) {
	e, tr := newTriageRecordsEnv(t)
	seedAgent(t, e, "implementer", "implementer-bot") // the step says NAME; filed_by must land the SLUG
	out, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "bug_file",
		json.RawMessage(`{"componentRoot":"/repo/org-group","title":"ledger total drifts after a partial refund","body":"refund half an invoice; the report total is off by the refunded cents"}`))
	if err != nil || f != nil {
		t.Fatalf("bug_file: f=%v err=%v", f, err)
	}
	view, ok := out.(map[string]any)
	if !ok {
		// The view is *protocol.TriageView — go through canonical JSON.
		if err := json.Unmarshal(triageCanon(t, out), &view); err != nil {
			t.Fatal(err)
		}
	}
	if view["validation"] != "unvalidated" {
		t.Errorf("an agent's claim arrived validated: %v (TM6 — the store's default is the backstop)", view["validation"])
	}
	if view["priorityState"] != "unknown" {
		t.Errorf("an agent's claim arrived with a priority: %v (D2's honest absence)", view["priorityState"])
	}
	if view["filedBy"] != "implementer-bot" {
		t.Errorf("filedBy = %v, want the resolved slug (TM3 — the stamp, never a param)", view["filedBy"])
	}
	id := int64(view["id"].(float64))
	hs, ref := tr.History(id, 10)
	if ref != nil {
		t.Fatal(ref.Reason)
	}
	if len(hs) != 1 || hs[0].Op != "file" || hs[0].ActorKind != "agent" || hs[0].ActorID != "implementer-bot" {
		t.Errorf("the filing audit is not the one agent-filed row: %+v", hs)
	}
}

func TestBugFileCapRefusesByNameAndSaysHowToSeeThem(t *testing.T) {
	e, _ := newTriageRecordsEnv(t)
	seedAgent(t, e, "implementer", "implementer-bot")
	for i := 0; i < triage.MaxOpenClaimsPerAgent; i++ {
		if _, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "bug_file",
			json.RawMessage(`{"componentRoot":"/repo/org-group","title":"claim `+itoa(int64(i))+`","body":"b"}`)); err != nil || f != nil {
			t.Fatalf("filing %d under the cap: f=%v err=%v", i, f, err)
		}
	}
	_, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "bug_file",
		json.RawMessage(`{"componentRoot":"/repo/org-group","title":"one claim too many","body":"b"}`))
	if err != nil || f == nil {
		t.Fatalf("the cap did not refuse: f=%v err=%v", f, err)
	}
	for _, want := range []string{"implementer-bot", itoa(int64(triage.MaxOpenClaimsPerAgent)), "MaxOpenClaimsPerAgent", "bug_list"} {
		if !strings.Contains(f.Sentence, want) {
			t.Errorf("the cap refusal does not name %q: %s", want, f.Sentence)
		}
	}
	held, err := e.s.CountOpenClaimsByFiler("implementer-bot")
	if err != nil || held != triage.MaxOpenClaimsPerAgent {
		t.Errorf("the refused filing stored a row anyway: held=%d err=%v", held, err)
	}
}

func TestBugFileRefusesASmuggledFieldAndAnUnknownAgent(t *testing.T) {
	e, _ := newTriageRecordsEnv(t)
	seedAgent(t, e, "implementer", "implementer-bot")
	_, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "bug_file",
		json.RawMessage(`{"componentRoot":"/r","title":"t","body":"b","filedBy":"someone-else"}`))
	if err == nil && f == nil {
		t.Fatal("a smuggled filedBy was accepted")
	}
	if err != nil && !strings.Contains(err.Error(), "filedBy") {
		t.Errorf("the strict decode did not name the smuggled field: %v", err)
	}
	// An unknown stamped id: the step's own agent is NOT in the agent table.
	e2 := newRecordsEnv(t, "")
	e2.svc.triagePlane = triage.New(e2.s)
	_, f, err = e2.svc.Do(e2.ctx, Actor{Kind: "agent", ID: "implementer"}, "bug_file",
		json.RawMessage(`{"componentRoot":"/repo/org-group","title":"t","body":"b"}`))
	if err != nil || f == nil || !strings.Contains(f.Sentence, "implementer") {
		t.Errorf("an unattributable filing was not refused by name: f=%v err=%v", f, err)
	}
	held, _ := e2.s.CountOpenClaimsByFiler("implementer")
	if held != 0 {
		t.Error("the refused filing stored a row anyway")
	}
}

func TestBugFileIsRefusedToAPersonWithThePointerSentence(t *testing.T) {
	e, _ := newTriageRecordsEnv(t)
	_, f, err := e.svc.Do(context.Background(), Actor{Kind: "user", ID: "1000"}, "bug_file",
		json.RawMessage(`{"componentRoot":"/repo/org-group","title":"t","body":"b"}`))
	if err != nil || f == nil {
		t.Fatalf("a person passed the filing door: f=%v err=%v", f, err)
	}
	if !strings.Contains(f.Sentence, "triage.file") || !strings.Contains(f.Sentence, "person's own act") {
		t.Errorf("the person refusal lost the pointer sentence: %s", f.Sentence)
	}
	// The two reads likewise point a person at their unscoped doors.
	for name, door := range map[string]string{"bug_list": "triage.board", "bug_show": "triage.show"} {
		_, f, err := e.svc.Do(context.Background(), Actor{Kind: "user", ID: "1000"}, name, json.RawMessage(`{"componentRoot":"/r","id":1}`))
		if err != nil || f == nil || !strings.Contains(f.Sentence, door) {
			t.Errorf("%s for a person: f=%v err=%v — must point at %s", name, f, err, door)
		}
	}
}

// triageCanon renders a value through its JSON — the bytes a client sees —
// so the tool answer and the door answer are compared as the wire compares
// them (rule 4's test is about the answer, not about Go pointers).
func triageCanon(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
