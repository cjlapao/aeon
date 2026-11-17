package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// appenvPinnedEnv are the variables appenv.Apply() sets (setDefault-style, at
// the head of every dial) — the T8 Notes #8 fixture finding: an in-process
// dial pins the store paths PROCESS-WIDE, and a later fixture built from the
// environment then inherits a store another test chose. The both-doors suite
// solved this for its own socket arm by snapshotting and restoring; the
// proposal verbs hit the same mechanism the moment `agent_import` commits
// stopped being refused by name (S45 T10 — a refused commit never reached
// the import, so the leak stayed invisible). Same mechanism, same remedy,
// centralized in the helper every proposal verb test already calls: the
// dial's defaults live only as long as the call.
var appenvPinnedEnv = []string{
	"LOCALRPC_NAMESPACE", "DATABASE_TYPE", "DATABASE_SQLITE_STORAGE_PATH", "DATABASE_SQLITE_FILE_NAME",
}

func runProposal(t *testing.T, args ...string) (string, error) {
	t.Helper()
	restore := map[string]string{}
	for _, k := range appenvPinnedEnv {
		restore[k] = os.Getenv(k)
	}
	defer func() {
		for k, v := range restore {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}()
	var b strings.Builder
	err := cmdProposal(&b, args)
	return b.String(), err
}

func proposalHome(t *testing.T) *store.Store {
	t.Helper()
	// Pin BEFORE the handle opens: an earlier in-process daemon boot left
	// DATABASE_SQLITE_STORAGE_PATH behind (appenv.Apply, process-wide), it
	// outranks XDG in the resolver — a handle opened under the leak and a
	// later CLI open under the pin would be two different databases (the
	// S46 ghost-rows hunt; S45's pin, policy_golden_test.go:179).
	isolateStoreEnv(t)
	agentHome(t) // shared XDG_STATE_HOME setup — generic, not plugin-specific
	return openAgentStore(t)
}

func createTestProposal(t *testing.T, st *store.Store, kind string, payload any) store.Proposal {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.CreateProposal(kind, string(raw), store.Actor{Kind: "user", ID: "1000"}, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	return p
}

func TestCommitAppliesThroughExistingPath(t *testing.T) {
	st := proposalHome(t)
	p := createTestProposal(t, st, "policy_add", policy.Policy{
		ID: "commit-matches-cli", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Deny,
		Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"webfetch","state":"deny"}`),
	})
	out, err := runProposal(t, "commit", itoa(p.ID))
	if err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	if !strings.Contains(out, "committed") {
		t.Fatalf("output does not report commit: %q", out)
	}
	boundary, closeFn, err := policyBoundary()
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	rows, rej := boundary.List(context.Background(), time.Now().UTC())
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	var found *policy.Policy
	for i, r := range rows {
		if r.ID == "commit-matches-cli" {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatal("the corpus row was not written — commit must apply through boundary.Add, the same function aeon policy add uses")
	}
	if found.Action != policy.Deny || string(found.Spec) != `{"toolName":"webfetch","state":"deny"}` {
		t.Fatalf("row = %+v, not what aeon policy add would have produced for the same payload", found)
	}
}

func TestCommitRevalidatesNotJustTrusts(t *testing.T) {
	st := proposalHome(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"public":[],"agents":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(paths.MCPConfigEnv, path)
	// S46 T3: the rows are the truth — the empty config this test writes
	// must exist as the (empty) rows world too, or leftover rows from an
	// earlier test in this store read as "already configured" and MC12
	// refuses a commit that the file-era world made valid.
	seedMCPConfigRows(t, mcp.Config{})

	// Two proposals, both valid AT CREATE TIME (the server does not exist yet).
	first := createTestProposal(t, st, "mcp_server_add", mcpServerAddPayload{
		Tier: "public", Server: mcp.ServerConfig{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_repos", Rerun: "safe", Effect: "local"}}},
	})
	second := createTestProposal(t, st, "mcp_server_add", mcpServerAddPayload{
		Tier: "public", Server: mcp.ServerConfig{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "a-different-gh-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_repos", Rerun: "safe", Effect: "local"}}},
	})

	// Commit the SECOND one first — it lands, changing the world the first
	// one was validated against.
	if out, err := runProposal(t, "commit", itoa(second.ID)); err != nil {
		t.Fatalf("commit second: %v\n%s", err, out)
	}

	// Now the first, naming a DIFFERENT definition for the same server
	// name, must be refused at commit — re-validated, not blindly applied.
	out, err := runProposal(t, "commit", itoa(first.ID))
	if err == nil {
		t.Fatalf("the first proposal must now fail re-validation (MC12: github already configured): %s", out)
	}
	// S46 T6 validator move (recorded deviation, extends T6.md Notes 1):
	// the collision is now caught by the candidate gate's own named
	// refusal — Rows.With(AddServer)'s MC8 "already exists in" fires
	// before Assemble reaches Validate's MC12 "configured twice". Either
	// sentence names the real conflict; the test's claim (the FIRST one
	// must now fail re-validation) is unchanged.
	if !strings.Contains(err.Error(), "configured twice") && !strings.Contains(err.Error(), "already exists in") {
		t.Fatalf("refusal must name the real MC12/MC8 conflict, got: %v", err)
	}

	// And it must have left the row proposed, with applied_error set — not committed.
	row, found, gerr := st.GetProposal(first.ID)
	if gerr != nil || !found {
		t.Fatalf("read back: %v / found=%v", gerr, found)
	}
	if row.Status != "proposed" {
		t.Fatalf("status = %q, want proposed — a refused re-validation must never flip the row", row.Status)
	}
	if row.AppliedError == "" {
		t.Fatal("applied_error must be set on a re-validation refusal")
	}
}

func TestCommitIdempotentAfterCrashBetweenEffectAndFlip(t *testing.T) {
	st := proposalHome(t)
	p := createTestProposal(t, st, "policy_add", policy.Policy{
		ID: "crash-retry", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Deny,
		Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	})

	// Simulate the crash: apply the effect directly, bypassing the normal
	// commit flow entirely, and never flip the row.
	boundary, closeFn, err := policyBoundary()
	if err != nil {
		t.Fatal(err)
	}
	var seeded policy.Policy
	if jerr := json.Unmarshal([]byte(p.Payload), &seeded); jerr != nil {
		t.Fatal(jerr)
	}
	if rej := boundary.Add(context.Background(), seeded, time.Now().UTC()); rej != nil {
		t.Fatalf("seed the effect manually: %s", rej.Reason)
	}
	closeFn()

	// A plain re-apply here would hit the PRIMARY KEY and refuse as a
	// store failure — the real proof that idempotency detection, not luck,
	// is what makes this pass.
	out, err := runProposal(t, "commit", itoa(p.ID))
	if err != nil {
		t.Fatalf("the crash-retry commit must complete the flip without reapplying: %v\n%s", err, out)
	}
	row, found, gerr := st.GetProposal(p.ID)
	if gerr != nil || !found {
		t.Fatalf("read back: %v / found=%v", gerr, found)
	}
	if row.Status != "committed" {
		t.Fatalf("status = %q, want committed", row.Status)
	}

	// And the corpus holds exactly one row for this id — no duplicate.
	boundary2, closeFn2, err := policyBoundary()
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn2()
	rows, rej := boundary2.List(context.Background(), time.Now().UTC())
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	n := 0
	for _, r := range rows {
		if r.ID == "crash-retry" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("found %d rows for crash-retry, want exactly 1 — the effect must not have been reapplied", n)
	}
}

func TestCommitSetsAppliedErrorOnFailure(t *testing.T) {
	st := proposalHome(t)
	// S46 T6 (recorded deviation, extends T6.md Notes): the effect left the
	// file, so the file-era injection — a read-only directory around
	// mcp.json — has no effect to fail any more. The test's CLAIM is kept
	// intact ("an effect that fails mid-apply records applied_error and
	// leaves the row proposed for retry") and the fault is injected where
	// the effect now lives: a BEFORE INSERT trigger on the table the door
	// writes, aborted RAISE. Validation passes (the gate only reads); the
	// land is the thing that fails — exactly the window the test pins.
	seedMCPConfigRows(t, mcp.Config{})
	p := createTestProposal(t, st, "mcp_server_add", mcpServerAddPayload{
		Tier: "public", Server: mcp.ServerConfig{Name: "github", Transport: mcp.TransportConfig{Type: "stdio", Command: "gh-mcp"}, Tools: []mcp.ToolConfig{{Name: "list_repos", Rerun: "safe", Effect: "local"}}},
	})
	if err := st.GormDB().Exec(`CREATE TRIGGER s46_t6_inject_fail BEFORE INSERT ON mcp_server
		BEGIN SELECT RAISE(ABORT, 'injected: the land failed'); END`).Error; err != nil {
		t.Fatalf("arm the fault: %v", err)
	}
	t.Cleanup(func() { _ = st.GormDB().Exec(`DROP TRIGGER IF EXISTS s46_t6_inject_fail`).Error })

	out, err := runProposal(t, "commit", itoa(p.ID))
	if err == nil {
		t.Fatalf("a failed effect must refuse the commit: %s", out)
	}
	row, found, gerr := st.GetProposal(p.ID)
	if gerr != nil || !found {
		t.Fatalf("read back: %v / found=%v", gerr, found)
	}
	if row.Status != "proposed" {
		t.Fatalf("status = %q, want proposed — a failed effect must never flip to committed", row.Status)
	}
	if row.AppliedError == "" {
		t.Fatal("applied_error must be set when the effect fails")
	}
}

func TestDoubleCommitFails(t *testing.T) {
	st := proposalHome(t)
	p := createTestProposal(t, st, "policy_add", policy.Policy{
		ID: "double-commit", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Deny,
		Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	})
	if out, err := runProposal(t, "commit", itoa(p.ID)); err != nil {
		t.Fatalf("first commit: %v\n%s", err, out)
	}
	out, err := runProposal(t, "commit", itoa(p.ID))
	if err == nil {
		t.Fatalf("a second commit on an already-committed row must fail: %s", out)
	}
	if !strings.Contains(err.Error(), "already committed") && !strings.Contains(err.Error(), "not proposed") {
		t.Fatalf("the refusal must name the guard, got: %v", err)
	}
}

func TestRejectNeedsNoReason(t *testing.T) {
	st := proposalHome(t)
	p := createTestProposal(t, st, "policy_remove", policyRemovePayload{ID: "whatever"})
	out, err := runProposal(t, "reject", itoa(p.ID))
	if err != nil {
		t.Fatalf("reject with no --reason must succeed (TD5): %v\n%s", err, out)
	}
	row, found, gerr := st.GetProposal(p.ID)
	if gerr != nil || !found {
		t.Fatalf("read back: %v / found=%v", gerr, found)
	}
	if row.Status != "rejected" {
		t.Fatalf("status = %q, want rejected", row.Status)
	}
}

// TestCliListMatchesSocketList proves the two doors render the same
// command string for the same row — the real drift risk TD6 names,
// checked by actually dispatching the socket method against the real
// daemon wiring rather than hand-rolling a second renderer.
func TestCliListMatchesSocketList(t *testing.T) {
	st := proposalHome(t)
	p1 := createTestProposal(t, st, "policy_add", policy.Policy{ID: "cli-vs-socket-1", Type: policy.ToolAccess, Target: policy.Target{Kind: policy.AllAgents}, Action: policy.Deny, Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`)})
	p2 := createTestProposal(t, st, "policy_remove", policyRemovePayload{ID: "cli-vs-socket-2"})

	cliOut, err := runProposal(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	wantCommands := []string{proposalCommand(p1.ID), proposalCommand(p2.ID)}
	for _, c := range wantCommands {
		if !strings.Contains(cliOut, c) {
			t.Fatalf("CLI list does not show %q:\n%s", c, cliOut)
		}
	}

	d := daemon.New(daemon.Options{Store: st})
	meth, ok := d.Table.Lookup("proposal.list")
	if !ok {
		t.Fatal("proposal.list is not registered")
	}
	res, err := meth.Handle(context.Background(), nil, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Proposals []struct {
			ID      int64  `json:"id"`
			Command string `json:"command"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	gotCommands := map[int64]string{}
	for _, row := range parsed.Proposals {
		gotCommands[row.ID] = row.Command
	}
	for _, p := range []store.Proposal{p1, p2} {
		want := proposalCommand(p.ID)
		got, ok := gotCommands[p.ID]
		if !ok {
			t.Fatalf("socket list is missing proposal %d", p.ID)
		}
		if got != want {
			t.Fatalf("socket command = %q, CLI command = %q — the two doors drifted (TD6)", got, want)
		}
	}
}
