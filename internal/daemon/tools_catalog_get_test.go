// What `tools.get` answers (S47 T5): the schema the model is handed comes from CODE
// and survives a tampered row (TC2), the per-agent table arrives with it, the
// policy refs are the one describer's sentences, and an unknown name is refused
// by naming the nearest things that exist.

package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/toolcatalog"
	"github.com/cjlapao/aeon/internal/tools"
)

// --- tools.get -----------------------------------------------------------------------

// TestSchemaServedIsTheCodeSchema is TC2, the ruling the table exists to make
// provable: the catalog serves the schema the EXECUTOR parses a call against, and
// the row's copy exists only so a disagreement can be detected. The row is then
// tampered with — description, schema text and hash all — and the served schema is
// UNCHANGED while the drift report gains the field names. That pair is the point:
// the catalog keeps telling the truth about the tool, and the lie in the row is
// reported rather than served.
func TestSchemaServedIsTheCodeSchema(t *testing.T) {
	env := newToolsCatalogEnv(t, false)
	reg := tools.Registry()
	bash := reg["bash"]
	wantSchema := string(tools.CanonicalSchema(bash.Spec))
	wantHash := tools.SchemaHash(bash.Spec)

	e, ok := entryByName(mustList(t, env, protocol.ToolsListFilter{Q: strptr("bash")}), "bash")
	if !ok {
		t.Fatal("bash is missing")
	}
	if string(e.Schema) != wantSchema {
		t.Errorf("served schema = %s, want the code's %s", e.Schema, wantSchema)
	}
	if e.SchemaHash != wantHash {
		t.Errorf("served hash = %q, want the code's %q", e.SchemaHash, wantHash)
	}
	if e.RerunClass != string(bash.Class) {
		t.Errorf("rerun class = %q, want the code's %q", e.RerunClass, bash.Class)
	}
	if !e.RowCurrent {
		t.Error("a freshly applied set reports drift it does not have")
	}

	// Tamper with the ROW. The only writer is the applier, so this goes through the
	// applier's own transaction — a test cannot smuggle a row in any other way,
	// which is itself the property T2 built.
	row, err := env.st.ToolGet(context.Background(), "bash")
	if err != nil {
		t.Fatalf("read the row: %v", err)
	}
	row.Description = "the row says something the code does not"
	row.Schema = `{"type":"object","properties":{"lie":{"type":"string"}}}`
	row.SchemaHash = strings.Repeat("a", 64)
	tx := beginForTest(t, env.st)
	if err := env.st.ToolUpsertTx(tx, row); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	e2, ok := entryByName(mustList(t, env, protocol.ToolsListFilter{Q: strptr("bash")}), "bash")
	if !ok {
		t.Fatal("bash disappeared after a row edit")
	}
	if string(e2.Schema) != wantSchema || e2.SchemaHash != wantHash {
		t.Errorf("the catalog started serving the ROW once it disagreed with the code: %s / %q", e2.Schema, e2.SchemaHash)
	}
	if e2.RowCurrent {
		t.Error("rowCurrent is true while the row and the code disagree (TC11)")
	}
	st := statusOrFatal(t, env)
	d, ok := driftFor(st, "bash")
	if !ok || d.State != protocol.ToolsDriftDiffers {
		t.Fatalf("the tampered row is not reported as `differs`: %+v", st.Drift)
	}
	if !hasString(d.Fields, "description") || !hasString(d.Fields, "schema_hash") {
		t.Errorf("the drift report does not name the disagreeing fields: %v", d.Fields)
	}
	// And the tampering SURVIVES — nothing here repaired it (TC11).
	after, err := env.st.ToolGet(context.Background(), "bash")
	if err != nil {
		t.Fatal(err)
	}
	if after.Description != "the row says something the code does not" {
		t.Error("the read repaired the row — TC11 says report, never repair")
	}
}

