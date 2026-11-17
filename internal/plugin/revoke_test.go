// The gates' decisions at the plugin layer: who is refused at CREATION
// (everything not enabled, with the two remedies differing — enable for a
// disabled agent, nothing for a revoked one), who is refused at ARRIVAL
// (revoked ONLY — the Q3 contrast that makes disable mean something), and
// what the refusal records (agent, revocation instant, reason, cost
// RECORDED-not-recovered). The REAL-Task tests — where a Task is created,
// revoked mid-flight and completed for real — live with the layer, in
// internal/agentsvc's revoke_gate_test.go: the S14 sweep forbids this
// package's test binary from linking agentsvc at all, and a unit stub of a
// race proves nothing (brief R2), so the honest home for the racing test is
// where the racing machinery lives.
package plugin

import (
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

func gatedAgent(t *testing.T, name string) (*fakeStateStore, *Agent) {
	t.Helper()
	return withStore(t, name)
}

func TestRevokeStopsNewInvocations(t *testing.T) {
	_, a := gatedAgent(t, "gated")
	gate := InvocationGate()
	// Imported means disabled means NO new invocations — the refusal exists
	// before anyone enabled anything.
	if _, refused := gate(a.Name); !refused {
		t.Fatal("a freshly imported (disabled) agent must not be invokable — arrival is not permission")
	}
	if _, rj := Enable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	if _, refused := gate(a.Name); refused {
		t.Fatal("an enabled plugin agent must be invokable or the feature is a museum")
	}
	// Disable: refused, with the ENABLE remedy and the in-flight promise.
	if _, rj := Disable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	refusal, refused := gate(a.Name)
	if !refused || !strings.Contains(refusal, "enable") || !strings.Contains(strings.ToLower(refusal), "in-flight") {
		t.Fatalf("the disabled refusal names its remedy AND its difference from revoke: %q", refusal)
	}
	// Revoke: refused, naming the instant and the reason.
	if _, rj := Enable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	if _, rj := Revoke(a.Name, "the vendor withdrew its card"); rj != nil {
		t.Fatal(rj.Error())
	}
	refusal, refused = gate(a.Name)
	if !refused || !strings.Contains(refusal, "withdrew its card") || !strings.Contains(refusal, "2026") {
		t.Fatalf("the revoked refusal names the reason and the instant: %q", refusal)
	}
	// First-party and unknown names are not plugin-managed: never refused.
	if _, refused := gate("golang-engineer"); refused {
		t.Fatal("refusing one of the 25 for lacking a plugin row is the daemon tripping over its own abstraction")
	}
	if _, refused := gate("never-imported"); refused {
		t.Fatal("unknown names are not plugin-managed")
	}
}

func TestArrivalGateRefusesOnlyRevoked(t *testing.T) {
	_, a := gatedAgent(t, "arrivals")
	gate := ArrivalGate()
	cost := &model.Cost{State: model.CostKnown, USD: 0.042}
	if _, refused := gate(a.Name, 7, cost); refused {
		t.Fatal("disabled ≠ revoked: a disabled agent's arrival is NOT stopped (Q3, AP12 — the two tests that disagree)")
	}
	if _, rj := Enable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	if _, refused := gate(a.Name, 7, cost); refused {
		t.Fatal("an enabled agent's result arrives, obviously")
	}
	if _, rj := Revoke(a.Name, "prompt-injection in its upstream docs"); rj != nil {
		t.Fatal(rj.Error())
	}
	refusal, refused := gate(a.Name, 7, cost)
	if !refused {
		t.Fatal("a revoked agent's in-flight result must be refused ON ARRIVAL — the whole point of AP7")
	}
	if !strings.Contains(refusal, "PAID FOR") || !strings.Contains(refusal, "0.042") {
		t.Fatalf("the refusal states the cost as RECORDED, not recovered: %q", refusal)
	}
	if !strings.Contains(refusal, "prompt-injection in its upstream docs") {
		t.Fatalf("the refusal names the revocation reason: %q", refusal)
	}
	// The record: agent, instant, reason, cost — what AP7 demands kept.
	rows, rj := Refusals(a.Name)
	if rj != nil {
		t.Fatal(rj.Error())
	}
	if len(rows) != 1 {
		t.Fatalf("one arrival refused, one record: %+v", rows)
	}
	r := rows[0]
	if r.Agent != a.Name || r.TaskID != 7 || r.RevokedAt.IsZero() || !r.CostKnown || !strings.Contains(r.CostJSON, "0.042") {
		t.Fatalf("the record names agent, task, instant, reason and cost: %+v", r)
	}
}

func TestArrivalGateUnreportedCostIsUnknownNeverZero(t *testing.T) {
	_, a := gatedAgent(t, "unreported")
	if _, rj := Revoke(a.Name, "vendor withdrew"); rj != nil {
		t.Fatal(rj.Error())
	}
	refusal, refused := ArrivalGate()(a.Name, 9, nil)
	if !refused {
		t.Fatal("refused regardless of cost reporting")
	}
	if !strings.Contains(refusal, "unknown, not zero") {
		t.Fatalf("an unreported cost says UNKNOWN (S3's three states), never a confident 0: %q", refusal)
	}
	rows, _ := Refusals(a.Name)
	if len(rows) != 1 || rows[0].CostKnown || rows[0].CostJSON != "" {
		t.Fatalf("the record keeps the unknown as unknown: %+v", rows)
	}
}

func TestArrivalGateReadsTheDurableRecord(t *testing.T) {
	// A restarted daemon holds the in-memory copy from boot, but the ROWS
	// are the truth (P2's discipline): a durable revoke must refuse even if
	// the in-memory state never heard about it.
	f, a := gatedAgent(t, "restarted")
	if _, rj := Enable(a.Name); rj != nil {
		t.Fatal(rj.Error())
	}
	// Reach around the verb: flip ONLY the durable record (simulates the
	// stale in-memory copy after a crash + operator revoke elsewhere).
	f.mu.Lock()
	f.live[a.Name] = StateRevoked
	f.history[a.Name] = append(f.history[a.Name], TransitionRecord{Agent: a.Name, From: StateEnabled, To: StateRevoked, Reason: "revoke landed while this process slept", At: time.Now().UTC()})
	f.mu.Unlock()
	if _, refused := ArrivalGate()(a.Name, 11, nil); !refused {
		t.Fatal("the durable record outranks the in-memory copy — revocation that needs every copy in sync is revocation with a hole")
	}
}

func TestRefusalRecordsReachPromptFalseAndNeverCarryStrangerWords(t *testing.T) {
	// The refusal sentence replaces the result; nothing here ever takes the
	// result text as input — the arrival gate sees the AGENT, the task id
	// and the cost, and nothing else. That is the shape proof: the stranger's
	// words cannot reach the record because they are never handed to it.
	_, a := gatedAgent(t, "quiet-stranger")
	if _, rj := Revoke(a.Name, "quiet"); rj != nil {
		t.Fatal(rj.Error())
	}
	// (The model-facing half — "never reaches a prompt" against a REAL
	// question row — is asserted in agentsvc's revoke_gate_test.go.)
	// The signature itself is the shape proof — (agent, taskID, cost) and
	// NO result parameter: the stranger's words are never handed to the
	// refusal machinery, so they cannot leak into a record or a prompt
	// through this path. Assert the sentence carries only our own words.
	refusal, _ := ArrivalGate()(a.Name, 12, &model.Cost{State: model.CostKnown, USD: 0.01})
	for _, ours := range []string{"quiet", "refused on arrival", "PAID FOR"} {
		if !strings.Contains(refusal, ours) {
			t.Fatalf("the refusal is our own sentence, naming %q: %q", ours, refusal)
		}
	}
}
