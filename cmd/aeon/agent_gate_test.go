// The real-Task half of T5/T6, at the SEAM — cmd/aeon is the one package
// the sweeps let link BOTH the agent-services layer and the plugin layer,
// so this is where the racing assertions live: a unit stub of a race proves
// nothing (brief R2), and the gates only meet a real async Task inside a
// binary that imports both halves (T8's deletability: `rm -r
// internal/plugin` must leave internal/agentsvc building, so those tests
// cannot live there — the move is recorded in T8's notes).
//
// What runs: a REAL package imported (real signed card verified through the
// production loader wired by wirePluginSeams), a REAL loop lane and running
// step (S14's fixture shape), a REAL async Task created, REVOKED mid-flight,
// and completed — with the arrival refused, recorded (agent, instant,
// reason, cost RECORDED-not-recovered), and answered to the caller by OUR
// sentence, never the stranger's words. The contrast test (disable → the
// Task COMPLETES, AS12-labelled) is the other half of the meaning: if these
// two tests ever agree, `disable` has stopped meaning "not now".
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/agentsvc"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/plugin"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// importForGateTest writes the three allow-listed files (the card signed by
// the ONE authoring helper) and imports through the REAL three-step path
// against the real 0023/0024 rows, with the production seams wired exactly
// as serve wires them.
func importForGateTest(t *testing.T, s *store.Store, name string, enable bool) *plugin.Agent {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: " + name + "\ndescription: in-flight fixture\nrequests:\n  tools: [read]\n---\nYou are " + name + ". Answer in one line.\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	card, err := json.Marshal(map[string]any{
		"name": name, "description": "fixture", "version": "1.0.0",
		"protocolVersion": "0.3", "url": "local",
		"defaultInputModes": []string{"text/plain"}, "defaultOutputModes": []string{"text/plain"},
		"skills": []map[string]any{{"id": "do-thing", "name": "Do the thing", "description": "does it", "inputModes": []string{"text/plain"}, "outputModes": []string{"text/plain"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte(name))
	priv := ed25519.NewKeyFromSeed(seed[:])
	env, err := json.Marshal(agentsvc.SignedCard{
		Card:      card,
		PublicKey: base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, card)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "card.json"), env, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wirePluginSeams(s); err != nil {
		t.Fatal(err)
	}
	plugin.Reset()
	t.Cleanup(func() { plugin.Reset() })
	a, rj := plugin.ImportChecked(dir, nil, time.Now().UTC())
	if rj != nil {
		t.Fatalf("import: %s", rj.Error())
	}
	if enable {
		if _, rj := plugin.Enable(a.Name); rj != nil {
			t.Fatal(rj.Error())
		}
	}
	return a
}

func gateSetup(t *testing.T, slug, agent string) (*agentsvc.TaskStore, int64, *plugin.Agent, loop.LoopState, *store.Store) {
	t.Helper()
	s, _ := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	t.Cleanup(func() { _ = s.Close() })
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "tasklane.yaml")
	raw := "id: tasklane\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: work\n    step: {kind: model-step, agent: golang-engineer, produces: OUT.md}\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := loop.Start(context.Background(), s, store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "delegate"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stepRow, err := s.CreateStep(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.Root},
		store.NewStep{Phase: "model", Agent: "golang-engineer", Attempt: 1, Status: store.StepRunning, Source: "gate-test"})
	if err != nil {
		t.Fatal(err)
	}
	a := importForGateTest(t, s, agent, true)
	ts := agentsvc.NewTaskStore(s)
	ts.SetInvocationGate(plugin.InvocationGate())
	ts.SetArrivalGate(plugin.ArrivalGate())
	return ts, stepRow.ID, a, st, s
}

const strangerWords = "the stranger's confident wrong answer"

func asyncFor(stepID int64, st loop.LoopState, agent string) agentsvc.CreateTask {
	return agentsvc.CreateTask{
		CallerLoopID: st.ID, CallerLane: "work", CallerStepID: stepID,
		CalleeAgent: agent, SkillID: "do-thing",
		Mode: agentsvc.TaskAsync, Params: map[string]any{"q": "hello"},
		Frozen: []policy.Policy{},
	}
}

// TestRevokeRefusesInFlightTaskResult is the brief's R2 in full: created
// while enabled, revoked mid-flight, completed after — REFUSED at arrival,
// the answer the model would read is our refusal, and the wait ENDED (a
// refusal that leaves the step suspended is a hang, not a refusal).
func TestRevokeRefusesInFlightTaskResult(t *testing.T) {
	ts, stepID, a, st, s := gateSetup(t, "revoke-inflight", "groot-translator")
	task, rj := ts.Create(context.Background(), asyncFor(stepID, st, a.Name))
	if rj != nil {
		t.Fatalf("creation while enabled: %s", rj.Detail)
	}
	if _, r := plugin.Revoke(a.Name, "the vendor shipped a data-exfil update"); r != nil {
		t.Fatal(r.Error())
	}
	if r := ts.Complete(context.Background(), task.ID, strangerWords, "ok"); r != nil {
		t.Fatalf("arrival: %s", r.Detail)
	}
	got, err := ts.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != agentsvc.TaskRefused {
		t.Fatalf("AP7: a revoked agent's completed Task must land REFUSED (not completed, not failed): %s", got.Status)
	}
	q, err := s.ReadQuestion(task.QuestionID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Answer, "refused on arrival") || strings.Contains(q.Answer, strangerWords) {
		t.Fatalf("the answer the model would read is the refusal, not the stranger: %+v", q)
	}
	step, err := s.ReadStep(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != string(store.StepDone) {
		t.Fatalf("the caller resumed carrying the refusal: step is %s", step.Status)
	}
}

// TestDisabledAgentInFlightTaskCompletes is the deliberate disagreement
// (Q3, AP12): same timing, disable instead of revoke — the work ARRIVES,
// AS12-labelled, because the label is all disable ever was.
func TestDisabledAgentInFlightTaskCompletes(t *testing.T) {
	ts, stepID, a, st, s := gateSetup(t, "disable-inflight", "slow-echo")
	task, rj := ts.Create(context.Background(), asyncFor(stepID, st, a.Name))
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if _, r := plugin.Disable(a.Name); r != nil {
		t.Fatal(r.Error())
	}
	if r := ts.Complete(context.Background(), task.ID, strangerWords, "ok"); r != nil {
		t.Fatalf("a disabled agent's arrival is NOT stopped: %s", r.Detail)
	}
	got, err := ts.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != agentsvc.TaskCompleted {
		t.Fatalf("disable is \"not now\": the in-flight Task COMPLETES — got %s", got.Status)
	}
	q, err := s.ReadQuestion(task.QuestionID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Answer, strangerWords) || !strings.Contains(strings.ToLower(q.Answer), "untrusted") {
		t.Fatalf("the result arrives, AS12-labelled (the label, not a ban, is what disable leaves you with): %q", q.Answer)
	}
}

func TestRefusalRecordedWithAgentInstantReasonCost(t *testing.T) {
	ts, stepID, a, st, s := gateSetup(t, "revoke-cost", "groot-costed")
	task, rj := ts.Create(context.Background(), asyncFor(stepID, st, a.Name))
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	if _, r := plugin.Revoke(a.Name, "incident 4471: exfiltration via the vendor's docs"); r != nil {
		t.Fatal(r.Error())
	}
	cost := &model.Cost{State: model.CostKnown, USD: 0.042}
	if r := ts.CompleteWithCost(context.Background(), task.ID, strangerWords, "ok", cost); r != nil {
		t.Fatal(r.Detail)
	}
	rows, r := plugin.Refusals(a.Name)
	if r != nil {
		t.Fatal(r.Error())
	}
	if len(rows) != 1 {
		t.Fatalf("one arrival stopped, one record: %+v", rows)
	}
	rec := rows[0]
	if rec.Agent != a.Name || rec.TaskID != task.ID {
		t.Fatalf("the record names the agent and the Task: %+v", rec)
	}
	if rec.RevokedAt.IsZero() || !strings.Contains(rec.Reason, "4471") {
		t.Fatalf("the record carries the revocation instant and its reason: %+v", rec)
	}
	if !rec.CostKnown || !strings.Contains(rec.CostJSON, "0.042") {
		t.Fatalf("the cost stands RECORDED — not recovered, not zeroed: %+v", rec)
	}
	raw, err := s.PluginRefusals(a.Name)
	if err != nil || len(raw) != 1 || raw[0].ReachedPrompt {
		t.Fatalf("reached_prompt is written false (a refusal whose words reached a prompt is a bug, and bugs are not written into ledgers as normal): %+v err=%v", raw, err)
	}
}

func TestRevokeStopsNewInvocationsImmediately(t *testing.T) {
	ts, stepID, a, st, s := gateSetup(t, "revoke-create", "groot-create")
	if _, r := plugin.Revoke(a.Name, "never again"); r != nil {
		t.Fatal(r.Error())
	}
	_, rj := ts.Create(context.Background(), asyncFor(stepID, st, a.Name))
	if rj == nil {
		t.Fatal("a revoked agent must take no NEW Tasks")
	}
	if rj.Field != "state" || !strings.Contains(rj.Detail, "never again") {
		t.Fatalf("the refusal names the state and the why: %s", rj.Detail)
	}
	if n, err := s.CountAgentTasksForCaller(st.ID, stepID); err != nil || n != 0 {
		t.Fatalf("the refusal wrote nothing: n=%d err=%v", n, err)
	}
}

func TestRevokeDoesNotDeletePolicies(t *testing.T) {
	_, _, a, _, s := gateSetup(t, "revoke-policies", "groot-policies")
	if err := s.PolicyAdd(context.Background(), store.PolicyRow{
		ID: "keep-me", Type: "tool_access", TargetKind: "agent", TargetID: a.Name,
		Action: "deny", Source: "operator", SpecJSON: `{"toolName":"webfetch","state":"deny"}`,
		CreatedAt: "2026-10-25T10:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if _, r := plugin.Revoke(a.Name, "revoke for any reason at all"); r != nil {
		t.Fatal(r.Error())
	}
	rows, err := s.PolicyListAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for _, row := range rows {
		if row.ID == "keep-me" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Q2: revoke does NOT delete the granting policies — they are the operator's, with their own lifecycle (and T4's consent shows them on re-import): n=%d", n)
	}
}

// TestImportedResultIsLabelledUntrusted completes the T6 end-to-end claim at
// the seam: the assembled answer carries S14's region GRAMMAR (a fresh
// nonce per call makes byte-equality a fiction — one opener, the naming
// what-string verbatim, the result verbatim inside, matching closer), and
// the first-party boundary reads from the registry's predicate: the
// compiled 25 go unlabelled, an imported agent never does — whatever its
// manifest claims.
func TestImportedResultIsLabelledUntrusted(t *testing.T) {
	ts, stepID, a, st, s := gateSetup(t, "prov-label", "groot-labelled")
	task, rj := ts.Create(context.Background(), asyncFor(stepID, st, a.Name))
	if rj != nil {
		t.Fatal(rj.Detail)
	}
	ts.SetFirstParty(func(agent string) bool { return agent == "golang-engineer" })
	result := "Groot say: I come in peace"
	if r := ts.Complete(context.Background(), task.ID, result, "ok"); r != nil {
		t.Fatal(r.Detail)
	}
	q, err := s.ReadQuestion(task.QuestionID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(q.Answer, "[untrusted-") || strings.Count(q.Answer, "[untrusted-") != 1 {
		t.Fatalf("exactly one S14 region, no double-labelling: %q", q.Answer)
	}
	if !strings.Contains(q.Answer, `skill result from agent "`+a.Name+`" skill "do-thing"`) {
		t.Fatalf("the label NAMES the stranger (S14's what-string, verbatim): %q", q.Answer)
	}
	if !strings.Contains(q.Answer, "\n"+result+"\n") {
		t.Fatalf("the result rides VERBATIM inside the region: %q", q.Answer)
	}
	parts := strings.Split(q.Answer, "\n")
	opener := strings.TrimSuffix(strings.TrimPrefix(q.Answer[:strings.Index(q.Answer, " — ")], "[untrusted-"), "]")
	last := parts[len(parts)-1]
	if !strings.HasPrefix(last, "[/untrusted-") || last != "[/untrusted-"+opener+"]" {
		t.Fatalf("the closer is the same nonce as the opener (multipart boundary discipline): %q", q.Answer)
	}
}
