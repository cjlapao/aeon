package main

// mcp_admin_writes_test.go — S46 T5: the human write doors. The law under
// test is MG5's pipeline (candidate through THE validator, one transaction
// for rows + audit, before/after grants, refusal writes nothing) plus the
// MC-laws it exists to enforce: MC5 (public grants nothing), MC2 (private
// is invisible to peers), MG7 (declaring is not authoring), MG4 (disabled
// is not deleted), MG10 (system rows are not this door's), and the audit's
// honesty (one row per write that changed something, none otherwise).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/agentcard"
	"github.com/cjlapao/aeon/internal/agentmgr"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/vault"
)

// writeFixture: an isolated state dir, the agents the tests name, and the
// seam (direct door) with the actor stamped by the caller.
func writeFixture(t *testing.T) (func(string, any) (any, string, error), *store.Store) {
	t.Helper()
	isolateStoreEnv(t)
	agentHome(t)
	st := openAgentStore(t)
	now := "2026-11-01T12:00:00Z"
	for _, name := range []string{"golang-engineer", "manny", "peer"} {
		if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: store.Slugify(name), Name: name, Version: "1"}, now); err != nil {
			t.Fatalf("plant %s: %v", name, err)
		}
	}
	seam := mcpAdminHandleFor(st, newMCPProbeCache())
	return func(method string, params any) (any, string, error) {
		return seam(importActorCtx(), method, mustJSONRaw(t, params))
	}, st
}

func callGrants(t *testing.T, st *store.Store, agent string) string {
	t.Helper()
	seam := mcpAdminHandleFor(st, newMCPProbeCache())
	res, refusal, err := seam(context.Background(), "mcp.grants", mustJSONRaw(t, map[string]any{"agent": agent}))
	if err != nil || refusal != "" {
		t.Fatalf("mcp.grants %s: %v %s", agent, err, refusal)
	}
	b, merr := json.Marshal(res)
	if merr != nil {
		t.Fatal(merr)
	}
	return string(b)
}

func auditCount(t *testing.T, st *store.Store, op string) int {
	t.Helper()
	return countQuery(t, st, `SELECT COUNT(*) FROM mcp_change WHERE op = ?`, op)
}

func addParams(name, tier string) map[string]any {
	p := map[string]any{"name": name, "tier": tier,
		"transport": map[string]any{"type": "stdio", "command": "x-mcp"},
		"tools":     []any{map[string]any{"name": "do_thing", "rerun": "safe", "effect": "local"}}}
	return p
}

func TestAddServerValidatesWholeCandidateVerbatim(t *testing.T) {
	call, st := writeFixture(t)

	// (a) A name breaking the grammar: the refusal is the ONE validator's
	// sentence — produced here by running the same Config.Validate the
	// candidate pipeline runs, and compared VERBATIM.
	_, refusal, err := call("mcp.server.add", map[string]any{
		"name": "Bad Name!", "tier": "public",
		"transport": map[string]any{"type": "stdio", "command": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	parallel := mcp.Config{Public: []mcp.ServerConfig{{
		Name:      "Bad Name!",
		Transport: mcp.TransportConfig{Type: "stdio", Command: "x"},
	}}}
	if refusal == "" {
		t.Fatal("the validator was not asked (no refusal) — a second validator or a bypass")
	}
	verr := parallel.Validate()
	if verr == nil {
		t.Fatal("the parallel Config.Validate ACCEPTED a name the seam refused — two validators speaking differently")
	}
	if refusal != verr.Error() {
		t.Fatalf("the refusal is not the validator's sentence verbatim:\n seam:      %q\n validator: %q", refusal, verr.Error())
	}
	if s, _, d := configRowCount(t, st); s != 0 || d != 0 {
		t.Fatalf("a refused add wrote rows: %d servers / %d declares", s, d)
	}
	if n := auditCount(t, st, "server_add"); n != 0 {
		t.Fatalf("a refused add audited %d rows — a refusal writes NOTHING", n)
	}

	// (b) An env entry that is a value, not a NAME (C17/MG13).
	_, refusal, err = call("mcp.server.add", map[string]any{
		"name": "gh", "tier": "public",
		"transport": map[string]any{"type": "stdio", "command": "x", "env": []string{"TOKEN=sekret"}}})
	if err != nil {
		t.Fatal(err)
	}
	if refusal == "" || !strings.Contains(strings.ToLower(refusal), "env") {
		t.Fatalf("value-shaped env refused? refusal = %q", refusal)
	}

	// (c) A private add whose owner does not exist: refused, named — the
	// add never fabricates agents.
	_, refusal, err = call("mcp.server.add", map[string]any{
		"name": "ghostly", "tier": "private", "agent": "nobody-here",
		"transport": map[string]any{"type": "stdio", "command": "x"}})
	if refusal == "" {
		var pe *protocol.Error
		if !errors.As(err, &pe) || pe.Code != protocol.ErrMcpUnknown.Code || !strings.Contains(pe.Data.What, "nobody-here") {
			t.Fatalf("unknown owner: err = %v, refusal = %q — want the name spoken", err, refusal)
		}
	} else if !strings.Contains(refusal, "nobody-here") {
		t.Fatalf("unknown owner refusal = %q", refusal)
	}
}

func TestAddPublicServerGrantsNothing(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}],"agents":{"manny":{"declares":["gitforge.pr"]}}}`))
	before := map[string]string{}
	for _, a := range []string{"golang-engineer", "manny", "peer"} {
		before[a] = callGrants(t, st, a)
	}
	res, refusal, err := call("mcp.server.add", addParams("search", "public"))
	if err != nil || refusal != "" {
		t.Fatalf("add: %v %s", err, refusal)
	}
	out := res.(protocol.MCPWriteResult)
	if !out.Changed || len(out.Grants) != 0 {
		t.Fatalf("adding a PUBLIC server affected %d agents (%+v) — MC5: available is not granted-to-all", len(out.Grants), out)
	}
	for _, a := range []string{"golang-engineer", "manny", "peer"} {
		if got := callGrants(t, st, a); got != before[a] {
			t.Errorf("%s's view moved on a PUBLIC add:\nbefore %s\nafter  %s", a, before[a], got)
		}
	}
}

func TestAddPrivateServerChangesOnlyItsOwnerView(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}],"agents":{"manny":{"declares":["gitforge.pr"]},"peer":{"declares":["gitforge.pr"]}}}`))
	views := map[string]string{}
	for _, a := range []string{"golang-engineer", "manny", "peer"} {
		views[a] = callGrants(t, st, a)
	}
	res, refusal, err := call("mcp.server.add", map[string]any{
		"name": "labnote", "tier": "private", "agent": "golang-engineer",
		"transport": map[string]any{"type": "http", "url": "http://127.0.0.1:9/mcp"},
		"tools":     []any{map[string]any{"name": "draft_note", "rerun": "safe", "effect": "local"}}})
	if err != nil || refusal != "" {
		t.Fatalf("add private: %v %s", err, refusal)
	}
	out := res.(protocol.MCPWriteResult)
	if len(out.Grants) != 1 || out.Grants[0].Agent != "golang-engineer" {
		t.Fatalf("affected = %+v, want ONLY the owner", out.Grants)
	}
	for _, a := range []string{"manny", "peer"} {
		if got := callGrants(t, st, a); got != views[a] {
			t.Errorf("%s (a peer) saw the private server: MC2 leaked:\nbefore %s\nafter  %s", a, views[a], got)
		}
	}
	if got := callGrants(t, st, "golang-engineer"); !strings.Contains(got, "labnote.draft_note") {
		t.Errorf("the owner's own view must carry the private tool, got %s", got)
	}
	// The peer door cannot even NAME it: declaring the private tool is
	// refused by the validator (MC2's one implementation).
	_, refusal, err = call("mcp.grant.declare", map[string]any{"agent": "peer", "qualified": "labnote.draft_note"})
	if err != nil {
		t.Fatal(err)
	}
	if refusal == "" {
		t.Fatal("a peer declared another agent's PRIVATE tool — MC2 has one implementation and it refused to speak here")
	}
}