// TestToolsGetCarriesPerAgentTableAndPolicyRefs is TC5's "who can call each and why not"
// from the tool's side: the column lists EVERY agent that exists — granted or not —
// because a missing column would let absence mean "denied" and send someone to
// delete a rule that does not exist. The rules ride with `describe` =
// `policy.Describe(p)` verbatim, the sentence S45's one describer produces, so the
// app assembles nothing.
func TestToolsGetCarriesPerAgentTableAndPolicyRefs(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	res, err := env.svc.ToolsGet(context.Background(), "bash")
	if err != nil {
		t.Fatalf("tools.get: %v", err)
	}
	if res.Tool.Name != "bash" || res.Tool.Family == "" || res.Tool.RerunClass == "" {
		t.Fatalf("the tool half is thin: %+v", res.Tool)
	}
	if !hasString(res.Tool.Governed, string(policy.CommandAccess)) {
		t.Errorf("bash is not reported as governed by command_access: %v", res.Tool.Governed)
	}
	got := map[string]toolcatalog.Entry{}
	for _, e := range res.Agents {
		got[e.Agent] = e
	}
	for _, slug := range []string{"atlas", "sentry", "cursitor"} {
		if _, ok := got[slug]; !ok {
			t.Errorf("agent %s has no column — the roster must be complete, granted or not", slug)
		}
	}
	if e := got["atlas"]; e.State != toolcatalog.StateGrantedDenied || e.Reason == "" {
		t.Errorf("atlas holds bash and a deny refuses it; got %q / %q", e.State, e.Reason)
	}
	if e := got["sentry"]; e.State != toolcatalog.StateNotGranted || e.Reason != "" {
		t.Errorf("sentry does not hold bash: the answer must be not_granted with NO reason (a fabricated verdict is worse than none): %q / %q", e.State, e.Reason)
	}
	// The first-party default set rides here too (system-origin ask rows for the shipped agents); the rule this test
	// authored is the one user-origin row.
	var mine []int
	for i, row := range res.Policies {
		if row.Origin != "system" {
			mine = append(mine, i)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("user-origin policies at %v of %+v, want the one bash row", mine, res.Policies)
	}
	p := res.Policies[mine[0]]
	if p.Action != "deny" || p.Source != "operator" || p.Target != "agent:atlas" || p.Expired {
		t.Errorf("the policy row is mis-rendered: %+v", p)
	}
	want := policy.Describe(policy.Policy{ID: p.ID, Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: "atlas"}, Action: policy.Deny,
		Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`), Source: policy.Operator})
	if p.Describe != want {
		t.Errorf("describe = %q, want policy.Describe verbatim %q — a second formatter is a second truth", p.Describe, want)
	}
	if p.Origin == "" {
		t.Error("the row carries no origin — the panel cannot say where the rule came from")
	}
	if res.MCP != nil {
		t.Error("a system tool carries an MCP link")
	}

	// The alias resolves to the CANONICAL tool with its aliases riding — the same
	// resolution the grant chain and the policy rows share (SEC-051/SEC-059).
	alias, err := env.svc.ToolsGet(context.Background(), "write_file")
	if err != nil {
		t.Fatalf("tools.get write_file: %v", err)
	}
	if alias.Tool.Name != "write" || !hasString(alias.Tool.Aliases, "write_file") {
		t.Errorf("write_file answered %q (aliases %v)", alias.Tool.Name, alias.Tool.Aliases)
	}

	mcpRes, err := env.svc.ToolsGet(context.Background(), "acme.search")
	if err != nil {
		t.Fatalf("tools.get acme.search: %v", err)
	}
	if mcpRes.MCP == nil || mcpRes.MCP.Server != "acme" || mcpRes.MCP.Tier != "private" ||
		mcpRes.MCP.Link.Kind != "mcp_server" || mcpRes.MCP.Link.ID != "acme" {
		t.Errorf("the MCP link is wrong: %+v", mcpRes.MCP)
	}
}

// TestToolsGetUnknownNamesNearest is the typed refusal from the only
// layer that can produce it: the adapter has read the registry and the rows, so it
// names the near ones, and the door ships the sentence untouched.
func TestToolsGetUnknownNamesNearest(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	_, err := env.svc.ToolsGet(context.Background(), "reed")
	e, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("err = %T (%v), want a typed *protocol.Error", err, err)
	}
	if e.Code != protocol.ErrToolUnknown.Code {
		t.Fatalf("code = %d, want %d", e.Code, protocol.ErrToolUnknown.Code)
	}
	if !strings.Contains(e.Data.What, `"read"`) {
		t.Errorf("the refusal does not name the nearest name: %s", e.Data.What)
	}
	if _, err := env.svc.ToolsGet(context.Background(), "  "); err == nil {
		t.Error("the empty name was answered instead of refused")
	}
	// An MCP name is matched exactly: no canonicalisation may rewrite
	// `acme.search` into `search`, and an unqualified `search` must never answer
	// for a qualified one.
	if _, err := env.svc.ToolsGet(context.Background(), "acme.nope"); err == nil {
		t.Error("a qualified name that does not exist was answered")
	}
}
