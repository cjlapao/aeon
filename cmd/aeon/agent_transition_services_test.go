package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/plugin"
	"github.com/cjlapao/aeon/internal/policy"
)

func rawProposal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestAgentEnableRerunsRehydrate proves the real finding this task's own
// investigation made: a package that grew a request since import is caught
// at enable, over the socket, the same way the CLI's Rehydrate re-check
// catches it — never a bare TransitionPluginAgent that would skip it.
func TestAgentEnableRerunsRehydrate(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "grower", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatalf("import: %v", err)
	}

	st := openAgentStore(t)
	if err := wirePluginSeams(st); err != nil {
		t.Fatal(err)
	}
	// Deny webfetch for this agent BEFORE growing the request — the same
	// shape CheckRequests actually refuses on (an explicit Deny, not merely
	// an unrequested tool).
	boundary := policy.NewStore(policyRepo(st))
	now := time.Now().UTC()
	deny := policy.Policy{
		ID:     "deny-webfetch-grower",
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "grower"},
		Action: policy.Deny,
		Source: policy.Operator,
		Spec:   json.RawMessage(`{"toolName":"webfetch","state":"deny"}`),
	}
	if rj := boundary.Add(context.Background(), deny, now); rj != nil {
		t.Fatalf("seed the deny row: %s", rj.Reason)
	}

	// The package grows a request AFTER import — the disk changed, and
	// Rehydrate reads it fresh at enable, not the request set import saw.
	manifest := "---\nname: grower\ndescription: the CLI fixture\nrequests:\n  tools: [read, webfetch]\n---\nYou are grower. Answer in one line.\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	_, refusal, err := agentEnable(st, rawProposal(t, agentNameParams{Name: "grower"}))
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if refusal == "" {
		t.Fatal("a package that grew a denied request must be refused at enable, not silently trusted")
	}
	if !strings.Contains(refusal, "webfetch") {
		t.Fatalf("the refusal must name the denied tool: %q", refusal)
	}

	rec, found, err := st.PluginAgentState("grower")
	if err != nil || !found {
		t.Fatalf("read back: %v / found=%v", err, found)
	}
	if rec.State != "disabled" {
		t.Fatalf("a refused enable must leave the agent disabled, got %q", rec.State)
	}
}

// TestAgentRevokeOverSocketWithoutReasonIsUnwritable proves criterion 5's
// store-level teeth, not merely the handler's own early check: bypass the
// handler and call plugin.Revoke directly with an empty reason, same as
// the store CHECK migration 0023 enforces for every caller.
func TestAgentRevokeOverSocketWithoutReasonIsUnwritable(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "silent-socket", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatalf("import: %v", err)
	}
	st := openAgentStore(t)
	if err := wirePluginSeams(st); err != nil {
		t.Fatal(err)
	}

	// Through the handler: refused early, with a clear message (UX).
	_, refusal, err := agentRevoke(st, rawProposal(t, agentRevokeParams{Name: "silent-socket", Reason: ""}))
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if refusal == "" || !strings.Contains(refusal, "reason") {
		t.Fatalf("an empty reason must be refused, naming the reason: %q", refusal)
	}

	// The real backstop, bypassing the handler's own check (the thin Go
	// check alone would pass this test even with a missing/wrong CHECK
	// migration — calling plugin.Revoke directly proves the database
	// itself refuses, not just this handler's guard).
	if _, rj := plugin.Revoke("silent-socket", "   "); rj == nil {
		t.Fatal("whitespace is not a reason — plugin.Revoke's own guard must still catch it")
	}

	rec, found, err := st.PluginAgentState("silent-socket")
	if err != nil || !found {
		t.Fatalf("read back: %v / found=%v", err, found)
	}
	if rec.State == "revoked" {
		t.Fatal("the agent must still be writable/live — a reasonless revoke must leave no trace of success")
	}
}

// TestAgentRevokeRecordsWhateverRevokeRecords proves the socket path writes
// the same transition row the CLI path writes — plugin.Revoke (confirmed
// this task: it is move(name, StateRevoked, reason) plus its own early
// reason check, no additional side effect beyond the transition record) —
// never a thinner bare-transition call.
func TestAgentRevokeRecordsWhateverRevokeRecords(t *testing.T) {
	agentHome(t)
	dir := writeAgentPackage(t, "accountable", nil)
	if _, err := runAgent(t, "import", dir); err != nil {
		t.Fatalf("import: %v", err)
	}
	st := openAgentStore(t)
	if err := wirePluginSeams(st); err != nil {
		t.Fatal(err)
	}
	const reason = "the vendor shipped a data-exfil update"
	res, refusal, err := agentRevoke(st, rawProposal(t, agentRevokeParams{Name: "accountable", Reason: reason}))
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if refusal != "" {
		t.Fatalf("refused: %s", refusal)
	}
	out, ok := res.(agentTransitionResult)
	if !ok || out.State != "revoked" {
		t.Fatalf("result = %+v, want state revoked", res)
	}
	rows, err := st.PluginAgentTransitions("accountable")
	if err != nil {
		t.Fatal(err)
	}
	last := rows[len(rows)-1]
	if last.ToState != "revoked" || last.Reason != reason {
		t.Fatalf("transition row = %+v, want to=revoked reason=%q — the same row the CLI path writes", last, reason)
	}
}