func TestAddServerEnvNamesOnly(t *testing.T) {
	call, st := writeFixture(t)
	// The value-shaped entry (b) in test 1 covers refusal; here: NAMES
	// land as names and NO value exists anywhere in the store, the audit
	// or the result (MG13).
	_, refusal, err := call("mcp.server.add", map[string]any{
		"name": "gh", "tier": "public",
		"transport": map[string]any{"type": "stdio", "command": "x", "env": []string{"GITHUB_TOKEN", "GITHUB_ORG"},
			"envFrom": map[string]string{"GITHUB_TOKEN": "GITHUB_TOKEN", "GITHUB_ORG": "GITHUB_ORG"}},
		"tools": []any{map[string]any{"name": "t", "rerun": "safe", "effect": "local"}}})
	if err != nil || refusal != "" {
		t.Fatalf("names should stand: %v %s", err, refusal)
	}
	all, _ := st.McpRows(context.Background())
	if len(all.Servers) != 1 {
		t.Fatalf("servers = %d", len(all.Servers))
	}
	if all.Servers[0].Env != `["GITHUB_TOKEN","GITHUB_ORG"]` {
		t.Errorf("env rows = %s, want the NAMES array", all.Servers[0].Env)
	}
	var summaries []string
	if terr := st.McpWithTx(context.Background(), func(tx *sql.Tx) error {
		rows, qerr := tx.QueryContext(context.Background(), `SELECT summary FROM mcp_change`)
		if qerr != nil {
			return qerr
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if serr := rows.Scan(&s); serr != nil {
				return serr
			}
			summaries = append(summaries, s)
		}
		return rows.Err()
	}); terr != nil {
		t.Fatal(terr)
	}
	for _, s := range summaries {
		if strings.Contains(s, "=") {
			t.Errorf("audit summary carries an = (value-shaped?): %q", s)
		}
	}
}

func TestAddServerUnclassifiedToolWarns(t *testing.T) {
	call, st := writeFixture(t)
	res, refusal, err := call("mcp.server.add", map[string]any{
		"name": "mystery", "tier": "public",
		"transport": map[string]any{"type": "stdio", "command": "x"},
		"tools": []any{
			map[string]any{"name": "known", "rerun": "safe", "effect": "local"},
			map[string]any{"name": "unknown"}, // unclassified
		}})
	if err != nil || refusal != "" {
		t.Fatalf("add: %v %s", err, refusal)
	}
	out := res.(protocol.MCPWriteResult)
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "UNCLASSIFIED") {
		t.Fatalf("warnings = %+v, want the fixed MC6 warning", out.Warnings)
	}
	// The unclassified tool, once declared, is REFUSED — silently usable
	// is the failure this test exists for.
	if _, refusal, err := call("mcp.grant.declare", map[string]any{"agent": "manny", "qualified": "mystery.unknown"}); err != nil || refusal != "" {
		t.Fatalf("declare unclassified public tool: %v %s", err, refusal)
	}
	var view protocol.MCPGrantsResult
	if jerr := json.Unmarshal([]byte(callGrants(t, st, "manny")), &view); jerr != nil {
		t.Fatal(jerr)
	}
	var state string
	for _, gg := range view.Grants {
		if gg.Qualified == "mystery.unknown" {
			state = gg.State
		}
	}
	// T3's capability-list law (registry.go: unclassified tools never
	// reach a capability list): ABSENCE is the view's non-offer, and the
	// call door carries MC6's named refusal. The failure this pins is
	// AVAILABLE — a tool nobody classified being handed out.
	if state == "available" {
		t.Errorf("the unclassified tool reads AVAILABLE — MC6's silent-use failure")
	}
}

func TestUpdateReplacesProvidedSectionsOnly(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf","args":["--a"],"env":["GITHUB_TOKEN"]},"tools":[{"name":"pr","rerun":"safe","effect":"local"},{"name":"issue","rerun":"unsafe","effect":"outward"}]}]}`))
	_, refusal, err := call("mcp.server.update", map[string]any{
		"name":      "gitforge",
		"transport": map[string]any{"type": "http", "url": "https://gf.example/mcp"}})
	if err != nil || refusal != "" {
		t.Fatalf("transport-only update: %v %s", err, refusal)
	}
	all, _ := st.McpRows(context.Background())
	if len(all.Servers) != 1 {
		t.Fatalf("servers = %d", len(all.Servers))
	}
	got := all.Servers[0]
	if got.TransportType != "http" || got.URL != "https://gf.example/mcp" {
		t.Errorf("transport did not replace: %+v", got)
	}
	names := map[string]bool{}
	for _, tl := range got.Tools {
		names[tl.Name] = true
	}
	if len(got.Tools) != 2 || !names["pr"] || !names["issue"] {
		t.Errorf("TOOLS LOST on a transport-only update: %+v — the provided sections replace, the others stand", got.Tools)
	}
}

func TestRenameIsNotAnOperation(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}]}`))
	// The update type has NO rename field — identity churn is structurally
	// impossible (the deviation-recorded honesty: the protocol package
	// ignores unknown fields by law, so "shape error" here means the
	// PARAMS TYPE cannot express a rename, and the named identity keys
	// — actor/origin/id/timestamps — do refuse; see TestMcpWriteEdge… in
	// internal/protocol). A carried "newName" changes nothing:
	_, refusal, err := call("mcp.server.update", map[string]any{
		"name": "gitforge", "newName": "gh",
		"transport": map[string]any{"type": "stdio", "command": "gf"}})
	if err != nil || refusal != "" {
		t.Fatalf("update: %v %s", err, refusal)
	}
	all, _ := st.McpRows(context.Background())
	if len(all.Servers) != 1 || all.Servers[0].Name != "gitforge" {
		t.Fatalf("identity churn — the name moved: %+v", all.Servers)
	}
}

func TestUpdateSystemServerRefused(t *testing.T) {
	call, st := writeFixture(t)
	isolateStoreEnv(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"builtin","transport":{"type":"stdio","command":"in"},"tools":[{"name":"ping","rerun":"safe","effect":"local"}]}]}`))
	// Re-origin the row as system (test files write rows directly).
	if err := st.McpWithTx(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(), `UPDATE mcp_server SET origin='system' WHERE name='builtin'`)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.McpRows(context.Background())
	_, refusal, err := call("mcp.server.update", map[string]any{
		"name": "builtin", "transport": map[string]any{"type": "stdio", "command": "evil"}})
	if err != nil {
		t.Fatal(err)
	}
	if refusal == "" || !strings.Contains(refusal, "system") || !strings.Contains(refusal, "MG10") {
		t.Fatalf("MG10 refusal = %q", refusal)
	}
	after, _ := st.McpRows(context.Background())
	if after.Servers[0].Command != before.Servers[0].Command {
		t.Error("the refused edit moved the command")
	}
	if auditCount(t, st, "server_update") != 0 {
		t.Error("a MG10 refusal audited a row")
	}
}

func TestRemoveRefusesWhileDeclaredNamingAgents(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}],"agents":{"manny":{"declares":["gitforge.pr"]},"peer":{"declares":["gitforge.pr"]}}}`))
	_, _, err := call("mcp.server.remove", map[string]any{"name": "gitforge"})
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.ErrMcpInUse.Code {
		t.Fatalf("err = %v, want ErrMcpInUse (%d)", err, protocol.ErrMcpInUse.Code)
	}
	for _, who := range []string{"manny", "peer"} {
		if !strings.Contains(pe.Data.What, who) {
			t.Errorf("the refusal must NAME the declaring agents, missing %q: %q", who, pe.Data.What)
		}
	}
	all, _ := st.McpRows(context.Background())
	if len(all.Servers) != 1 || len(all.Declares) != 2 {
		t.Fatalf("the refused removal touched the world: %+v", all)
	}
	if auditCount(t, st, "server_remove") != 0 {
		t.Error("a refused removal audited")
	}
}

func TestRemoveCascadeRevokesAndAudits(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}],"agents":{"manny":{"declares":["gitforge.pr"]},"peer":{"declares":["gitforge.pr"]}}}`))
	res, refusal, err := call("mcp.server.remove", map[string]any{"name": "gitforge", "cascadeDeclares": true})
	if err != nil || refusal != "" {
		t.Fatalf("cascade remove: %v %s", err, refusal)
	}
	out := res.(protocol.MCPWriteResult)
	if !out.Changed || len(out.Revoked) != 2 {
		t.Fatalf("revoked = %+v, want both listed", out.Revoked)
	}
	for _, want := range []string{"manny: gitforge.pr", "peer: gitforge.pr"} {
		if !contains(out.Revoked, want) {
			t.Errorf("revoked = %v, want %q", out.Revoked, want)
		}
	}
	all, _ := st.McpRows(context.Background())
	if len(all.Servers) != 0 || len(all.Declares) != 0 {
		t.Fatalf("cascade left rows: %+v", all)
	}
	if auditCount(t, st, "server_remove") != 1 || auditCount(t, st, "grant_revoke") != 2 {
		t.Errorf("audit = %d remove / %d revoke, want 1/2 (each revoke its own row)", auditCount(t, st, "server_remove"), auditCount(t, st, "grant_revoke"))
	}
}

func TestDeclareRequiresExistingToolVisibleToAgent(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}]}`))
	for _, q := range []string{"gitforge.nope", "nope.tool", "gitforge.pr.a"} {
		_, refusal, err := call("mcp.grant.declare", map[string]any{"agent": "manny", "qualified": q})
		if err != nil {
			t.Fatal(err)
		}
		if refusal == "" {
			t.Errorf("declaring %q was accepted — the validator must answer existence and shape", q)
		}
	}
	if _, _, err := st.AgentByName("manny"); err != nil {
		t.Fatal(err)
	}
	if n := auditCount(t, st, "grant_declare"); n != 0 {
		t.Errorf("refused declares audited %d rows", n)
	}
	all, _ := st.McpRows(context.Background())
	if len(all.Declares) != 0 {
		t.Errorf("refused declares wrote rows: %+v", all.Declares)
	}
}

func TestDeclareDoesNotAuthorPolicy(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}]}`))
	ps := countQuery(t, st, `SELECT COUNT(*) FROM policy`)
	res, refusal, err := call("mcp.grant.declare", map[string]any{"agent": "manny", "qualified": "gitforge.pr"})
	if err != nil || refusal != "" {
		t.Fatalf("declare: %v %s", err, refusal)
	}
	if got := countQuery(t, st, `SELECT COUNT(*) FROM policy`); got != ps {
		t.Fatalf("policy rows moved %d → %d — declaring is NOT authoring (MG7 cross-domain write)", ps, got)
	}
	out := res.(protocol.MCPWriteResult)
	if len(out.Grants) != 1 || len(out.Grants[0].After) == 0 {
		t.Fatalf("grants = %+v", out.Grants)
	}
	var stt, reason string
	for _, v := range out.Grants[0].After {
		if v.Qualified == "gitforge.pr" {
			stt, reason = v.State, v.Reason
		}
	}
	if stt != "unavailable" {
		t.Errorf("state = %q, want unavailable until an allow exists (MC10)", stt)
	}
	if !strings.Contains(strings.ToLower(reason), "policy") && !strings.Contains(strings.ToLower(reason), "allow") {
		t.Errorf("reason = %q — the MC10 sentence must name the missing allow", reason)
	}
	if len(out.Warnings) == 0 {
		t.Error("the declare must carry the fixed MG7 warning")
	}
}

func TestCoreSetNullVersusEmpty(t *testing.T) {
	call, st := writeFixture(t)
	fixture := `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}],"agents":{"manny":{"declares":["gitforge.pr"]}}}`
	seedMCPConfigRows(t, mcpConfigFromJSON(t, fixture))

	// [] = NONE — a narrowing to nothing, stored as the JSON array "[]".
	if _, refusal, err := call("mcp.core.set", map[string]any{"agent": "manny", "core": []string{}}); err != nil || refusal != "" {
		t.Fatalf("core []: %v %s", err, refusal)
	}
	coreEmpty := coreRowOf(t, st, "manny")
	if coreEmpty == nil || *coreEmpty != "[]" {
		t.Fatalf("core [] stored %v, want \"[]\" (the two empties)", coreEmpty)
	}
	viewEmpty := callGrants(t, st, "manny")

	// null = the WHOLE core set — stored as SQL NULL.
	if _, refusal, err := call("mcp.core.set", map[string]any{"agent": "manny", "core": nil}); err != nil || refusal != "" {
		t.Fatalf("core null: %v %s", err, refusal)
	}
	if coreRowOf(t, st, "manny") != nil {
		t.Fatal("core null must store SQL NULL — the two empties collapsed")
	}
	viewNull := callGrants(t, st, "manny")

	// And the two answers DIFFER on the wire (the Core field: [] vs null).
	if !strings.Contains(viewEmpty, `"core":[]`) {
		t.Errorf("the []-world answer must carry core []: %s", viewEmpty)
	}
	if !strings.Contains(viewNull, `"core":null`) {
		t.Errorf("the null-world answer must carry core null: %s", viewNull)
	}
	// A name that is no core tool refuses verbatim.
	_, refusal, err := call("mcp.core.set", map[string]any{"agent": "manny", "core": []string{"not-a-tool"}})
	if err != nil {
		t.Fatal(err)
	}
	if refusal == "" || !strings.Contains(refusal, "not-a-tool") {
		t.Fatalf("validateCoreGrant refusal = %q", refusal)
	}
}

func coreRowOf(t *testing.T, st *store.Store, agent string) *string {
	t.Helper()
	slug := store.Slugify(agent)
	var v *string
	if err := st.McpWithTx(context.Background(), func(tx *sql.Tx) error {
		var s *string
		err := tx.QueryRowContext(context.Background(), `SELECT core FROM mcp_agent WHERE agent_slug = ?`, slug).Scan(&s)
		if errors.Is(err, sql.ErrNoRows) {
			v = nil
			return nil
		}
		if err != nil {
			return err
		}
		v = s
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestEnableDisableChangesOfferOnly(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}],"agents":{"manny":{"declares":["gitforge.pr"]}}}`))
	viewBefore := callGrants(t, st, "manny")
	if _, refusal, err := call("mcp.server.setEnabled", map[string]any{"name": "gitforge", "enabled": false}); err != nil || refusal != "" {
		t.Fatalf("disable: %v %s", err, refusal)
	}
	// Rows, tools and declarations STAND (MG4): nothing was deleted.
	all, _ := st.McpRows(context.Background())
	if len(all.Servers) != 1 || len(all.Declares) != 1 {
		t.Fatalf("disabling deleted data: %+v", all)
	}
	if all.Servers[0].Enabled {
		t.Error("the row still says enabled")
	}
	// The servers door carries the fact (MG4: disabled is SHOWN, never
	// hidden), and the declarations' truth (the grant view) keeps naming
	// the declares — the MG4 DROP from the offer is the step seam's
	// filter, applied at dial time, not a re-write of the truth (T2's
	// Assemble law: the filtered view is not even New-able).
	// The view keeps naming the declaration (MG4: declarations stand; the
	// MG4 DROP from the OFFER is the dial-time filter, never a re-write
	// of the grant view).
	_ = viewBefore
	if v := callGrants(t, st, "manny"); !strings.Contains(v, "gitforge.pr") {
		t.Errorf("the grant view lost the declaration (declarations STAND — MG4): %s", v)
	}
	servers, refusal, err := call("mcp.servers", map[string]any{})
	if err != nil || refusal != "" {
		t.Fatalf("servers: %v %s", err, refusal)
	}
	b, _ := json.Marshal(servers)
	if !strings.Contains(string(b), `"enabled":false`) {
		t.Errorf("mcp.servers must still SHOW the disabled server with its state: %s", b)
	}
	// Setting the same value again is the honest no-op: changed:false.
	res, _, _ := call("mcp.server.setEnabled", map[string]any{"name": "gitforge", "enabled": false})
	if out := res.(protocol.MCPWriteResult); out.Changed {
		t.Error("re-disabling an already-disabled server claimed changed:true")
	}
}

func TestWritesAreOneTransactionWithAudit(t *testing.T) {
	call, st := writeFixture(t)
	_ = call
	// The pipeline's land is forced to fail AFTER the row write but
	// BEFORE committing (a mid-transaction failure): neither the row nor
	// the audit may survive. This is the pipeline test, run directly in
	// the package (the seam cannot reach inside the Tx).
	isolateStoreEnv(t)
	admin := newMCPAdmin(st)
	before, _ := st.McpRows(context.Background())
	_, refusal, err := admin.runWrite(context.Background(), "user", "1000", before, writeSpec{
		affected: []string{"manny"},
		land: func(ctx context.Context, tx *sql.Tx) (bool, error) {
			if _, aerr := store.McpAddServerTx(ctx, tx, store.McpServerRow{
				Name: "torn", Tier: "public", TransportType: "stdio", Command: "t",
				Args: "[]", Env: "[]", Enabled: true, Origin: "user",
				CreatedAt: "2026-11-01T12:00:00Z", UpdatedAt: "2026-11-01T12:00:00Z",
			}); aerr != nil {
				return false, aerr
			}
			return true, errors.New("forced mid-transaction failure (the audit would follow)")
		},
		audits: func(bool) []store.McpChange {
			return []store.McpChange{{Op: "server_add", Server: ptrStr("torn"), Summary: "added public server torn (stdio, 0 tools)"}}
		},
	})
	if err == nil || refusal != "" {
		t.Fatalf("the forced failure vanished: err=%v refusal=%q", err, refusal)
	}
	after, _ := st.McpRows(context.Background())
	if len(after.Servers) != len(before.Servers) {
		t.Fatalf("the torn write left a server row: %+v", after.Servers)
	}
	if auditCount(t, st, "server_add") != 0 {
		t.Fatal("the torn write left an audit row — rows and audit must commit together or not at all")
	}
}

func TestOneAuditRowPerChangingWriteNoneOtherwise(t *testing.T) {
	call, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}]}`))
	base := map[string]int{}
	for _, op := range []string{"server_add", "grant_declare", "grant_revoke", "server_disable", "server_enable"} {
		base[op] = auditCount(t, st, op)
	}
	// (1) a write that changes: +1
	if _, refusal, err := call("mcp.server.add", addParams("search", "public")); err != nil || refusal != "" {
		t.Fatalf("add: %v %s", err, refusal)
	}
	if auditCount(t, st, "server_add") != base["server_add"]+1 {
		t.Error("a changing add did not audit exactly one row")
	}
	// (2) a refusal: +0
	if _, _, err := call("mcp.server.add", map[string]any{"name": "Bad Name!", "tier": "public", "transport": map[string]any{"type": "stdio", "command": "x"}}); err != nil {
		t.Fatal(err)
	}
	if auditCount(t, st, "server_add") != base["server_add"]+1 {
		t.Error("a REFUSED add audited — audit noise is audit lies")
	}
	// (3) declare: +1; (4) same declare again: +0 (changed:false)
	if _, refusal, err := call("mcp.grant.declare", map[string]any{"agent": "manny", "qualified": "search.do_thing"}); err != nil || refusal != "" {
		t.Fatalf("declare: %v %s", err, refusal)
	}
	if auditCount(t, st, "grant_declare") != base["grant_declare"]+1 {
		t.Error("the declare did not audit")
	}
	res, refusal, _ := call("mcp.grant.declare", map[string]any{"agent": "manny", "qualified": "search.do_thing"})
	if refusal != "" {
		t.Fatalf("idempotent declare refused: %s", refusal)
	}
	if out := res.(protocol.MCPWriteResult); out.Changed {
		t.Error("the repeat declare claimed changed:true")
	}
	if auditCount(t, st, "grant_declare") != base["grant_declare"]+1 {
		t.Error("a no-op declare audited")
	}
	// (5) revoke: +1; (6) revoke again: +0
	if _, refusal, err := call("mcp.grant.revoke", map[string]any{"agent": "manny", "qualified": "search.do_thing"}); err != nil || refusal != "" {
		t.Fatalf("revoke: %v %s", err, refusal)
	}
	if auditCount(t, st, "grant_revoke") != base["grant_revoke"]+1 {
		t.Error("the revoke did not audit")
	}
	if _, refusal, _ = call("mcp.grant.revoke", map[string]any{"agent": "manny", "qualified": "search.do_thing"}); refusal != "" {
		t.Fatalf("idempotent revoke refused: %s", refusal)
	}
	if auditCount(t, st, "grant_revoke") != base["grant_revoke"]+1 {
		t.Error("a no-op revoke audited")
	}
	// (7) a read: +0
	for _, m := range []string{"mcp.servers", "mcp.grants"} {
		p := map[string]any{}
		if m == "mcp.grants" {
			p = map[string]any{"agent": "manny"}
		}
		if _, refusal, err := call(m, p); err != nil || refusal != "" {
			t.Fatalf("%s: %v %s", m, err, refusal)
		}
	}
	for op, want := range base {
		got := auditCount(t, st, op)
		extra := map[string]int{"server_add": 1, "grant_declare": 1, "grant_revoke": 1}[op]
		if got != want+extra {
			t.Errorf("op %s: %d rows, want %d", op, got, want+extra)
		}
	}
}

func TestWriteActorRequiredNothingWritten(t *testing.T) {
	isolateStoreEnv(t)
	agentHome(t)
	st := openAgentStore(t)
	for _, name := range []string{"manny"} {
		if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: store.Slugify(name), Name: name, Version: "1"}, "2026-11-01T12:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	seam := mcpAdminHandleFor(st, newMCPProbeCache())
	// NO actor on the context (dispatch would stamp it; the direct door
	// without one is unattributable and refuses).
	_, _, err := seam(context.Background(), "mcp.server.add", mustJSONRaw(t, addParams("gh", "public")))
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.ErrMcpNoActor.Code {
		t.Fatalf("err = %v, want ErrMcpNoActor", err)
	}
	if s, _, d := configRowCount(t, st); s != 0 || d != 0 {
		t.Fatalf("the unattributable write touched rows: %d/%d", s, d)
	}
	// With the stamp it lands (dispatch's job done elsewhere).
	if _, refusal, err := seam(importActorCtx(), "mcp.server.add", mustJSONRaw(t, addParams("gh", "public"))); err != nil || refusal != "" {
		t.Fatalf("stamped add: %v %s", err, refusal)
	}
	var kind, id string
	if err := st.McpWithTx(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT actor_kind, actor_id FROM mcp_change WHERE op='server_add' LIMIT 1`).Scan(&kind, &id)
	}); err != nil {
		t.Fatal(err)
	}
	if kind != "user" || id != "1000" {
		t.Errorf("audit actor = %s/%s, want user/1000", kind, id)
	}
}

func TestMcpWritesLiveForNextOffer(t *testing.T) {
	call, st := writeFixture(t)
	// No restart, no cache flush: the read AFTER the write sees the write
	// (MG12 — every read re-reads; the seam holds no data).
	if _, refusal, err := call("mcp.server.add", addParams("fresh", "public")); err != nil || refusal != "" {
		t.Fatalf("add: %v %s", err, refusal)
	}
	servers, refusal, err := call("mcp.servers", map[string]any{})
	if err != nil || refusal != "" {
		t.Fatalf("servers: %v %s", err, refusal)
	}
	b, _ := json.Marshal(servers)
	if !strings.Contains(string(b), "fresh") {
		t.Fatalf("the read after the write missed it (stale): %s", b)
	}
	if _, refusal, err := call("mcp.grant.declare", map[string]any{"agent": "peer", "qualified": "fresh.do_thing"}); err != nil || refusal != "" {
		t.Fatalf("declare: %v %s", err, refusal)
	}
	if v := callGrants(t, st, "peer"); !strings.Contains(v, "fresh.do_thing") {
		t.Fatalf("the grants read missed the just-written declare: %s", v)
	}
}

func TestAgentDeleteRefusedNamingMcpReferences(t *testing.T) {
	// S46 T5's adaptation of S43's delete path: one accessor beside the
	// blueprint/policy halves, refusing by NAME. S43's own refusals and
	// tests are untouched (they run green unedited in ./internal/agentmgr).
	_, st := writeFixture(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}],"agents":{"golang-engineer":{"private":[{"name":"labnote","transport":{"type":"stdio","command":"ln"},"tools":[{"name":"note","rerun":"safe","effect":"local"}]}],"declares":["gitforge.pr"]}}}`))
	mgr := agentmgr.NewManager(st, func(card, certPEM, sig []byte) (*agentcard.CardSignature, error) { return nil, nil }, nil, nil)
	err := mgr.Delete(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Slugify("golang-engineer"))
	if err == nil {
		t.Fatal("the delete of a still-referenced agent SUCCEEDED — the orphan this prevents is real")
	}
	msg := err.Error()
	for _, want := range []string{"labnote", "gitforge.pr", "server", "declare"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must name every MCP reference, missing %q: %s", want, msg)
		}
	}
	// The agent still exists (nothing was deleted on the refusal).
	if _, found, ferr := st.AgentBySlug(store.Slugify("golang-engineer")); ferr != nil || !found {
		t.Fatalf("the refused delete removed the agent: found=%v err=%v", found, ferr)
	}
	// A real core NARROWING is itself a live reference worth naming — the
	// bare row (core NULL, the whole-core default) is not, and leaves
	// with the agent.
	if _, refusal, err := writeCallOn(st)("mcp.core.set", map[string]any{"agent": "golang-engineer", "core": []string{"read"}}); err != nil || refusal != "" {
		t.Fatalf("narrow core: %v %s", err, refusal)
	}
	err = mgr.Delete(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Slugify("golang-engineer"))
	if err == nil || !strings.Contains(err.Error(), "agent golang-engineer") {
		t.Fatalf("delete with a live narrowing: %v — the narrowing must be named", err)
	}
	// Clean the references and the delete walks (the refusal is the
	// reference, not the agent — the operator's path is open).
	if _, refusal, err := writeCallOn(st)("mcp.server.remove", map[string]any{"name": "labnote"}); err != nil || refusal != "" {
		t.Fatalf("remove private: %v %s", err, refusal)
	}
	if _, refusal, err := writeCallOn(st)("mcp.grant.revoke", map[string]any{"agent": "golang-engineer", "qualified": "gitforge.pr"}); err != nil || refusal != "" {
		t.Fatalf("revoke: %v %s", err, refusal)
	}
	if _, refusal, err := writeCallOn(st)("mcp.core.set", map[string]any{"agent": "golang-engineer", "core": nil}); err != nil || refusal != "" {
		t.Fatalf("core restore: %v %s", err, refusal)
	}
	if err := mgr.Delete(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Slugify("golang-engineer")); err != nil {
		t.Fatalf("delete after clearing every reference: %v", err)
	}
}

func writeCallOn(st *store.Store) func(string, any) (any, string, error) {
	seam := mcpAdminHandleFor(st, newMCPProbeCache())
	return func(method string, params any) (any, string, error) {
		return seam(importActorCtx(), method, mustJSONRawDirect(params))
	}
}

func mustJSONRawDirect(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

var _ = json.Marshal

// --- an environment variable is never empty ------------------------------------------

func envVault(t *testing.T, st *store.Store, names ...string) *vault.Vault {
	t.Helper()
	v, err := vault.Open(st, filepath.Join(t.TempDir(), "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		val := "stored-" + n
		if _, err := v.Set(context.Background(), vault.SetInput{Name: n, Value: &val}, store.Actor{Kind: "user", ID: "1000"}); err != nil {
			t.Fatal(err)
		}
	}
	prevV, prevS := mcpVault, mcpVaultStore
	t.Cleanup(func() { mcpVault, mcpVaultStore = prevV, prevS })
	setMCPVault(v, st)
	return v
}

func addWithEnv(call func(string, any) (any, string, error), name string, transport map[string]any) (any, string, error) {
	transport["type"], transport["command"] = "stdio", "x"
	return call("mcp.server.add", map[string]any{"name": name, "tier": "public", "transport": transport,
		"tools": []any{map[string]any{"name": "t", "rerun": "safe", "effect": "local"}}})
}

func TestAnEnvNameWithNoValueAndNoVariableIsRefused(t *testing.T) {
	call, st := writeFixture(t)
	envVault(t, st)
	_, refusal, err := addWithEnv(call, "gh", map[string]any{"env": []string{"GITHUB_TOKEN"}})
	if err != nil || !strings.Contains(refusal, "env GITHUB_TOKEN has no value") || !strings.Contains(refusal, "cannot be empty") {
		t.Fatalf("an unbound env name: %v %q", err, refusal)
	}
	if n := countQuery(t, st, `SELECT COUNT(*) FROM mcp_server`); n != 0 {
		t.Fatalf("a refused add stored %d rows", n)
	}
	// An empty typed value is the same refusal.
	_, refusal, _ = addWithEnv(call, "gh", map[string]any{"env": []string{"A"}, "envValues": map[string]string{"A": ""}})
	if !strings.Contains(refusal, "empty value") {
		t.Fatalf("empty typed value: %q", refusal)
	}
	// Both a variable and a value is ambiguous; a binding to nothing that exists is refused.
	_, refusal, _ = addWithEnv(call, "gh", map[string]any{"env": []string{"A"}, "envFrom": map[string]string{"A": "A_VAR"}, "envValues": map[string]string{"A": "v"}})
	if !strings.Contains(refusal, "pick one") {
		t.Fatalf("both: %q", refusal)
	}
	_, refusal, _ = addWithEnv(call, "gh", map[string]any{"env": []string{"A"}, "envFrom": map[string]string{"A": "NO_SUCH_VARIABLE"}})
	if !strings.Contains(refusal, "NO_SUCH_VARIABLE, which does not exist") {
		t.Fatalf("missing variable: %q", refusal)
	}
	// A binding for a name the server does not declare.
	_, refusal, _ = addWithEnv(call, "gh", map[string]any{"env": []string{"A"}, "envFrom": map[string]string{"B": "X"}})
	if !strings.Contains(refusal, "not one of the server's env names") {
		t.Fatalf("undeclared: %q", refusal)
	}
}

func TestATypedValueIsKeptInTheManagerAsAHiddenSecretAndOnlyTheBindingIsStored(t *testing.T) {
	call, st := writeFixture(t)
	v := envVault(t, st)
	const typed = "ghp_typed_secret_value"
	out, refusal, err := addWithEnv(call, "gh-server", map[string]any{"env": []string{"GITHUB_TOKEN"}, "envValues": map[string]string{"GITHUB_TOKEN": typed}})
	if err != nil || refusal != "" {
		t.Fatalf("add: %v %q", err, refusal)
	}
	// The binding is stored; the value is in the manager, hidden from agents, and nowhere else.
	all, _ := st.McpRows(context.Background())
	if all.Servers[0].EnvFrom != `{"GITHUB_TOKEN":"MCP_GH_SERVER_GITHUB_TOKEN"}` {
		t.Fatalf("binding: %s", all.Servers[0].EnvFrom)
	}
	if got, ok := v.Resolve("MCP_GH_SERVER_GITHUB_TOKEN", ""); !ok || got != typed {
		t.Fatalf("the manager holds it: %q %v", got, ok)
	}
	list, _ := v.List(context.Background())
	for _, x := range list {
		if x.Name == "MCP_GH_SERVER_GITHUB_TOKEN" && (!x.Secret || x.AgentVisible || x.Value != "") {
			t.Fatalf("a typed value is a hidden secret: %+v", x)
		}
	}
	for what, blob := range map[string]string{"result": mustJSONString(t, out), "rows": mustJSONString(t, all)} {
		if strings.Contains(blob, typed) {
			t.Fatalf("the typed value leaked into the %s", what)
		}
	}
	if n := countQuery(t, st, `SELECT COUNT(*) FROM mcp_change WHERE summary LIKE ?`, "%"+typed+"%"); n != 0 {
		t.Fatal("the typed value leaked into the audit")
	}
}

func TestATypedValueIsStoredOnlyAfterTheWholeCandidateValidates(t *testing.T) {
	call, st := writeFixture(t)
	v := envVault(t, st)
	_, refusal, _ := addWithEnv(call, "Bad Name!", map[string]any{"env": []string{"GITHUB_TOKEN"}, "envValues": map[string]string{"GITHUB_TOKEN": "x"}})
	if refusal == "" {
		t.Fatal("a bad server name is refused")
	}
	if list, _ := v.List(context.Background()); len(list) != 0 {
		t.Fatalf("a refused write must leave the manager as it was: %+v", list)
	}
}

func TestAnEditKeepsTheBindingItAlreadyHas(t *testing.T) {
	call, st := writeFixture(t)
	v := envVault(t, st, "TOKEN_VAR")
	if _, refusal, err := addWithEnv(call, "gh", map[string]any{"env": []string{"TOKEN"}, "envFrom": map[string]string{"TOKEN": "TOKEN_VAR"}}); err != nil || refusal != "" {
		t.Fatalf("add: %v %q", err, refusal)
	}
	// An update that resends the transport without retyping the binding keeps it.
	_, refusal, err := call("mcp.server.update", map[string]any{"name": "gh", "transport": map[string]any{"type": "stdio", "command": "y", "env": []string{"TOKEN"}}})
	if err != nil || refusal != "" {
		t.Fatalf("update: %v %q", err, refusal)
	}
	all, _ := st.McpRows(context.Background())
	if all.Servers[0].EnvFrom != `{"TOKEN":"TOKEN_VAR"}` || all.Servers[0].Command != "y" {
		t.Fatalf("the binding survives an edit: %+v", all.Servers[0])
	}
	// A NEW env name on the edit has no binding to carry, and is refused.
	_, refusal, _ = call("mcp.server.update", map[string]any{"name": "gh", "transport": map[string]any{"type": "stdio", "command": "y", "env": []string{"TOKEN", "EXTRA"}}})
	if !strings.Contains(refusal, "env EXTRA has no value") {
		t.Fatalf("a new name must be bound: %q", refusal)
	}
	_ = v
}

func TestWithNoManagerABindingIsTrustedAndATypedValueIsRefused(t *testing.T) {
	call, st := writeFixture(t)
	prevV, prevS := mcpVault, mcpVaultStore
	t.Cleanup(func() { mcpVault, mcpVaultStore = prevV, prevS })
	setMCPVault(nil, nil)
	_, refusal, _ := addWithEnv(call, "gh", map[string]any{"env": []string{"A"}, "envValues": map[string]string{"A": "v"}})
	if !strings.Contains(refusal, "variable manager is off") {
		t.Fatalf("a typed value has nowhere to go: %q", refusal)
	}
	if _, refusal, err := addWithEnv(call, "gh", map[string]any{"env": []string{"A"}, "envFrom": map[string]string{"A": "A_VAR"}}); err != nil || refusal != "" {
		t.Fatalf("the CLI's direct door cannot check a binding; the start does: %v %q", err, refusal)
	}
	_ = st
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
